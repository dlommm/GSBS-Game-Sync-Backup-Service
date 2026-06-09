package webui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gsbs/gsbs/pkg/logview"
	"github.com/gsbs/gsbs/server/logx"
)

func (h *WebHandler) serveAdminLogs(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	q := logview.ParseQuery(r)
	sourcePath, sourceInfo, sourcePresent := resolveAdminLogSource()
	entries := []logview.Entry{}
	if sourcePresent {
		loaded, err := loadAdminLogEntries(sourcePath, q)
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
		Query: q,
	})
}

func (h *WebHandler) serveAdminLogsPartial(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	q := logview.ParseQuery(r)
	sourcePath, sourceInfo, sourcePresent := resolveAdminLogSource()
	entries := []logview.Entry{}
	if sourcePresent {
		loaded, err := loadAdminLogEntries(sourcePath, q)
		if err != nil {
			sourceInfo = fmt.Sprintf("Failed to read log source: %v", err)
		} else {
			entries = loaded
		}
	}
	h.renderPartial(w, "partials/admin_logs_table.html", map[string]interface{}{
		"Entries":          entries,
		"LogSourcePath":    sourcePath,
		"LogSourceInfo":    sourceInfo,
		"LogSourcePresent": sourcePresent,
		"Query":            q,
	})
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

func loadAdminLogEntries(path string, q logview.Query) ([]logview.Entry, error) {
	return logview.LoadEntries(path, q, logview.ParseZerologLine)
}
