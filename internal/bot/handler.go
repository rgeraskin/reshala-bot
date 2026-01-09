package bot

import (
	"fmt"
	"log/slog"
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
	expiryWorker   *context.ExpiryWorker
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
	expiryWorker *context.ExpiryWorker,
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
		expiryWorker:   expiryWorker,
		validator:      validator,
		processManager: processManager,
		executor:       executor,
		sanitizer:      sanitizer,
		storage:        storage,
		allowedChatIDs: allowedMap,
	}
}

func (h *Handler) HandleMessage(msg *messaging.IncomingMessage) error {
	slog.Info("Received message",
		"chat_id", msg.ChatID,
		"user_id", msg.From.ID,
		"text", truncateText(msg.Text, 100))

	// Check whitelist - can contain both user IDs and chat/group IDs
	if !h.allowedChatIDs[msg.ChatID] && !h.allowedChatIDs[msg.From.ID] {
		slog.Warn("Ignoring non-whitelisted message",
			"chat_id", msg.ChatID,
			"user_id", msg.From.ID)
		return nil
	}

	// Check for slash commands
	if strings.HasPrefix(msg.Text, "/") {
		cmd := strings.Fields(msg.Text)[0] // Extract command
		switch cmd {
		case "/new":
			return h.handleNewCommand(msg.ChatID)
		// Future commands can be added here
		default:
			// Let other slash commands pass through to Claude
		}
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
		slog.Warn("Failed to refresh context", "chat_id", msg.ChatID, "error", err)
	}

	if err := h.storage.SaveMessage(msg.ChatID, "user", msg.Text); err != nil {
		slog.Warn("Failed to save user message", "chat_id", msg.ChatID, "error", err)
	}

	valid, reason, err := h.validator.ValidateQuery(ctx, msg.Text)
	if err != nil {
		slog.Warn("Validation error", "chat_id", msg.ChatID, "error", err)
	}
	if !valid && reason != "" {
		return h.platform.SendMessage(msg.ChatID, fmt.Sprintf("⚠️ %s", reason))
	}

	if err := h.platform.SendTyping(msg.ChatID); err != nil {
		slog.Warn("Failed to send typing indicator", "chat_id", msg.ChatID, "error", err)
	}

	_, err = h.processManager.GetOrCreateProcess(msg.ChatID, ctx.SessionID)
	if err != nil {
		return h.sendError(msg.ChatID, "Failed to initialize Claude process. Please try again later.")
	}

	// Execute query with Claude session ID for conversation isolation
	response, err := h.executor.Execute(ctx.SessionID, msg.Text, ctx.ClaudeSessionID)
	if err != nil {
		slog.Error("Execution error", "chat_id", msg.ChatID, "session_id", ctx.SessionID, "error", err)
		return h.sendError(msg.ChatID, "Failed to execute query. The service may be temporarily unavailable.")
	}

	// If this was the first message, store the Claude session ID
	if ctx.ClaudeSessionID == "" && response.SessionID != "" {
		if err := h.storage.UpdateClaudeSessionID(msg.ChatID, response.SessionID); err != nil {
			slog.Warn("Failed to save Claude session ID", "chat_id", msg.ChatID, "error", err)
		} else {
			slog.Info("Saved Claude session ID", "chat_id", msg.ChatID, "claude_session_id", response.SessionID)
		}
	}

	sanitized := h.sanitizer.Sanitize(response.Result)

	if err := h.storage.SaveMessage(msg.ChatID, "assistant", sanitized); err != nil {
		slog.Warn("Failed to save assistant message", "chat_id", msg.ChatID, "error", err)
	}

	tools := claude.ExtractToolExecutions(response.Result)
	for _, tool := range tools {
		if err := h.storage.SaveToolExecution(msg.ChatID, tool.ToolName, tool.Status); err != nil {
			slog.Warn("Failed to save tool execution",
				"chat_id", msg.ChatID,
				"tool", tool.ToolName,
				"error", err)
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

func (h *Handler) handleNewCommand(chatID string) error {
	slog.Info("Processing /new command", "chat_id", chatID)

	// Trigger full cleanup (kills process, deletes data, deactivates)
	if err := h.expiryWorker.ManualCleanup(chatID); err != nil {
		slog.Error("Failed to cleanup session for /new command",
			"chat_id", chatID,
			"error", err)

		// Send error message to user
		return h.platform.SendMessage(chatID,
			"❌ Failed to reset session. Please try again or contact support.")
	}

	// Send success confirmation
	return h.platform.SendMessage(chatID,
		"✅ Session reset complete! Your next message will start a fresh conversation with Claude.")
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
