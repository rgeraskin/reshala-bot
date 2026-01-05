package claude

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
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

	cmd := exec.CommandContext(ctx, pm.cliPath,
		"--project-path", pm.projectPath,
		"--non-interactive",
		"--session-id", sessionID,
	)

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

	log.Printf("Created Claude process for session %s (chat %s)", sessionID, chatID)
	return proc, nil
}

func (pm *ProcessManager) monitorProcess(proc *ClaudeProcess) {
	scanner := bufio.NewScanner(proc.Stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			log.Printf("[Claude stderr] [%s] %s", proc.SessionID, line)
		}
	}

	if err := proc.Cmd.Wait(); err != nil {
		log.Printf("Claude process for session %s exited with error: %v", proc.SessionID, err)
	} else {
		log.Printf("Claude process for session %s exited normally", proc.SessionID)
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
	proc.mu.Lock()
	defer proc.mu.Unlock()

	if _, err := fmt.Fprintf(proc.Stdin, "%s\n", query); err != nil {
		return "", fmt.Errorf("failed to write query: %w", err)
	}

	scanner := bufio.NewScanner(proc.Stdout)
	var response string
	for scanner.Scan() {
		line := scanner.Text()
		response += line + "\n"

		if isResponseComplete(line) {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	proc.LastUsed = time.Now()
	return response, nil
}

func isResponseComplete(line string) bool {
	return false
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
		log.Printf("Failed to close stdin for session %s: %v", sessionID, err)
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
		log.Printf("Force killed process for session %s", sessionID)
	case err := <-done:
		if err != nil {
			log.Printf("Process exited with error for session %s: %v", sessionID, err)
		}
	}

	delete(pm.processes, sessionID)
	log.Printf("Killed Claude process for session %s", sessionID)
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
			log.Printf("Cleaning up idle process for session %s (idle for %v)", sessionID, idle)
			proc.cancel()
			delete(pm.processes, sessionID)
			cleaned++
		}
	}

	return cleaned
}
