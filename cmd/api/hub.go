package main

import (
	"fmt"
	"sync"

	v1 "github.com/PaulBabatuyi/reaTimeChat-gRPC/proto/chat/v1"
)

// StreamSender defines the minimal interface the hub needs from a stream: the ability
// to send ChatStreamResponse messages to the connected client.
type StreamSender interface {
	Send(*v1.ChatStreamResponse) error
}

// ConnectionHub manages active chat streams for connected users.
// It maps user email addresses to one or more active stream connections so the
// server can push messages to all currently-connected endpoints for a user.
type ConnectionHub struct {
	mu      sync.RWMutex
	streams map[string]map[int64]StreamSender
	nextID  int64
}

// NewConnectionHub creates a new hub instance.
func NewConnectionHub() *ConnectionHub {
	return &ConnectionHub{streams: make(map[string]map[int64]StreamSender)}
}

// Register registers a stream for the given email and returns a connection id which
// should be used later to unregister the stream when it closes.
func (h *ConnectionHub) Register(email string, s StreamSender) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.streams[email]; !ok {
		h.streams[email] = make(map[int64]StreamSender)
	}

	h.nextID++
	id := h.nextID
	h.streams[email][id] = s
	return id
}

// Unregister removes a previously-registered stream for the given user/email.
func (h *ConnectionHub) Unregister(email string, id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if conns, ok := h.streams[email]; ok {
		delete(conns, id)
		if len(conns) == 0 {
			delete(h.streams, email)
		}
	}
}

// SendToUser attempts to send the provided response to all currently-connected
// streams for the given email. If the user is not connected, returns an error.
// The hub does best-effort delivery: it tries to send to all streams and returns
// the first error encountered (if any).
func (h *ConnectionHub) SendToUser(email string, resp *v1.ChatStreamResponse) error {
	h.mu.RLock()
	conns, ok := h.streams[email]
	h.mu.RUnlock()

	if !ok || len(conns) == 0 {
		return fmt.Errorf("user %s not connected", email)
	}

	var firstErr error
	// Track connection ids which failed so we can unregister them and avoid
	// keeping stale/broken streams in the hub.
	var failedIDs []int64

	// Send to each active connection. If one fails, capture the error but keep
	// trying the others so we attempt best-effort delivery to all endpoints.
	for id, st := range conns {
		if err := st.Send(resp); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failedIDs = append(failedIDs, id)
		}
	}

	// Unregister any connections that failed to receive the message. It's
	// safe to call Unregister concurrently; it will lock the hub for writes.
	for _, id := range failedIDs {
		h.Unregister(email, id)
	}

	return firstErr
}
