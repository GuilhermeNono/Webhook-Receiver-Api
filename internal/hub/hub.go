package hub

import (
	"sync"
	"webhook-api/internal/store"
)

type Hub struct {
	mu          sync.Mutex
	subscribers map[chan store.LogEntry]struct{}
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[chan store.LogEntry]struct{})}
}

func (h *Hub) Subscribe() chan store.LogEntry {
	ch := make(chan store.LogEntry, 16)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan store.LogEntry) {
	h.mu.Lock()
	delete(h.subscribers, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *Hub) Publish(entry store.LogEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subscribers {
		select {
		case ch <- entry:
		default:
		}
	}
}
