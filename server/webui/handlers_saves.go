package webui

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
)

func (h *WebHandler) serveSaveVersions(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	gameID := strings.TrimSpace(r.URL.Query().Get("game_id"))
	pathKey := strings.TrimSpace(r.URL.Query().Get("path_key"))
	if gameID == "" || pathKey == "" {
		Redirect(w, r, "/dashboard?error=missing_game_or_path")
		return
	}
	versions, err := h.store.ListSaveVersions(r.Context(), userID, gameID, pathKey, 20)
	if err != nil {
		log.Printf("webui save versions: list failed: %v", err)
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "save_versions.html", saveVersionsData{
			PageData: PageData{
				Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username),
				CSRFToken: csrfToken, NavActive: "dashboard", Error: "Failed to load versions",
			},
			GameID: gameID, PathKey: pathKey,
		})
		return
	}
	gameTitle := gameID
	currentVersion := 0
	if len(versions) > 0 {
		currentVersion = versions[0].Version
	}
	if summaries, err := h.store.ListSaveSummaries(r.Context(), userID); err == nil {
		for _, s := range summaries {
			if s.GameID == gameID && s.PathKey == pathKey {
				gameTitle = s.GameTitle
				break
			}
		}
	}
	errorMsg := r.URL.Query().Get("error")
	if errorMsg == "restore_failed" {
		errorMsg = "Restore failed. Version may not exist."
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	h.render(w, "save_versions.html", saveVersionsData{
		PageData: PageData{
			Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username),
			CSRFToken: csrfToken, NavActive: "dashboard", Error: errorMsg,
		},
		GameID: gameID, PathKey: pathKey, GameTitle: gameTitle,
		Versions: versions, CurrentVersion: currentVersion,
	})
}

func (h *WebHandler) handleRestoreVersion(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	if h.readOnly {
		Redirect(w, r, "/dashboard?error=read_only")
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	gameID := strings.TrimSpace(r.FormValue("game_id"))
	pathKey := strings.TrimSpace(r.FormValue("path_key"))
	versionStr := strings.TrimSpace(r.FormValue("version"))
	if gameID == "" || pathKey == "" || versionStr == "" {
		Redirect(w, r, "/dashboard?error=restore_missing_params")
		return
	}
	var version int
	if _, err := fmt.Sscanf(versionStr, "%d", &version); err != nil || version < 1 {
		Redirect(w, r, "/dashboard?error=restore_invalid_version")
		return
	}
	if err := h.store.RestoreSaveVersion(r.Context(), userID, gameID, pathKey, version); err != nil {
		log.Printf("webui restore version: %v", err)
		Redirect(w, r, "/dashboard/save/versions?game_id="+url.QueryEscape(gameID)+"&path_key="+url.QueryEscape(pathKey)+"&error=restore_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "restore_version", "", fmt.Sprintf("game_id=%s path_key=%s version=%d", gameID, pathKey, version))
	log.Printf("webui restore version: ok user=%s game_id=%s path_key=%s version=%d", userID, gameID, pathKey, version)
	Redirect(w, r, "/dashboard?restored=1")
}

func (h *WebHandler) serveSaveVersionDownload(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	gameID := strings.TrimSpace(r.URL.Query().Get("game_id"))
	pathKey := strings.TrimSpace(r.URL.Query().Get("path_key"))
	versionStr := strings.TrimSpace(r.URL.Query().Get("version"))
	if gameID == "" || pathKey == "" || versionStr == "" {
		http.Error(w, "game_id, path_key and version required", http.StatusBadRequest)
		return
	}
	var version int
	if _, err := fmt.Sscanf(versionStr, "%d", &version); err != nil || version < 1 {
		http.Error(w, "invalid version", http.StatusBadRequest)
		return
	}
	blob, err := h.store.GetSaveVersion(r.Context(), userID, gameID, pathKey, version)
	if err != nil || blob == nil {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}
	filename := fmt.Sprintf("save-%s-v%d.bin", strings.ReplaceAll(gameID, "/", "_"), version)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(blob.Content)
}

func (h *WebHandler) handleDeleteSave(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	if h.readOnly {
		Redirect(w, r, "/dashboard?error=read_only")
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	gameID := strings.TrimSpace(r.FormValue("game_id"))
	pathKey := strings.TrimSpace(r.FormValue("path_key"))
	if gameID == "" || pathKey == "" {
		Redirect(w, r, "/dashboard?error=delete_missing_params")
		return
	}
	if err := h.store.DeleteSave(r.Context(), userID, gameID, pathKey); err != nil {
		log.Printf("webui delete save: %v", err)
		Redirect(w, r, "/dashboard?error=delete_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "delete_save", "", fmt.Sprintf("game_id=%s path_key=%s", gameID, pathKey))
	log.Printf("webui delete save: ok user=%s game_id=%s path_key=%s", userID, gameID, pathKey)
	Redirect(w, r, "/dashboard?deleted=1")
}
