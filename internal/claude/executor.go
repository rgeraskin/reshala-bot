package claude

import (
	"fmt"
	"log/slog"
	"time"
)

type Executor struct {
	sm          *SessionManager
	projectPath string
	timeout     time.Duration
}

func NewExecutor(sm *SessionManager, projectPath string, timeout time.Duration) *Executor {
	return &Executor{
		sm:          sm,
		projectPath: projectPath,
		timeout:     timeout,
	}
}

func (e *Executor) Execute(sessionID, query string, claudeSessionID string) (*ClaudeJSONOutput, error) {
	slog.Info("Executing query", "session_id", sessionID, "query", truncateQuery(query))

	response, err := e.sm.ExecuteQuery(sessionID, query, claudeSessionID)
	if err != nil {
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	return response, nil
}

func truncateQuery(query string) string {
	if len(query) > 100 {
		return query[:100] + "..."
	}
	return query
}
