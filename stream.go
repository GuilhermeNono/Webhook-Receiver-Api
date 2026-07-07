package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	sseHeartbeatInterval = 15 * time.Second
	sseCatchUpLimit      = 500
)

// logsStreamHandler serves Server-Sent Events for newly received webhooks.
// Unlike GET /admin/logs, the client doesn't poll: it opens one long-lived
// HTTP response and the server pushes each new log the moment it's
// persisted, via the shared Hub.
func logsStreamHandler(store *Store, hub *Hub) http.HandlerFunc {
	return withMethods(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Connection", "keep-alive")
		// Some reverse proxies (nginx-based ones especially) buffer
		// responses by default, which would hold every event until the
		// buffer fills instead of streaming them as they arrive.
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		// Subscribe before replaying history: otherwise an entry inserted
		// between the catch-up query and the subscription would never
		// reach this client.
		ch := hub.subscribe()
		defer hub.unsubscribe(ch)

		if sinceID, ok := parseLastEventID(r); ok {
			ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
			missed, err := store.ListLogsSince(ctx, sinceID, sseCatchUpLimit)
			cancel()
			if err == nil {
				for _, entry := range missed {
					if !writeSSELogEntry(w, entry) {
						return
					}
				}
				flusher.Flush()
			}
		}

		heartbeat := time.NewTicker(sseHeartbeatInterval)
		defer heartbeat.Stop()

		for {
			select {
			case <-r.Context().Done():
				// Client disconnected (tab closed, network dropped, etc).
				return

			case entry := <-ch:
				if !writeSSELogEntry(w, entry) {
					return
				}
				flusher.Flush()

			case <-heartbeat.C:
				// A ":"-prefixed line is an SSE comment: it's ignored by
				// EventSource but keeps the connection from looking idle
				// to proxies that time out quiet connections.
				if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}, http.MethodGet)
}

// parseLastEventID reads the id the browser's EventSource sent back
// automatically on reconnect. A plain curl/test client can achieve the same
// thing with ?last_event_id=<id>, since it has no built-in reconnect logic.
func parseLastEventID(r *http.Request) (int64, bool) {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("last_event_id")
	}
	if raw == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// writeSSELogEntry writes one entry as a single SSE "log" event. It returns
// false if the write failed (client gone), signalling the caller to stop.
func writeSSELogEntry(w http.ResponseWriter, entry LogEntry) bool {
	payload, err := json.Marshal(entry)
	if err != nil {
		return true // skip a malformed entry, don't kill the whole stream
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: log\ndata: %s\n\n", entry.ID, payload)
	return err == nil
}
