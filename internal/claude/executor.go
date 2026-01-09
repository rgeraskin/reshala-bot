package claude

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
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

func (e *Executor) ExecuteOneShot(query string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude-code",
		"--project-path", e.projectPath,
		"--non-interactive",
	)

	cmd.Stdin = strings.NewReader(query + "\n")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("command failed: %w, stderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

func (e *Executor) ValidateQuery(query string) error {
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("query cannot be empty")
	}
	if len(query) > 10000 {
		return fmt.Errorf("query too long (max 10000 characters)")
	}
	return nil
}

func truncateQuery(query string) string {
	if len(query) > 100 {
		return query[:100] + "..."
	}
	return query
}
