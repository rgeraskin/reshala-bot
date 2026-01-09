package claude

import (
	"fmt"
	"log/slog"
	"time"
)

// Executor provides a simple interface for executing Claude queries.
// Configuration (projectPath, timeout) is managed by SessionManager.
type Executor struct {
	sm *SessionManager
}

// NewExecutor creates a new Executor. The projectPath and timeout parameters
// are accepted for API compatibility but are unused - SessionManager holds
// the actual configuration values.
func NewExecutor(sm *SessionManager, projectPath string, timeout time.Duration) *Executor {
	// projectPath and timeout are intentionally unused - they exist in SessionManager
	_ = projectPath
	_ = timeout
	return &Executor{
		sm: sm,
	}
}

func (e *Executor) Execute(sessionID, query string, claudeSessionID string) (*ClaudeJSONOutput, error) {
	logQuery := query
	if len(logQuery) > 100 {
		logQuery = logQuery[:100] + "..."
	}
	slog.Info("Executing query", "session_id", sessionID, "query", logQuery)

	response, err := e.sm.ExecuteQuery(sessionID, query, claudeSessionID)
	if err != nil {
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	return response, nil
}
