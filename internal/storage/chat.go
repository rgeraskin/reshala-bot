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

func (s *Storage) GetExpiredContexts() ([]*ChatContext, error) {
	now := time.Now()
	rows, err := s.db.Query(`
		SELECT id, chat_id, chat_type, session_id, claude_session_id, created_at, last_interaction, expires_at, is_active
		FROM chat_contexts
		WHERE expires_at < ? AND is_active = 1
	`, now)
	if err != nil {
		return nil, fmt.Errorf("failed to get expired contexts: %w", err)
	}
	defer rows.Close()

	var contexts []*ChatContext
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
		// Handle NULL claude_session_id
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
