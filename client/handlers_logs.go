package main

import (
	"fmt"
	"net/http"
	"os"

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
