package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/rg/aiops/internal/storage"
)

func TestTruncateText(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		maxLen int
		want   string
	}{
		{"short text", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncated", "hello world", 8, "hello wo..."},
		{"empty", "", 10, ""},
		{"single char max", "hello", 1, "h..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateText(tt.text, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateText(%q, %d) = %q, want %q", tt.text, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestSplitResponse(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxLen   int
		wantLen  int
		wantAll  bool
		contains []string
	}{
		{
			name:    "short text",
			text:    "hello",
			maxLen:  100,
			wantLen: 1,
		},
		{
			name:    "exact max",
			text:    "hello",
			maxLen:  5,
			wantLen: 1,
		},
		{
			name:    "multiple lines split",
			text:    "line1\nline2\nline3",
			maxLen:  8,
			wantLen: 3,
		},
		{
			name:    "long single line",
			text:    "abcdefghij",
			maxLen:  5,
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitResponse(tt.text, tt.maxLen)
			if len(got) != tt.wantLen {
				t.Errorf("splitResponse() returned %d chunks, want %d", len(got), tt.wantLen)
			}

			// For single chunk case, chunk should match original
			if tt.wantLen == 1 && got[0] != tt.text {
				t.Errorf("single chunk should equal original text")
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"just now", 3 * time.Second, "just now"},
		{"seconds", 30 * time.Second, "30s"},
		{"minutes", 5 * time.Minute, "5m"},
		{"hours", 2 * time.Hour, "2h"},
		{"hours and minutes", 2*time.Hour + 30*time.Minute, "2h 30m"},
		{"negative", -5 * time.Minute, "5m"}, // Takes absolute value
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestFormatDurationAgo(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"just now", 3 * time.Second, "just now"},
		{"seconds ago", 30 * time.Second, "30s ago"},
		{"minutes ago", 5 * time.Minute, "5m ago"},
		{"hours ago", 2 * time.Hour, "2h ago"},
		{"hours and minutes ago", 2*time.Hour + 30*time.Minute, "2h 30m ago"},
		{"negative is just now", -5 * time.Minute, "just now"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDurationAgo(tt.d)
			if got != tt.want {
				t.Errorf("formatDurationAgo(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestGetHelpText(t *testing.T) {
	helpText := getHelpText()

	// Verify help text contains expected commands
	expectedCommands := []string{"/status", "/help", "/history", "/new"}
	for _, cmd := range expectedCommands {
		if !strings.Contains(helpText, cmd) {
			t.Errorf("Help text should contain %q", cmd)
		}
	}

	// Verify it mentions key features
	if !strings.Contains(helpText, "2 hours") {
		t.Error("Help text should mention session TTL")
	}
}

func TestFormatStatusResponse(t *testing.T) {
	ctx := &storage.ChatContext{
		SessionID:       "test-session-123",
		ClaudeSessionID: "claude-456",
		CreatedAt:       time.Now().Add(-1 * time.Hour),
		LastInteraction: time.Now().Add(-5 * time.Minute),
		ExpiresAt:       time.Now().Add(1 * time.Hour),
		IsActive:        true,
	}

	response := formatStatusResponse(ctx, 10, 5)

	// Check that response contains key information
	if !strings.Contains(response, "test-session-123") {
		t.Error("Response should contain session ID")
	}
	if !strings.Contains(response, "claude-456") {
		t.Error("Response should contain Claude session ID")
	}
	if !strings.Contains(response, "10") {
		t.Error("Response should contain message count")
	}
	if !strings.Contains(response, "5") {
		t.Error("Response should contain tool count")
	}
	if !strings.Contains(response, "Active") {
		t.Error("Response should show active status")
	}
}

func TestFormatStatusResponse_NoClaudeSession(t *testing.T) {
	ctx := &storage.ChatContext{
		SessionID:       "test-session-123",
		ClaudeSessionID: "", // No Claude session yet
		CreatedAt:       time.Now().Add(-1 * time.Hour),
		LastInteraction: time.Now().Add(-5 * time.Minute),
		ExpiresAt:       time.Now().Add(1 * time.Hour),
		IsActive:        true,
	}

	response := formatStatusResponse(ctx, 0, 0)

	if !strings.Contains(response, "Not yet initialized") {
		t.Error("Response should indicate Claude session not initialized")
	}
}

func TestFormatStatusResponse_ExpiredSession(t *testing.T) {
	ctx := &storage.ChatContext{
		SessionID:       "test-session-123",
		CreatedAt:       time.Now().Add(-3 * time.Hour),
		LastInteraction: time.Now().Add(-2 * time.Hour),
		ExpiresAt:       time.Now().Add(-1 * time.Hour), // Expired
		IsActive:        false,
	}

	response := formatStatusResponse(ctx, 0, 0)

	if !strings.Contains(response, "expired") || !strings.Contains(response, "Inactive") {
		t.Error("Response should indicate expired/inactive status")
	}
}

func TestFormatHistoryResponse(t *testing.T) {
	ctx := &storage.ChatContext{
		SessionID: "test-session",
	}

	messages := []*storage.Message{
		{Role: "user", Content: "Hello", CreatedAt: time.Now().Add(-10 * time.Minute)},
		{Role: "assistant", Content: "Hi there!", CreatedAt: time.Now().Add(-9 * time.Minute)},
	}

	response := formatHistoryResponse(ctx, messages)

	if !strings.Contains(response, "test-session") {
		t.Error("Response should contain session ID")
	}
	if !strings.Contains(response, "2") {
		t.Error("Response should show message count")
	}
	if !strings.Contains(response, "User") || !strings.Contains(response, "Assistant") {
		t.Error("Response should show role labels")
	}
}

func TestFormatHistoryResponse_LongMessage(t *testing.T) {
	ctx := &storage.ChatContext{
		SessionID: "test-session",
	}

	// Create a message longer than 500 characters
	longContent := strings.Repeat("a", 600)
	messages := []*storage.Message{
		{Role: "user", Content: longContent, CreatedAt: time.Now()},
	}

	response := formatHistoryResponse(ctx, messages)

	if !strings.Contains(response, "[... truncated ...]") {
		t.Error("Long messages should be truncated")
	}
	// Should not contain the full 600 characters
	if strings.Contains(response, strings.Repeat("a", 600)) {
		t.Error("Response should not contain the full long message")
	}
}

func TestNewHandler(t *testing.T) {
	allowedChatIDs := []string{"123", "456", "789"}

	handler := NewHandler(
		nil, // platform
		nil, // contextManager
		nil, // expiryWorker
		nil, // validator
		nil, // sessionManager
		nil, // executor
		nil, // sanitizer
		nil, // storage
		allowedChatIDs,
	)

	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}

	// Check allowed chat IDs map was built correctly
	for _, id := range allowedChatIDs {
		if !handler.allowedChatIDs[id] {
			t.Errorf("Expected chat ID %s to be allowed", id)
		}
	}

	// Check a non-allowed ID
	if handler.allowedChatIDs["999"] {
		t.Error("Chat ID 999 should not be allowed")
	}
}
