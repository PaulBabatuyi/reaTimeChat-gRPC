package middleware

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// LimiterStore maintains per-key rate limiters and performs periodic cleanup.
type LimiterStore struct {
	mu              sync.Mutex
	limit           rate.Limit
	burst           int
	clients         map[string]*clientEntry
	cleanupInterval time.Duration
	stopCh          chan struct{}
}

type clientEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewLimiterStore creates a new store for per-key rate limiters.
// limitPerMinute controls allowed events per minute; burst is the burst capacity.
func NewLimiterStore(limitPerMinute int, burst int, cleanupInterval time.Duration) *LimiterStore {
	if limitPerMinute <= 0 {
		limitPerMinute = 60
	}
	s := &LimiterStore{
		limit:           rate.Every(time.Minute / time.Duration(limitPerMinute)),
		burst:           burst,
		clients:         map[string]*clientEntry{},
		cleanupInterval: cleanupInterval,
		stopCh:          make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

func (s *LimiterStore) cleanupLoop() {
	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cutoff := time.Now().Add(-10 * time.Minute)
			s.mu.Lock()
			for k, v := range s.clients {
				if v.lastSeen.Before(cutoff) {
					delete(s.clients, k)
				}
			}
			s.mu.Unlock()
		case <-s.stopCh:
			return
		}
	}
}

// Stop stops internal goroutines (useful for tests).
func (s *LimiterStore) Stop() {
	close(s.stopCh)
}

// getLimiter returns or creates a limiter for key
func (s *LimiterStore) getLimiter(key string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.clients[key]; ok {
		e.lastSeen = time.Now()
		return e.limiter
	}
	limiter := rate.NewLimiter(s.limit, s.burst)
	s.clients[key] = &clientEntry{limiter: limiter, lastSeen: time.Now()}
	return limiter
}

// Allow checks whether an event for the given key is permitted.
func (s *LimiterStore) Allow(key string) bool {
	l := s.getLimiter(key)
	return l.Allow()
}

// RateLimitUnaryInterceptor returns a grpc.UnaryServerInterceptor that applies
// rate limiting to the supplied methods. For Register/Login we prefer to key by
// the provided email (extracted from the request), falling back to remote IP.
func RateLimitUnaryInterceptor(store *LimiterStore, limitedMethods map[string]bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Only apply to selected methods
		if !limitedMethods[info.FullMethod] {
			return handler(ctx, req)
		}

		// Try to extract remote peer IP
		key := "unknown"
		if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
			key = p.Addr.String()
		}

		// If request implements GetEmail, prefer that as the key to protect accounts
		type emailGetter interface{ GetEmail() string }
		if eg, ok := req.(emailGetter); ok {
			if e := eg.GetEmail(); e != "" {
				key = fmt.Sprintf("email:%s", e)
			}
		}

		if !store.Allow(key) {
			return nil, status.Errorf(codes.ResourceExhausted, "rate limit exceeded")
		}

		return handler(ctx, req)
	}
}
