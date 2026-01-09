package bot

import (
	"testing"
	"time"

	"github.com/rg/aiops/internal/messaging"
)

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10, time.Minute)

	if rl == nil {
		t.Fatal("Expected non-nil rate limiter")
	}
	if rl.limit != 10 {
		t.Errorf("limit = %d, want 10", rl.limit)
	}
	if rl.window != time.Minute {
		t.Errorf("window = %v, want 1m", rl.window)
	}
}

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)

	// First 3 requests should be allowed
	for i := 0; i < 3; i++ {
		if !rl.Allow("chat1") {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	if rl.Allow("chat1") {
		t.Error("4th request should be denied")
	}
}

func TestRateLimiter_Allow_DifferentChats(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)

	// Each chat has its own limit
	if !rl.Allow("chat1") {
		t.Error("chat1 request 1 should be allowed")
	}
	if !rl.Allow("chat1") {
		t.Error("chat1 request 2 should be allowed")
	}
	if rl.Allow("chat1") {
		t.Error("chat1 request 3 should be denied")
	}

	// chat2 should have its own quota
	if !rl.Allow("chat2") {
		t.Error("chat2 request 1 should be allowed")
	}
	if !rl.Allow("chat2") {
		t.Error("chat2 request 2 should be allowed")
	}
}

func TestRateLimiter_Allow_WindowExpiry(t *testing.T) {
	// Use a very short window for testing
	rl := NewRateLimiter(2, 50*time.Millisecond)

	// Use up the quota
	rl.Allow("chat1")
	rl.Allow("chat1")

	// Should be denied
	if rl.Allow("chat1") {
		t.Error("Should be denied when quota exhausted")
	}

	// Wait for window to pass
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again
	if !rl.Allow("chat1") {
		t.Error("Should be allowed after window expires")
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := NewRateLimiter(10, 50*time.Millisecond)

	// Add some requests
	rl.Allow("chat1")
	rl.Allow("chat2")

	// Wait for window to pass (cleanup removes entries older than 2x window)
	time.Sleep(120 * time.Millisecond)

	// Trigger cleanup
	rl.Cleanup()

	// Internal state should be cleared
	rl.mu.Lock()
	chat1Len := len(rl.requests["chat1"])
	chat2Len := len(rl.requests["chat2"])
	rl.mu.Unlock()

	if chat1Len != 0 || chat2Len != 0 {
		t.Errorf("Expected empty request slices after cleanup, got chat1=%d, chat2=%d", chat1Len, chat2Len)
	}
}

func TestNewMiddleware(t *testing.T) {
	m := NewMiddleware(10, time.Minute, nil)
	defer m.Stop()

	if m == nil {
		t.Fatal("Expected non-nil middleware")
	}
	if m.rateLimiter == nil {
		t.Error("Expected non-nil rate limiter")
	}
}

func TestMiddleware_RateLimit(t *testing.T) {
	m := NewMiddleware(2, time.Minute, nil)
	defer m.Stop()

	callCount := 0
	innerHandler := func(msg *messaging.IncomingMessage) error {
		callCount++
		return nil
	}

	wrappedHandler := m.RateLimit(innerHandler)

	msg := &messaging.IncomingMessage{ChatID: "test-chat"}

	// First 2 calls should go through
	wrappedHandler(msg)
	wrappedHandler(msg)
	if callCount != 2 {
		t.Errorf("Expected 2 calls, got %d", callCount)
	}

	// 3rd call should be rate limited
	wrappedHandler(msg)
	if callCount != 2 {
		t.Errorf("Expected still 2 calls after rate limiting, got %d", callCount)
	}
}

func TestMiddleware_Logger(t *testing.T) {
	m := NewMiddleware(10, time.Minute, nil)
	defer m.Stop()

	callCount := 0
	innerHandler := func(msg *messaging.IncomingMessage) error {
		callCount++
		return nil
	}

	wrappedHandler := m.Logger(innerHandler)

	msg := &messaging.IncomingMessage{ChatID: "test-chat"}

	wrappedHandler(msg)

	if callCount != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}

func TestMiddleware_CleanupWorker(t *testing.T) {
	m := NewMiddleware(10, time.Minute, nil)

	// Start cleanup worker
	m.StartCleanupWorker()

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Stop should not hang
	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Error("Stop() should not hang")
	}
}
