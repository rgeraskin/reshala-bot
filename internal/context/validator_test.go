package context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rg/aiops/internal/storage"
)

func TestNewValidator(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validator-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a CLAUDE.md file
	claudeMd := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.WriteFile(claudeMd, []byte("# Test Context"), 0644); err != nil {
		t.Fatalf("Failed to write CLAUDE.md: %v", err)
	}

	validator, err := NewValidator(nil, tmpDir, true)
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}

	if validator == nil {
		t.Fatal("Expected non-nil validator")
	}
	if !validator.validationEnabled {
		t.Error("Expected validation to be enabled")
	}
}

func TestNewValidator_NoContextFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validator-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Don't create any context files
	validator, err := NewValidator(nil, tmpDir, true)
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}

	// Should succeed even without context files
	if validator == nil {
		t.Fatal("Expected non-nil validator")
	}
}

func TestValidateQuery_ValidationDisabled(t *testing.T) {
	validator := &Validator{
		validationEnabled: false,
	}

	ctx := &storage.ChatContext{ChatID: "test-chat"}
	valid, reason, err := validator.ValidateQuery(ctx, "random query")

	if err != nil {
		t.Fatalf("ValidateQuery failed: %v", err)
	}
	if !valid {
		t.Error("Expected query to be valid when validation is disabled")
	}
	if reason != "" {
		t.Errorf("Expected empty reason, got %q", reason)
	}
}

func TestValidateQuery_EmptyQuery(t *testing.T) {
	validator := &Validator{
		validationEnabled: true,
	}

	ctx := &storage.ChatContext{ChatID: "test-chat"}
	valid, reason, err := validator.ValidateQuery(ctx, "   ")

	if err != nil {
		t.Fatalf("ValidateQuery failed: %v", err)
	}
	if valid {
		t.Error("Expected empty query to be invalid")
	}
	if reason != "empty query" {
		t.Errorf("Expected 'empty query' reason, got %q", reason)
	}
}

func TestValidateQuery_SREKeywords(t *testing.T) {
	// Only test queries that should match SRE keywords (no storage needed)
	validator := &Validator{
		validationEnabled: true,
	}

	ctx := &storage.ChatContext{ChatID: "test-chat"}

	// These queries contain SRE keywords and should be valid without checking storage
	validQueries := []string{
		"Show me the pods in production",
		"Check kubectl logs",
		"ArgoCD sync status",
		"Create a Jira ticket",
		"kubernetes deployment status",
		"Check datadog alerts",
		"GitHub pull request review",
		"Check the log files",
		"Show error messages",
	}

	for _, query := range validQueries {
		t.Run(query, func(t *testing.T) {
			valid, _, err := validator.ValidateQuery(ctx, query)
			if err != nil {
				t.Fatalf("ValidateQuery failed: %v", err)
			}
			if !valid {
				t.Errorf("ValidateQuery(%q) = false, expected true (should match SRE keyword)", query)
			}
		})
	}
}

func TestValidateQuery_SlashCommand(t *testing.T) {
	validator := &Validator{
		validationEnabled: true,
	}

	ctx := &storage.ChatContext{ChatID: "test-chat"}
	valid, reason, err := validator.ValidateQuery(ctx, "/status")

	if err != nil {
		t.Fatalf("ValidateQuery failed: %v", err)
	}
	if !valid {
		t.Error("Expected slash command to be valid")
	}
	if reason != "" {
		t.Errorf("Expected empty reason, got %q", reason)
	}
}

func TestLoadSREContext(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sre-context-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create all three context files
	if err := os.WriteFile(filepath.Join(tmpDir, "CLAUDE.md"), []byte("# Claude Instructions"), 0644); err != nil {
		t.Fatalf("Failed to write CLAUDE.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "RUNBOOKS.md"), []byte("# Runbooks"), 0644); err != nil {
		t.Fatalf("Failed to write RUNBOOKS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "RESOURCES.md"), []byte("# Resources"), 0644); err != nil {
		t.Fatalf("Failed to write RESOURCES.md: %v", err)
	}

	context, err := loadSREContext(tmpDir)
	if err != nil {
		t.Fatalf("loadSREContext failed: %v", err)
	}

	if context == "" {
		t.Error("Expected non-empty context")
	}
	if !contains(context, "CLAUDE.md") {
		t.Error("Expected context to contain CLAUDE.md marker")
	}
	if !contains(context, "RUNBOOKS.md") {
		t.Error("Expected context to contain RUNBOOKS.md marker")
	}
	if !contains(context, "RESOURCES.md") {
		t.Error("Expected context to contain RESOURCES.md marker")
	}
}

func TestLoadSREContext_NoFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sre-context-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	_, err = loadSREContext(tmpDir)
	if err == nil {
		t.Error("Expected error when no context files exist")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
