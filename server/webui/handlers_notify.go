package webui

import (
	"net/http"
	"strings"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/notify"
	"github.com/gsbs/gsbs/server/store"
)

// handleAdminNotifyTest fires a test event through every configured channel.
func (h *WebHandler) handleAdminNotifyTest(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	h.notifyEvent(notify.Event{
		Type:   notify.EventTest,
		Title:  "GSBS test notification",
		Body:   "Notifications are configured correctly. This test was triggered from the admin settings page.",
		UserID: userID, // also exercises the admin's own personal sinks
	})
	h.appendAuditBroadcast(r.Context(), userID, username, "notify_test", "", "")
	Redirect(w, r, "/admin/settings?saved=1")
}

// handleUserNotifySave stores a user's personal notification channels.
func (h *WebHandler) handleUserNotifySave(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	ns := store.UserNotifySettings{
		WebhookURL: strings.TrimSpace(r.FormValue("notify_webhook_url")),
		DiscordURL: strings.TrimSpace(r.FormValue("notify_discord_url")),
		NtfyURL:    strings.TrimSpace(r.FormValue("notify_ntfy_url")),
	}
	for _, u := range []string{ns.WebhookURL, ns.DiscordURL, ns.NtfyURL} {
		if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			Redirect(w, r, "/dashboard/settings?error=invalid_notify_url")
			return
		}
	}
	if err := h.store.SetUserNotifySettings(r.Context(), userID, ns); err != nil {
		logx.Logger().Error().Err(err).Str("username", username).Msg("save user notify settings")
		Redirect(w, r, "/dashboard/settings?error=save_failed")
		return
	}
	if r.FormValue("send_test") == "1" {
		h.notifyEvent(notify.Event{
			Type:   notify.EventTest,
			Title:  "GSBS test notification",
			Body:   "Your personal notification channels are working.",
			UserID: userID,
		})
	}
	Redirect(w, r, "/dashboard/settings?updated=1")
}
