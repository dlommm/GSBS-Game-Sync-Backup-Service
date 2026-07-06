package sse

import (
	"fmt"
	"sync"

	"github.com/gsbs/gsbs/server/logx"
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
	userID   string
	// seq orders subscriptions for cap eviction. A counter (not a
	// timestamp): wall-clock resolution is coarse enough on some
	// platforms that back-to-back subscribes tie, making "oldest"
	// nondeterministic.
	seq uint64
}

// Hub manages SSE client connections and broadcasts events.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[*subscriber]struct{}
	nextSeq     uint64
	closed      bool
}

// NewHub creates a new SSE hub.
func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[*subscriber]struct{}),
	}
}

// Subscribe registers a new SSE client for userID. Returns the event channel and unsubscribe func.
func (h *Hub) Subscribe(userID string) (<-chan Event, func()) {
	return h.SubscribeCapped(userID, 0)
}

// SubscribeCapped registers a new SSE client for userID. If the user already has maxPerUser
// active connections (maxPerUser > 0), the oldest one is evicted before adding the new one.
// Returns the event channel and unsubscribe func.
func (h *Hub) SubscribeCapped(userID string, maxPerUser int) (<-chan Event, func()) {
	h.mu.Lock()
	if h.closed {
		ch := make(chan Event)
		close(ch)
		h.mu.Unlock()
		return ch, func() {}
	}

	if maxPerUser > 0 {
		// Collect all subscriptions for this user.
		var userSubs []*subscriber
		for sub := range h.subscribers {
			if sub.userID == userID {
				userSubs = append(userSubs, sub)
			}
		}
		// Evict oldest until we are below the cap.
		for len(userSubs) >= maxPerUser {
			oldest := userSubs[0]
			for _, s := range userSubs[1:] {
				if s.seq < oldest.seq {
					oldest = s
				}
			}
			delete(h.subscribers, oldest)
			close(oldest.ch)
			// Remove evicted entry from the local slice.
			for i, s := range userSubs {
				if s == oldest {
					userSubs = append(userSubs[:i], userSubs[i+1:]...)
					break
				}
			}
			logx.Logger().Warn().Str("component", "sse").Str("user_id", userID).Int("cap", maxPerUser).
				Msg("sse: evicted oldest connection for user (cap)")
		}
	}

	h.nextSeq++
	sub := &subscriber{
		ch:       make(chan Event, 16),
		clientID: userID,
		userID:   userID,
		seq:      h.nextSeq,
	}
	h.subscribers[sub] = struct{}{}
	h.mu.Unlock()
	logx.Logger().Info().Str("component", "sse").Str("user_id", userID).Int("total", h.Count()).
		Msg("sse: client subscribed")

	unsub := func() {
		h.mu.Lock()
		delete(h.subscribers, sub)
		h.mu.Unlock()
		logx.Logger().Info().Str("component", "sse").Str("user_id", userID).Int("total", h.Count()).
			Msg("sse: client unsubscribed")
	}
	return sub.ch, unsub
}

// BroadcastToUser sends an event to subscribers for the given user only.
func (h *Hub) BroadcastToUser(userID string, evt Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for sub := range h.subscribers {
		if sub.userID != userID {
			continue
		}
		select {
		case sub.ch <- evt:
		default:
			logx.Logger().Warn().Str("component", "sse").Str("client_id", sub.clientID).
				Msg("sse: dropping event for slow client")
		}
	}
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
	logx.Logger().Info().Str("component", "sse").Msg("sse: hub shut down")
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
			logx.Logger().Warn().Str("component", "sse").Str("client_id", sub.clientID).
				Msg("sse: dropping event for slow client")
		}
	}
}

// Count returns the number of active SSE connections.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}
