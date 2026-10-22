package webui

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
)

// clientOnlineWindow: a device is shown "online" when its last_seen is within
// this window. Pushes/pulls bump last_seen, so an actively-syncing device shows
// online; the clients list polls every 30s to keep this fresh.
const clientOnlineWindow = 5 * time.Minute

// staleAfter: a device that hasn't been seen for this long gets a "hasn't
// synced in N days" callout on the Devices page (matches the stale-device
// notification default of 14 days).
const staleAfter = 14 * 24 * time.Hour

type clientRow struct {
	store.ClientInfo
	Online bool
	// Device Health (v5.1): computed presentation fields.
	StaleDays        int  // days since last_seen when past staleAfter; 0 when fresh
	TokenExpiresDays int  // whole days until the device token expires (floor 0)
	TokenExpirySoon  bool // under 7 days — the device must come online to auto-renew
}

func buildClientHealth(row *clientRow, now time.Time) {
	if !row.Online {
		if t, err := time.Parse(time.RFC3339, row.LastSeen); err == nil {
			if age := now.Sub(t); age >= staleAfter {
				row.StaleDays = int(age.Hours() / 24)
			}
		}
	}
	if t, err := time.Parse(time.RFC3339, row.TokenCreatedAt); err == nil {
		left := store.TokenMaxAge() - now.Sub(t)
		if left < 0 {
			left = 0
		}
		row.TokenExpiresDays = int(left.Hours() / 24)
		row.TokenExpirySoon = left < 7*24*time.Hour
	} else {
		row.TokenExpiresDays = -1 // unknown; template hides the line
	}
}

type dashboardClientsData struct {
	PageData
	Clients  []clientRow
	Online   int
	Total    int
	ReadOnly bool
	// Device lanes: the last laneDays days, one lane per device, one tick per
	// save it sent (from save_versions.client_id).
	LaneDays []laneDay
	Lanes    []deviceLane
}

const laneDays = 14

type laneDay struct {
	Weekday string // "Mon"
	Day     string // "12"
	Weekend bool
	LeftPct float64
	Today   bool
}

type laneTick struct {
	LeftPct float64
	Label   string // "Tidebreaker v4 · Oct 20, 21:15" (tooltip)
	Latest  bool   // the newest save of this lane
}

type deviceLane struct {
	ClientID string
	Name     string
	Sub      string
	Silent   bool // no check-in for staleAfter
	Ticks    []laneTick
}

// buildDeviceLanes places each recent save on its device's lane. Positions
// are percentages of the window (oldest day left, now right) so the
// template stays declarative; app.js applies them via CSSOM (CSP-safe).
func buildDeviceLanes(rows []clientRow, events []store.SaveEvent, now time.Time) ([]laneDay, []deviceLane) {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(laneDays - 1))
	span := now.Sub(start).Seconds()
	days := make([]laneDay, 0, laneDays)
	for i := 0; i < laneDays; i++ {
		d := start.AddDate(0, 0, i)
		days = append(days, laneDay{
			Weekday: d.Format("Mon"), Day: d.Format("2"),
			Weekend: d.Weekday() == time.Saturday || d.Weekday() == time.Sunday,
			LeftPct: d.Sub(start).Seconds() / span * 100, Today: i == laneDays-1,
		})
	}
	lanes := make([]deviceLane, 0, len(rows))
	for _, r := range rows {
		sub := osLabel(r.OS)
		switch {
		case r.Online:
			sub += " · online now"
		case r.StaleDays > 0:
			sub = fmt.Sprintf("silent for %d days", r.StaleDays)
		default:
			sub += " · seen " + formatTime(r.LastSeen)
		}
		lanes = append(lanes, deviceLane{ClientID: r.ID, Name: r.Name, Sub: sub, Silent: r.StaleDays > 0})
	}
	byClient := map[string]*deviceLane{}
	for i := range lanes {
		byClient[lanes[i].ClientID] = &lanes[i]
	}
	for _, e := range events { // newest first
		lane := byClient[e.ClientID]
		if lane == nil {
			continue
		}
		t, err := time.Parse(time.RFC3339, e.UpdatedAt)
		if err != nil || t.Before(start) {
			continue
		}
		pct := t.Sub(start).Seconds() / span * 100
		if pct > 99.6 {
			pct = 99.6
		}
		lane.Ticks = append(lane.Ticks, laneTick{
			LeftPct: pct, Latest: len(lane.Ticks) == 0,
			Label: fmt.Sprintf("%s v%d · %s", e.GameTitle, e.Version, t.Local().Format("Jan 2, 15:04")),
		})
	}
	return days, lanes
}

func clientIsOnline(lastSeen string) bool {
	t, err := time.Parse(time.RFC3339, lastSeen)
	if err != nil {
		return false
	}
	return time.Since(t) <= clientOnlineWindow
}

func (h *WebHandler) loadClientRows(ctx context.Context, userID string) (rows []clientRow, online int) {
	clients, err := h.store.ListClientsByUserID(ctx, userID)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("clients page: list clients failed")
		return nil, 0
	}
	rows = make([]clientRow, 0, len(clients))
	now := time.Now().UTC()
	for _, c := range clients {
		on := clientIsOnline(c.LastSeen)
		if on {
			online++
		}
		row := clientRow{ClientInfo: c, Online: on}
		buildClientHealth(&row, now)
		rows = append(rows, row)
	}
	return rows, online
}

func (h *WebHandler) serveDashboardClientsPage(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	success := ""
	switch r.URL.Query().Get("ok") {
	case "renamed":
		success = "Device renamed."
	case "revoked":
		success = "Device revoked. Run gsbs-client login to reconnect it."
	}
	rows, online := h.loadClientRows(r.Context(), userID)
	events, err := h.store.RecentSaveEvents(r.Context(), userID, laneDays, 2000)
	if err != nil {
		logx.Logger().Error().Str("user_id", userID).Err(err).Msg("devices page: recent save events failed")
	}
	days, lanes := buildDeviceLanes(rows, events, time.Now())
	h.render(w, "dashboard_clients.html", dashboardClientsData{
		PageData: pageDataWithAppearance(h, r, userID, PageData{
			PageName: "dashboard_clients", Username: username,
			IsAdmin:   h.isAdminUser(r.Context(), userID, username),
			CSRFToken: csrfToken, NavActive: "clients",
			Error:   dashboardErrorMsg(r.URL.Query().Get("error")),
			Success: success,
		}),
		Clients:  rows,
		Online:   online,
		Total:    len(rows),
		ReadOnly: h.readOnly,
		LaneDays: days,
		Lanes:    lanes,
	})
}

func (h *WebHandler) serveDashboardClientsListPartial(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	rows, online := h.loadClientRows(r.Context(), userID)
	h.renderPartial(w, "partials/clients_list.html", dashboardClientsData{
		PageData: PageData{CSRFToken: csrfToken},
		Clients:  rows,
		Online:   online,
		Total:    len(rows),
		ReadOnly: h.readOnly,
	})
}

func (h *WebHandler) handleDashboardRenameClient(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	if h.readOnly {
		Redirect(w, r, "/dashboard/clients?error=read_only")
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	name := strings.TrimSpace(r.FormValue("name"))
	if clientID == "" || name == "" {
		Redirect(w, r, "/dashboard/clients?error=missing_client")
		return
	}
	if len(name) > 64 {
		name = name[:64]
	}
	owner, err := h.store.ClientUserID(r.Context(), clientID)
	if err != nil || owner != userID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err := h.store.RenameClient(r.Context(), userID, clientID, name); err != nil {
		logx.Logger().Error().Str("client_id", clientID).Err(err).Msg("webui rename client failed")
		Redirect(w, r, "/dashboard/clients?error=rename_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "rename_client", clientID, "name="+name)
	Redirect(w, r, "/dashboard/clients?ok=renamed")
}
