package webui

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
	"github.com/robfig/cron/v3"
)

type adminSettingsData struct {
	PageData
	PCGWCron                   string
	PCGWCronSource             string
	PCGWCronDisabled           bool
	PCGWCronEnvOverride        bool
	PCGWCronNextRun            string
	PCGWTitleExcludesJSON      string
	PCGWPathExcludesJSON       string
	AutoRunFirstStart          bool
	PCGWSyncSource             string
	PCGWBundleCron             string
	PCGWBundleURL              string
	PCGWBundleDeltaURL         string
	PCGWBundleIncrementalFB    bool
	PCGWSyncSourceEnvOverride  bool
	PCGWBundleCronEnvOverride  bool
	PCGWBundleCronNextRun      string
}

func (h *WebHandler) serveAdminSettings(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	settings, _ := h.store.ListAdminSettings(ctx)

	data := adminSettingsData{
		PageData:              h.adminPageData(w, r, userID, username, "settings", "admin_settings"),
		PCGWTitleExcludesJSON: settings[store.AdminSettingPCGWTitleExcludes],
		PCGWPathExcludesJSON:  settings[store.AdminSettingPCGWPathExcludes],
		PCGWSyncSource:        store.PCGWSyncSourceFromSettings(settings),
		PCGWBundleCron:        store.PCGWBundleCronFromSettings(settings),
		PCGWBundleURL:         store.PCGWBundleURLFromSettings(settings),
		PCGWBundleDeltaURL:    store.PCGWBundleDeltaURLFromSettings(settings),
		PCGWBundleIncrementalFB: store.PCGWBundleIncrementalFallbackFromSettings(settings),
	}
	if data.PCGWPathExcludesJSON == "" {
		data.PCGWPathExcludesJSON = store.DefaultPCGWPathExcludesJSON
	}
	if data.PCGWTitleExcludesJSON == "" {
		data.PCGWTitleExcludesJSON = "[]"
	}
	auto := settings[store.AdminSettingPCGWAutoRunFirstStart]
	data.AutoRunFirstStart = auto == "true" || auto == "1"

	if _, ok := os.LookupEnv(store.EnvPCGWSyncSource); ok {
		data.PCGWSyncSourceEnvOverride = true
	}
	if _, ok := os.LookupEnv(store.EnvPCGWBundleCron); ok {
		data.PCGWBundleCronEnvOverride = true
	}

	if h.pcgwCron != nil {
		view := h.pcgwCron.View(ctx)
		data.PCGWCron = view.Expr
		data.PCGWCronSource = view.Source
		data.PCGWCronDisabled = view.Disabled
		data.PCGWCronEnvOverride = view.EnvOverride
		if !view.NextRun.IsZero() {
			data.PCGWCronNextRun = view.NextRun.Format("Mon, Jan 2 2006 15:04 MST")
		}
		if !view.BundleNext.IsZero() {
			data.PCGWBundleCronNextRun = view.BundleNext.Format("Mon, Jan 2 2006 15:04 MST")
		}
	} else {
		data.PCGWCron = store.PCGWCronFromSettings(settings)
		if v, ok := os.LookupEnv("GSBS_PCGW_CRON"); ok {
			data.PCGWCronEnvOverride = true
			data.PCGWCron = v
			data.PCGWCronDisabled = v == ""
			data.PCGWCronSource = "env"
		}
	}

	h.render(w, "admin_settings.html", data)
}

func (h *WebHandler) handleAdminSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	cronExpr := strings.TrimSpace(r.FormValue("pcgw_cron"))
	envCronOverride := false
	if _, ok := os.LookupEnv("GSBS_PCGW_CRON"); ok {
		envCronOverride = true
	} else {
		if cronExpr != "" {
			if _, err := cron.ParseStandard(cronExpr); err != nil {
				Redirect(w, r, "/admin/settings?error=invalid_cron")
				return
			}
		}
		if err := h.store.SetAdminSetting(ctx, store.AdminSettingPCGWCron, cronExpr); err != nil {
			logx.Logger().Error().Err(err).Msg("admin settings save pcgw_cron")
			Redirect(w, r, "/admin/settings?error=save_failed")
			return
		}
	}
	_ = envCronOverride

	if _, ok := os.LookupEnv(store.EnvPCGWSyncSource); !ok {
		syncSource := strings.TrimSpace(strings.ToLower(r.FormValue("pcgw_sync_source")))
		if syncSource != store.PCGWSyncSourceGitHub && syncSource != store.PCGWSyncSourceAPI {
			syncSource = store.PCGWSyncSourceGitHub
		}
		if err := h.store.SetAdminSetting(ctx, store.AdminSettingPCGWSyncSource, syncSource); err != nil {
			Redirect(w, r, "/admin/settings?error=save_failed")
			return
		}
	}

	if _, ok := os.LookupEnv(store.EnvPCGWBundleCron); !ok {
		bundleCron := strings.TrimSpace(r.FormValue("pcgw_bundle_cron"))
		if bundleCron != "" {
			if _, err := cron.ParseStandard(bundleCron); err != nil {
				Redirect(w, r, "/admin/settings?error=invalid_bundle_cron")
				return
			}
		}
		if err := h.store.SetAdminSetting(ctx, store.AdminSettingPCGWBundleCron, bundleCron); err != nil {
			Redirect(w, r, "/admin/settings?error=save_failed")
			return
		}
	}

	bundleURL := strings.TrimSpace(r.FormValue("pcgw_bundle_url"))
	if bundleURL != "" {
		if err := h.store.SetAdminSetting(ctx, store.AdminSettingPCGWBundleURL, bundleURL); err != nil {
			Redirect(w, r, "/admin/settings?error=save_failed")
			return
		}
	}
	bundleDeltaURL := strings.TrimSpace(r.FormValue("pcgw_bundle_delta_url"))
	if bundleDeltaURL != "" {
		if err := h.store.SetAdminSetting(ctx, store.AdminSettingPCGWBundleDeltaURL, bundleDeltaURL); err != nil {
			Redirect(w, r, "/admin/settings?error=save_failed")
			return
		}
	}

	incFB := "false"
	if r.FormValue("pcgw_bundle_incremental_fallback") == "1" {
		incFB = "true"
	}
	if err := h.store.SetAdminSetting(ctx, store.AdminSettingPCGWBundleIncrementalFB, incFB); err != nil {
		Redirect(w, r, "/admin/settings?error=save_failed")
		return
	}

	titleJSON := strings.TrimSpace(r.FormValue("pcgw_title_excludes"))
	if titleJSON == "" {
		titleJSON = "[]"
	}
	if !json.Valid([]byte(titleJSON)) {
		Redirect(w, r, "/admin/settings?error=invalid_title_excludes")
		return
	}
	pathJSON := strings.TrimSpace(r.FormValue("pcgw_path_excludes"))
	if pathJSON == "" {
		pathJSON = store.DefaultPCGWPathExcludesJSON
	}
	if !json.Valid([]byte(pathJSON)) {
		Redirect(w, r, "/admin/settings?error=invalid_path_excludes")
		return
	}

	autoRun := r.FormValue("pcgw_auto_run_on_first_start") == "1"

	if err := h.store.SetAdminSetting(ctx, store.AdminSettingPCGWTitleExcludes, titleJSON); err != nil {
		Redirect(w, r, "/admin/settings?error=save_failed")
		return
	}
	if err := h.store.SetAdminSetting(ctx, store.AdminSettingPCGWPathExcludes, pathJSON); err != nil {
		Redirect(w, r, "/admin/settings?error=save_failed")
		return
	}
	autoVal := "false"
	if autoRun {
		autoVal = "true"
	}
	if err := h.store.SetAdminSetting(ctx, store.AdminSettingPCGWAutoRunFirstStart, autoVal); err != nil {
		Redirect(w, r, "/admin/settings?error=save_failed")
		return
	}

	if h.pcgwCron != nil {
		if err := h.pcgwCron.Reschedule(ctx); err != nil {
			logx.Logger().Error().Err(err).Msg("admin settings reschedule pcgw cron")
			Redirect(w, r, "/admin/settings?error=cron_reschedule_failed")
			return
		}
	}

	h.appendAuditBroadcast(ctx, userID, username, "admin_settings_save", "", "")
	Redirect(w, r, "/admin/settings?saved=1")
}
