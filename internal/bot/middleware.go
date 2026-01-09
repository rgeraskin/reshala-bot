package bot

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/rg/aiops/internal/messaging"
)

const (
	// cleanupWindowMultiplier determines how far back to look when cleaning up
	// old rate limit entries. Using 2x the window ensures we keep entries long
	// enough to accurately track the rate limit window.
	cleanupWindowMultiplier = 2
)

type RateLimiter struct {
	requests map[string][]time.Time
	mu       sync.Mutex
	limit    int
	window   time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (rl *RateLimiter) Allow(chatID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	requests, exists := rl.requests[chatID]
	if !exists {
		rl.requests[chatID] = []time.Time{now}
		return true
	}

	var validRequests []time.Time
	for _, t := range requests {
		if t.After(cutoff) {
			validRequests = append(validRequests, t)
		}
	}

	if len(validRequests) >= rl.limit {
		rl.requests[chatID] = validRequests
		return false
	}

	validRequests = append(validRequests, now)
	rl.requests[chatID] = validRequests
	return true
}

func (rl *RateLimiter) Cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window * cleanupWindowMultiplier)

	for chatID, requests := range rl.requests {
		var validRequests []time.Time
		for _, t := range requests {
			if t.After(cutoff) {
				validRequests = append(validRequests, t)
			}
		}

		if len(validRequests) == 0 {
			delete(rl.requests, chatID)
		} else {
			rl.requests[chatID] = validRequests
		}
	}
}

type Middleware struct {
	rateLimiter *RateLimiter
	platform    messaging.Platform
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

func NewMiddleware(rateLimit int, window time.Duration, platform messaging.Platform) *Middleware {
	ctx, cancel := context.WithCancel(context.Background())
	return &Middleware{
		rateLimiter: NewRateLimiter(rateLimit, window),
		platform:    platform,
		ctx:         ctx,
		cancel:      cancel,
	}
}

func (m *Middleware) RateLimit(handler messaging.MessageHandler) messaging.MessageHandler {
	return func(msg *messaging.IncomingMessage) error {
		if !m.rateLimiter.Allow(msg.ChatID) {
			slog.Warn("Rate limit exceeded", "chat_id", msg.ChatID)
			if m.platform != nil {
				if err := m.platform.SendMessage(msg.ChatID,
					"Rate limit exceeded. Please wait a moment before sending more messages."); err != nil {
					slog.Warn("Failed to send rate limit notification", "chat_id", msg.ChatID, "error", err)
				}
			}
			return nil
		}
		return handler(msg)
	}
}

func (m *Middleware) Logger(handler messaging.MessageHandler) messaging.MessageHandler {
	return func(msg *messaging.IncomingMessage) error {
		start := time.Now()
		err := handler(msg)
		duration := time.Since(start)

		if err != nil {
			slog.Error("Request failed", "chat_id", msg.ChatID, "duration", duration, "error", err)
		} else {
			slog.Info("Request completed", "chat_id", msg.ChatID, "duration", duration)
		}

		return err
	}
}

func (m *Middleware) StartCleanupWorker() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				m.rateLimiter.Cleanup()
			case <-m.ctx.Done():
				return
			}
		}
	}()
}

// Stop gracefully shuts down the middleware cleanup worker.
func (m *Middleware) Stop() {
	m.cancel()
	m.wg.Wait()
}
