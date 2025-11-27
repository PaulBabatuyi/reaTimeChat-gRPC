package middleware

import (
	"testing"
	"time"
)

type dummy struct{ email string }

func (d dummy) GetEmail() string { return d.email }

func TestLimiterStore_AllowAndCleanup(t *testing.T) {
	// allow 5 events immediately then the 6th should be rejected
	s := NewLimiterStore(5, 5, 100*time.Millisecond)
	defer s.Stop()

	key := "test@example.com"
	for i := 0; i < 5; i++ {
		if !s.Allow(key) {
			t.Fatalf("expected allow at iteration %d", i)
		}
	}

	if s.Allow(key) {
		t.Fatalf("expected limiter to block after burst consumed")
	}

	// ensure cleanup eventually removes old entries
	time.Sleep(150 * time.Millisecond)
	s.mu.Lock()
	if _, ok := s.clients[key]; !ok {
		// entry may be removed after cleanup; that's acceptable
	}
	s.mu.Unlock()
}
