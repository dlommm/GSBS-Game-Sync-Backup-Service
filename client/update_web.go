package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"fyne.io/systray"
)

// handleApplyUpdate lets the local WebUI trigger the same self-update flow as
// the tray (v5.2, FIX-6): POST /api/apply-update. The download+apply runs in
// the background and the client restarts itself on success, so the response
// only acknowledges the start. Loopback-only like every setup-server route.
func handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if isFlatpak() {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "updates are managed by your software center"})
		return
	}

	updateMu.Lock()
	info := pendingUpdate
	busy := updateInProgress
	updateMu.Unlock()
	if info == nil {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "no update pending — run a check first"})
		return
	}
	if info.Manual {
		repo := ""
		if cfg, _ := loadConfig(); cfg != nil {
			repo = strings.TrimSpace(cfg.UpdateRepo)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"manual_url": ReleasePageURL(repo)})
		return
	}
	if busy {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "an update operation is already in progress"})
		return
	}

	updateMu.Lock()
	updateInProgress = true
	updateMu.Unlock()
	go func() {
		defer func() {
			updateMu.Lock()
			updateInProgress = false
			updateMu.Unlock()
		}()
		path, err := DownloadUpdate(info)
		if err != nil {
			log.Printf("update (webui): download failed: %v", err)
			return
		}
		if err := ApplyUpdate(path); err != nil {
			log.Printf("update (webui): apply failed: %v", err)
			return
		}
		log.Printf("update (webui): %s applied — restarting", info.Tag)
		systray.Quit()
	}()
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "installing", "tag": info.Tag})
}
