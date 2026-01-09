package context

import (
	"context"
	"log/slog"
	"time"

	"github.com/rg/aiops/internal/claude"
	"github.com/rg/aiops/internal/storage"
)

// CleanupCallback is called after a context is cleaned up (e.g., to remove per-chat locks)
type CleanupCallback func(chatID string)

type ExpiryWorker struct {
	storage         *storage.Storage
	sessionManager  *claude.SessionManager
	interval        time.Duration
	cleanupCallback CleanupCallback
}

func NewExpiryWorker(storage *storage.Storage, sm *claude.SessionManager, interval time.Duration) *ExpiryWorker {
	return &ExpiryWorker{
		storage:        storage,
		sessionManager: sm,
		interval:       interval,
	}
}

// SetCleanupCallback sets a callback to be invoked after each context cleanup
func (ew *ExpiryWorker) SetCleanupCallback(cb CleanupCallback) {
	ew.cleanupCallback = cb
}

func (ew *ExpiryWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(ew.interval)
	defer ticker.Stop()

	slog.Info("Starting expiry worker", "interval", ew.interval)

	for {
		select {
		case <-ticker.C:
			if err := ew.cleanupExpired(); err != nil {
				slog.Error("Error during cleanup", "error", err)
			}
		case <-ctx.Done():
			slog.Info("Expiry worker stopped")
			return
		}
	}
}

func (ew *ExpiryWorker) cleanupExpired() error {
	expiredContexts, err := ew.storage.GetExpiredContexts()
	if err != nil {
		return err
	}

	if len(expiredContexts) == 0 {
		return nil
	}

	slog.Info("Found expired contexts to clean up", "count", len(expiredContexts))

	for _, ctx := range expiredContexts {
		if err := ew.cleanupContext(ctx, "expired"); err != nil {
			slog.Warn("Failed to cleanup context", "chat_id", ctx.ChatID, "error", err)
			continue
		}
	}

	return nil
}

func (ew *ExpiryWorker) cleanupContext(ctx *storage.ChatContext, cleanupType string) error {
	slog.Info("Cleaning up context", "chat_id", ctx.ChatID, "session_id", ctx.SessionID, "type", cleanupType)

	// Kill session from memory (non-transactional, but safe to fail)
	if err := ew.sessionManager.KillSession(ctx.SessionID); err != nil {
		slog.Debug("No session to cleanup", "session_id", ctx.SessionID, "error", err)
	}

	// Perform database cleanup in a single transaction
	result, err := ew.storage.CleanupContextTx(ctx.ChatID, cleanupType)
	if err != nil {
		return err
	}

	// Invoke cleanup callback (e.g., to remove per-chat locks from Manager)
	if ew.cleanupCallback != nil {
		ew.cleanupCallback(ctx.ChatID)
	}

	slog.Info("Cleaned up context",
		"chat_id", ctx.ChatID,
		"messages_deleted", result.MessagesDeleted,
		"tools_deleted", result.ToolsDeleted)

	return nil
}

func (ew *ExpiryWorker) ManualCleanup(chatID string) error {
	ctx, err := ew.storage.GetContext(chatID)
	if err != nil {
		return err
	}
	if ctx == nil {
		return nil
	}

	return ew.cleanupContext(ctx, "manual")
}
