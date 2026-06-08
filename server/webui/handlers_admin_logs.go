package webui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/gsbs/gsbs/server/logx"
)

const (
	defaultAdminLogLimit      = 200
	defaultAdminRefreshSecond = 5
	maxAdminLogLimit          = 1000
	maxLogReadBytes           = 4 << 20
)

func (h *WebHandler) serveAdminLogs(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	level, query, limit, autoRefresh, refreshSeconds := parseAdminLogQuery(r)
	sourcePath, sourceInfo, sourcePresent := resolveAdminLogSource()
	entries := []adminLogEntry{}
	if sourcePresent {
		loaded, err := loadAdminLogEntries(sourcePath, level, query, limit)
		if err != nil {
			sourceInfo = fmt.Sprintf("Failed to read log source: %v", err)
		} else {
			entries = loaded
		}
	}

	h.render(w, "admin_logs.html", adminLogsData{
		PageData:      h.adminPageData(w, r, userID, username, "logs", "admin_logs"),
		Entries:       entries,
		LogSourcePath: sourcePath, LogSourceInfo: sourceInfo, LogSourcePresent: sourcePresent,
		Level: level, Query: query, Limit: limit, AutoRefresh: autoRefresh, RefreshSeconds: refreshSeconds,
	})
}

func (h *WebHandler) serveAdminLogsPartial(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	level, query, limit, _, _ := parseAdminLogQuery(r)
	sourcePath, sourceInfo, sourcePresent := resolveAdminLogSource()
	entries := []adminLogEntry{}
	if sourcePresent {
		loaded, err := loadAdminLogEntries(sourcePath, level, query, limit)
		if err != nil {
			sourceInfo = fmt.Sprintf("Failed to read log source: %v", err)
		} else {
			entries = loaded
		}
	}
	h.renderPartial(w, "partials/admin_logs_table.html", map[string]interface{}{
		"Entries":       entries,
		"LogSourcePath": sourcePath, "LogSourceInfo": sourceInfo, "LogSourcePresent": sourcePresent,
		"Level": level, "Query": query, "Limit": limit,
	})
}

func parseAdminLogQuery(r *http.Request) (level, query string, limit int, autoRefresh bool, refreshSeconds int) {
	level = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("level")))
	switch level {
	case "", "all", "debug", "info", "warn", "error":
		if level == "" {
			level = "all"
		}
	default:
		level = "all"
	}
	query = strings.TrimSpace(r.URL.Query().Get("q"))
	limit = defaultAdminLogLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			if n < 1 {
				n = 1
			}
			if n > maxAdminLogLimit {
				n = maxAdminLogLimit
			}
			limit = n
		}
	}
	autoRefresh = r.URL.Query().Get("auto") == "1"
	refreshSeconds = defaultAdminRefreshSecond
	if raw := strings.TrimSpace(r.URL.Query().Get("refresh")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			if n < 2 {
				n = 2
			}
			if n > 30 {
				n = 30
			}
			refreshSeconds = n
		}
	}
	return level, query, limit, autoRefresh, refreshSeconds
}

func resolveAdminLogSource() (path, info string, present bool) {
	return resolveAdminLogSourceFor(runtime.GOOS, os.Getenv, os.Stat)
}

type adminLogSourceCandidate struct {
	path   string
	source string
}

func resolveAdminLogSourceFor(goos string, getenv func(string) string, statFn func(string) (os.FileInfo, error)) (path, info string, present bool) {
	candidates := adminLogSourceCandidates(goos, getenv)
	attempted := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		attempted = append(attempted, candidate.path)
		if fi, err := statFn(candidate.path); err == nil && !fi.IsDir() {
			return candidate.path, fmt.Sprintf("Using %s.", candidate.source), true
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Sprintf("No log file source configured. Set %s (preferred) or %s to write server logs to a file; this runtime is likely stdout-only.", logx.ServiceLogPathEnv, logx.LegacyLogFileEnv), false
	}
	primary := candidates[0].path
	return primary, fmt.Sprintf("No log file found yet. Attempted: %s. Set %s (preferred) or %s to a writable file path and restart if needed.", strings.Join(attempted, ", "), logx.ServiceLogPathEnv, logx.LegacyLogFileEnv), false
}

func adminLogSourceCandidates(goos string, getenv func(string) string) []adminLogSourceCandidate {
	candidates := make([]adminLogSourceCandidate, 0, 3)
	seen := make(map[string]struct{}, 3)
	add := func(path, source string) {
		clean := filepath.Clean(strings.TrimSpace(path))
		if clean == "" || clean == "." {
			return
		}
		if _, ok := seen[clean]; ok {
			return
		}
		seen[clean] = struct{}{}
		candidates = append(candidates, adminLogSourceCandidate{path: clean, source: source})
	}
	if custom := strings.TrimSpace(getenv(logx.ServiceLogPathEnv)); custom != "" {
		add(custom, logx.ServiceLogPathEnv)
	}
	if legacy := strings.TrimSpace(getenv(logx.LegacyLogFileEnv)); legacy != "" {
		add(legacy, logx.LegacyLogFileEnv)
	}
	if goos == "windows" {
		base := strings.TrimSpace(getenv("ProgramData"))
		if base == "" {
			base = `C:\ProgramData`
		}
		add(filepath.Join(base, "GSBS", "logs", "server.log"), "default Windows service log path")
	}
	return candidates
}

func loadAdminLogEntries(path, level, query string, limit int) ([]adminLogEntry, error) {
	lines, err := readRecentLines(path, maxLogReadBytes)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]adminLogEntry, 0, limit)
	for i := len(lines) - 1; i >= 0 && len(out) < limit; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		entry := parseAdminLogLine(line)
		if level != "all" && entry.Level != level {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(entry.Raw), q) {
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

func readRecentLines(path string, maxBytes int64) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	start := int64(0)
	if size > maxBytes {
		start = size - maxBytes
	}
	if _, err := f.Seek(start, 0); err != nil {
		return nil, err
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.ReplaceAll(string(buf), "\r\n", "\n"), "\n"), nil
}

func parseAdminLogLine(line string) adminLogEntry {
	entry := adminLogEntry{
		Level:   "raw",
		Message: line,
		Raw:     line,
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return entry
	}
	if ts, ok := payload["time"].(string); ok {
		entry.Timestamp = ts
	}
	if msg, ok := payload["message"].(string); ok && strings.TrimSpace(msg) != "" {
		entry.Message = msg
	}
	if lvl, ok := payload["level"].(string); ok {
		switch strings.ToLower(strings.TrimSpace(lvl)) {
		case "warning":
			entry.Level = "warn"
		case "debug", "info", "warn", "error":
			entry.Level = strings.ToLower(strings.TrimSpace(lvl))
		default:
			entry.Level = strings.ToLower(strings.TrimSpace(lvl))
		}
	}
	return entry
}
