package context

import (
	"log/slog"
	"strings"

	"github.com/rg/aiops/internal/storage"
)

type Validator struct {
	storage           *storage.Storage
	validationEnabled bool
}

// NewValidator creates a new Validator. The projectPath parameter is accepted
// for API compatibility but is currently unused.
func NewValidator(storage *storage.Storage, projectPath string, validationEnabled bool) (*Validator, error) {
	_ = projectPath // Reserved for future use (e.g., loading SRE context)

	return &Validator{
		storage:           storage,
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
