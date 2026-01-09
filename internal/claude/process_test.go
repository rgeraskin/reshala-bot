package claude

import (
	"sync"
	"testing"
	"time"
)

func TestNewSessionManager(t *testing.T) {
	sm := NewSessionManager("/usr/bin/claude", "/tmp/project", "sonnet", 10, 5*time.Minute)

	if sm == nil {
		t.Fatal("NewSessionManager returned nil")
	}
	if sm.maxSessions != 10 {
		t.Errorf("maxSessions = %d, want 10", sm.maxSessions)
	}
	if sm.cliPath != "/usr/bin/claude" {
		t.Errorf("cliPath = %s, want /usr/bin/claude", sm.cliPath)
	}
}

func TestGetOrCreateSession_New(t *testing.T) {
	sm := NewSessionManager("/usr/bin/claude", "/tmp/project", "sonnet", 10, 5*time.Minute)

	session, err := sm.GetOrCreateSession("chat123", "session-abc")
	if err != nil {
		t.Fatalf("GetOrCreateSession failed: %v", err)
	}

	if session.SessionID != "session-abc" {
		t.Errorf("SessionID = %s, want session-abc", session.SessionID)
	}
	if session.ChatID != "chat123" {
		t.Errorf("ChatID = %s, want chat123", session.ChatID)
	}
}

func TestGetOrCreateSession_Existing(t *testing.T) {
	sm := NewSessionManager("/usr/bin/claude", "/tmp/project", "sonnet", 10, 5*time.Minute)

	// Create first
	session1, _ := sm.GetOrCreateSession("chat123", "session-abc")

	// Get same session
	session2, err := sm.GetOrCreateSession("chat123", "session-abc")
	if err != nil {
		t.Fatalf("GetOrCreateSession failed: %v", err)
	}

	if session1 != session2 {
		t.Error("Should return same session instance")
	}
}

func TestGetOrCreateSession_MaxSessions(t *testing.T) {
	sm := NewSessionManager("/usr/bin/claude", "/tmp/project", "sonnet", 2, 5*time.Minute)

	// Create max sessions
	_, _ = sm.GetOrCreateSession("chat1", "session-1")
	_, _ = sm.GetOrCreateSession("chat2", "session-2")

	// Try to create one more
	_, err := sm.GetOrCreateSession("chat3", "session-3")
	if err == nil {
		t.Error("Expected error when max sessions reached")
	}
}

func TestGetOrCreateSession_Concurrent(t *testing.T) {
	sm := NewSessionManager("/usr/bin/claude", "/tmp/project", "sonnet", 100, 5*time.Minute)

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	// Spawn 50 goroutines trying to get/create the same session
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := sm.GetOrCreateSession("chat123", "session-abc")
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent GetOrCreateSession failed: %v", err)
	}

	// Should only have 1 session
	if sm.GetActiveSessionCount() != 1 {
		t.Errorf("ActiveSessionCount = %d, want 1", sm.GetActiveSessionCount())
	}
}

func TestKillSession(t *testing.T) {
	sm := NewSessionManager("/usr/bin/claude", "/tmp/project", "sonnet", 10, 5*time.Minute)

	_, _ = sm.GetOrCreateSession("chat123", "session-abc")

	err := sm.KillSession("session-abc")
	if err != nil {
		t.Fatalf("KillSession failed: %v", err)
	}

	if sm.GetActiveSessionCount() != 0 {
		t.Errorf("ActiveSessionCount = %d, want 0", sm.GetActiveSessionCount())
	}
}

func TestKillSession_NotFound(t *testing.T) {
	sm := NewSessionManager("/usr/bin/claude", "/tmp/project", "sonnet", 10, 5*time.Minute)

	err := sm.KillSession("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent session")
	}
}

func TestGetActiveSessionCount(t *testing.T) {
	sm := NewSessionManager("/usr/bin/claude", "/tmp/project", "sonnet", 10, 5*time.Minute)

	if sm.GetActiveSessionCount() != 0 {
		t.Error("Initial count should be 0")
	}

	_, _ = sm.GetOrCreateSession("chat1", "session-1")
	_, _ = sm.GetOrCreateSession("chat2", "session-2")

	if sm.GetActiveSessionCount() != 2 {
		t.Errorf("ActiveSessionCount = %d, want 2", sm.GetActiveSessionCount())
	}
}

func TestCleanupIdleSessions(t *testing.T) {
	sm := NewSessionManager("/usr/bin/claude", "/tmp/project", "sonnet", 10, 5*time.Minute)

	// Create sessions with different LastUsed times
	session1, _ := sm.GetOrCreateSession("chat1", "session-1")
	session1.mu.Lock()
	session1.LastUsed = time.Now().Add(-2 * time.Hour) // Old
	session1.mu.Unlock()

	session2, _ := sm.GetOrCreateSession("chat2", "session-2")
	session2.mu.Lock()
	session2.LastUsed = time.Now() // Recent
	session2.mu.Unlock()

	// Cleanup sessions idle for more than 1 hour
	cleaned := sm.CleanupIdleSessions(1 * time.Hour)

	if cleaned != 1 {
		t.Errorf("Cleaned = %d, want 1", cleaned)
	}
	if sm.GetActiveSessionCount() != 1 {
		t.Errorf("ActiveSessionCount = %d, want 1", sm.GetActiveSessionCount())
	}
}

func TestParseClaudeJSON_ValidResponse(t *testing.T) {
	json := `{
		"type": "result",
		"subtype": "success",
		"result": "Hello, this is Claude!",
		"session_id": "abc-123-xyz"
	}`

	output, err := parseClaudeJSON(json)
	if err != nil {
		t.Fatalf("parseClaudeJSON failed: %v", err)
	}

	if output.Result != "Hello, this is Claude!" {
		t.Errorf("Result = %s, want 'Hello, this is Claude!'", output.Result)
	}
	if output.SessionID != "abc-123-xyz" {
		t.Errorf("SessionID = %s, want abc-123-xyz", output.SessionID)
	}
}

func TestParseClaudeJSON_InvalidJSON(t *testing.T) {
	// Invalid JSON should return the raw input as result
	json := "not valid json"

	output, err := parseClaudeJSON(json)
	if err != nil {
		t.Fatalf("parseClaudeJSON failed: %v", err)
	}

	if output.Result != json {
		t.Errorf("Result should be raw input for invalid JSON")
	}
}

func TestParseClaudeJSON_EmptyResult(t *testing.T) {
	json := `{
		"type": "result",
		"result": "",
		"session_id": "abc-123"
	}`

	output, err := parseClaudeJSON(json)
	if err != nil {
		t.Fatalf("parseClaudeJSON failed: %v", err)
	}

	if output.Result != "No response from Claude" {
		t.Errorf("Result = %s, want 'No response from Claude'", output.Result)
	}
}

// Test that LastUsed is NOT updated in GetOrCreateSession (only in ExecuteQuery)
func TestGetOrCreateSession_DoesNotUpdateLastUsed(t *testing.T) {
	sm := NewSessionManager("/usr/bin/claude", "/tmp/project", "sonnet", 10, 5*time.Minute)

	// Create session
	session, _ := sm.GetOrCreateSession("chat123", "session-abc")
	originalLastUsed := session.LastUsed

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Get same session again
	_, _ = sm.GetOrCreateSession("chat123", "session-abc")

	// LastUsed should NOT have changed
	if session.LastUsed != originalLastUsed {
		t.Error("GetOrCreateSession should not update LastUsed")
	}
}

// Backward compatibility tests
func TestDeprecatedAliases(t *testing.T) {
	sm := NewProcessManager("/usr/bin/claude", "/tmp/project", "sonnet", 10, 5*time.Minute)
	if sm == nil {
		t.Fatal("NewProcessManager should work as alias")
	}

	session, _ := sm.GetOrCreateProcess("chat123", "session-abc")
	if session == nil {
		t.Fatal("GetOrCreateProcess should work as alias")
	}

	count := sm.GetActiveProcessCount()
	if count != 1 {
		t.Errorf("GetActiveProcessCount = %d, want 1", count)
	}

	cleaned := sm.CleanupIdleProcesses(24 * time.Hour)
	if cleaned != 0 {
		t.Errorf("CleanupIdleProcesses = %d, want 0", cleaned)
	}

	err := sm.KillProcess("session-abc")
	if err != nil {
		t.Errorf("KillProcess failed: %v", err)
	}
}
