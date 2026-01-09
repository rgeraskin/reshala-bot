package storage

import (
	"fmt"
	"time"
)

type Message struct {
	ID        int64
	ChatID    string
	Role      string
	Content   string
	CreatedAt time.Time
}

func (s *Storage) SaveMessage(chatID, role, content string) error {
	_, err := s.db.Exec(`
		INSERT INTO messages (chat_id, role, content, created_at)
		VALUES (?, ?, ?, ?)
	`, chatID, role, content, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}
	return nil
}

func (s *Storage) GetRecentMessages(chatID string, limit int) ([]*Message, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, role, content, created_at
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
		if err := rows.Scan(&msg.ID, &msg.ChatID, &msg.Role, &msg.Content, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		messages = append(messages, &msg)
	}

	// Reverse to get chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

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

