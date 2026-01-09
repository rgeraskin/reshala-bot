package security

import (
	"strings"
	"testing"
)

func TestNewSanitizer_ValidPatterns(t *testing.T) {
	patterns := []string{
		`api[_-]?key[s]?\s*[:=]\s*["']?([^"'\s]+)`,
		`token[s]?\s*[:=]\s*["']?([^"'\s]+)`,
	}

	sanitizer, err := NewSanitizer(patterns)
	if err != nil {
		t.Fatalf("NewSanitizer failed with valid patterns: %v", err)
	}
	if sanitizer == nil {
		t.Fatal("NewSanitizer returned nil")
	}
	if len(sanitizer.patterns) != 2 {
		t.Errorf("Expected 2 patterns, got %d", len(sanitizer.patterns))
	}
}

func TestNewSanitizer_InvalidPattern(t *testing.T) {
	patterns := []string{
		`valid.*pattern`,
		`[invalid`,  // Unclosed bracket
	}

	sanitizer, err := NewSanitizer(patterns)
	if err == nil {
		t.Fatal("Expected error for invalid pattern, got nil")
	}
	if sanitizer != nil {
		t.Fatal("Expected nil sanitizer on error")
	}
	if !strings.Contains(err.Error(), "[invalid") {
		t.Errorf("Error should contain the invalid pattern: %v", err)
	}
}

func TestNewSanitizer_EmptyPatterns(t *testing.T) {
	sanitizer, err := NewSanitizer([]string{})
	if err != nil {
		t.Fatalf("NewSanitizer failed with empty patterns: %v", err)
	}
	if sanitizer == nil {
		t.Fatal("NewSanitizer returned nil")
	}
}

func TestSanitize_APIKeys(t *testing.T) {
	sanitizer, _ := NewSanitizer(DefaultPatterns)

	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "api_key with equals",
			input:    "api_key=sk_live_abc123xyz",
			contains: "***REDACTED***",
		},
		{
			name:     "api-key with colon",
			input:    "api-key: secret123",
			contains: "***REDACTED***",
		},
		{
			name:     "apikey quoted",
			input:    `apikey="my-secret-key"`,
			contains: "***REDACTED***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tt.input)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("Expected result to contain %q, got %q", tt.contains, result)
			}
		})
	}
}

func TestSanitize_JWTTokens(t *testing.T) {
	sanitizer, _ := NewSanitizer(DefaultPatterns)

	// Real JWT structure (header.payload.signature)
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	input := "Authorization: Bearer " + jwt

	result := sanitizer.Sanitize(input)
	// The JWT pattern matches individual parts, so at least something should be redacted
	if !strings.Contains(result, "***REDACTED***") {
		t.Errorf("JWT should have redacted parts, got: %s", result)
	}
}

func TestSanitize_Base64Secrets(t *testing.T) {
	sanitizer, _ := NewSanitizer(DefaultPatterns)

	// 40+ character base64 string
	base64Secret := "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY3ODkw"
	input := "secret: " + base64Secret

	result := sanitizer.Sanitize(input)
	if strings.Contains(result, base64Secret) {
		t.Errorf("Base64 secret should be redacted, got: %s", result)
	}
}

func TestSanitize_SlackTokens(t *testing.T) {
	sanitizer, _ := NewSanitizer(DefaultPatterns)

	slackToken := "xoxb-1234567890123-1234567890123-1234567890123-abcdefghijklmnopqrstuvwxyz123456"
	input := "SLACK_TOKEN=" + slackToken

	result := sanitizer.Sanitize(input)
	if strings.Contains(result, "xoxb-") {
		t.Errorf("Slack token should be redacted, got: %s", result)
	}
}

func TestSanitize_NoSensitiveData(t *testing.T) {
	sanitizer, _ := NewSanitizer(DefaultPatterns)

	input := "This is a normal message with no secrets"
	result := sanitizer.Sanitize(input)
	if result != input {
		t.Errorf("Message without secrets should not be modified, got: %s", result)
	}
}

func TestSanitize_Password(t *testing.T) {
	sanitizer, _ := NewSanitizer(DefaultPatterns)

	tests := []struct {
		input string
	}{
		{`password=mysecretpassword`},
		{`password: "hunter2"`},
		{`passwords = secret123`},
	}

	for _, tt := range tests {
		result := sanitizer.Sanitize(tt.input)
		if !strings.Contains(result, "***REDACTED***") {
			t.Errorf("Password should be redacted in %q, got: %s", tt.input, result)
		}
	}
}

func TestContainsSensitiveData(t *testing.T) {
	sanitizer, _ := NewSanitizer(DefaultPatterns)

	tests := []struct {
		input    string
		expected bool
	}{
		{"api_key=secret123", true},
		{"token: abc123", true},
		{"Normal text", false},
		{"password=hunter2", true},
	}

	for _, tt := range tests {
		result := sanitizer.ContainsSensitiveData(tt.input)
		if result != tt.expected {
			t.Errorf("ContainsSensitiveData(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestSanitizeMultiple(t *testing.T) {
	sanitizer, _ := NewSanitizer(DefaultPatterns)

	inputs := []string{
		"api_key=secret1",
		"Normal text",
		"password=secret2",
	}

	results := sanitizer.SanitizeMultiple(inputs...)

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}
	if !strings.Contains(results[0], "***REDACTED***") {
		t.Errorf("First result should be redacted")
	}
	if results[1] != "Normal text" {
		t.Errorf("Second result should be unchanged")
	}
	if !strings.Contains(results[2], "***REDACTED***") {
		t.Errorf("Third result should be redacted")
	}
}

func TestValidate(t *testing.T) {
	t.Run("with patterns", func(t *testing.T) {
		sanitizer, _ := NewSanitizer(DefaultPatterns)
		if err := sanitizer.Validate(); err != nil {
			t.Errorf("Validate should pass with patterns: %v", err)
		}
	})

	t.Run("without patterns", func(t *testing.T) {
		sanitizer, _ := NewSanitizer([]string{})
		if err := sanitizer.Validate(); err == nil {
			t.Error("Validate should fail without patterns")
		}
	})
}
