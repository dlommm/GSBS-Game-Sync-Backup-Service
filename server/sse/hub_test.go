package sse

import (
	"testing"
	"time"
)

func recvOne(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case evt := <-ch:
		return evt
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

func TestBroadcastToUserIsolation(t *testing.T) {
	h := NewHub()
	chA, unsubA := h.Subscribe("user-a")
	chB, unsubB := h.Subscribe("user-b")
	defer unsubA()
	defer unsubB()

	h.BroadcastToUser("user-a", Event{Type: "push", Data: "a-only"})

	if evt := recvOne(t, chA); evt.Data != "a-only" {
		t.Fatalf("user-a got %+v", evt)
	}
	select {
	case evt := <-chB:
		t.Fatalf("user-b must not receive user-a's event, got %+v", evt)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBroadcastReachesAll(t *testing.T) {
	h := NewHub()
	chA, unsubA := h.Subscribe("user-a")
	chB, unsubB := h.Subscribe("user-b")
	defer unsubA()
	defer unsubB()

	h.Broadcast(Event{Type: "manifest-updated", Data: "{}"})
	if evt := recvOne(t, chA); evt.Type != "manifest-updated" {
		t.Fatalf("user-a got %+v", evt)
	}
	if evt := recvOne(t, chB); evt.Type != "manifest-updated" {
		t.Fatalf("user-b got %+v", evt)
	}
}

func TestSubscribeCappedEvictsOldest(t *testing.T) {
	h := NewHub()
	oldest, _ := h.SubscribeCapped("user-a", 2)
	_, unsub2 := h.SubscribeCapped("user-a", 2)
	defer unsub2()

	// Third connection exceeds the cap of 2: the oldest channel is closed.
	_, unsub3 := h.SubscribeCapped("user-a", 2)
	defer unsub3()

	select {
	case _, ok := <-oldest:
		if ok {
			t.Fatal("expected oldest channel to be closed, got an event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("oldest channel was not closed on eviction")
	}
	if got := h.Count(); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
}

func TestShutdown(t *testing.T) {
	h := NewHub()
	ch, _ := h.Subscribe("user-a")

	h.Shutdown()

	// Subscribers receive the shutdown event, then the channel closes.
	if evt := recvOne(t, ch); evt.Type != "server-shutting-down" {
		t.Fatalf("got %+v, want server-shutting-down", evt)
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after shutdown")
	}
	if h.Count() != 0 {
		t.Fatalf("count = %d after shutdown, want 0", h.Count())
	}

	// Subscribing after shutdown returns an already-closed channel.
	late, _ := h.Subscribe("user-b")
	if _, ok := <-late; ok {
		t.Fatal("post-shutdown subscribe should return a closed channel")
	}

	// Second shutdown is a no-op, not a panic.
	h.Shutdown()
}

func TestSlowClientDoesNotBlockBroadcast(t *testing.T) {
	h := NewHub()
	_, unsub := h.Subscribe("user-a") // never drained
	defer unsub()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ { // buffer is 16; the rest must be dropped, not block
			h.Broadcast(Event{Type: "tick"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("broadcast blocked on a slow client")
	}
}
