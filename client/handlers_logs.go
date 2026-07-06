package main

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"time"

	clientwebui "github.com/gsbs/gsbs/client/webui"
	"github.com/gsbs/gsbs/pkg/logview"
)

func handleLogsPage(w http.ResponseWriter, r *http.Request) {
	q := logview.ParseQuery(r)
	sourcePath, sourcePresent := resolveClientLogSource()
	sourceInfo := clientLogSourceInfo(sourcePath, sourcePresent)

	entries := []logview.Entry{}
	if sourcePresent {
		loaded, err := loadClientLogEntries(sourcePath, q)
		if err != nil {
			sourceInfo = fmt.Sprintf("Failed to read log file: %v", err)
		} else {
			entries = loaded
		}
	}

	clientwebui.RenderLogsPage(w, clientwebui.LogsPageData{
		PageData: clientwebui.PageData{
			NavActive: "logs",
			Title:     "Logs",
		},
		Entries:          entries,
		LogSourcePath:    sourcePath,
		LogSourcePresent: sourcePresent,
		LogSourceInfo:    sourceInfo,
		Query:            q,
	})
}

func handleLogsPartial(w http.ResponseWriter, r *http.Request) {
	q := logview.ParseQuery(r)
	sourcePath, sourcePresent := resolveClientLogSource()
	sourceInfo := clientLogSourceInfo(sourcePath, sourcePresent)

	entries := []logview.Entry{}
	if sourcePresent {
		loaded, err := loadClientLogEntries(sourcePath, q)
		if err != nil {
			sourceInfo = fmt.Sprintf("Failed to read log file: %v", err)
		} else {
			entries = loaded
		}
	}

	clientwebui.RenderPartial(w, "partials/logs_table.html", clientwebui.LogsPageData{
		Entries:          entries,
		LogSourcePath:    sourcePath,
		LogSourcePresent: sourcePresent,
		LogSourceInfo:    sourceInfo,
		Query:            q,
	})
}

// handleLogsCSV exports the client log as CSV, mirroring the server admin
// logs export (same column set). Honors the same filter query params as the
// logs page; always exports from the newest entry.
func handleLogsCSV(w http.ResponseWriter, r *http.Request) {
	q := logview.ParseQuery(r)
	if q.Limit < logview.MaxLimit {
		q.Limit = logview.MaxLimit
	}
	q.Offset = 0
	sourcePath, sourcePresent := resolveClientLogSource()
	entries := []logview.Entry{}
	if sourcePresent {
		if loaded, err := loadClientLogEntries(sourcePath, q); err == nil {
			entries = loaded
		}
	}

	filename := "client-logs-" + time.Now().UTC().Format("2006-01-02T150405") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"time", "app", "level", "event", "summary", "context", "raw"})
	for _, e := range entries {
		_ = cw.Write([]string{e.Timestamp, e.Component, e.Level, e.Event, e.Summary, e.Context, e.Raw})
	}
	cw.Flush()
}

func resolveClientLogSource() (path string, present bool) {
	path = ClientLogPath()
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		return path, true
	}
	return path, false
}

func clientLogSourceInfo(path string, present bool) string {
	if present {
		return "Client log file found."
	}
	return fmt.Sprintf("No log file found yet at %s. Logs are created when the client starts.", path)
}

func loadClientLogEntries(path string, q logview.Query) ([]logview.Entry, error) {
	return logview.LoadEntries(path, q, logview.ParseClientLine)
}
