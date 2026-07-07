package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type LogEntry struct {
	ID         int64           `json:"id"`
	ReceivedAt time.Time       `json:"received_at"`
	Method     string          `json:"method"`
	Path       string          `json:"path"`
	Host       string          `json:"host"`
	RemoteAddr string          `json:"remote_addr"`
	Query      string          `json:"query,omitempty"`
	Headers    json.RawMessage `json:"headers"`
	Body       string          `json:"body,omitempty"`
}

type Store struct {
	db *sql.DB
}

func openStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply pragma: %w", err)
		}
	}

	schema := `
CREATE TABLE IF NOT EXISTS webhook_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	received_at TEXT NOT NULL,
	method TEXT NOT NULL,
	path TEXT NOT NULL,
	host TEXT NOT NULL,
	remote_addr TEXT NOT NULL,
	query TEXT NOT NULL DEFAULT '',
	headers TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT ''
);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// InsertLog persists one received request and returns the entry exactly as
// stored, including the id SQLite assigned to it. The caller (the webhook
// handler) uses that returned entry to publish a real-time SSE event, so the
// id in the stream always matches the id a client would see via
// GET /admin/logs or a Last-Event-ID catch-up query.
func (s *Store) InsertLog(ctx context.Context, method, path, host, remoteAddr, query string, headers map[string][]string, body string) (LogEntry, error) {
	headersJSON, err := json.Marshal(headers)
	if err != nil {
		return LogEntry{}, err
	}

	receivedAt := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_logs (received_at, method, path, host, remote_addr, query, headers, body)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		receivedAt.Format(time.RFC3339Nano), method, path, host, remoteAddr, query, string(headersJSON), body,
	)
	if err != nil {
		return LogEntry{}, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return LogEntry{}, err
	}

	return LogEntry{
		ID:         id,
		ReceivedAt: receivedAt,
		Method:     method,
		Path:       path,
		Host:       host,
		RemoteAddr: remoteAddr,
		Query:      query,
		Headers:    json.RawMessage(headersJSON),
		Body:       body,
	}, nil
}

// ListLogsSince returns entries with id greater than sinceID, oldest first.
// It backs the SSE stream's reconnect/catch-up path: a client that comes
// back after a dropped connection sends the last id it saw (via the
// Last-Event-ID mechanism) and gets exactly what it missed, instead of
// silently losing events or re-reading the whole history.
func (s *Store) ListLogsSince(ctx context.Context, sinceID int64, limit int) ([]LogEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, received_at, method, path, host, remote_addr, query, headers, body
		 FROM webhook_logs WHERE id > ? ORDER BY id ASC LIMIT ?`, sinceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []LogEntry{}
	for rows.Next() {
		var e LogEntry
		var receivedAt, headersRaw string
		if err := rows.Scan(&e.ID, &receivedAt, &e.Method, &e.Path, &e.Host, &e.RemoteAddr, &e.Query, &headersRaw, &e.Body); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, receivedAt); err == nil {
			e.ReceivedAt = t
		}
		e.Headers = json.RawMessage(headersRaw)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Store) ListLogs(ctx context.Context, page, limit int) ([]LogEntry, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM webhook_logs").Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, received_at, method, path, host, remote_addr, query, headers, body
		 FROM webhook_logs ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries := []LogEntry{}
	for rows.Next() {
		var e LogEntry
		var receivedAt, headersRaw string
		if err := rows.Scan(&e.ID, &receivedAt, &e.Method, &e.Path, &e.Host, &e.RemoteAddr, &e.Query, &headersRaw, &e.Body); err != nil {
			return nil, 0, err
		}
		if t, err := time.Parse(time.RFC3339Nano, receivedAt); err == nil {
			e.ReceivedAt = t
		}
		e.Headers = json.RawMessage(headersRaw)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}
