package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var routePattern = regexp.MustCompile(`^/[A-Za-z0-9/_-]*$`)

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func normalizeRoute(route string) string {
	route = strings.TrimSpace(route)
	if !strings.HasPrefix(route, "/") {
		route = "/" + route
	}
	return route
}

// validateRoute rejects anything that isn't a plain, absolute path made of
// safe characters, guarding against path traversal and injection attempts.
func validateRoute(route string) error {
	if len(route) < 2 || len(route) > 200 {
		return fmt.Errorf("route must be between 2 and 200 characters")
	}
	if strings.Contains(route, "..") || strings.Contains(route, "//") {
		return fmt.Errorf("route must not contain '..' or '//'")
	}
	if !routePattern.MatchString(route) {
		return fmt.Errorf("route may only contain letters, numbers, '-', '_' and '/'")
	}
	return nil
}

func validatePort(raw string) (int, error) {
	p, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("port must be a number")
	}
	if p < 1 || p > 65535 {
		return 0, fmt.Errorf("port must be between 1 and 65535")
	}
	return p, nil
}

// updateEnvFile rewrites (or appends) key=value pairs in the given .env
// file, preserving existing lines, comments and ordering. Written
// atomically so a crash mid-write can't corrupt the file.
func updateEnvFile(path string, updates map[string]string) error {
	var lines []string
	if f, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		closeErr := f.Close()
		if err := scanner.Err(); err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	remaining := make(map[string]string, len(updates))
	for k, v := range updates {
		remaining[k] = v
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		eq := strings.Index(trimmed, "=")
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eq])
		if v, ok := remaining[key]; ok {
			lines[i] = key + "=" + v
			delete(remaining, key)
		}
	}

	for k, v := range remaining {
		lines = append(lines, k+"="+v)
	}

	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
