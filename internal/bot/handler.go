package bot

import (
	"fmt"
	"log"
	"strings"

	"github.com/rg/aiops/internal/claude"
	"github.com/rg/aiops/internal/context"
	"github.com/rg/aiops/internal/messaging"
	"github.com/rg/aiops/internal/security"
	"github.com/rg/aiops/internal/storage"
)

type Handler struct {
	platform       messaging.Platform
	contextManager *context.Manager
	validator      *context.Validator
	processManager *claude.ProcessManager
	executor       *claude.Executor
	sanitizer      *security.Sanitizer
	storage        *storage.Storage
	allowedChatIDs map[string]bool
}

func NewHandler(
	platform messaging.Platform,
	contextManager *context.Manager,
	validator *context.Validator,
	processManager *claude.ProcessManager,
	executor *claude.Executor,
	sanitizer *security.Sanitizer,
	storage *storage.Storage,
	allowedChatIDs []string,
) *Handler {
	// Build allowed chat IDs map for O(1) lookup
	allowedMap := make(map[string]bool)
	for _, chatID := range allowedChatIDs {
		allowedMap[chatID] = true
	}

	return &Handler{
		platform:       platform,
		contextManager: contextManager,
		validator:      validator,
		processManager: processManager,
		executor:       executor,
		sanitizer:      sanitizer,
		storage:        storage,
		allowedChatIDs: allowedMap,
	}
}

func (h *Handler) HandleMessage(msg *messaging.IncomingMessage) error {
	log.Printf("Received message from chat %s (user %s): %s", msg.ChatID, msg.From.ID, truncateText(msg.Text, 100))

	// Check whitelist - can contain both user IDs and chat/group IDs
	if !h.allowedChatIDs[msg.ChatID] && !h.allowedChatIDs[msg.From.ID] {
		log.Printf("Ignoring message from non-whitelisted chat %s / user %s", msg.ChatID, msg.From.ID)
		return nil
	}

	chatType, err := h.platform.GetChatType(msg.ChatID)
	if err != nil {
		return fmt.Errorf("failed to get chat type: %w", err)
	}

	ctx, err := h.contextManager.GetOrCreate(msg.ChatID, chatType.String())
	if err != nil {
		return h.sendError(msg.ChatID, "Failed to initialize context. Please try again later.")
	}

	if err := h.contextManager.Refresh(msg.ChatID); err != nil {
		log.Printf("Failed to refresh context: %v", err)
	}

	if err := h.storage.SaveMessage(msg.ChatID, "user", msg.Text); err != nil {
		log.Printf("Failed to save user message: %v", err)
	}

	valid, reason, err := h.validator.ValidateQuery(ctx, msg.Text)
	if err != nil {
		log.Printf("Validation error: %v", err)
	}
	if !valid && reason != "" {
		return h.platform.SendMessage(msg.ChatID, fmt.Sprintf("⚠️ %s", reason))
	}

	if err := h.platform.SendTyping(msg.ChatID); err != nil {
		log.Printf("Failed to send typing indicator: %v", err)
	}

	_, err = h.processManager.GetOrCreateProcess(msg.ChatID, ctx.SessionID)
	if err != nil {
		return h.sendError(msg.ChatID, "Failed to initialize Claude process. Please try again later.")
	}

	response, err := h.executor.Execute(ctx.SessionID, msg.Text)
	if err != nil {
		log.Printf("Execution error: %v", err)
		return h.sendError(msg.ChatID, "Failed to execute query. The service may be temporarily unavailable.")
	}

	sanitized := h.sanitizer.Sanitize(response)

	if err := h.storage.SaveMessage(msg.ChatID, "assistant", sanitized); err != nil {
		log.Printf("Failed to save assistant message: %v", err)
	}

	tools := claude.ExtractToolExecutions(response)
	for _, tool := range tools {
		if err := h.storage.SaveToolExecution(msg.ChatID, tool.ToolName, tool.Status); err != nil {
			log.Printf("Failed to save tool execution: %v", err)
		}
	}

	return h.sendResponse(msg.ChatID, sanitized)
}

func (h *Handler) sendResponse(chatID, text string) error {
	if strings.TrimSpace(text) == "" {
		text = "I received your message but have no response to provide."
	}

	chunks := splitResponse(text, 4000)
	for i, chunk := range chunks {
		if err := h.platform.SendMessage(chatID, chunk); err != nil {
			return fmt.Errorf("failed to send response chunk %d: %w", i+1, err)
		}
	}

	return nil
}

func (h *Handler) sendError(chatID, errorMsg string) error {
	return h.platform.SendMessage(chatID, fmt.Sprintf("❌ %s", errorMsg))
}

func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

func splitResponse(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	lines := strings.Split(text, "\n")
	var currentChunk strings.Builder

	for _, line := range lines {
		if currentChunk.Len()+len(line)+1 > maxLen {
			if currentChunk.Len() > 0 {
				chunks = append(chunks, currentChunk.String())
				currentChunk.Reset()
			}

			if len(line) > maxLen {
				for i := 0; i < len(line); i += maxLen {
					end := i + maxLen
					if end > len(line) {
						end = len(line)
					}
					chunks = append(chunks, line[i:end])
				}
			} else {
				currentChunk.WriteString(line)
			}
		} else {
			if currentChunk.Len() > 0 {
				currentChunk.WriteString("\n")
			}
			currentChunk.WriteString(line)
		}
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	return chunks
}
