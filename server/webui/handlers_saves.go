package webui

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
)

// sanitizeFilenamePart keeps download filenames safe for a Content-Disposition
// header: path separators, quotes, and control characters become underscores.
func sanitizeFilenamePart(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == '"' || r == ';':
			return '_'
		case r < 0x20 || r == 0x7f:
			return '_'
		}
		return r
	}, s)
}

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
		logx.Logger().Error().Err(err).Msg("webui save versions list failed")
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "save_versions.html", saveVersionsData{
			PageData: PageData{
				PageName: "save_versions", Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username),
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
			PageName: "save_versions", Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username),
			CSRFToken: csrfToken, NavActive: "dashboard", Error: errorMsg,
		},
		GameID: gameID, PathKey: pathKey, GameTitle: gameTitle,
		Versions: versions, CurrentVersion: currentVersion,
		SessionNotes: h.buildSessionNotes(r.Context(), userID, gameID, versions),
	})
}

// buildSessionNotes maps version numbers to play-session annotations (v5.2):
// a version written during a session, or within 15 minutes after it ended,
// gets "during/after a <duration> session on <device>".
func (h *WebHandler) buildSessionNotes(ctx context.Context, userID, gameID string, versions []store.SaveVersionInfo) map[int]string {
	sessions, err := h.store.ListGameSessions(ctx, userID, gameID, 50)
	if err != nil || len(sessions) == 0 {
		return nil
	}
	notes := map[int]string{}
	for _, v := range versions {
		t, err := time.Parse(time.RFC3339, v.UpdatedAt)
		if err != nil {
			continue
		}
		for _, gs := range sessions {
			start, err1 := time.Parse(time.RFC3339, gs.StartedAt)
			end, err2 := time.Parse(time.RFC3339, gs.EndedAt)
			if err1 != nil || err2 != nil {
				continue
			}
			device := gs.ClientName
			if device == "" {
				device = "a device"
			}
			dur := end.Sub(start).Round(time.Minute)
			switch {
			case !t.Before(start) && !t.After(end):
				notes[v.Version] = fmt.Sprintf("during a %s session on %s", formatSessionDur(dur), device)
			case t.After(end) && t.Sub(end) <= 15*time.Minute:
				notes[v.Version] = fmt.Sprintf("after a %s session on %s", formatSessionDur(dur), device)
			default:
				continue
			}
			break
		}
	}
	if len(notes) == 0 {
		return nil
	}
	return notes
}

// formatSessionDur renders a session length compactly: "47m", "2h05m".
func formatSessionDur(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
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
		logx.Logger().Error().Err(err).Str("user_id", userID).Str("game_id", gameID).Str("path_key", pathKey).Int("version", version).Msg("webui restore version failed")
		Redirect(w, r, "/dashboard/save/versions?game_id="+url.QueryEscape(gameID)+"&path_key="+url.QueryEscape(pathKey)+"&error=restore_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "restore_version", "", fmt.Sprintf("game_id=%s path_key=%s version=%d", gameID, pathKey, version))
	logx.Logger().Info().Str("user_id", userID).Str("game_id", gameID).Str("path_key", pathKey).Int("version", version).Msg("webui restore version ok")
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
	filename := fmt.Sprintf("save-%s-v%d.bin", sanitizeFilenamePart(gameID), version)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(blob.Content)
}

func (h *WebHandler) handleDeleteGameSaves(w http.ResponseWriter, r *http.Request) {
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
	if gameID == "" {
		Redirect(w, r, "/dashboard?error=delete_missing_params")
		return
	}
	n, err := h.store.DeleteSavesForGame(r.Context(), userID, gameID)
	if err != nil {
		logx.Logger().Error().Err(err).Str("user_id", userID).Str("game_id", gameID).Msg("webui delete game saves failed")
		Redirect(w, r, "/dashboard?error=delete_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "delete_game_saves", "", fmt.Sprintf("game_id=%s deleted=%d", gameID, n))
	logx.Logger().Info().Str("user_id", userID).Str("game_id", gameID).Int("deleted", n).Msg("webui delete game saves ok")
	Redirect(w, r, "/dashboard?deleted=1")
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
		logx.Logger().Error().Err(err).Str("user_id", userID).Str("game_id", gameID).Str("path_key", pathKey).Msg("webui delete save failed")
		Redirect(w, r, "/dashboard?error=delete_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "delete_save", "", fmt.Sprintf("game_id=%s path_key=%s", gameID, pathKey))
	logx.Logger().Info().Str("user_id", userID).Str("game_id", gameID).Str("path_key", pathKey).Msg("webui delete save ok")
	Redirect(w, r, "/dashboard?deleted=1")
}
