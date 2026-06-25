package webui

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
)

// ── Cmd+K global search ───────────────────────────────────────────────

type cmdkCommand struct {
	Label string
	Sub   string
	Href  string
	Icon  string
}

type cmdkGameResult struct {
	GameID string
	Title  string
	Meta   string
}

type cmdkResults struct {
	Query    string
	Commands []cmdkCommand
	Games    []cmdkGameResult
}

func (h *WebHandler) serveGlobalSearch(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	ql := strings.ToLower(q)

	nav := []cmdkCommand{
		{Label: "Dashboard", Sub: "Overview", Href: "/dashboard", Icon: "🏠"},
		{Label: "My Games", Sub: "Browse synced saves", Href: "/dashboard/games", Icon: "🎮"},
		{Label: "Devices", Sub: "Connected devices", Href: "/dashboard/clients", Icon: "🖥️"},
		{Label: "Insights", Sub: "Backup analytics", Href: "/dashboard/analytics", Icon: "📊"},
		{Label: "Settings", Sub: "Security & account", Href: "/dashboard/settings", Icon: "⚙️"},
	}
	if h.isAdminUser(r.Context(), userID, username) {
		nav = append(nav, cmdkCommand{Label: "Admin", Sub: "Server administration", Href: "/admin", Icon: "🛡️"})
	}
	var cmds []cmdkCommand
	for _, c := range nav {
		if ql == "" || strings.Contains(strings.ToLower(c.Label), ql) || strings.Contains(strings.ToLower(c.Sub), ql) {
			cmds = append(cmds, c)
		}
	}

	var games []cmdkGameResult
	if q != "" {
		if saves, err := h.store.ListSaveSummariesFiltered(r.Context(), userID, q); err == nil {
			for _, g := range groupSaves(saves) {
				meta := strconv.Itoa(g.FileCount) + " files · " + formatBytes(g.TotalBytes)
				games = append(games, cmdkGameResult{GameID: g.GameID, Title: g.Title, Meta: meta})
				if len(games) >= 6 {
					break
				}
			}
		}
	}
	h.renderPartial(w, "partials/cmdk_results.html", cmdkResults{Query: q, Commands: cmds, Games: games})
}

// ── Export save metadata ──────────────────────────────────────────────

func (h *WebHandler) handleExportSaves(w http.ResponseWriter, r *http.Request, format string) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	saves, err := h.store.ListSaveSummaries(r.Context(), userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("export saves failed")
		http.Error(w, "Failed to load saves", http.StatusInternalServerError)
		return
	}
	stamp := time.Now().UTC().Format("2006-01-02T150405")
	switch format {
	case "json":
		if saves == nil {
			saves = []store.SaveSummary{} // emit [] rather than null for an empty export
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="gsbs-saves-`+stamp+`.json"`)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(saves)
	default: // csv
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="gsbs-saves-`+stamp+`.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"game_id", "game_title", "path_key", "relative_path", "size_bytes", "updated_at", "encrypted", "content_hash"})
		for _, s := range saves {
			_ = cw.Write([]string{
				s.GameID, s.GameTitle, s.PathKey, s.RelativePath,
				strconv.FormatInt(s.SizeBytes, 10), s.UpdatedAt,
				strconv.FormatBool(s.Encrypted), s.ContentHash,
			})
		}
		cw.Flush()
	}
}

// ── Bulk delete games ─────────────────────────────────────────────────

func (h *WebHandler) handleBulkDeleteGames(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	if h.readOnly {
		Redirect(w, r, "/dashboard/games?error=read_only")
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		Redirect(w, r, "/dashboard/games?error=delete_failed")
		return
	}
	gameIDs := r.Form["game_id"]
	if len(gameIDs) == 0 {
		Redirect(w, r, "/dashboard/games")
		return
	}
	var deleted, games int
	for _, gid := range gameIDs {
		gid = strings.TrimSpace(gid)
		if gid == "" {
			continue
		}
		n, err := h.store.DeleteSavesForGame(r.Context(), userID, gid)
		if err != nil {
			logx.Logger().Error().Str("user_id", userID).Str("game_id", gid).Err(err).Msg("bulk delete game failed")
			continue
		}
		deleted += n
		games++
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "delete_game_saves", "", "bulk games="+strconv.Itoa(games)+" deleted="+strconv.Itoa(deleted))
	Redirect(w, r, "/dashboard/games?deleted=1")
}
