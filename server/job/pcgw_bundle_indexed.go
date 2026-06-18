package job

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
)

// pcgwBundleFetchIndexed implements the versioned-index sync flow.
//
// It fetches index.json (the small atomic pointer), compares the server's merged
// version to the latest version it advertises, and — when behind — fetches and
// merges the current full bundle. The downloaded artifact is SHA256-verified
// against the index before import, and the merged version is advanced only after
// a successful import, so an interrupted run resumes correctly on the next pass.
//
// handled is false only when index.json is absent (HTTP 404), signalling the
// caller to fall back to the direct full-bundle flow. All other outcomes (304,
// no-op, success, or a hard error) return handled=true.
func pcgwBundleFetchIndexed(ctx context.Context, st store.Store, settings map[string]string, indexURL string, opts PCGWBundleFetchOptions) (BundleFetchResult, bool, error) {
	storedIndexETag := strings.TrimSpace(settings[store.AdminSettingPCGWBundleIndexETag])
	merged := parseVersion(settings[store.AdminSettingPCGWBundleMergedVersion])

	idxData, idxETag, notModified, err := fetchBundleHTTP(ctx, indexURL, storedIndexETag)
	if err != nil {
		if errors.Is(err, errBundleNotFound) {
			return BundleFetchResult{}, false, nil // fall back to legacy
		}
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, err.Error())
		return BundleFetchResult{Indexed: true, URL: indexURL, MergedVersion: merged}, true, err
	}

	if notModified && !opts.ForceFull {
		// Index unchanged since last fetch; nothing new to merge.
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchedAt, nowRFC3339())
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, "")
		return BundleFetchResult{Indexed: true, URL: indexURL, NotModified: true, MergedVersion: merged, LatestVersion: merged}, true, nil
	}

	idx, err := store.ParsePCGWBundleIndex(idxData)
	if err != nil {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, err.Error())
		return BundleFetchResult{Indexed: true, URL: indexURL, MergedVersion: merged}, true, err
	}

	// ForceFull re-baselines from scratch by planning as if nothing is merged.
	planFrom := merged
	if opts.ForceFull {
		planFrom = 0
	}
	steps := store.PlanBundleCatchup(planFrom, idx)

	result := BundleFetchResult{
		Indexed: true, URL: indexURL, ETag: idxETag,
		MergedVersion: merged, LatestVersion: idx.ManifestVersion,
	}

	if len(steps) == 0 {
		// Already current: persist the index ETag and latest version, done.
		if idxETag != "" {
			_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleIndexETag, idxETag)
		}
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLatestVersion, strconv.Itoa(idx.ManifestVersion))
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchedAt, nowRFC3339())
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, "")
		logx.Logger().Info().Int("version", merged).Msg("pcgw bundle already current")
		return result, true, nil
	}

	logx.Logger().Info().
		Int("from_version", merged).Int("to_version", idx.ManifestVersion).Int("steps", len(steps)).
		Msg("pcgw bundle catch-up starting")

	for _, step := range steps {
		data, _, _, ferr := fetchBundleHTTP(ctx, step.URL, "")
		if ferr != nil {
			_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, ferr.Error())
			result.MergedVersion = merged
			return result, true, fmt.Errorf("fetch %s bundle v%d: %w", step.Kind, step.Version, ferr)
		}
		if step.SHA256 != "" {
			if got := sha256Hex(data); !strings.EqualFold(got, step.SHA256) {
				err := fmt.Errorf("%s bundle v%d sha256 mismatch: got %s want %s", step.Kind, step.Version, got, step.SHA256)
				_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, err.Error())
				result.MergedVersion = merged
				return result, true, err
			}
		}
		importRes, ierr := st.ImportPCGWManifestBundle(ctx, data, step.Mode)
		if ierr != nil {
			_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, ierr.Error())
			result.MergedVersion = merged
			return result, true, fmt.Errorf("import %s bundle v%d: %w", step.Kind, step.Version, ierr)
		}
		// Advance the merged version only after a successful import so an
		// interruption mid-plan resumes from the right place next run.
		merged = step.Version
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleMergedVersion, strconv.Itoa(merged))
		result.ImportResult = importRes
		result.StepsApplied++
		logx.Logger().Info().
			Str("kind", step.Kind).Int("version", merged).Int("rows_changed", importRes.RowsChanged).
			Msg("pcgw bundle step applied")
	}

	if idxETag != "" {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleIndexETag, idxETag)
	}
	result.MergedVersion = merged
	_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLatestVersion, strconv.Itoa(idx.ManifestVersion))
	_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchedAt, nowRFC3339())
	_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, "")
	return result, true, nil
}

func parseVersion(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }
