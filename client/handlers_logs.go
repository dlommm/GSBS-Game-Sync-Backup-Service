package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/gsbs/gsbs/pkg/logview"
	clientwebui "github.com/gsbs/gsbs/client/webui"
)

func handleLogsPage(w http.ResponseWriter, r *http.Request) {
	level, query, limit, autoRefresh, refreshSeconds := logview.ParseQuery(r)
	sourcePath, sourcePresent := resolveClientLogSource()
	sourceInfo := clientLogSourceInfo(sourcePath, sourcePresent)

	entries := []logview.Entry{}
	if sourcePresent {
		loaded, err := loadClientLogEntries(sourcePath, level, query, limit)
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
		Level:            level,
		Query:            query,
		Limit:            limit,
		AutoRefresh:      autoRefresh,
		RefreshSeconds:   refreshSeconds,
	})
}

func handleLogsPartial(w http.ResponseWriter, r *http.Request) {
	level, query, limit, _, _ := logview.ParseQuery(r)
	sourcePath, sourcePresent := resolveClientLogSource()
	sourceInfo := clientLogSourceInfo(sourcePath, sourcePresent)

	entries := []logview.Entry{}
	if sourcePresent {
		loaded, err := loadClientLogEntries(sourcePath, level, query, limit)
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
		Level:            level,
		Query:            query,
		Limit:            limit,
	})
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

func loadClientLogEntries(path, level, query string, limit int) ([]logview.Entry, error) {
	return logview.LoadEntries(path, level, query, limit, logview.ParseClientLine)
}
