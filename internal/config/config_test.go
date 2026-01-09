package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaskSecret(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"short", "***"},
		{"exactly8", "***"},
		{"longerstring", "long...ring"},
		{"abcdefghij", "abcd...ghij"},
	}

	for _, tt := range tests {
		result := maskSecret(tt.input)
		if result != tt.expected {
			t.Errorf("maskSecret(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestExpandEnv(t *testing.T) {
	os.Setenv("TEST_VAR", "test_value")
	defer os.Unsetenv("TEST_VAR")

	input := "prefix_${TEST_VAR}_suffix"
	result := expandEnv(input)
	expected := "prefix_test_value_suffix"

	if result != expected {
		t.Errorf("expandEnv(%q) = %q, want %q", input, result, expected)
	}
}

func TestExpandEnv_MissingVar(t *testing.T) {
	os.Unsetenv("MISSING_VAR")

	input := "prefix_${MISSING_VAR}_suffix"
	result := expandEnv(input)
	expected := "prefix__suffix" // Missing var expands to empty string

	if result != expected {
		t.Errorf("expandEnv(%q) = %q, want %q", input, result, expected)
	}
}

func createTestConfig(t *testing.T, content string) (string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to write config: %v", err)
	}

	return configPath, func() {
		os.RemoveAll(tmpDir)
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	// Create a mock CLI file
	tmpDir, _ := os.MkdirTemp("", "cli-test-*")
	defer os.RemoveAll(tmpDir)

	cliPath := filepath.Join(tmpDir, "claude")
	if err := os.WriteFile(cliPath, []byte("#!/bin/bash\necho 1.0.0"), 0755); err != nil {
		t.Fatalf("Failed to create mock CLI: %v", err)
	}

	projectPath := tmpDir // Use tmpDir as project path

	config := `
telegram:
  token: "test-token-12345678"
  allowed_chat_ids:
    - "123456"

claude:
  cli_path: "` + cliPath + `"
  project_path: "` + projectPath + `"
  query_timeout: 5m
  max_concurrent_sessions: 10

context:
  ttl: 2h
  cleanup_interval: 5m
  validation_enabled: true

storage:
  db_path: "./data/test.db"

security:
  secret_patterns:
    - "api_key=.*"
`

	configPath, cleanup := createTestConfig(t, config)
	defer cleanup()

	os.Setenv("CONFIG_PATH", configPath)
	defer os.Unsetenv("CONFIG_PATH")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Telegram.Token != "test-token-12345678" {
		t.Errorf("Token = %s, want test-token-12345678", cfg.Telegram.Token)
	}
	if cfg.Claude.MaxConcurrentSessions != 10 {
		t.Errorf("MaxConcurrentSessions = %d, want 10", cfg.Claude.MaxConcurrentSessions)
	}
}

func TestLoad_MissingRequiredField(t *testing.T) {
	config := `
telegram:
  token: ""
  allowed_chat_ids:
    - "123456"
`

	configPath, cleanup := createTestConfig(t, config)
	defer cleanup()

	os.Setenv("CONFIG_PATH", configPath)
	defer os.Unsetenv("CONFIG_PATH")

	_, err := Load()
	if err == nil {
		t.Fatal("Expected error for missing token")
	}
	if !strings.Contains(err.Error(), "telegram.token") {
		t.Errorf("Error should mention telegram.token: %v", err)
	}
}

func TestLoad_EmptyAllowedChatIDs(t *testing.T) {
	config := `
telegram:
  token: "test-token-12345678"
  allowed_chat_ids: []
`

	configPath, cleanup := createTestConfig(t, config)
	defer cleanup()

	os.Setenv("CONFIG_PATH", configPath)
	defer os.Unsetenv("CONFIG_PATH")

	_, err := Load()
	if err == nil {
		t.Fatal("Expected error for empty allowed_chat_ids")
	}
	if !strings.Contains(err.Error(), "allowed_chat_ids") {
		t.Errorf("Error should mention allowed_chat_ids: %v", err)
	}
}

func TestLoad_InvalidCLIPath(t *testing.T) {
	config := `
telegram:
  token: "test-token-12345678"
  allowed_chat_ids:
    - "123456"

claude:
  cli_path: "/nonexistent/path/to/claude"
  project_path: "/tmp"
  query_timeout: 5m
  max_concurrent_sessions: 10

context:
  ttl: 2h
  cleanup_interval: 5m

storage:
  db_path: "./data/test.db"
`

	configPath, cleanup := createTestConfig(t, config)
	defer cleanup()

	os.Setenv("CONFIG_PATH", configPath)
	defer os.Unsetenv("CONFIG_PATH")

	_, err := Load()
	if err == nil {
		t.Fatal("Expected error for invalid CLI path")
	}
	if !strings.Contains(err.Error(), "cli_path") {
		t.Errorf("Error should mention cli_path: %v", err)
	}
}

func TestLoad_CLINotExecutable(t *testing.T) {
	// Create a non-executable file
	tmpDir, _ := os.MkdirTemp("", "cli-test-*")
	defer os.RemoveAll(tmpDir)

	cliPath := filepath.Join(tmpDir, "claude")
	if err := os.WriteFile(cliPath, []byte("not executable"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	config := `
telegram:
  token: "test-token-12345678"
  allowed_chat_ids:
    - "123456"

claude:
  cli_path: "` + cliPath + `"
  project_path: "` + tmpDir + `"
  query_timeout: 5m
  max_concurrent_sessions: 10

context:
  ttl: 2h
  cleanup_interval: 5m

storage:
  db_path: "./data/test.db"
`

	configPath, cleanup := createTestConfig(t, config)
	defer cleanup()

	os.Setenv("CONFIG_PATH", configPath)
	defer os.Unsetenv("CONFIG_PATH")

	_, err := Load()
	if err == nil {
		t.Fatal("Expected error for non-executable CLI")
	}
	if !strings.Contains(err.Error(), "not executable") {
		t.Errorf("Error should mention not executable: %v", err)
	}
}

func TestLoad_InvalidProjectPath(t *testing.T) {
	// Create executable CLI
	tmpDir, _ := os.MkdirTemp("", "cli-test-*")
	defer os.RemoveAll(tmpDir)

	cliPath := filepath.Join(tmpDir, "claude")
	if err := os.WriteFile(cliPath, []byte("#!/bin/bash"), 0755); err != nil {
		t.Fatalf("Failed to create CLI: %v", err)
	}

	config := `
telegram:
  token: "test-token-12345678"
  allowed_chat_ids:
    - "123456"

claude:
  cli_path: "` + cliPath + `"
  project_path: "/nonexistent/project/path"
  query_timeout: 5m
  max_concurrent_sessions: 10

context:
  ttl: 2h
  cleanup_interval: 5m

storage:
  db_path: "./data/test.db"
`

	configPath, cleanup := createTestConfig(t, config)
	defer cleanup()

	os.Setenv("CONFIG_PATH", configPath)
	defer os.Unsetenv("CONFIG_PATH")

	_, err := Load()
	if err == nil {
		t.Fatal("Expected error for invalid project path")
	}
	if !strings.Contains(err.Error(), "project_path") {
		t.Errorf("Error should mention project_path: %v", err)
	}
}

func TestLoad_EnvVarExpansion(t *testing.T) {
	// Create valid CLI and project
	tmpDir, _ := os.MkdirTemp("", "config-test-*")
	defer os.RemoveAll(tmpDir)

	cliPath := filepath.Join(tmpDir, "claude")
	if err := os.WriteFile(cliPath, []byte("#!/bin/bash"), 0755); err != nil {
		t.Fatalf("Failed to create CLI: %v", err)
	}

	os.Setenv("TEST_BOT_TOKEN", "expanded-token-12345678")
	defer os.Unsetenv("TEST_BOT_TOKEN")

	config := `
telegram:
  token: "${TEST_BOT_TOKEN}"
  allowed_chat_ids:
    - "123456"

claude:
  cli_path: "` + cliPath + `"
  project_path: "` + tmpDir + `"
  query_timeout: 5m
  max_concurrent_sessions: 10

context:
  ttl: 2h
  cleanup_interval: 5m

storage:
  db_path: "./data/test.db"
`

	configPath, cleanup := createTestConfig(t, config)
	defer cleanup()

	os.Setenv("CONFIG_PATH", configPath)
	defer os.Unsetenv("CONFIG_PATH")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Telegram.Token != "expanded-token-12345678" {
		t.Errorf("Token = %s, want expanded-token-12345678", cfg.Telegram.Token)
	}
}

func TestConfig_String(t *testing.T) {
	cfg := &Config{
		Telegram: TelegramConfig{
			Token: "secret-bot-token",
		},
		Claude: ClaudeConfig{
			CLIPath: "/usr/bin/claude",
			Model:   "sonnet",
		},
	}

	str := cfg.String()

	// Should mask the token
	if strings.Contains(str, "secret-bot-token") {
		t.Error("String() should not contain unmasked token")
	}
	if !strings.Contains(str, "secr...oken") {
		t.Error("String() should contain masked token")
	}

	// Should contain other fields
	if !strings.Contains(str, "/usr/bin/claude") {
		t.Error("String() should contain CLI path")
	}
}
