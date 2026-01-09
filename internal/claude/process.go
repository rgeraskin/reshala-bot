package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"
)

// SessionManager tracks active sessions and executes Claude CLI queries.
// Unlike the previous ProcessManager, it does NOT spawn dummy processes.
// Sessions are lightweight in-memory trackers; actual queries are one-shot CLI calls.
type SessionManager struct {
	sessions    map[string]*Session
	mu          sync.RWMutex
	querySem    chan struct{} // Semaphore for limiting concurrent queries
	maxSessions int
	cliPath     string
	projectPath string
	model       string
	timeout     time.Duration
}

// Session tracks an active chat session without any OS process.
type Session struct {
	SessionID string
	ChatID    string
	CreatedAt time.Time
	LastUsed  time.Time
	mu        sync.Mutex
}

func NewSessionManager(cliPath, projectPath, model string, maxSessions int, timeout time.Duration) *SessionManager {
	return &SessionManager{
		sessions:    make(map[string]*Session),
		querySem:    make(chan struct{}, maxSessions),
		maxSessions: maxSessions,
		cliPath:     cliPath,
		projectPath: projectPath,
		model:       model,
		timeout:     timeout,
	}
}

// NewProcessManager is an alias for backwards compatibility during refactoring.
// Deprecated: Use NewSessionManager instead.
func NewProcessManager(cliPath, projectPath, model string, maxSessions int, timeout time.Duration) *SessionManager {
	return NewSessionManager(cliPath, projectPath, model, maxSessions, timeout)
}

// ValidateCLI checks if the Claude CLI is available and executable.
func (sm *SessionManager) ValidateCLI() error {
	info, err := os.Stat(sm.cliPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("claude CLI not found at path: %s", sm.cliPath)
		}
		return fmt.Errorf("failed to stat claude CLI path: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("claude CLI path is a directory, not a file: %s", sm.cliPath)
	}

	if info.Mode()&0111 == 0 {
		return fmt.Errorf("claude CLI is not executable: %s (mode: %s)", sm.cliPath, info.Mode().String())
	}

	// Verify it's actually the Claude CLI
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, sm.cliPath, "--version")
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

	slog.Info("Claude CLI validation successful", "path", sm.cliPath, "version", version)
	return nil
}

// GetOrCreateSession returns an existing session or creates a new one.
// This is a lightweight operation - no OS processes are spawned.
// Note: LastUsed is only updated in ExecuteQuery to avoid race conditions.
func (sm *SessionManager) GetOrCreateSession(chatID, sessionID string) (*Session, error) {
	sm.mu.RLock()
	if session, exists := sm.sessions[sessionID]; exists {
		sm.mu.RUnlock()
		// Don't update LastUsed here - ExecuteQuery handles it to avoid race
		return session, nil
	}
	sm.mu.RUnlock()

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Double-check after acquiring write lock
	if session, exists := sm.sessions[sessionID]; exists {
		// Don't update LastUsed here - ExecuteQuery handles it to avoid race
		return session, nil
	}

	if len(sm.sessions) >= sm.maxSessions {
		return nil, fmt.Errorf("max concurrent sessions reached (%d)", sm.maxSessions)
	}

	session := &Session{
		SessionID: sessionID,
		ChatID:    chatID,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}

	sm.sessions[sessionID] = session
	slog.Info("Created session", "session_id", sessionID, "chat_id", chatID)
	return session, nil
}

// GetOrCreateProcess is an alias for backwards compatibility.
// Deprecated: Use GetOrCreateSession instead.
func (sm *SessionManager) GetOrCreateProcess(chatID, sessionID string) (*Session, error) {
	return sm.GetOrCreateSession(chatID, sessionID)
}

// ExecuteQuery runs a query against Claude CLI for the given session.
// Concurrency is controlled via semaphore - this blocks if max concurrent queries reached.
func (sm *SessionManager) ExecuteQuery(sessionID, query string, claudeSessionID string) (*ClaudeJSONOutput, error) {
	sm.mu.RLock()
	session, exists := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// Acquire semaphore slot (blocks if at capacity)
	select {
	case sm.querySem <- struct{}{}:
		defer func() { <-sm.querySem }()
	case <-time.After(sm.timeout):
		return nil, fmt.Errorf("timeout waiting for available query slot")
	}

	ctx, cancel := context.WithTimeout(context.Background(), sm.timeout)
	defer cancel()

	result, err := sm.executeQuerySync(ctx, query, claudeSessionID)
	if err != nil {
		return nil, err
	}

	session.mu.Lock()
	session.LastUsed = time.Now()
	session.mu.Unlock()

	return result, nil
}

// executeQuerySync runs a one-shot Claude CLI command.
func (sm *SessionManager) executeQuerySync(ctx context.Context, query string, claudeSessionID string) (*ClaudeJSONOutput, error) {
	args := []string{
		"-p",
		"--output-format", "json",
	}

	if sm.model != "" {
		args = append(args, "--model", sm.model)
	}

	args = append(args, "--disable-slash-commands")

	// Use --resume to continue existing conversation
	if claudeSessionID != "" {
		args = append(args, "--resume", claudeSessionID)
		slog.Debug("Resuming Claude session", "claude_session_id", claudeSessionID)
	} else {
		slog.Debug("Creating new Claude session")
	}

	args = append(args, query)

	cmd := exec.CommandContext(ctx, sm.cliPath, args...)
	cmd.Dir = sm.projectPath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("command failed: %w, stderr: %s", err, stderr.String())
	}

	slog.Debug("Claude raw JSON output", "output", stdout.String())

	parsedResponse, err := parseClaudeJSON(stdout.String())
	if err != nil {
		return nil, err
	}

	slog.Debug("Parsed Claude response",
		"claude_session_id", parsedResponse.SessionID,
		"response_length", len(parsedResponse.Result))

	return parsedResponse, nil
}

// KillSession removes a session from tracking.
// Since there's no OS process to kill, this just removes the session from the map.
func (sm *SessionManager) KillSession(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.sessions[sessionID]; !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	delete(sm.sessions, sessionID)
	slog.Info("Removed session", "session_id", sessionID)
	return nil
}

// KillProcess is an alias for backwards compatibility.
// Deprecated: Use KillSession instead.
func (sm *SessionManager) KillProcess(sessionID string) error {
	return sm.KillSession(sessionID)
}

// GetActiveSessionCount returns the number of active sessions.
func (sm *SessionManager) GetActiveSessionCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

// GetActiveProcessCount is an alias for backwards compatibility.
// Deprecated: Use GetActiveSessionCount instead.
func (sm *SessionManager) GetActiveProcessCount() int {
	return sm.GetActiveSessionCount()
}

// CleanupIdleSessions removes sessions that have been idle longer than maxIdleTime.
func (sm *SessionManager) CleanupIdleSessions(maxIdleTime time.Duration) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	cleaned := 0

	for sessionID, session := range sm.sessions {
		session.mu.Lock()
		idle := now.Sub(session.LastUsed)
		session.mu.Unlock()

		if idle > maxIdleTime {
			slog.Info("Cleaning up idle session", "session_id", sessionID, "idle_duration", idle)
			delete(sm.sessions, sessionID)
			cleaned++
		}
	}

	return cleaned
}

// CleanupIdleProcesses is an alias for backwards compatibility.
// Deprecated: Use CleanupIdleSessions instead.
func (sm *SessionManager) CleanupIdleProcesses(maxIdleTime time.Duration) int {
	return sm.CleanupIdleSessions(maxIdleTime)
}

// ClaudeJSONOutput represents the parsed JSON response from Claude CLI.
type ClaudeJSONOutput struct {
	Result    string
	SessionID string
}

// parseClaudeJSON extracts the text content and session ID from Claude's JSON output.
func parseClaudeJSON(jsonOutput string) (*ClaudeJSONOutput, error) {
	var result struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
		Result    string `json:"result"`
		SessionID string `json:"session_id"`
	}

	if err := json.Unmarshal([]byte(jsonOutput), &result); err != nil {
		slog.Warn("Failed to parse Claude JSON output", "error", err)
		return &ClaudeJSONOutput{
			Result:    jsonOutput,
			SessionID: "",
		}, nil
	}

	response := &ClaudeJSONOutput{
		Result:    result.Result,
		SessionID: result.SessionID,
	}

	if response.Result == "" {
		response.Result = "No response from Claude"
	}

	return response, nil
}
