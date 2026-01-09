package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) (*Storage, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "aiops-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")

	// Create migrations directory and file
	migrationsDir := filepath.Join(tmpDir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0755); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create migrations dir: %v", err)
	}

	migrationSQL := `
CREATE TABLE IF NOT EXISTS chat_contexts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id TEXT NOT NULL UNIQUE,
    chat_type TEXT NOT NULL,
    session_id TEXT NOT NULL UNIQUE,
    claude_session_id TEXT,
    created_at DATETIME NOT NULL,
    last_interaction DATETIME NOT NULL,
    expires_at DATETIME NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id TEXT NOT NULL,
    session_id TEXT,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    FOREIGN KEY (chat_id) REFERENCES chat_contexts(chat_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS tool_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id TEXT NOT NULL,
    session_id TEXT,
    tool_name TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    FOREIGN KEY (chat_id) REFERENCES chat_contexts(chat_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS cleanup_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id TEXT NOT NULL,
    cleanup_type TEXT NOT NULL,
    messages_deleted INTEGER NOT NULL,
    tools_deleted INTEGER NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_chat_contexts_expires ON chat_contexts(expires_at);
CREATE INDEX IF NOT EXISTS idx_messages_chat_id ON messages(chat_id);
CREATE INDEX IF NOT EXISTS idx_tool_executions_chat_id ON tool_executions(chat_id);
`

	if err := os.WriteFile(filepath.Join(migrationsDir, "001_initial_schema.sql"), []byte(migrationSQL), 0644); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to write migration: %v", err)
	}

	// Change to temp dir so migrations can be found
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)

	store, err := NewStorage(dbPath)
	if err != nil {
		os.Chdir(oldWd)
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create storage: %v", err)
	}

	cleanup := func() {
		store.Close()
		os.Chdir(oldWd)
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func TestCreateContext(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, err := store.CreateContext("chat123", "group", "session-abc", 2*time.Hour)
	if err != nil {
		t.Fatalf("CreateContext failed: %v", err)
	}

	if ctx.ChatID != "chat123" {
		t.Errorf("ChatID = %s, want chat123", ctx.ChatID)
	}
	if ctx.SessionID != "session-abc" {
		t.Errorf("SessionID = %s, want session-abc", ctx.SessionID)
	}
	if !ctx.IsActive {
		t.Error("IsActive should be true")
	}
}

func TestCreateContext_InsertOrReplace(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create first context
	ctx1, err := store.CreateContext("chat123", "group", "session-1", 2*time.Hour)
	if err != nil {
		t.Fatalf("First CreateContext failed: %v", err)
	}

	// Create second context for same chat (should replace)
	ctx2, err := store.CreateContext("chat123", "group", "session-2", 2*time.Hour)
	if err != nil {
		t.Fatalf("Second CreateContext failed: %v", err)
	}

	// Should have different session IDs
	if ctx1.SessionID == ctx2.SessionID {
		t.Error("Second context should have different session ID")
	}

	// Fetching should return the new one
	fetched, err := store.GetContext("chat123")
	if err != nil {
		t.Fatalf("GetContext failed: %v", err)
	}
	if fetched.SessionID != "session-2" {
		t.Errorf("SessionID = %s, want session-2", fetched.SessionID)
	}
}

func TestGetContext_NotFound(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, err := store.GetContext("nonexistent")
	if err != nil {
		t.Fatalf("GetContext failed: %v", err)
	}
	if ctx != nil {
		t.Error("Expected nil for nonexistent context")
	}
}

func TestRefreshContext(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create context
	_, err := store.CreateContext("chat123", "group", "session-1", 1*time.Hour)
	if err != nil {
		t.Fatalf("CreateContext failed: %v", err)
	}

	// Refresh with new TTL
	time.Sleep(10 * time.Millisecond)
	err = store.RefreshContext("chat123", 2*time.Hour)
	if err != nil {
		t.Fatalf("RefreshContext failed: %v", err)
	}

	// Verify expiry was extended
	ctx, _ := store.GetContext("chat123")
	expectedExpiry := time.Now().Add(2 * time.Hour)
	if ctx.ExpiresAt.Before(expectedExpiry.Add(-1 * time.Minute)) {
		t.Error("ExpiresAt should be extended")
	}
}

func TestRefreshContext_InactiveContext(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create and deactivate context
	_, _ = store.CreateContext("chat123", "group", "session-1", 1*time.Hour)
	_ = store.DeactivateContext("chat123")

	// Refresh should fail
	err := store.RefreshContext("chat123", 2*time.Hour)
	if err == nil {
		t.Error("RefreshContext should fail for inactive context")
	}
}

func TestGetExpiredContexts(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create expired context (TTL of 0 means already expired)
	_, _ = store.CreateContext("expired1", "group", "session-1", -1*time.Hour)
	_, _ = store.CreateContext("expired2", "group", "session-2", -30*time.Minute)

	// Create non-expired context
	_, _ = store.CreateContext("active", "group", "session-3", 2*time.Hour)

	expired, err := store.GetExpiredContexts()
	if err != nil {
		t.Fatalf("GetExpiredContexts failed: %v", err)
	}

	if len(expired) != 2 {
		t.Errorf("Expected 2 expired contexts, got %d", len(expired))
	}
}

func TestDeactivateContext(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	_, _ = store.CreateContext("chat123", "group", "session-1", 2*time.Hour)

	err := store.DeactivateContext("chat123")
	if err != nil {
		t.Fatalf("DeactivateContext failed: %v", err)
	}

	ctx, _ := store.GetContext("chat123")
	if ctx.IsActive {
		t.Error("Context should be inactive after deactivation")
	}
}

func TestDeactivateContext_NotFound(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	err := store.DeactivateContext("nonexistent")
	if err == nil {
		t.Error("DeactivateContext should fail for nonexistent context")
	}
}

func TestCleanupContextTx(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create context with messages and tool executions
	_, _ = store.CreateContext("chat123", "group", "session-1", 2*time.Hour)
	_ = store.SaveMessage("chat123", "session-1", "user", "Hello")
	_ = store.SaveMessage("chat123", "session-1", "assistant", "Hi there")
	_ = store.SaveToolExecution("chat123", "session-1", "kubectl", "success")

	// Run transactional cleanup
	result, err := store.CleanupContextTx("chat123", "test")
	if err != nil {
		t.Fatalf("CleanupContextTx failed: %v", err)
	}

	// Verify preserved counts (data is NOT deleted)
	if result.MessagesPreserved != 2 {
		t.Errorf("MessagesPreserved = %d, want 2", result.MessagesPreserved)
	}
	if result.ToolsPreserved != 1 {
		t.Errorf("ToolsPreserved = %d, want 1", result.ToolsPreserved)
	}

	// Verify context is deactivated
	ctx, _ := store.GetContext("chat123")
	if ctx.IsActive {
		t.Error("Context should be inactive after cleanup")
	}

	// Verify messages are PRESERVED (not deleted)
	messages, _ := store.GetRecentMessages("chat123", 100)
	if len(messages) != 2 {
		t.Errorf("Expected 2 messages preserved after cleanup, got %d", len(messages))
	}

	// Verify tool executions are PRESERVED
	tools, _ := store.GetToolExecutions("chat123", 100)
	if len(tools) != 1 {
		t.Errorf("Expected 1 tool execution preserved after cleanup, got %d", len(tools))
	}
}

func TestSessionIsolation(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create first session and add messages
	_, _ = store.CreateContext("chat123", "group", "session-1", 2*time.Hour)
	_ = store.SaveMessage("chat123", "session-1", "user", "Message from session 1")

	// Cleanup (simulate expiry) - data preserved but context deactivated
	_, _ = store.CleanupContextTx("chat123", "expired")

	// Create second session and add messages
	_, _ = store.CreateContext("chat123", "group", "session-2", 2*time.Hour)
	_ = store.SaveMessage("chat123", "session-2", "user", "Message from session 2")

	// Session-scoped query should only return session-2 messages
	session2Messages, _ := store.GetRecentMessagesBySession("chat123", "session-2", 100)
	if len(session2Messages) != 1 {
		t.Errorf("Expected 1 message in session-2, got %d", len(session2Messages))
	}
	if len(session2Messages) > 0 && session2Messages[0].Content != "Message from session 2" {
		t.Errorf("Expected 'Message from session 2', got '%s'", session2Messages[0].Content)
	}

	// All-chat query should return both sessions' messages
	allMessages, _ := store.GetRecentMessages("chat123", 100)
	if len(allMessages) != 2 {
		t.Errorf("Expected 2 total messages, got %d", len(allMessages))
	}
}

func TestUpdateClaudeSessionID(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	_, _ = store.CreateContext("chat123", "group", "session-1", 2*time.Hour)

	err := store.UpdateClaudeSessionID("chat123", "claude-session-xyz")
	if err != nil {
		t.Fatalf("UpdateClaudeSessionID failed: %v", err)
	}

	ctx, _ := store.GetContext("chat123")
	if ctx.ClaudeSessionID != "claude-session-xyz" {
		t.Errorf("ClaudeSessionID = %s, want claude-session-xyz", ctx.ClaudeSessionID)
	}
}

func TestGetActiveContextCount(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	// Create some contexts
	_, _ = store.CreateContext("chat1", "group", "session-1", 2*time.Hour)
	_, _ = store.CreateContext("chat2", "group", "session-2", 2*time.Hour)
	_, _ = store.CreateContext("chat3", "group", "session-3", 2*time.Hour)

	// Deactivate one
	_ = store.DeactivateContext("chat2")

	count, err := store.GetActiveContextCount()
	if err != nil {
		t.Fatalf("GetActiveContextCount failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Active count = %d, want 2", count)
	}
}
