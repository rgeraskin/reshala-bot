package bot

import (
	"log"
	"sync"
	"time"

	"github.com/rg/aiops/internal/messaging"
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
	cutoff := now.Add(-rl.window * 2)

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
}

func NewMiddleware(rateLimit int, window time.Duration) *Middleware {
	return &Middleware{
		rateLimiter: NewRateLimiter(rateLimit, window),
	}
}

func (m *Middleware) RateLimit(handler messaging.MessageHandler) messaging.MessageHandler {
	return func(msg *messaging.IncomingMessage) error {
		if !m.rateLimiter.Allow(msg.ChatID) {
			log.Printf("Rate limit exceeded for chat %s", msg.ChatID)
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
			log.Printf("[%s] Error after %v: %v", msg.ChatID, duration, err)
		} else {
			log.Printf("[%s] Success in %v", msg.ChatID, duration)
		}

		return err
	}
}

func (m *Middleware) StartCleanupWorker() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			m.rateLimiter.Cleanup()
		}
	}()
}
