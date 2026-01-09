package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"
)

type ProcessManager struct {
	processes   map[string]*ClaudeProcess
	mu          sync.RWMutex
	maxProcesses int
	cliPath     string
	projectPath string
	timeout     time.Duration
}

type ClaudeProcess struct {
	SessionID   string
	ChatID      string
	Cmd         *exec.Cmd
	Stdin       io.WriteCloser
	Stdout      io.ReadCloser
	Stderr      io.ReadCloser
	StartedAt   time.Time
	LastUsed    time.Time
	mu          sync.Mutex
	cancel      context.CancelFunc
}

func NewProcessManager(cliPath, projectPath string, maxProcesses int, timeout time.Duration) *ProcessManager {
	return &ProcessManager{
		processes:   make(map[string]*ClaudeProcess),
		maxProcesses: maxProcesses,
		cliPath:     cliPath,
		projectPath: projectPath,
		timeout:     timeout,
	}
}

// ValidateCLI checks if the Claude CLI is available and executable
func (pm *ProcessManager) ValidateCLI() error {
	// Check if file exists
	info, err := os.Stat(pm.cliPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("claude CLI not found at path: %s", pm.cliPath)
		}
		return fmt.Errorf("failed to stat claude CLI path: %w", err)
	}

	// Check if it's a regular file (not a directory)
	if info.IsDir() {
		return fmt.Errorf("claude CLI path is a directory, not a file: %s", pm.cliPath)
	}

	// Check if it's executable
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("claude CLI is not executable: %s (mode: %s)", pm.cliPath, info.Mode().String())
	}

	// Try running with --version to verify it's actually the Claude CLI
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, pm.cliPath, "--version")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to execute claude CLI --version: %w (stderr: %s)", err, stderr.String())
	}

	version := stdout.String()
	if version == "" {
		version = stderr.String()
	}

	slog.Info("Claude CLI validation successful", "path", pm.cliPath, "version", version)
	return nil
}

func (pm *ProcessManager) GetOrCreateProcess(chatID, sessionID string) (*ClaudeProcess, error) {
	pm.mu.RLock()
	if proc, exists := pm.processes[sessionID]; exists {
		pm.mu.RUnlock()
		proc.mu.Lock()
		proc.LastUsed = time.Now()
		proc.mu.Unlock()
		return proc, nil
	}
	pm.mu.RUnlock()

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if proc, exists := pm.processes[sessionID]; exists {
		proc.mu.Lock()
		proc.LastUsed = time.Now()
		proc.mu.Unlock()
		return proc, nil
	}

	if len(pm.processes) >= pm.maxProcesses {
		return nil, fmt.Errorf("max concurrent processes reached (%d)", pm.maxProcesses)
	}

	proc, err := pm.createProcess(chatID, sessionID)
	if err != nil {
		return nil, err
	}

	pm.processes[sessionID] = proc
	return proc, nil
}

func (pm *ProcessManager) createProcess(chatID, sessionID string) (*ClaudeProcess, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Don't actually start a persistent process since we're using one-shot execution
	// Just create a placeholder - the real execution happens in executeQuerySync
	cmd := exec.CommandContext(ctx, "sleep", "86400")  // Sleep for 24 hours
	cmd.Dir = pm.projectPath

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start claude process: %w", err)
	}

	proc := &ClaudeProcess{
		SessionID: sessionID,
		ChatID:    chatID,
		Cmd:       cmd,
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
		StartedAt: time.Now(),
		LastUsed:  time.Now(),
		cancel:    cancel,
	}

	go pm.monitorProcess(proc)

	slog.Info("Created Claude process", "session_id", sessionID, "chat_id", chatID)
	return proc, nil
}

func (pm *ProcessManager) monitorProcess(proc *ClaudeProcess) {
	scanner := bufio.NewScanner(proc.Stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			slog.Debug("Claude stderr", "session_id", proc.SessionID, "message", line)
		}
	}

	if err := proc.Cmd.Wait(); err != nil {
		slog.Warn("Claude process exited with error", "session_id", proc.SessionID, "error", err)
	} else {
		slog.Info("Claude process exited normally", "session_id", proc.SessionID)
	}

	pm.mu.Lock()
	delete(pm.processes, proc.SessionID)
	pm.mu.Unlock()
}

func (pm *ProcessManager) ExecuteQuery(sessionID, query string) (string, error) {
	pm.mu.RLock()
	proc, exists := pm.processes[sessionID]
	pm.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("process not found for session %s", sessionID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), pm.timeout)
	defer cancel()

	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		result, err := pm.executeQuerySync(proc, query)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("query timeout after %v", pm.timeout)
	case err := <-errCh:
		return "", err
	case result := <-resultCh:
		return result, nil
	}
}

func (pm *ProcessManager) executeQuerySync(proc *ClaudeProcess, query string) (string, error) {
	// For now, use one-shot execution instead of persistent process
	// This is a workaround until we implement proper interactive mode handling
	ctx, cancel := context.WithTimeout(context.Background(), pm.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, pm.cliPath,
		"-p",
		"--output-format", "json",
		"--continue",
		query,
	)
	cmd.Dir = pm.projectPath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("command failed: %w, stderr: %s", err, stderr.String())
	}

	proc.LastUsed = time.Now()

	// Log raw JSON output for debugging
	slog.Debug("Claude raw JSON output", "session_id", proc.SessionID, "output", stdout.String())

	// Parse JSON output to extract text content
	parsedResponse, err := parseClaudeJSON(stdout.String())
	if err != nil {
		return "", err
	}

	slog.Debug("Parsed Claude response", "session_id", proc.SessionID, "response", parsedResponse)
	return parsedResponse, nil
}

func isResponseComplete(line string) bool {
	return false
}

// parseClaudeJSON extracts the text content from Claude's JSON output
func parseClaudeJSON(jsonOutput string) (string, error) {
	var result struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Result  string `json:"result"`
	}

	if err := json.Unmarshal([]byte(jsonOutput), &result); err != nil {
		// If JSON parsing fails, return the raw output
		slog.Warn("Failed to parse Claude JSON output", "error", err)
		return jsonOutput, nil
	}

	// Extract the result text
	if result.Result != "" {
		return result.Result, nil
	}

	return "No response from Claude", nil
}

func (pm *ProcessManager) KillProcess(sessionID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	proc, exists := pm.processes[sessionID]
	if !exists {
		return fmt.Errorf("process not found for session %s", sessionID)
	}

	proc.cancel()

	if err := proc.Stdin.Close(); err != nil {
		slog.Warn("Failed to close stdin", "session_id", sessionID, "error", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- proc.Cmd.Wait()
	}()

	select {
	case <-time.After(5 * time.Second):
		if err := proc.Cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process: %w", err)
		}
		slog.Warn("Force killed process", "session_id", sessionID)
	case err := <-done:
		if err != nil {
			slog.Warn("Process exited with error", "session_id", sessionID, "error", err)
		}
	}

	delete(pm.processes, sessionID)
	slog.Info("Killed Claude process", "session_id", sessionID)
	return nil
}

func (pm *ProcessManager) GetActiveProcessCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.processes)
}

func (pm *ProcessManager) CleanupIdleProcesses(maxIdleTime time.Duration) int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	now := time.Now()
	cleaned := 0

	for sessionID, proc := range pm.processes {
		proc.mu.Lock()
		idle := now.Sub(proc.LastUsed)
		proc.mu.Unlock()

		if idle > maxIdleTime {
			slog.Info("Cleaning up idle process", "session_id", sessionID, "idle_duration", idle)
			proc.cancel()
			delete(pm.processes, sessionID)
			cleaned++
		}
	}

	return cleaned
}
