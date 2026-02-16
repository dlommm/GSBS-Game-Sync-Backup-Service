package sse

import (
	"fmt"
	"log"
	"sync"
)

// Event is a server-sent event.
type Event struct {
	Type string // SSE event type (e.g. "manifest-updated")
	Data string // SSE data field
}

// Format returns the SSE wire format for the event.
func (e Event) Format() string {
	s := ""
	if e.Type != "" {
		s += fmt.Sprintf("event: %s\n", e.Type)
	}
	s += fmt.Sprintf("data: %s\n\n", e.Data)
	return s
}

// subscriber is one SSE client connection.
type subscriber struct {
	ch       chan Event
	clientID string
}

// Hub manages SSE client connections and broadcasts events.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[*subscriber]struct{}
	closed      bool
}

// NewHub creates a new SSE hub.
func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[*subscriber]struct{}),
	}
}

// Subscribe registers a new SSE client. Returns the event channel and an
// unsubscribe function that MUST be called when the client disconnects.
// After Shutdown, Subscribe returns an already-closed channel so callers exit quickly.
func (h *Hub) Subscribe(clientID string) (<-chan Event, func()) {
	h.mu.Lock()
	if h.closed {
		ch := make(chan Event)
		close(ch)
		h.mu.Unlock()
		return ch, func() {}
	}
	sub := &subscriber{
		ch:       make(chan Event, 16),
		clientID: clientID,
	}
	h.subscribers[sub] = struct{}{}
	h.mu.Unlock()
	log.Printf("sse: client %s subscribed (%d total)", clientID, h.Count())

	unsub := func() {
		h.mu.Lock()
		delete(h.subscribers, sub)
		h.mu.Unlock()
		log.Printf("sse: client %s unsubscribed (%d total)", clientID, h.Count())
	}
	return sub.ch, unsub
}

// Shutdown broadcasts server-shutting-down to all subscribers, then closes their channels
// so they disconnect gracefully. New Subscribe calls after Shutdown return a closed channel.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	evt := Event{Type: "server-shutting-down", Data: "{}"}
	for sub := range h.subscribers {
		select {
		case sub.ch <- evt:
		default:
		}
		close(sub.ch)
	}
	h.subscribers = make(map[*subscriber]struct{})
	h.mu.Unlock()
	log.Printf("sse: hub shut down")
}

// Broadcast sends an event to all connected subscribers.
// Non-blocking: if a subscriber's buffer is full the event is dropped for that client.
func (h *Hub) Broadcast(evt Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for sub := range h.subscribers {
		select {
		case sub.ch <- evt:
		default:
			log.Printf("sse: dropping event for slow client %s", sub.clientID)
		}
	}
}

// Count returns the number of active SSE connections.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}
