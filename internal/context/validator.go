package context

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/rg/aiops/internal/storage"
)

type Validator struct {
	storage        *storage.Storage
	sreContext     string
	validationEnabled bool
}

func NewValidator(storage *storage.Storage, projectPath string, validationEnabled bool) (*Validator, error) {
	sreContext, err := loadSREContext(projectPath)
	if err != nil {
		slog.Warn("Failed to load SRE context", "error", err)
		sreContext = "No context available"
	}

	return &Validator{
		storage:        storage,
		sreContext:     sreContext,
		validationEnabled: validationEnabled,
	}, nil
}

func (v *Validator) ValidateQuery(ctx *storage.ChatContext, query string) (bool, string, error) {
	if !v.validationEnabled {
		return true, "", nil
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return false, "empty query", nil
	}

	keywords := []string{
		"pod", "deployment", "service", "namespace", "kubectl",
		"argocd", "application", "sync", "deploy",
		"jira", "ticket", "issue", "sprint", "story",
		"log", "logs", "error", "crash", "incident",
		"monitor", "metric", "dashboard", "alert",
		"kafka", "redis", "database", "postgres",
		"payment", "provider", "transaction",
		"kubernetes", "k8s", "helm", "kustomize",
		"datadog", "slack", "github", "pr", "pull request",
	}

	queryLower := strings.ToLower(query)
	for _, keyword := range keywords {
		if strings.Contains(queryLower, keyword) {
			return true, "", nil
		}
	}

	if strings.HasPrefix(query, "/") {
		return true, "", nil
	}

	messages, err := v.storage.GetRecentMessages(ctx.ChatID, 5)
	if err != nil {
		slog.Warn("Failed to get recent messages", "chat_id", ctx.ChatID, "error", err)
		return true, "", nil
	}

	if len(messages) > 0 {
		return true, "", nil
	}

	return false, "Query doesn't appear to be related to SRE operations. Please ask about infrastructure, deployments, incidents, or related topics.", nil
}

func loadSREContext(projectPath string) (string, error) {
	var context strings.Builder

	claudeMdPath := filepath.Join(projectPath, "CLAUDE.md")
	if content, err := os.ReadFile(claudeMdPath); err == nil {
		context.WriteString("=== CLAUDE.md ===\n")
		context.Write(content)
		context.WriteString("\n\n")
	}

	runbooksPath := filepath.Join(projectPath, "RUNBOOKS.md")
	if content, err := os.ReadFile(runbooksPath); err == nil {
		context.WriteString("=== RUNBOOKS.md ===\n")
		context.Write(content)
		context.WriteString("\n\n")
	}

	resourcesPath := filepath.Join(projectPath, "RESOURCES.md")
	if content, err := os.ReadFile(resourcesPath); err == nil {
		context.WriteString("=== RESOURCES.md ===\n")
		context.Write(content)
		context.WriteString("\n\n")
	}

	if context.Len() == 0 {
		return "", fmt.Errorf("no context files found")
	}

	return context.String(), nil
}

func (v *Validator) GetSREContext() string {
	return v.sreContext
}
