package context

import (
	"os"
	"testing"

	"github.com/rg/aiops/internal/storage"
)

func TestNewValidator(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validator-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

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

