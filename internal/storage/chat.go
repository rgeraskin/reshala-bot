package storage

import (
	"database/sql"
	"fmt"
	"time"
)

type ChatContext struct {
	ID              int64
	ChatID          string
	ChatType        string
	SessionID       string
	CreatedAt       time.Time
	LastInteraction time.Time
	ExpiresAt       time.Time
	IsActive        bool
}

func (s *Storage) CreateContext(chatID, chatType, sessionID string, ttl time.Duration) (*ChatContext, error) {
	now := time.Now()
	expiresAt := now.Add(ttl)

	result, err := s.db.Exec(`
		INSERT OR REPLACE INTO chat_contexts (chat_id, chat_type, session_id, created_at, last_interaction, expires_at, is_active)
		VALUES (?, ?, ?, ?, ?, ?, 1)
	`, chatID, chatType, sessionID, now, now, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create context: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
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
	err := s.db.QueryRow(`
		SELECT id, chat_id, chat_type, session_id, created_at, last_interaction, expires_at, is_active
		FROM chat_contexts
		WHERE chat_id = ?
	`, chatID).Scan(
		&ctx.ID,
		&ctx.ChatID,
		&ctx.ChatType,
		&ctx.SessionID,
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
		SELECT id, chat_id, chat_type, session_id, created_at, last_interaction, expires_at, is_active
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
		if err := rows.Scan(
			&ctx.ID,
			&ctx.ChatID,
			&ctx.ChatType,
			&ctx.SessionID,
			&ctx.CreatedAt,
			&ctx.LastInteraction,
			&ctx.ExpiresAt,
			&ctx.IsActive,
		); err != nil {
			return nil, fmt.Errorf("failed to scan context: %w", err)
		}
		contexts = append(contexts, &ctx)
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
