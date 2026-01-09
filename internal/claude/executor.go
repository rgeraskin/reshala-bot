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
	pm          *ProcessManager
	projectPath string
	timeout     time.Duration
}

func NewExecutor(pm *ProcessManager, projectPath string, timeout time.Duration) *Executor {
	return &Executor{
		pm:          pm,
		projectPath: projectPath,
		timeout:     timeout,
	}
}

func (e *Executor) Execute(sessionID, query string) (string, error) {
	slog.Info("Executing query", "session_id", sessionID, "query", truncateQuery(query))

	response, err := e.pm.ExecuteQuery(sessionID, query)
	if err != nil {
		return "", fmt.Errorf("execution failed: %w", err)
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
