package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
	"webhook-api/internal/config"
	"webhook-api/internal/hub"
	"webhook-api/internal/store"
)

const (
	maxWebhookBodyBytes = 5 << 20 // 5MB
	maxConfigBodyBytes  = 1 << 16 // 64KB
	defaultLogsLimit    = 20
	maxLogsLimit        = 100
	storeTimeout        = 3 * time.Second
)

func WebhookHandler(logger *log.Logger, store *store.Store, hub *hub.Hub) http.HandlerFunc {
	return withMethods(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body (or payload too large)", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var payload interface{}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
		}

		logger.Printf("raw body: %q", string(body))
		logger.Printf("webhook received: %+v", payload)

		ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
		entry, err := store.InsertLog(ctx, r.Method, r.URL.Path, r.Host, r.RemoteAddr, r.URL.RawQuery, r.Header, string(body))
		cancel()
		if err != nil {
			logger.Printf("failed to persist webhook log: %v", err)
		} else {
			hub.Publish(entry)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "received",
			"payload": payload,
		})
	}, http.MethodPost)
}

func ConfigHandler(envPath, currentPort, currentEndpoint string, reservedRoutes ...string) http.HandlerFunc {
	return withMethods(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxConfigBodyBytes)
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]string{
				"port":             currentPort,
				"webhook_endpoint": currentEndpoint,
				"note":             "reflects the currently running configuration; changes made via POST/PUT require an application restart",
			})

		case http.MethodPost, http.MethodPut:
			var body struct {
				Port            string `json:"port"`
				WebhookEndpoint string `json:"webhook_endpoint"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}

			updates := map[string]string{}

			if body.Port != "" {
				port, err := config.ValidatePort(body.Port)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				updates["API_PORT"] = strconv.Itoa(port)
			}

			if body.WebhookEndpoint != "" {
				route := config.NormalizeRoute(body.WebhookEndpoint)
				if err := config.ValidateRoute(route); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				for _, reserved := range reservedRoutes {
					if route == reserved {
						http.Error(w, fmt.Sprintf("route must not collide with the admin route %q", reserved), http.StatusBadRequest)
						return
					}
				}
				updates["WEBHOOK_ENDPOINT"] = route
			}

			if len(updates) == 0 {
				http.Error(w, "provide at least one of: port, webhook_endpoint", http.StatusBadRequest)
				return
			}

			if err := config.UpdateEnvFile(envPath, updates); err != nil {
				http.Error(w, "failed to persist configuration", http.StatusInternalServerError)
				return
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":  "saved",
				"updated": updates,
				"note":    "restart the application for these changes to take effect",
			})
		}
	}, http.MethodGet, http.MethodPost, http.MethodPut)
}

func LogsHandler(store *store.Store) http.HandlerFunc {
	return withMethods(func(w http.ResponseWriter, r *http.Request) {
		page, err := parsePositiveIntParam(r.URL.Query().Get("page"), 1)
		if err != nil {
			http.Error(w, "invalid page parameter", http.StatusBadRequest)
			return
		}
		limit, err := parsePositiveIntParam(r.URL.Query().Get("limit"), defaultLogsLimit)
		if err != nil {
			http.Error(w, "invalid limit parameter", http.StatusBadRequest)
			return
		}
		if limit > maxLogsLimit {
			limit = maxLogsLimit
		}

		ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
		defer cancel()
		entries, total, err := store.ListLogs(ctx, page, limit)
		if err != nil {
			http.Error(w, "failed to fetch logs", http.StatusInternalServerError)
			return
		}

		totalPages := 0
		if total > 0 {
			totalPages = (total + limit - 1) / limit
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"page":        page,
			"limit":       limit,
			"total":       total,
			"total_pages": totalPages,
			"logs":        entries,
		})
	}, http.MethodGet)
}

func parsePositiveIntParam(raw string, def int) (int, error) {
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 {
		return 0, fmt.Errorf("must be a positive integer")
	}
	return v, nil
}
