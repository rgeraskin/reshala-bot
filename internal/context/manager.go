package context

import (
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/rg/aiops/internal/storage"
)

type Manager struct {
	storage *storage.Storage
	ttl     time.Duration
}

func NewManager(storage *storage.Storage, ttl time.Duration) *Manager {
	return &Manager{
		storage: storage,
		ttl:     ttl,
	}
}

func (m *Manager) GetOrCreate(chatID, chatType string) (*storage.ChatContext, error) {
	ctx, err := m.storage.GetContext(chatID)
	if err != nil {
		return nil, fmt.Errorf("failed to get context: %w", err)
	}

	if ctx != nil && ctx.IsActive {
		if time.Now().Before(ctx.ExpiresAt) {
			return ctx, nil
		}
		log.Printf("Context for chat %s has expired, creating new one", chatID)
	}

	sessionID := uuid.New().String()
	ctx, err = m.storage.CreateContext(chatID, chatType, sessionID, m.ttl)
	if err != nil {
		return nil, fmt.Errorf("failed to create context: %w", err)
	}

	log.Printf("Created new context for chat %s with session %s", chatID, ctx.SessionID)
	return ctx, nil
}

func (m *Manager) Refresh(chatID string) error {
	if err := m.storage.RefreshContext(chatID, m.ttl); err != nil {
		return fmt.Errorf("failed to refresh context: %w", err)
	}
	return nil
}

func (m *Manager) Deactivate(chatID string) error {
	if err := m.storage.DeactivateContext(chatID); err != nil {
		return fmt.Errorf("failed to deactivate context: %w", err)
	}
	log.Printf("Deactivated context for chat %s", chatID)
	return nil
}

func (m *Manager) GetActiveCount() (int, error) {
	count, err := m.storage.GetActiveContextCount()
	if err != nil {
		return 0, fmt.Errorf("failed to get active count: %w", err)
	}
	return count, nil
}
