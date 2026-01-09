package bot

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/rg/aiops/internal/claude"
	"github.com/rg/aiops/internal/context"
	"github.com/rg/aiops/internal/messaging"
	"github.com/rg/aiops/internal/security"
	"github.com/rg/aiops/internal/storage"
)

const (
	// maxTelegramMessageLen is Telegram's maximum message length
	maxTelegramMessageLen = 4000
	// maxHistoryContentLen is the max length for message content in /history output
	maxHistoryContentLen = 500
)

type Handler struct {
	platform       messaging.Platform
	contextManager *context.Manager
	expiryWorker   *context.ExpiryWorker
	validator      *context.Validator
	sessionManager *claude.SessionManager
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
	sessionManager *claude.SessionManager,
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
		sessionManager: sessionManager,
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
		// Send permission denied message to user
		return h.platform.SendMessage(msg.ChatID,
			"🚫 Access denied. This bot is restricted to authorized users only.")
	}

	// Validate input size to prevent DoS
	const maxQuerySize = 10000
	if len(msg.Text) > maxQuerySize {
		slog.Warn("Query too large", "chat_id", msg.ChatID, "size", len(msg.Text), "max", maxQuerySize)
		return h.platform.SendMessage(msg.ChatID,
			fmt.Sprintf("Message too long (%d characters). Maximum is %d characters.", len(msg.Text), maxQuerySize))
	}

	// Check for slash commands
	if strings.HasPrefix(msg.Text, "/") {
		fields := strings.Fields(msg.Text)
		if len(fields) == 0 {
			return nil // Ignore whitespace-only messages starting with /
		}
		cmd := fields[0]
		switch cmd {
		case "/new":
			return h.handleNewCommand(msg.ChatID)
		case "/status":
			return h.handleStatusCommand(msg.ChatID)
		case "/help":
			return h.handleHelpCommand(msg.ChatID)
		case "/history":
			return h.handleHistoryCommand(msg.ChatID)
		default:
			// Unknown slash command - return helpful message
			return h.platform.SendMessage(msg.ChatID,
				fmt.Sprintf("❓ Unknown command: %s\n\nAvailable commands:\n"+
					"/status - Show session info\n"+
					"/help - Show help message\n"+
					"/history - Export conversation history\n"+
					"/new - Reset session\n\n"+
					"For other queries, just ask without using a slash command.",
					cmd))
		}
	}

	chatType, err := h.platform.GetChatType(msg.ChatID)
	if err != nil {
		return fmt.Errorf("failed to get chat type: %w", err)
	}

	ctx, err := h.contextManager.GetOrCreate(msg.ChatID, chatType.String())
	if err != nil {
		slog.Error("Failed to get or create context", "chat_id", msg.ChatID, "error", err)
		return h.sendError(msg.ChatID, "Failed to initialize context. Please try again later.")
	}

	if err := h.contextManager.Refresh(msg.ChatID); err != nil {
		slog.Warn("Failed to refresh context", "chat_id", msg.ChatID, "error", err)
	}

	if err := h.storage.SaveMessage(msg.ChatID, ctx.SessionID, "user", msg.Text); err != nil {
		// Log error but continue - user message loss is acceptable, we still want to respond
		slog.Error("Failed to save user message", "chat_id", msg.ChatID, "error", err)
	}

	// Validate query if validator is configured
	if h.validator != nil {
		valid, reason, err := h.validator.ValidateQuery(ctx, msg.Text)
		if err != nil {
			slog.Warn("Validation error", "chat_id", msg.ChatID, "error", err)
		}
		if !valid && reason != "" {
			return h.platform.SendMessage(msg.ChatID, fmt.Sprintf("⚠️ %s", reason))
		}
	}

	if err := h.platform.SendTyping(msg.ChatID); err != nil {
		slog.Warn("Failed to send typing indicator", "chat_id", msg.ChatID, "error", err)
	}

	_, err = h.sessionManager.GetOrCreateSession(msg.ChatID, ctx.SessionID)
	if err != nil {
		return h.sendError(msg.ChatID, "Failed to initialize Claude process. Please try again later.")
	}

	// Execute query with Claude session ID for conversation isolation
	response, err := h.executor.Execute(ctx.SessionID, msg.Text, ctx.ClaudeSessionID)
	if err != nil {
		slog.Error("Execution error", "chat_id", msg.ChatID, "session_id", ctx.SessionID, "query", msg.Text, "error", err)
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

	// Critical: Don't send response if we can't persist it (prevents data loss)
	if err := h.storage.SaveMessage(msg.ChatID, ctx.SessionID, "assistant", sanitized); err != nil {
		slog.Error("Failed to save assistant message", "chat_id", msg.ChatID, "error", err)
		return h.sendError(msg.ChatID, "Failed to save response. Please try again.")
	}

	tools := claude.ExtractToolExecutions(response.Result)
	for _, tool := range tools {
		if err := h.storage.SaveToolExecution(msg.ChatID, ctx.SessionID, tool.ToolName, tool.Status); err != nil {
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

	chunks := splitResponse(text, maxTelegramMessageLen)
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

func (h *Handler) handleStatusCommand(chatID string) error {
	slog.Info("Processing /status command", "chat_id", chatID)

	// Get context
	ctx, err := h.storage.GetContext(chatID)
	if err != nil {
		slog.Error("Failed to get context for /status", "chat_id", chatID, "error", err)
		return h.sendError(chatID, "Failed to retrieve session status.")
	}

	if ctx == nil || !ctx.IsActive {
		return h.platform.SendMessage(chatID,
			"ℹ️ No active session. Send a message to start a new conversation with Claude.")
	}

	// Get message count for current session
	msgCount, err := h.storage.GetMessageCountBySession(chatID, ctx.SessionID)
	if err != nil {
		slog.Warn("Failed to get message count", "chat_id", chatID, "error", err)
		msgCount = 0
	}

	// Get tool execution count for current session
	tools, err := h.storage.GetToolExecutionsBySession(chatID, ctx.SessionID, 1000)
	if err != nil {
		slog.Warn("Failed to get tool executions", "chat_id", chatID, "error", err)
		tools = []*storage.ToolExecution{}
	}

	response := formatStatusResponse(ctx, msgCount, len(tools))
	return h.platform.SendMessage(chatID, response)
}

func (h *Handler) handleHelpCommand(chatID string) error {
	slog.Info("Processing /help command", "chat_id", chatID)
	return h.platform.SendMessage(chatID, getHelpText())
}

func (h *Handler) handleHistoryCommand(chatID string) error {
	slog.Info("Processing /history command", "chat_id", chatID)

	ctx, err := h.storage.GetContext(chatID)
	if err != nil {
		slog.Error("Failed to get context for /history", "chat_id", chatID, "error", err)
		return h.sendError(chatID, "Failed to retrieve conversation history.")
	}

	if ctx == nil || !ctx.IsActive {
		return h.platform.SendMessage(chatID,
			"📜 No active session. Start chatting to build history!")
	}

	messages, err := h.storage.GetRecentMessagesBySession(chatID, ctx.SessionID, 1000)
	if err != nil {
		slog.Error("Failed to get messages for /history", "chat_id", chatID, "error", err)
		return h.sendError(chatID, "Failed to retrieve messages.")
	}

	if len(messages) == 0 {
		return h.platform.SendMessage(chatID,
			"📜 Session exists but no messages yet. Send a message to start!")
	}

	response := formatHistoryResponse(ctx, messages)
	return h.sendResponse(chatID, response)
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

func formatStatusResponse(ctx *storage.ChatContext, msgCount, toolCount int) string {
	var b strings.Builder

	b.WriteString("📊 *Session Status*\n\n")

	// Session IDs
	b.WriteString(fmt.Sprintf("*Session ID:* `%s`\n", ctx.SessionID))
	if ctx.ClaudeSessionID != "" {
		b.WriteString(fmt.Sprintf("*Claude Session:* `%s`\n", ctx.ClaudeSessionID))
	} else {
		b.WriteString("*Claude Session:* Not yet initialized\n")
	}

	// Timing
	b.WriteString("\n⏱️ *Timing*\n")
	b.WriteString(fmt.Sprintf("Created: %s (%s)\n",
		formatDurationAgo(time.Since(ctx.CreatedAt)),
		ctx.CreatedAt.Format("Jan 2, 3:04 PM")))
	b.WriteString(fmt.Sprintf("Last active: %s\n",
		formatDurationAgo(time.Since(ctx.LastInteraction))))

	if time.Now().Before(ctx.ExpiresAt) {
		b.WriteString(fmt.Sprintf("Expires: in %s (%s)\n",
			formatDuration(time.Until(ctx.ExpiresAt)),
			ctx.ExpiresAt.Format("Jan 2, 3:04 PM")))
	} else {
		b.WriteString("Expires: ⚠️ Session expired\n")
	}

	// Activity
	b.WriteString("\n💬 *Activity*\n")
	b.WriteString(fmt.Sprintf("Messages: %d\n", msgCount))
	b.WriteString(fmt.Sprintf("Tools used: %d executions\n", toolCount))

	// Status
	if ctx.IsActive && time.Now().Before(ctx.ExpiresAt) {
		b.WriteString("\n*Status:* ✅ Active")
	} else {
		b.WriteString("\n*Status:* ⚠️ Inactive - send a message to start fresh")
	}

	return b.String()
}

// formatDuration returns a human-readable duration string without "ago" suffix.
// For negative durations (shouldn't happen normally), returns absolute value.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}

	if minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	}

	if seconds > 5 {
		return fmt.Sprintf("%ds", seconds)
	}

	return "just now"
}

// formatDurationAgo returns a human-readable duration string with "ago" suffix.
func formatDurationAgo(d time.Duration) string {
	if d < 0 {
		return "just now"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm ago", hours, minutes)
		}
		return fmt.Sprintf("%dh ago", hours)
	}

	if minutes > 0 {
		return fmt.Sprintf("%dm ago", minutes)
	}

	if seconds > 5 {
		return fmt.Sprintf("%ds ago", seconds)
	}

	return "just now"
}

func getHelpText() string {
	return `🤖 *AIOps Bot - Available Commands*

/status - Show session information and statistics
/help - Display this help message
/history - Export conversation history
/new - Reset session and start fresh

💡 *Usage Tips*
• Sessions expire after 2 hours of inactivity
• Each message extends the session TTL
• All MCP tools are read-only for safety
• Bot only responds in whitelisted groups

*For SRE operations, just ask naturally:*
"Show pods in production"
"Check ArgoCD app status"
"Get recent Datadog alerts"
"Search Jira for incidents"`
}

func formatHistoryResponse(ctx *storage.ChatContext, messages []*storage.Message) string {
	var b strings.Builder

	b.WriteString("📜 *Conversation History*\n\n")
	b.WriteString(fmt.Sprintf("*Session:* `%s`\n", ctx.SessionID))

	if len(messages) > 0 {
		firstMsg := messages[0]
		lastMsg := messages[len(messages)-1]
		duration := lastMsg.CreatedAt.Sub(firstMsg.CreatedAt)

		b.WriteString(fmt.Sprintf("*Period:* %s - %s (%s)\n",
			firstMsg.CreatedAt.Format("Jan 2, 3:04 PM"),
			lastMsg.CreatedAt.Format("3:04 PM"),
			formatDuration(duration)))
	}

	b.WriteString(fmt.Sprintf("*Messages:* %d\n\n", len(messages)))
	b.WriteString("---\n\n")

	for _, msg := range messages {
		roleLabel := "User"
		if msg.Role == "assistant" {
			roleLabel = "Assistant"
		}

		timestamp := msg.CreatedAt.Format("3:04 PM")
		b.WriteString(fmt.Sprintf("*[%s] %s:*\n", timestamp, roleLabel))

		// Truncate very long messages
		content := msg.Content
		if len(content) > maxHistoryContentLen {
			content = content[:maxHistoryContentLen] + "\n[... truncated ...]"
		}

		b.WriteString(content)
		b.WriteString("\n\n")
	}

	b.WriteString("---\n\n")
	b.WriteString("💡 Use /new to reset the session and start fresh")

	return b.String()
}
