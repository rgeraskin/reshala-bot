package context

import (
	"context"
	"log"
	"time"

	"github.com/rg/aiops/internal/claude"
	"github.com/rg/aiops/internal/storage"
)

type ExpiryWorker struct {
	storage       *storage.Storage
	processManager *claude.ProcessManager
	interval      time.Duration
}

func NewExpiryWorker(storage *storage.Storage, pm *claude.ProcessManager, interval time.Duration) *ExpiryWorker {
	return &ExpiryWorker{
		storage:       storage,
		processManager: pm,
		interval:      interval,
	}
}

func (ew *ExpiryWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(ew.interval)
	defer ticker.Stop()

	log.Printf("Starting expiry worker with interval %v", ew.interval)

	for {
		select {
		case <-ticker.C:
			if err := ew.cleanupExpired(); err != nil {
				log.Printf("Error during cleanup: %v", err)
			}
		case <-ctx.Done():
			log.Println("Expiry worker stopped")
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

	log.Printf("Found %d expired contexts to clean up", len(expiredContexts))

	for _, ctx := range expiredContexts {
		if err := ew.cleanupContext(ctx); err != nil {
			log.Printf("Failed to cleanup context for chat %s: %v", ctx.ChatID, err)
			continue
		}
	}

	return nil
}

func (ew *ExpiryWorker) cleanupContext(ctx *storage.ChatContext) error {
	log.Printf("Cleaning up expired context for chat %s (session %s)", ctx.ChatID, ctx.SessionID)

	if err := ew.processManager.KillProcess(ctx.SessionID); err != nil {
		log.Printf("Warning: failed to kill process for session %s: %v", ctx.SessionID, err)
	}

	messagesDeleted, err := ew.storage.DeleteMessagesByChat(ctx.ChatID)
	if err != nil {
		log.Printf("Warning: failed to delete messages for chat %s: %v", ctx.ChatID, err)
	}

	toolsDeleted, err := ew.storage.DeleteToolExecutionsByChat(ctx.ChatID)
	if err != nil {
		log.Printf("Warning: failed to delete tool executions for chat %s: %v", ctx.ChatID, err)
	}

	if err := ew.storage.DeactivateContext(ctx.ChatID); err != nil {
		return err
	}

	if err := ew.storage.LogCleanup(ctx.ChatID, "expired", messagesDeleted, toolsDeleted); err != nil {
		log.Printf("Warning: failed to log cleanup for chat %s: %v", ctx.ChatID, err)
	}

	log.Printf("Cleaned up context for chat %s (deleted %d messages, %d tool executions)",
		ctx.ChatID, messagesDeleted, toolsDeleted)

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

	return ew.cleanupContext(ctx)
}
