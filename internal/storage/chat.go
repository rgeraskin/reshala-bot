package storage

import (
	"database/sql"
	"fmt"
	"time"
)

type ChatContext struct {
	ID               int64
	ChatID           string
	ChatType         string
	SessionID        string
	ClaudeSessionID  string
	CreatedAt        time.Time
	LastInteraction  time.Time
	ExpiresAt        time.Time
	IsActive         bool
}

// scanChatContexts is a helper that scans ChatContext rows from a query result.
// The rows must include all columns in order: id, chat_id, chat_type, session_id,
// claude_session_id, created_at, last_interaction, expires_at, is_active.
// Returns an empty slice (not nil) when there are no rows.
func scanChatContexts(rows *sql.Rows) ([]*ChatContext, error) {
	contexts := make([]*ChatContext, 0)
	for rows.Next() {
		var ctx ChatContext
		var claudeSessionID sql.NullString
		if err := rows.Scan(
			&ctx.ID,
			&ctx.ChatID,
			&ctx.ChatType,
			&ctx.SessionID,
			&claudeSessionID,
			&ctx.CreatedAt,
			&ctx.LastInteraction,
			&ctx.ExpiresAt,
			&ctx.IsActive,
		); err != nil {
			return nil, fmt.Errorf("failed to scan context: %w", err)
		}
		if claudeSessionID.Valid {
			ctx.ClaudeSessionID = claudeSessionID.String
		}
		contexts = append(contexts, &ctx)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating contexts: %w", err)
	}
	return contexts, nil
}

func (s *Storage) CreateContext(chatID, chatType, sessionID string, ttl time.Duration) (*ChatContext, error) {
	now := time.Now()
	expiresAt := now.Add(ttl)

	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO chat_contexts (chat_id, chat_type, session_id, created_at, last_interaction, expires_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, 1)
	`, chatID, chatType, sessionID, now, now, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create context: %w", err)
	}

	// Get the actual record ID via SELECT instead of LastInsertId()
	// because LastInsertId() is unreliable after INSERT OR REPLACE
	var id int64
	err = s.db.QueryRow(`
		SELECT id FROM chat_contexts WHERE chat_id = ?
	`, chatID).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to get context id: %w", err)
	}

	return &ChatContext{
		ID:              id,
		ChatID:          chatID,
		ChatType:        chatType,
		SessionID:       sessionID,
		CreatedAt:       now,
		LastInteraction: now,
		ExpiresAt:       expiresAt,
		IsActive:        true,
	}, nil
}

func (s *Storage) GetContext(chatID string) (*ChatContext, error) {
	var ctx ChatContext
	var claudeSessionID sql.NullString
	err := s.db.QueryRow(`
		SELECT id, chat_id, chat_type, session_id, claude_session_id, created_at, last_interaction, expires_at, is_active
		FROM chat_contexts
		WHERE chat_id = ?
	`, chatID).Scan(
		&ctx.ID,
		&ctx.ChatID,
		&ctx.ChatType,
		&ctx.SessionID,
		&claudeSessionID,
		&ctx.CreatedAt,
		&ctx.LastInteraction,
		&ctx.ExpiresAt,
		&ctx.IsActive,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get context: %w", err)
	}

	// Handle NULL claude_session_id
	if claudeSessionID.Valid {
		ctx.ClaudeSessionID = claudeSessionID.String
	}

	return &ctx, nil
}

func (s *Storage) RefreshContext(chatID string, ttl time.Duration) error {
	now := time.Now()
	expiresAt := now.Add(ttl)

	result, err := s.db.Exec(`
		UPDATE chat_contexts
		SET last_interaction = ?, expires_at = ?
		WHERE chat_id = ? AND is_active = 1
	`, now, expiresAt, chatID)
	if err != nil {
		return fmt.Errorf("failed to refresh context: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("context not found or inactive")
	}

	return nil
}

// GetAllContexts retrieves all chat contexts, optionally including inactive ones.
// Results are ordered by last_interaction ASC (oldest first).
func (s *Storage) GetAllContexts(includeInactive bool) ([]*ChatContext, error) {
	query := `
		SELECT id, chat_id, chat_type, session_id, claude_session_id,
		       created_at, last_interaction, expires_at, is_active
		FROM chat_contexts
	`
	if !includeInactive {
		query += " WHERE is_active = 1"
	}
	query += " ORDER BY last_interaction ASC"

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all contexts: %w", err)
	}
	defer rows.Close()

	return scanChatContexts(rows)
}

func (s *Storage) GetExpiredContexts() ([]*ChatContext, error) {
	now := time.Now()
	rows, err := s.db.Query(`
		SELECT id, chat_id, chat_type, session_id, claude_session_id,
		       created_at, last_interaction, expires_at, is_active
		FROM chat_contexts
		WHERE expires_at < ? AND is_active = 1
	`, now)
	if err != nil {
		return nil, fmt.Errorf("failed to get expired contexts: %w", err)
	}
	defer rows.Close()

	return scanChatContexts(rows)
}

func (s *Storage) DeactivateContext(chatID string) error {
	result, err := s.db.Exec(`
		UPDATE chat_contexts
		SET is_active = 0
		WHERE chat_id = ?
	`, chatID)
	if err != nil {
		return fmt.Errorf("failed to deactivate context: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("context not found")
	}

	return nil
}

func (s *Storage) GetActiveContextCount() (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM chat_contexts WHERE is_active = 1
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get active context count: %w", err)
	}
	return count, nil
}

func (s *Storage) UpdateClaudeSessionID(chatID, claudeSessionID string) error {
	result, err := s.db.Exec(`
		UPDATE chat_contexts
		SET claude_session_id = ?
		WHERE chat_id = ? AND is_active = 1
	`, claudeSessionID, chatID)
	if err != nil {
		return fmt.Errorf("failed to update claude session id: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("context not found or inactive")
	}

	return nil
}

// CleanupResult holds the result of a transactional cleanup operation.
// MessagesPreserved and ToolsPreserved indicate counts that were kept (not deleted).
type CleanupResult struct {
	MessagesPreserved int
	ToolsPreserved    int
}

// CleanupContextTx deactivates a chat context while preserving all session data.
// Messages and tool executions are kept for audit/analysis purposes.
// Session isolation is maintained via session_id filtering in retrieval queries.
func (s *Storage) CleanupContextTx(chatID, cleanupType string) (*CleanupResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // No-op if committed

	// Count preserved messages (for logging purposes)
	var messagesPreserved int
	_ = tx.QueryRow(`SELECT COUNT(*) FROM messages WHERE chat_id = ?`, chatID).Scan(&messagesPreserved)

	// Count preserved tool executions (for logging purposes)
	var toolsPreserved int
	_ = tx.QueryRow(`SELECT COUNT(*) FROM tool_executions WHERE chat_id = ?`, chatID).Scan(&toolsPreserved)

	// Deactivate context (data is preserved, not deleted)
	_, err = tx.Exec(`UPDATE chat_contexts SET is_active = 0 WHERE chat_id = ?`, chatID)
	if err != nil {
		return nil, fmt.Errorf("failed to deactivate context: %w", err)
	}

	// Log cleanup (with 0 deleted since we preserve data)
	_, err = tx.Exec(`
		INSERT INTO cleanup_log (chat_id, cleanup_type, messages_deleted, tools_deleted, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, chatID, cleanupType, 0, 0, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to log cleanup: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &CleanupResult{
		MessagesPreserved: messagesPreserved,
		ToolsPreserved:    toolsPreserved,
	}, nil
}

// TransferResult holds the result of a session transfer operation.
type TransferResult struct {
	SourceChatID        string
	SourceWasActive     bool
	TargetChatID        string
	ClaudeSessionID     string
	MessagesTransferred int
	ToolsTransferred    int
}

// GetContextByClaudeSessionID finds a context by its Claude session ID.
// Prefers active contexts, then falls back to the most recently interacted inactive one.
// Returns (nil, nil) if not found.
func (s *Storage) GetContextByClaudeSessionID(claudeSessionID string) (*ChatContext, error) {
	var ctx ChatContext
	var claudeSID sql.NullString

	// ORDER BY is_active DESC puts active (1) before inactive (0)
	// Then by last_interaction DESC to get most recent
	err := s.db.QueryRow(`
		SELECT id, chat_id, chat_type, session_id, claude_session_id,
		       created_at, last_interaction, expires_at, is_active
		FROM chat_contexts
		WHERE claude_session_id = ?
		ORDER BY is_active DESC, last_interaction DESC
		LIMIT 1
	`, claudeSessionID).Scan(
		&ctx.ID, &ctx.ChatID, &ctx.ChatType, &ctx.SessionID,
		&claudeSID, &ctx.CreatedAt, &ctx.LastInteraction,
		&ctx.ExpiresAt, &ctx.IsActive,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get context by claude session id: %w", err)
	}

	if claudeSID.Valid {
		ctx.ClaudeSessionID = claudeSID.String
	}

	return &ctx, nil
}

// HasActiveContextWithClaudeSessionID checks if any chat has an active context
// with the given Claude session ID, excluding the specified chat.
func (s *Storage) HasActiveContextWithClaudeSessionID(claudeSessionID, excludeChatID string) (bool, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM chat_contexts
		WHERE claude_session_id = ? AND is_active = 1 AND chat_id != ?
	`, claudeSessionID, excludeChatID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check active context: %w", err)
	}
	return count > 0, nil
}

// ReactivateContext reactivates an inactive context and refreshes its TTL.
func (s *Storage) ReactivateContext(chatID string, ttl time.Duration) error {
	now := time.Now()
	expiresAt := now.Add(ttl)

	result, err := s.db.Exec(`
		UPDATE chat_contexts
		SET is_active = 1, last_interaction = ?, expires_at = ?
		WHERE chat_id = ? AND is_active = 0
	`, now, expiresAt, chatID)
	if err != nil {
		return fmt.Errorf("failed to reactivate context: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("context not found or already active")
	}

	return nil
}

// TransferSession atomically transfers a Claude session from source to target chat.
// Handles both active and inactive source sessions.
// Returns transfer details including whether source was active (for notification logic).
func (s *Storage) TransferSession(sourceChatID, targetChatID, targetChatType, newSessionID string, ttl time.Duration) (*TransferResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get source context details
	var sourceSessionID string
	var claudeSessionID sql.NullString
	var sourceIsActive bool
	err = tx.QueryRow(`
		SELECT session_id, claude_session_id, is_active
		FROM chat_contexts
		WHERE chat_id = ?
	`, sourceChatID).Scan(&sourceSessionID, &claudeSessionID, &sourceIsActive)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("source context not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get source context: %w", err)
	}

	if !claudeSessionID.Valid || claudeSessionID.String == "" {
		return nil, fmt.Errorf("source context has no claude_session_id")
	}

	// Count messages and tools to transfer (by session_id)
	var msgCount, toolCount int
	_ = tx.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = ?`, sourceSessionID).Scan(&msgCount)
	_ = tx.QueryRow(`SELECT COUNT(*) FROM tool_executions WHERE session_id = ?`, sourceSessionID).Scan(&toolCount)

	// Deactivate source context
	_, err = tx.Exec(`UPDATE chat_contexts SET is_active = 0 WHERE chat_id = ?`, sourceChatID)
	if err != nil {
		return nil, fmt.Errorf("failed to deactivate source context: %w", err)
	}

	// Create/replace target context with same claude_session_id but new session_id
	now := time.Now()
	expiresAt := now.Add(ttl)
	_, err = tx.Exec(`
		INSERT OR REPLACE INTO chat_contexts
		(chat_id, chat_type, session_id, claude_session_id, created_at, last_interaction, expires_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1)
	`, targetChatID, targetChatType, newSessionID, claudeSessionID.String, now, now, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create target context: %w", err)
	}

	// Transfer messages: update chat_id and session_id
	_, err = tx.Exec(`
		UPDATE messages SET chat_id = ?, session_id = ? WHERE session_id = ?
	`, targetChatID, newSessionID, sourceSessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to transfer messages: %w", err)
	}

	// Transfer tool executions: update chat_id and session_id
	_, err = tx.Exec(`
		UPDATE tool_executions SET chat_id = ?, session_id = ? WHERE session_id = ?
	`, targetChatID, newSessionID, sourceSessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to transfer tools: %w", err)
	}

	// Log transfer in cleanup_log
	_, err = tx.Exec(`
		INSERT INTO cleanup_log (chat_id, cleanup_type, messages_deleted, tools_deleted, created_at)
		VALUES (?, 'transfer', 0, 0, ?)
	`, sourceChatID, now)
	if err != nil {
		return nil, fmt.Errorf("failed to log transfer: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &TransferResult{
		SourceChatID:        sourceChatID,
		SourceWasActive:     sourceIsActive,
		TargetChatID:        targetChatID,
		ClaudeSessionID:     claudeSessionID.String,
		MessagesTransferred: msgCount,
		ToolsTransferred:    toolCount,
	}, nil
}
