package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const (
	envFilePath = ".env"
	// dataDir holds the SQLite database the app writes at runtime. Docker
	// Compose bind-mounts this whole directory instead of the db file
	// directly: mounting a directory that doesn't exist yet on the host is
	// unambiguous (Docker just creates a directory, which is what's
	// wanted), whereas mounting a single file path that doesn't exist yet
	// makes Docker create a directory there instead of a file - which then
	// breaks sql.Open inside the container.
	dataDir = "data"
)

var dbFilePath = filepath.Join(dataDir, "webhook.db")

func main() {
	endpoint := normalizeRoute(envOrDefault("WEBHOOK_ENDPOINT", "/webhook"))
	if err := validateRoute(endpoint); err != nil {
		log.Fatalf("invalid WEBHOOK_ENDPOINT: %v", err)
	}

	adminBase := normalizeRoute(envOrDefault("ADMIN_ROUTE", "/admin"))
	if err := validateRoute(adminBase); err != nil {
		log.Fatalf("invalid ADMIN_ROUTE: %v", err)
	}
	configRoute := adminBase + "/config"
	logsRoute := adminBase + "/logs"
	streamRoute := adminBase + "/logs/stream"

	if endpoint == configRoute || endpoint == logsRoute || endpoint == streamRoute {
		log.Fatalf("WEBHOOK_ENDPOINT must not collide with the admin routes (%s, %s, %s)", configRoute, logsRoute, streamRoute)
	}

	port := envOrDefault("API_PORT", "8080")
	if _, err := validatePort(port); err != nil {
		log.Fatalf("invalid API_PORT: %v", err)
	}

	logInBash := envBool("LOG_IN_BASH", true)

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("failed to create data directory %q: %v", dataDir, err)
	}

	adminToken := os.Getenv("ADMIN_TOKEN")
	generatedToken := false
	if adminToken == "" {
		token, err := generateToken()
		if err != nil {
			log.Fatalf("failed to generate admin token: %v", err)
		}
		adminToken = token
		generatedToken = true
	}

	// Startup messages always go to stdout (picked up by `docker logs`).
	// Every webhook request is already durably persisted in SQLite
	// regardless of this setting; LOG_IN_BASH only controls whether it's
	// *also* echoed to the terminal for live tailing.
	startupLogger := log.New(os.Stdout, "", log.LstdFlags)

	webhookOut := io.Discard
	if logInBash {
		webhookOut = os.Stdout
	}
	webhookLogger := log.New(webhookOut, "", log.LstdFlags)

	store, err := openStore(dbFilePath)
	if err != nil {
		log.Fatalf("failed to open log database: %v", err)
	}
	defer store.Close()

	hub := newHub()

	mux := http.NewServeMux()
	mux.HandleFunc(endpoint, webhookHandler(webhookLogger, store, hub))
	mux.HandleFunc(configRoute, withAdminAuth(adminToken, configHandler(envFilePath, port, endpoint, configRoute, logsRoute, streamRoute)))
	mux.HandleFunc(logsRoute, withAdminAuth(adminToken, logsHandler(store)))
	mux.HandleFunc(streamRoute, withAdminAuth(adminToken, logsStreamHandler(store, hub)))

	handler := securityHeaders(mux)

	addr := ":" + port
	startupLogger.Printf("listening on %s (webhook endpoint: %s)", addr, endpoint)
	startupLogger.Printf("admin config endpoint: %s | admin logs endpoint: %s | admin logs stream: %s", configRoute, logsRoute, streamRoute)
	if generatedToken {
		startupLogger.Printf("no ADMIN_TOKEN set - generated a random token for this run: %s", adminToken)
		startupLogger.Printf("set ADMIN_TOKEN in .env to keep it stable across restarts")
	}

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}
