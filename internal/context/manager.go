package context

import (
	"fmt"
	"log/slog"
	"sync"
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
	// Per-chatID locks to prevent race conditions during context creation/cleanup
	chatLocks   map[string]*sync.Mutex
	chatLocksMu sync.Mutex
}

func NewManager(storage *storage.Storage, sessionKiller SessionKiller, ttl time.Duration) *Manager {
	return &Manager{
		storage:       storage,
		sessionKiller: sessionKiller,
		ttl:           ttl,
		chatLocks:     make(map[string]*sync.Mutex),
	}
}

// getChatLock returns a mutex for the given chatID, creating one if needed
func (m *Manager) getChatLock(chatID string) *sync.Mutex {
	m.chatLocksMu.Lock()
	defer m.chatLocksMu.Unlock()

	if lock, exists := m.chatLocks[chatID]; exists {
		return lock
	}
	lock := &sync.Mutex{}
	m.chatLocks[chatID] = lock
	return lock
}

func (m *Manager) GetOrCreate(chatID, chatType string) (*storage.ChatContext, error) {
	// Acquire per-chatID lock to prevent race conditions during context operations
	lock := m.getChatLock(chatID)
	lock.Lock()
	defer lock.Unlock()

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
