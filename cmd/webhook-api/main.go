package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"webhook-api/internal/api"
	"webhook-api/internal/config"
	"webhook-api/internal/hub"
	"webhook-api/internal/store"
)

const (
	envFilePath = ".env"
	dataDir     = "data"
)

var dbFilePath = filepath.Join(dataDir, "webhook.db")

func main() {
	endpoint := config.NormalizeRoute(config.EnvOrDefault("WEBHOOK_ENDPOINT", "/webhook"))
	if err := config.ValidateRoute(endpoint); err != nil {
		log.Fatalf("invalid WEBHOOK_ENDPOINT: %v", err)
	}

	adminBase := config.NormalizeRoute(config.EnvOrDefault("ADMIN_ROUTE", "/admin"))
	if err := config.ValidateRoute(adminBase); err != nil {
		log.Fatalf("invalid ADMIN_ROUTE: %v", err)
	}
	configRoute := adminBase + "/config"
	logsRoute := adminBase + "/logs"
	streamRoute := adminBase + "/logs/stream"

	if endpoint == configRoute || endpoint == logsRoute || endpoint == streamRoute {
		log.Fatalf("WEBHOOK_ENDPOINT must not collide with the admin routes (%s, %s, %s)", configRoute, logsRoute, streamRoute)
	}

	port := config.EnvOrDefault("API_PORT", "8080")
	if _, err := config.ValidatePort(port); err != nil {
		log.Fatalf("invalid API_PORT: %v", err)
	}

	logInBash := config.EnvBool("LOG_IN_BASH", true)

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("failed to create data directory %q: %v", dataDir, err)
	}

	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		log.Fatal("ADMIN_TOKEN is required but not set - define it in .env before starting the server")
	}

	startupLogger := log.New(os.Stdout, "", log.LstdFlags)

	webhookOut := io.Discard
	if logInBash {
		webhookOut = os.Stdout
	}
	webhookLogger := log.New(webhookOut, "", log.LstdFlags)

	store, err := store.OpenStore(dbFilePath)
	if err != nil {
		log.Fatalf("failed to open log database: %v", err)
	}
	defer store.Close()

	hub := hub.NewHub()

	mux := http.NewServeMux()
	mux.HandleFunc(endpoint, api.WebhookHandler(webhookLogger, store, hub))
	mux.HandleFunc(configRoute, api.WithAdminAuth(adminToken, api.ConfigHandler(envFilePath, port, endpoint, configRoute, logsRoute, streamRoute)))
	mux.HandleFunc(logsRoute, api.WithAdminAuth(adminToken, api.LogsHandler(store)))
	mux.HandleFunc(streamRoute, api.WithAdminAuth(adminToken, api.LogsStreamHandler(store, hub)))

	handler := api.SecurityHeaders(mux)

	addr := ":" + port
	startupLogger.Printf("listening on %s (webhook endpoint: %s)", addr, endpoint)
	startupLogger.Printf("admin config endpoint: %s | admin logs endpoint: %s | admin logs stream: %s", configRoute, logsRoute, streamRoute)

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}
