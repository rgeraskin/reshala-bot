package context

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/rg/aiops/internal/storage"
)

// SessionKiller interface for killing sessions (avoids circular import with claude package)
type SessionKiller interface {
	KillSession(sessionID string) error
}

type Manager struct {
	storage       *storage.Storage
	sessionKiller SessionKiller
	ttl           time.Duration
}

func NewManager(storage *storage.Storage, sessionKiller SessionKiller, ttl time.Duration) *Manager {
	return &Manager{
		storage:       storage,
		sessionKiller: sessionKiller,
		ttl:           ttl,
	}
}

func (m *Manager) GetOrCreate(chatID, chatType string) (*storage.ChatContext, error) {
	ctx, err := m.storage.GetContext(chatID)
	if err != nil {
		return nil, fmt.Errorf("failed to get context: %w", err)
	}

	if ctx != nil {
		if ctx.IsActive && time.Now().Before(ctx.ExpiresAt) {
			return ctx, nil
		}

		// Context is expired or inactive - cleanup old session before creating new one
		if ctx.IsActive {
			slog.Info("Context expired, creating new one", "chat_id", chatID)
		}

		// Kill old session from SessionManager to prevent orphaning
		if ctx.SessionID != "" && m.sessionKiller != nil {
			if err := m.sessionKiller.KillSession(ctx.SessionID); err != nil {
				slog.Debug("No session to cleanup", "session_id", ctx.SessionID, "error", err)
			} else {
				slog.Info("Killed orphaned session", "session_id", ctx.SessionID)
			}
		}

		if err := m.storage.DeactivateContext(chatID); err != nil {
			slog.Warn("Failed to deactivate context", "chat_id", chatID, "error", err)
		}
	}

	sessionID := uuid.New().String()
	ctx, err = m.storage.CreateContext(chatID, chatType, sessionID, m.ttl)
	if err != nil {
		return nil, fmt.Errorf("failed to create context: %w", err)
	}

	slog.Info("Created new context", "chat_id", chatID, "session_id", ctx.SessionID)
	return ctx, nil
}

func (m *Manager) Refresh(chatID string) error {
	if err := m.storage.RefreshContext(chatID, m.ttl); err != nil {
		return fmt.Errorf("failed to refresh context: %w", err)
	}
	return nil
}
