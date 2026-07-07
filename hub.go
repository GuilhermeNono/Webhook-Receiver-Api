package main

import "sync"

// Hub fans newly inserted log entries out to every connected SSE client. It
// exists so webhookHandler doesn't need to know whether anyone is watching
// /admin/logs/stream, or how many - it just calls publish once per request.
type Hub struct {
	mu          sync.Mutex
	subscribers map[chan LogEntry]struct{}
}

func newHub() *Hub {
	return &Hub{subscribers: make(map[chan LogEntry]struct{})}
}

// subscribe registers a new listener. The channel is buffered so a brief
// stall on the client's side (slow network write) doesn't make publish
// block and delay the webhook response that triggered it.
func (h *Hub) subscribe() chan LogEntry {
	ch := make(chan LogEntry, 16)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) unsubscribe(ch chan LogEntry) {
	h.mu.Lock()
	delete(h.subscribers, ch)
	h.mu.Unlock()
	close(ch)
}

// publish delivers entry to every current subscriber without blocking: if a
// subscriber's buffer is full (it's not reading fast enough), that one event
// is dropped for that subscriber rather than stalling every other request.
// A client that misses events this way still recovers them on its next
// reconnect via ListLogsSince.
func (h *Hub) publish(entry LogEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subscribers {
		select {
		case ch <- entry:
		default:
		}
	}
}
