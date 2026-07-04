package webui

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
)

// buildGameCards loads the user's saves, groups them per game (reusing
// groupSaves), applies the status filter and sort, and returns the cards plus
// roll-up totals. groupSaves already returns games most-recently-synced first,
// which is the default ("recent") sort.
func (h *WebHandler) buildGameCards(ctx context.Context, userID, query, statusFilter, sortKey string) (cards []gameCard, totalFiles int, totalBytes int64, maxFiles int) {
	var saves []store.SaveSummary
	var err error
	if query != "" {
		saves, err = h.store.ListSaveSummariesFiltered(ctx, userID, query)
	} else {
		saves, err = h.store.ListSaveSummaries(ctx, userID)
	}
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("my games: list saves failed")
		return nil, 0, 0, 0
	}
	for _, g := range groupSaves(saves) {
		st := gameSyncStatus(g.LastSynced)
		if statusFilter != "" && statusFilter != "all" && st != statusFilter {
			continue
		}
		cards = append(cards, gameCard{
			GameID: g.GameID, Title: g.Title, FileCount: g.FileCount,
			TotalBytes: g.TotalBytes, LastSynced: g.LastSynced, Status: st,
		})
		totalFiles += g.FileCount
		totalBytes += g.TotalBytes
		if g.FileCount > maxFiles {
			maxFiles = g.FileCount
		}
	}
	switch sortKey {
	case "name":
		sort.SliceStable(cards, func(i, j int) bool {
			return strings.ToLower(cards[i].Title) < strings.ToLower(cards[j].Title)
		})
	case "size":
		sort.SliceStable(cards, func(i, j int) bool { return cards[i].TotalBytes > cards[j].TotalBytes })
	case "files":
		sort.SliceStable(cards, func(i, j int) bool { return cards[i].FileCount > cards[j].FileCount })
	default: // "recent": preserve groupSaves order (most-recent first)
	}
	return cards, totalFiles, totalBytes, maxFiles
}

func gamesViewParams(r *http.Request) (query, status, sortKey, view string) {
	query = strings.TrimSpace(r.URL.Query().Get("q"))
	status = r.URL.Query().Get("status")
	if status == "" {
		status = "all"
	}
	sortKey = r.URL.Query().Get("sort")
	if sortKey == "" {
		sortKey = "recent"
	}
	view = r.URL.Query().Get("view")
	if view != "list" {
		view = "grid"
	}
	return query, status, sortKey, view
}

func (h *WebHandler) gamesData(r *http.Request, userID string) dashboardGamesData {
	query, status, sortKey, view := gamesViewParams(r)
	cards, totalFiles, totalBytes, maxFiles := h.buildGameCards(r.Context(), userID, query, status, sortKey)
	return dashboardGamesData{
		Games:      cards,
		TotalGames: len(cards),
		TotalFiles: totalFiles,
		TotalBytes: totalBytes,
		MaxFiles:   maxFiles,
		Query:      query,
		Status:     status,
		Sort:       sortKey,
		View:       view,
		ReadOnly:   h.readOnly,
	}
}

func (h *WebHandler) serveDashboardGames(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	data := h.gamesData(r, userID)
	data.PageData = PageData{
		PageName: "dashboard_games", Username: username,
		IsAdmin:   h.isAdminUser(r.Context(), userID, username),
		CSRFToken: csrfToken, NavActive: "games",
	}
	h.render(w, "dashboard_games.html", data)
}

func (h *WebHandler) serveDashboardGamesPartial(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	data := h.gamesData(r, userID)
	data.PageData = PageData{CSRFToken: csrfToken}
	h.renderPartial(w, "partials/game_cards.html", data)
}

func (h *WebHandler) serveGameDetail(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	raw := strings.TrimPrefix(r.URL.EscapedPath(), "/dashboard/games/")
	gameID, err := url.PathUnescape(raw)
	if err != nil || strings.TrimSpace(gameID) == "" {
		Redirect(w, r, "/dashboard/games")
		return
	}
	saves, err := h.store.ListSaveSummaries(r.Context(), userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("game detail: list saves failed")
		Redirect(w, r, "/dashboard/games?error=load_failed")
		return
	}
	var mine []store.SaveSummary
	for _, s := range saves {
		if s.GameID == gameID {
			mine = append(mine, s)
		}
	}
	if len(mine) == 0 {
		Redirect(w, r, "/dashboard/games")
		return
	}
	groups := groupSaves(mine)
	g := groups[0] // exactly one game after filtering

	var anyEncrypted bool
	var largest saveFileRow
	for _, cat := range g.Categories {
		for _, f := range cat.Files {
			if f.Encrypted {
				anyEncrypted = true
			}
			if f.SizeBytes > largest.SizeBytes {
				largest = f
			}
		}
	}
	encLabel := "Standard"
	if anyEncrypted {
		encLabel = "Encrypted"
	}

	largestChange, hasLargestChange, _ := h.store.LargestChangeForGame(r.Context(), userID, g.GameID)

	csrfToken := SetCSRFToken(w, r, h.secret)
	h.render(w, "game_detail.html", gameDetailData{
		PageData: PageData{
			PageName: "game_detail", Username: username,
			IsAdmin:   h.isAdminUser(r.Context(), userID, username),
			CSRFToken: csrfToken, NavActive: "games",
		},
		GameID:           g.GameID,
		Title:            g.Title,
		FileCount:        g.FileCount,
		TotalBytes:       g.TotalBytes,
		LastSynced:       g.LastSynced,
		Status:           gameSyncStatus(g.LastSynced),
		Encrypted:        anyEncrypted,
		EncryptionLabel:  encLabel,
		CategoryCount:    len(g.Categories),
		Categories:       g.Categories,
		LargestFile:      largest,
		HasLargestChange: hasLargestChange,
		LargestChange:    largestChange,
		ReadOnly:         h.readOnly,
	})
}

// previewMaxBytes caps how much of a save version we read for an inline preview.
const previewMaxBytes = 16 * 1024

// serveSaveVersionPreview returns a small HTML fragment previewing the latest
// version of a save as text, for the game detail file explorer. Encrypted and
// binary content are not previewable; oversized content is truncated.
func (h *WebHandler) serveSaveVersionPreview(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	gameID := strings.TrimSpace(r.URL.Query().Get("game_id"))
	pathKey := strings.TrimSpace(r.URL.Query().Get("path_key"))
	if gameID == "" || pathKey == "" {
		http.Error(w, "game_id and path_key required", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Encrypted saves are opaque ciphertext on the server — nothing to preview.
	if summaries, err := h.store.ListSaveSummaries(r.Context(), userID); err == nil {
		for _, s := range summaries {
			if s.GameID == gameID && s.PathKey == pathKey && s.Encrypted {
				fmt.Fprint(w, `<p class="cell-muted">This save is end-to-end encrypted — preview is unavailable.</p>`)
				return
			}
		}
	}

	versions, err := h.store.ListSaveVersions(r.Context(), userID, gameID, pathKey, 1)
	if err != nil || len(versions) == 0 {
		fmt.Fprint(w, `<p class="cell-muted">No content to preview.</p>`)
		return
	}
	blob, err := h.store.GetSaveVersion(r.Context(), userID, gameID, pathKey, versions[0].Version)
	if err != nil || blob == nil || len(blob.Content) == 0 {
		fmt.Fprint(w, `<p class="cell-muted">No content to preview.</p>`)
		return
	}
	content := blob.Content
	truncated := false
	if len(content) > previewMaxBytes {
		content = content[:previewMaxBytes]
		truncated = true
	}
	if !looksTextual(content) {
		fmt.Fprintf(w, `<p class="cell-muted">This looks like a binary save file (%s) — preview is unavailable.</p>`, formatBytes(blob.ContentSize))
		return
	}
	fmt.Fprintf(w, `<pre class="preview-pre">%s</pre>`, template.HTMLEscapeString(string(content)))
	if truncated {
		fmt.Fprintf(w, `<p class="cell-muted">Showing first %s of %s.</p>`, formatBytes(previewMaxBytes), formatBytes(blob.ContentSize))
	}
}

// looksTextual reports whether a byte slice is likely human-readable text:
// valid UTF-8 with a low proportion of control characters.
func looksTextual(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	var control int
	for _, r := range string(b) {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) {
			control++
		}
	}
	return control*100 <= len(b) // <=1% control bytes
}
