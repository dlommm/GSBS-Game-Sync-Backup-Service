package webui

import (
	"context"
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
	PCGWCron                  string
	PCGWCronSource            string
	PCGWCronDisabled          bool
	PCGWCronEnvOverride       bool
	PCGWCronNextRun           string
	PCGWTitleExcludesJSON     string
	PCGWPathExcludesJSON      string
	AutoRunFirstStart         bool
	LegacyPushProtection      bool
	PCGWSyncSource            string
	PCGWBundleCron            string
	PCGWBundleURL             string
	PCGWBundleIncrementalFB   bool
	PCGWSyncSourceEnvOverride bool
	PCGWBundleCronEnvOverride bool
	PCGWBundleCronNextRun     string
	CoverCacheCount           int
	CoverRoot                 string
	CoversCleared             bool
}

func (h *WebHandler) serveAdminSettings(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	settings, _ := h.store.ListAdminSettings(ctx)

	data := adminSettingsData{
		PageData:                h.adminPageData(w, r, userID, username, "settings", "admin_settings"),
		PCGWTitleExcludesJSON:   settings[store.AdminSettingPCGWTitleExcludes],
		PCGWPathExcludesJSON:    settings[store.AdminSettingPCGWPathExcludes],
		PCGWSyncSource:          store.PCGWSyncSourceFromSettings(settings),
		PCGWBundleCron:          store.PCGWBundleCronFromSettings(settings),
		PCGWBundleURL:           store.PCGWBundleURLFromSettings(settings),
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
	data.LegacyPushProtection = store.LegacyPushProtectionFromSettings(settings)

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

	data.CoverCacheCount = h.coverCacheCount()
	data.CoverRoot = h.coverRoot
	data.CoversCleared = r.URL.Query().Get("ok") == "covers_cleared"

	h.render(w, "admin_settings.html", data)
}

// handleAdminChooseSource records the first-run choice of save-location source
// (prebuilt bundle vs live PCGW API) from the admin onboarding card and kicks
// off the matching initial sync. The card disappears once a source is set.
func (h *WebHandler) handleAdminChooseSource(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	if _, _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	if _, pinned := os.LookupEnv(store.EnvPCGWSyncSource); pinned {
		Redirect(w, r, "/admin")
		return
	}
	source := normalizeSyncSourceForm(r.FormValue("source"))
	if err := h.store.SetAdminSetting(r.Context(), store.AdminSettingPCGWSyncSource, source); err != nil {
		logx.Logger().Error().Err(err).Msg("admin choose-source save")
		Redirect(w, r, "/admin?error=save_failed")
		return
	}
	// Manual mode is manual-only by default: disable the scheduled PCGW crawl so
	// the operator triggers syncs on demand (unless pinned by env). The admin can
	// re-enable a schedule from Settings afterward.
	if source == store.PCGWSyncSourceAPI {
		if _, pinned := os.LookupEnv("GSBS_PCGW_CRON"); !pinned {
			_ = h.store.SetAdminSetting(r.Context(), store.AdminSettingPCGWCron, "")
		}
	}
	// Apply the new source to the cron schedule (bundle fetch vs. none).
	if h.pcgwCron != nil {
		if err := h.pcgwCron.Reschedule(r.Context()); err != nil {
			logx.Logger().Error().Err(err).Msg("admin choose-source reschedule")
		}
	}
	// Seed the local mirror from the prebuilt S3 bundle for BOTH modes — this
	// avoids an initial multi-day PCGW crawl and keeps API load off PCGW. In S3
	// mode the bundle is then refreshed on the bundle cron; in manual mode the
	// admin triggers any further API syncs by hand. A best-effort fetch that fails
	// (unpublished/unreachable bundle) falls back to an API sync automatically.
	if h.jobRunner != nil {
		_, _ = h.jobRunner.TryRunPCGWBundleFetch(context.Background(), true)
	}
	Redirect(w, r, "/admin?source_set=1")
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

	seedFromBundle := false
	if _, ok := os.LookupEnv(store.EnvPCGWSyncSource); !ok {
		prevSettings, _ := h.store.ListAdminSettings(ctx)
		prevSource := store.PCGWSyncSourceFromSettings(prevSettings)
		syncSource := normalizeSyncSourceForm(r.FormValue("pcgw_sync_source"))
		if err := h.store.SetAdminSetting(ctx, store.AdminSettingPCGWSyncSource, syncSource); err != nil {
			Redirect(w, r, "/admin/settings?error=save_failed")
			return
		}
		// Switching INTO manual mode seeds the mirror once from the S3 bundle so
		// the operator doesn't trigger a full PCGW crawl just to get current data,
		// and disables the scheduled crawl (manual-only by default; the cron field
		// above is honored on subsequent saves if the admin re-enables it).
		if syncSource == store.PCGWSyncSourceAPI && prevSource != store.PCGWSyncSourceAPI {
			seedFromBundle = true
			if _, pinned := os.LookupEnv("GSBS_PCGW_CRON"); !pinned {
				_ = h.store.SetAdminSetting(ctx, store.AdminSettingPCGWCron, "")
			}
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
	legacyGuardVal := "false"
	if r.FormValue("legacy_push_protection") == "1" {
		legacyGuardVal = "true"
	}
	if err := h.store.SetAdminSetting(ctx, store.AdminSettingLegacyPushProtection, legacyGuardVal); err != nil {
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

	// Seed once from the S3 bundle when the operator just switched into manual
	// mode (best-effort, background). See handleAdminChooseSource for rationale.
	if seedFromBundle && h.jobRunner != nil {
		_, _ = h.jobRunner.TryRunPCGWBundleFetch(context.Background(), true)
	}

	h.appendAuditBroadcast(ctx, userID, username, "admin_settings_save", "", "")
	Redirect(w, r, "/admin/settings?saved=1")
}

// normalizeSyncSourceForm maps a sync-source form value to its canonical stored
// form. Anything that isn't explicitly "api" (manual) — including "s3", the
// legacy "github", or an empty/unknown value — resolves to the S3 bundle source.
func normalizeSyncSourceForm(v string) string {
	if strings.TrimSpace(strings.ToLower(v)) == store.PCGWSyncSourceAPI {
		return store.PCGWSyncSourceAPI
	}
	return store.PCGWSyncSourceS3
}
