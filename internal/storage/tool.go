package storage

import (
	"fmt"
	"time"
)

type ToolExecution struct {
	ID        int64
	ChatID    string
	ToolName  string
	Status    string
	CreatedAt time.Time
}

func (s *Storage) SaveToolExecution(chatID, toolName, status string) error {
	_, err := s.db.Exec(`
		INSERT INTO tool_executions (chat_id, tool_name, status, created_at)
		VALUES (?, ?, ?, ?)
	`, chatID, toolName, status, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save tool execution: %w", err)
	}
	return nil
}

func (s *Storage) GetToolExecutions(chatID string, limit int) ([]*ToolExecution, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, tool_name, status, created_at
		FROM tool_executions
		WHERE chat_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool executions: %w", err)
	}
	defer rows.Close()

	var tools []*ToolExecution
	for rows.Next() {
		var tool ToolExecution
		if err := rows.Scan(&tool.ID, &tool.ChatID, &tool.ToolName, &tool.Status, &tool.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan tool execution: %w", err)
		}
		tools = append(tools, &tool)
	}

	return tools, nil
}

func (s *Storage) DeleteToolExecutionsByChat(chatID string) (int, error) {
	result, err := s.db.Exec(`
		DELETE FROM tool_executions WHERE chat_id = ?
	`, chatID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete tool executions: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rows), nil
}

func (s *Storage) LogCleanup(chatID, cleanupType string, messagesDeleted, toolsDeleted int) error {
	_, err := s.db.Exec(`
		INSERT INTO cleanup_log (chat_id, cleanup_type, messages_deleted, tools_deleted, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, chatID, cleanupType, messagesDeleted, toolsDeleted, time.Now())
	if err != nil {
		return fmt.Errorf("failed to log cleanup: %w", err)
	}
	return nil
}
