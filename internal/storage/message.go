package storage

import (
	"fmt"
	"time"
)

type Message struct {
	ID        int64
	ChatID    string
	SessionID string
	Role      string
	Content   string
	CreatedAt time.Time
}

func (s *Storage) SaveMessage(chatID, sessionID, role, content string) error {
	_, err := s.db.Exec(`
		INSERT INTO messages (chat_id, session_id, role, content, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, chatID, sessionID, role, content, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}
	return nil
}

// GetRecentMessages returns all recent messages for a chat (across all sessions).
// Use GetRecentMessagesBySession for session-isolated queries.
func (s *Storage) GetRecentMessages(chatID string, limit int) ([]*Message, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, COALESCE(session_id, ''), role, content, created_at
		FROM messages
		WHERE chat_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent messages: %w", err)
	}
	defer rows.Close()

	var messages []*Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.ChatID, &msg.SessionID, &msg.Role, &msg.Content, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		messages = append(messages, &msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating messages: %w", err)
	}

	// Reverse to get chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetRecentMessagesBySession returns recent messages for a specific session only.
func (s *Storage) GetRecentMessagesBySession(chatID, sessionID string, limit int) ([]*Message, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, session_id, role, content, created_at
		FROM messages
		WHERE chat_id = ? AND session_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, chatID, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent messages by session: %w", err)
	}
	defer rows.Close()

	var messages []*Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.ChatID, &msg.SessionID, &msg.Role, &msg.Content, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		messages = append(messages, &msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating messages: %w", err)
	}

	// Reverse to get chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetMessageCount returns the total message count for a chat (across all sessions).
func (s *Storage) GetMessageCount(chatID string) (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM messages WHERE chat_id = ?
	`, chatID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get message count: %w", err)
	}
	return count, nil
}

// GetMessageCountBySession returns the message count for a specific session only.
func (s *Storage) GetMessageCountBySession(chatID, sessionID string) (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM messages WHERE chat_id = ? AND session_id = ?
	`, chatID, sessionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get message count by session: %w", err)
	}
	return count, nil
}

