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
		outMsg := &messaging.OutgoingMessage{
			ChatID:           msg.ChatID,
			Text:             "🚫 Access denied. This bot is restricted to authorized users only.",
			ReplyToMessageID: msg.MessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
	}

	// Validate input size to prevent DoS
	const maxQuerySize = 10000
	if len(msg.Text) > maxQuerySize {
		slog.Warn("Query too large", "chat_id", msg.ChatID, "size", len(msg.Text), "max", maxQuerySize)
		outMsg := &messaging.OutgoingMessage{
			ChatID:           msg.ChatID,
			Text:             fmt.Sprintf("Message too long (%d characters). Maximum is %d characters.", len(msg.Text), maxQuerySize),
			ReplyToMessageID: msg.MessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
	}

	// Filter based on DM/group rules
	// DMs: respond to all messages
	// Groups: respond only if mentioned, replied to, or slash command
	if !h.shouldProcessMessage(msg) {
		slog.Info("Ignoring group message (no mention/reply/command)",
			"chat_id", msg.ChatID,
			"user_id", msg.From.ID,
			"chat_type", msg.ChatType,
			"is_mentioning_bot", msg.IsMentioningBot,
			"is_reply_to_bot", msg.IsReplyToBot,
			"text_prefix", truncateText(msg.Text, 50))
		return nil // Silently ignore (not an error)
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
			return h.handleNewCommand(msg.ChatID, msg.MessageID)
		case "/status":
			return h.handleStatusCommand(msg.ChatID, msg.MessageID)
		case "/help":
			return h.handleHelpCommand(msg.ChatID, msg.MessageID)
		case "/history":
			return h.handleHistoryCommand(msg.ChatID, msg.MessageID)
		case "/session":
			return h.handleSessionCommand(msg.ChatID, msg.MessageID)
		case "/sessions":
			return h.handleSessionsCommand(msg.ChatID, msg.MessageID)
		case "/resume":
			return h.handleResumeCommand(msg.ChatID, fields, msg.MessageID)
		default:
			// Unknown slash command - return helpful message
			outMsg := &messaging.OutgoingMessage{
				ChatID: msg.ChatID,
				Text: fmt.Sprintf("❓ Unknown command: %s\n\nAvailable commands:\n"+
					"/status - Show session info\n"+
					"/help - Show help message\n"+
					"/history - Export conversation history\n"+
					"/session - Show session ID for transfer\n"+
					"/sessions - List all sessions\n"+
					"/resume - Resume or transfer a session\n"+
					"/new - Reset session\n\n"+
					"For other queries, just ask without using a slash command.",
					cmd),
				ReplyToMessageID: msg.MessageID,
			}
			_, err := h.platform.SendMessage(outMsg)
			return err
		}
	}

	// Add reaction BEFORE processing (not for slash commands - they're instant)
	// This provides immediate feedback that the bot is working
	if err := h.platform.AddReaction(msg.ChatID, msg.MessageID, "👀"); err != nil {
		slog.Warn("Failed to add eyes reaction",
			"chat_id", msg.ChatID,
			"message_id", msg.MessageID,
			"error", err)
		// Continue processing even if reaction fails (non-blocking)
	}

	chatType, err := h.platform.GetChatType(msg.ChatID)
	if err != nil {
		return fmt.Errorf("failed to get chat type: %w", err)
	}

	ctx, err := h.contextManager.GetOrCreate(msg.ChatID, chatType.String())
	if err != nil {
		slog.Error("Failed to get or create context", "chat_id", msg.ChatID, "error", err)
		return h.sendError(msg.ChatID, "Failed to initialize context. Please try again later.", msg.MessageID)
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
			outMsg := &messaging.OutgoingMessage{
				ChatID:           msg.ChatID,
				Text:             fmt.Sprintf("⚠️ %s", reason),
				ReplyToMessageID: msg.MessageID,
			}
			_, err := h.platform.SendMessage(outMsg)
			return err
		}
	}

	if err := h.platform.SendTyping(msg.ChatID); err != nil {
		slog.Warn("Failed to send typing indicator", "chat_id", msg.ChatID, "error", err)
	}

	_, err = h.sessionManager.GetOrCreateSession(msg.ChatID, ctx.SessionID)
	if err != nil {
		return h.sendError(msg.ChatID, "Failed to initialize Claude process. Please try again later.", msg.MessageID)
	}

	// Execute query with Claude session ID for conversation isolation
	response, err := h.executor.Execute(ctx.SessionID, msg.Text, ctx.ClaudeSessionID)
	if err != nil {
		slog.Error("Execution error", "chat_id", msg.ChatID, "session_id", ctx.SessionID, "query", msg.Text, "error", err)
		return h.sendError(msg.ChatID, "Failed to execute query. The service may be temporarily unavailable.", msg.MessageID)
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
		return h.sendError(msg.ChatID, "Failed to save response. Please try again.", msg.MessageID)
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

	return h.sendResponse(msg.ChatID, sanitized, msg.MessageID)
}

func (h *Handler) sendResponse(chatID, text string, replyToMessageID string) error {
	if strings.TrimSpace(text) == "" {
		text = "I received your message but have no response to provide."
	}

	chunks := splitResponse(text, maxTelegramMessageLen)
	currentReplyTo := replyToMessageID // First chunk replies to user message

	for i, chunk := range chunks {
		outMsg := &messaging.OutgoingMessage{
			ChatID:           chatID,
			Text:             chunk,
			ReplyToMessageID: currentReplyTo,
		}

		sentMessageID, err := h.platform.SendMessage(outMsg)
		if err != nil {
			return fmt.Errorf("failed to send response chunk %d: %w", i+1, err)
		}

		// Subsequent chunks reply to previous chunk (creates chain)
		currentReplyTo = sentMessageID
	}

	return nil
}

func (h *Handler) sendError(chatID, errorMsg string, replyToMessageID string) error {
	outMsg := &messaging.OutgoingMessage{
		ChatID:           chatID,
		Text:             fmt.Sprintf("❌ %s", errorMsg),
		ReplyToMessageID: replyToMessageID,
	}
	_, err := h.platform.SendMessage(outMsg)
	return err
}

func (h *Handler) handleNewCommand(chatID string, replyToMessageID string) error {
	slog.Info("Processing /new command", "chat_id", chatID)

	// Trigger full cleanup (kills process, deletes data, deactivates)
	if err := h.expiryWorker.ManualCleanup(chatID); err != nil {
		slog.Error("Failed to cleanup session for /new command",
			"chat_id", chatID,
			"error", err)

		// Send error message to user
		outMsg := &messaging.OutgoingMessage{
			ChatID:           chatID,
			Text:             "❌ Failed to reset session. Please try again or contact support.",
			ReplyToMessageID: replyToMessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
	}

	// Send success confirmation
	outMsg := &messaging.OutgoingMessage{
		ChatID:           chatID,
		Text:             "✅ Session reset complete! Your next message will start a fresh conversation with Claude.",
		ReplyToMessageID: replyToMessageID,
	}
	_, err := h.platform.SendMessage(outMsg)
	return err
}

func (h *Handler) handleStatusCommand(chatID string, replyToMessageID string) error {
	slog.Info("Processing /status command", "chat_id", chatID)

	// Get context
	ctx, err := h.storage.GetContext(chatID)
	if err != nil {
		slog.Error("Failed to get context for /status", "chat_id", chatID, "error", err)
		return h.sendError(chatID, "Failed to retrieve session status.", replyToMessageID)
	}

	if ctx == nil || !ctx.IsActive {
		outMsg := &messaging.OutgoingMessage{
			ChatID:           chatID,
			Text:             "ℹ️ No active session. Send a message to start a new conversation with Claude.",
			ReplyToMessageID: replyToMessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
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
	outMsg := &messaging.OutgoingMessage{
		ChatID:           chatID,
		Text:             response,
		ReplyToMessageID: replyToMessageID,
	}
	_, err = h.platform.SendMessage(outMsg)
	return err
}

func (h *Handler) handleHelpCommand(chatID string, replyToMessageID string) error {
	slog.Info("Processing /help command", "chat_id", chatID)
	outMsg := &messaging.OutgoingMessage{
		ChatID:           chatID,
		Text:             getHelpText(),
		ReplyToMessageID: replyToMessageID,
	}
	_, err := h.platform.SendMessage(outMsg)
	return err
}

func (h *Handler) handleHistoryCommand(chatID string, replyToMessageID string) error {
	slog.Info("Processing /history command", "chat_id", chatID)

	ctx, err := h.storage.GetContext(chatID)
	if err != nil {
		slog.Error("Failed to get context for /history", "chat_id", chatID, "error", err)
		return h.sendError(chatID, "Failed to retrieve conversation history.", replyToMessageID)
	}

	if ctx == nil || !ctx.IsActive {
		outMsg := &messaging.OutgoingMessage{
			ChatID:           chatID,
			Text:             "📜 No active session. Start chatting to build history!",
			ReplyToMessageID: replyToMessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
	}

	messages, err := h.storage.GetRecentMessagesBySession(chatID, ctx.SessionID, 1000)
	if err != nil {
		slog.Error("Failed to get messages for /history", "chat_id", chatID, "error", err)
		return h.sendError(chatID, "Failed to retrieve messages.", replyToMessageID)
	}

	if len(messages) == 0 {
		outMsg := &messaging.OutgoingMessage{
			ChatID:           chatID,
			Text:             "📜 Session exists but no messages yet. Send a message to start!",
			ReplyToMessageID: replyToMessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
	}

	response := formatHistoryResponse(ctx, messages)
	return h.sendResponse(chatID, response, replyToMessageID)
}

func (h *Handler) handleSessionCommand(chatID string, replyToMessageID string) error {
	slog.Info("Processing /session command", "chat_id", chatID)

	ctx, err := h.storage.GetContext(chatID)
	if err != nil {
		slog.Error("Failed to get context for /session", "chat_id", chatID, "error", err)
		return h.sendError(chatID, "Failed to retrieve session information.", replyToMessageID)
	}

	if ctx == nil {
		outMsg := &messaging.OutgoingMessage{
			ChatID:           chatID,
			Text:             "ℹ️ No session found. Send a message to start a conversation.",
			ReplyToMessageID: replyToMessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
	}

	if ctx.ClaudeSessionID == "" {
		outMsg := &messaging.OutgoingMessage{
			ChatID: chatID,
			Text: "⚠️ Session exists but Claude session not yet initialized.\n\n" +
				"Send at least one message first to generate a Claude session ID.",
			ReplyToMessageID: replyToMessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
	}

	statusEmoji := "✅"
	statusText := "Active"
	if !ctx.IsActive {
		statusEmoji = "💤"
		statusText = "Inactive (expired)"
	}

	response := fmt.Sprintf(
		"🔑 *Session Information*\n\n"+
			"*Claude Session ID:*\n`%s`\n\n"+
			"*Status:* %s %s\n\n"+
			"💡 *To transfer this session to another chat:*\n"+
			"`/resume %s`\n\n"+
			"⚠️ Transferring will move the conversation and history to the new chat.",
		ctx.ClaudeSessionID,
		statusEmoji, statusText,
		ctx.ClaudeSessionID)

	outMsg := &messaging.OutgoingMessage{
		ChatID:           chatID,
		Text:             response,
		ReplyToMessageID: replyToMessageID,
	}
	_, err = h.platform.SendMessage(outMsg)
	return err
}

func (h *Handler) handleResumeCommand(chatID string, fields []string, replyToMessageID string) error {
	slog.Info("Processing /resume command", "chat_id", chatID, "args", fields)

	// /resume without args: reactivate current chat's own session
	if len(fields) < 2 || strings.TrimSpace(fields[1]) == "" {
		return h.handleResumeOwnSession(chatID, replyToMessageID)
	}

	// /resume <session_id>: transfer session from another chat
	claudeSessionID := strings.TrimSpace(fields[1])
	return h.handleResumeFromSession(chatID, claudeSessionID, replyToMessageID)
}

// handleResumeOwnSession reactivates the current chat's own expired session.
func (h *Handler) handleResumeOwnSession(chatID string, replyToMessageID string) error {
	slog.Info("Processing /resume (own session)", "chat_id", chatID)

	ctx, err := h.storage.GetContext(chatID)
	if err != nil {
		slog.Error("Failed to get context for /resume", "chat_id", chatID, "error", err)
		return h.sendError(chatID, "Failed to retrieve session information.", replyToMessageID)
	}

	if ctx == nil {
		outMsg := &messaging.OutgoingMessage{
			ChatID:           chatID,
			Text:             "ℹ️ No session found. Send a message to start a new conversation.",
			ReplyToMessageID: replyToMessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
	}

	if ctx.ClaudeSessionID == "" {
		outMsg := &messaging.OutgoingMessage{
			ChatID:           chatID,
			Text:             "⚠️ No Claude session to resume. Send a message to start a conversation.",
			ReplyToMessageID: replyToMessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
	}

	if ctx.IsActive {
		outMsg := &messaging.OutgoingMessage{
			ChatID:           chatID,
			Text:             "✅ Session is already active! Just send a message to continue.",
			ReplyToMessageID: replyToMessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
	}

	// Check if another chat has taken this session
	hasOther, err := h.storage.HasActiveContextWithClaudeSessionID(ctx.ClaudeSessionID, chatID)
	if err != nil {
		slog.Error("Failed to check for active sessions", "chat_id", chatID, "error", err)
		return h.sendError(chatID, "Failed to check session status.", replyToMessageID)
	}

	if hasOther {
		outMsg := &messaging.OutgoingMessage{
			ChatID: chatID,
			Text: fmt.Sprintf("⚠️ This session was transferred to another chat.\n\n"+
				"To reclaim it, use:\n`/resume %s`\n\n"+
				"Or send a message to start a fresh conversation.",
				ctx.ClaudeSessionID),
			ReplyToMessageID: replyToMessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
	}

	// Reactivate the session
	if err := h.storage.ReactivateContext(chatID, h.contextManager.GetTTL()); err != nil {
		slog.Error("Failed to reactivate context", "chat_id", chatID, "error", err)
		return h.sendError(chatID, "Failed to reactivate session.", replyToMessageID)
	}

	slog.Info("Reactivated session", "chat_id", chatID, "claude_session_id", ctx.ClaudeSessionID)

	outMsg := &messaging.OutgoingMessage{
		ChatID: chatID,
		Text: fmt.Sprintf("✅ *Session Reactivated*\n\n"+
			"*Claude Session ID:* `%s`\n\n"+
			"Your conversation has been restored. Continue chatting!",
			ctx.ClaudeSessionID),
		ReplyToMessageID: replyToMessageID,
	}
	_, err = h.platform.SendMessage(outMsg)
	return err
}

// handleResumeFromSession transfers a session from another chat to this one.
func (h *Handler) handleResumeFromSession(chatID, claudeSessionID string, replyToMessageID string) error {
	slog.Info("Processing /resume (from session)", "chat_id", chatID, "claude_session_id", claudeSessionID)

	// Find the source context
	sourceCtx, err := h.storage.GetContextByClaudeSessionID(claudeSessionID)
	if err != nil {
		slog.Error("Failed to lookup session", "chat_id", chatID, "claude_session_id", claudeSessionID, "error", err)
		return h.sendError(chatID, "Failed to lookup session.", replyToMessageID)
	}

	if sourceCtx == nil {
		outMsg := &messaging.OutgoingMessage{
			ChatID: chatID,
			Text: "❌ Session not found. Possible reasons:\n" +
				"• Session ID is incorrect\n" +
				"• Session has been reset with /new\n\n" +
				"Use /session in the source chat to get the correct ID.",
			ReplyToMessageID: replyToMessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
	}

	// Check if this chat already owns the session
	if sourceCtx.ChatID == chatID {
		if sourceCtx.IsActive {
			outMsg := &messaging.OutgoingMessage{
				ChatID: chatID,
				Text: "✅ This chat already owns this session and it's active!\n\n" +
					"Just send a message to continue.",
				ReplyToMessageID: replyToMessageID,
			}
			_, err := h.platform.SendMessage(outMsg)
			return err
		}
		// Reactivate own session
		return h.handleResumeOwnSession(chatID, replyToMessageID)
	}

	// Get target chat type
	chatType, err := h.platform.GetChatType(chatID)
	if err != nil {
		slog.Error("Failed to get chat type", "chat_id", chatID, "error", err)
		return h.sendError(chatID, "Failed to determine chat type.", replyToMessageID)
	}

	// Generate new session ID for target
	newSessionID := h.contextManager.GenerateSessionID()

	// Execute transfer
	result, err := h.storage.TransferSession(
		sourceCtx.ChatID,
		chatID,
		chatType.String(),
		newSessionID,
		h.contextManager.GetTTL(),
	)
	if err != nil {
		slog.Error("Failed to transfer session",
			"source_chat_id", sourceCtx.ChatID,
			"target_chat_id", chatID,
			"claude_session_id", claudeSessionID,
			"error", err)
		return h.sendError(chatID, "Failed to transfer session. Please try again.", replyToMessageID)
	}

	// Remove source session from SessionManager memory
	if err := h.sessionManager.KillSession(sourceCtx.SessionID); err != nil {
		slog.Debug("Failed to remove source session from manager", "session_id", sourceCtx.SessionID, "error", err)
	}

	slog.Info("Session transferred",
		"source_chat_id", result.SourceChatID,
		"target_chat_id", result.TargetChatID,
		"claude_session_id", result.ClaudeSessionID,
		"messages", result.MessagesTransferred,
		"tools", result.ToolsTransferred,
		"source_was_active", result.SourceWasActive)

	// Notify source chat only if it was active
	if result.SourceWasActive {
		notifyMsg := &messaging.OutgoingMessage{
			ChatID: result.SourceChatID,
			Text: fmt.Sprintf(
				"🔄 *Session Transferred*\n\n"+
					"Your Claude session has been transferred to another chat.\n\n"+
					"*Session ID:* `%s`\n"+
					"*Messages transferred:* %d\n"+
					"*Tools transferred:* %d\n\n"+
					"This chat's session is now inactive. Send a message to start fresh,\n"+
					"or use `/resume %s` to reclaim the session.",
				result.ClaudeSessionID,
				result.MessagesTransferred,
				result.ToolsTransferred,
				result.ClaudeSessionID),
			ReplyToMessageID: "", // No reply context for notification to source
		}
		if _, err := h.platform.SendMessage(notifyMsg); err != nil {
			slog.Warn("Failed to notify source chat", "chat_id", result.SourceChatID, "error", err)
		}
	}

	// Send success message to target chat
	outMsg := &messaging.OutgoingMessage{
		ChatID: chatID,
		Text: fmt.Sprintf("✅ *Session Transferred Successfully*\n\n"+
			"*Claude Session ID:* `%s`\n"+
			"*Messages restored:* %d\n"+
			"*Tools restored:* %d\n\n"+
			"You can now continue the conversation where it left off!",
			result.ClaudeSessionID,
			result.MessagesTransferred,
			result.ToolsTransferred),
		ReplyToMessageID: replyToMessageID,
	}
	_, err = h.platform.SendMessage(outMsg)
	return err
}

func (h *Handler) handleSessionsCommand(chatID string, replyToMessageID string) error {
	slog.Info("Processing /sessions command", "chat_id", chatID)

	// Get all contexts (both active and inactive)
	contexts, err := h.storage.GetAllContexts(true)
	if err != nil {
		slog.Error("Failed to get all contexts for /sessions", "chat_id", chatID, "error", err)
		return h.sendError(chatID, "Failed to retrieve sessions list.", replyToMessageID)
	}

	if len(contexts) == 0 {
		outMsg := &messaging.OutgoingMessage{
			ChatID:           chatID,
			Text:             "📋 No sessions found.\n\nSend a message to start your first conversation!",
			ReplyToMessageID: replyToMessageID,
		}
		_, err := h.platform.SendMessage(outMsg)
		return err
	}

	response := formatSessionsResponse(contexts)
	return h.sendResponse(chatID, response, replyToMessageID)
}

func truncateText(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "..."
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

			// Handle lines longer than maxLen (use runes to avoid breaking UTF-8)
			lineRunes := []rune(line)
			if len(lineRunes) > maxLen {
				for i := 0; i < len(lineRunes); i += maxLen {
					end := i + maxLen
					if end > len(lineRunes) {
						end = len(lineRunes)
					}
					chunks = append(chunks, string(lineRunes[i:end]))
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
/session - Show Claude session ID for transfer
/sessions - List all sessions across all chats
/resume - Reactivate expired session or transfer from another chat
/new - Reset session and start fresh

💡 *Usage Tips*
• Sessions expire after 2 hours of inactivity
• Each message extends the session TTL
• All MCP tools are read-only for safety

🔄 *Session Transfer*
To continue a conversation in another chat (e.g., move from group to DM):
1. Use /session in source chat to get the session ID
2. Use /resume <session_id> in target chat to transfer

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

		// Truncate very long messages (use runes to avoid breaking UTF-8)
		content := msg.Content
		if len([]rune(content)) > maxHistoryContentLen {
			runes := []rune(content)
			content = string(runes[:maxHistoryContentLen]) + "\n[... truncated ...]"
		}

		b.WriteString(content)
		b.WriteString("\n\n")
	}

	b.WriteString("---\n\n")
	b.WriteString("💡 Use /new to reset the session and start fresh")

	return b.String()
}

// formatSessionsResponse generates a formatted list of all sessions.
func formatSessionsResponse(contexts []*storage.ChatContext) string {
	var b strings.Builder

	// Count active vs inactive
	activeCount := 0
	for _, ctx := range contexts {
		if ctx.IsActive && time.Now().Before(ctx.ExpiresAt) {
			activeCount++
		}
	}
	inactiveCount := len(contexts) - activeCount

	// Header
	b.WriteString("📋 *All Sessions*\n\n")
	b.WriteString(fmt.Sprintf("*Total:* %d sessions\n", len(contexts)))
	b.WriteString(fmt.Sprintf("*Active:* %d | *Inactive:* %d\n\n", activeCount, inactiveCount))
	b.WriteString("---\n\n")

	// List each session
	for i, ctx := range contexts {
		// Determine status using positive logic (consistent with formatStatusResponse)
		isActive := ctx.IsActive && time.Now().Before(ctx.ExpiresAt)
		statusEmoji := "✅"
		statusText := "Active"
		if !isActive {
			statusEmoji = "💤"
			statusText = "Inactive"
		}

		// Session number and status
		b.WriteString(fmt.Sprintf("*%d.* %s %s\n", i+1, statusEmoji, statusText))

		// Claude session ID (or placeholder if not initialized)
		if ctx.ClaudeSessionID != "" {
			b.WriteString(fmt.Sprintf("   *Session:* `%s`\n", ctx.ClaudeSessionID))
		} else {
			b.WriteString("   *Session:* Not initialized\n")
		}

		// Timing info
		b.WriteString(fmt.Sprintf("   *Chat:* `%s` | *Created:* %s\n",
			ctx.ChatID,
			ctx.CreatedAt.Format("Jan 2, 3:04 PM")))

		// Resume hint for sessions with Claude session ID
		if ctx.ClaudeSessionID != "" {
			b.WriteString(fmt.Sprintf("   💡 `/resume %s`\n", ctx.ClaudeSessionID))
		}

		b.WriteString("\n")
	}

	// Footer
	b.WriteString("---\n\n")
	b.WriteString("💡 *Commands:*\n")
	b.WriteString("• `/status` - Show details for your current session\n")
	b.WriteString("• `/resume <session_id>` - Transfer a session to this chat")

	return b.String()
}

// shouldProcessMessage determines if the bot should respond to a message
// based on chat type and message context.
// DMs: Always respond
// Groups: Respond only if mentioned, replied to, or slash command
func (h *Handler) shouldProcessMessage(msg *messaging.IncomingMessage) bool {
	// DMs: Always respond
	if msg.ChatType == messaging.ChatTypePrivate {
		return true
	}

	// Groups/Channels: Check for trigger conditions
	if msg.ChatType.IsGroupOrChannel() {
		// Slash commands always trigger response
		if strings.HasPrefix(msg.Text, "/") {
			return true
		}

		// Mentions always trigger response
		if msg.IsMentioningBot {
			return true
		}

		// Direct replies always trigger response
		if msg.IsReplyToBot {
			return true
		}

		// Otherwise ignore in groups
		return false
	}

	// Unknown chat type - default to responding (fail-open for safety)
	return true
}
