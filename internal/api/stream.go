package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
	"webhook-api/internal/hub"
	"webhook-api/internal/store"
)

const (
	sseHeartbeatInterval = 15 * time.Second
	sseCatchUpLimit      = 500
)

func LogsStreamHandler(store *store.Store, hub *hub.Hub) http.HandlerFunc {
	return withMethods(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)

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
				return

			case entry := <-ch:
				if !writeSSELogEntry(w, entry) {
					return
				}
				flusher.Flush()

			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}, http.MethodGet)
}

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

func writeSSELogEntry(w http.ResponseWriter, entry store.LogEntry) bool {
	payload, err := json.Marshal(entry)
	if err != nil {
		return true // skip a malformed entry, don't kill the whole stream
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: log\ndata: %s\n\n", entry.ID, payload)
	return err == nil
}
