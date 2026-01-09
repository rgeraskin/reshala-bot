package storage

import (
	"fmt"
	"time"
)

type ToolExecution struct {
	ID        int64
	ChatID    string
	SessionID string
	ToolName  string
	Status    string
	CreatedAt time.Time
}

func (s *Storage) SaveToolExecution(chatID, sessionID, toolName, status string) error {
	_, err := s.db.Exec(`
		INSERT INTO tool_executions (chat_id, session_id, tool_name, status, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, chatID, sessionID, toolName, status, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save tool execution: %w", err)
	}
	return nil
}

// GetToolExecutions returns all tool executions for a chat (across all sessions).
// Use GetToolExecutionsBySession for session-isolated queries.
func (s *Storage) GetToolExecutions(chatID string, limit int) ([]*ToolExecution, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, COALESCE(session_id, ''), tool_name, status, created_at
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
		if err := rows.Scan(&tool.ID, &tool.ChatID, &tool.SessionID, &tool.ToolName, &tool.Status, &tool.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan tool execution: %w", err)
		}
		tools = append(tools, &tool)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tool executions: %w", err)
	}

	return tools, nil
}

// GetToolExecutionsBySession returns tool executions for a specific session only.
func (s *Storage) GetToolExecutionsBySession(chatID, sessionID string, limit int) ([]*ToolExecution, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, session_id, tool_name, status, created_at
		FROM tool_executions
		WHERE chat_id = ? AND session_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, chatID, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool executions by session: %w", err)
	}
	defer rows.Close()

	var tools []*ToolExecution
	for rows.Next() {
		var tool ToolExecution
		if err := rows.Scan(&tool.ID, &tool.ChatID, &tool.SessionID, &tool.ToolName, &tool.Status, &tool.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan tool execution: %w", err)
		}
		tools = append(tools, &tool)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tool executions: %w", err)
	}

	return tools, nil
}

