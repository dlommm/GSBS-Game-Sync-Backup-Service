package job

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
)

const (
	bundleFetchJobName = "pcgw_bundle_fetch"
	bundleHTTPTimeout  = 10 * time.Minute
	bundleUserAgent    = "GSBS/1.0 (manifest-bundle-fetch)"
)

var errBundleNotFound = errors.New("bundle not found")

// BundleFetchResult summarizes a remote bundle fetch run.
type BundleFetchResult struct {
	URL          string
	NotModified  bool
	ImportResult store.PCGWImportResult
	ETag         string
	// Versioned-index sync fields (zero when the legacy direct-URL path was used).
	Indexed       bool
	MergedVersion int // server's merged manifest version after this run
	LatestVersion int // latest manifest version offered by the index
	StepsApplied  int // number of full bundle imports applied this run
}

// PCGWBundleFetchOptions controls bundle fetch behavior.
type PCGWBundleFetchOptions struct {
	ForceFull bool
}

// PCGWBundleFetch downloads and imports the full manifest bundle. It prefers the
// versioned-index flow (index.json + monotonic version) for cheap change
// detection and falls back to fetching the full bundle URL directly only when the
// publisher has not published an index.json (HTTP 404). Either way the full bundle
// is merged; there are no deltas.
func PCGWBundleFetch(ctx context.Context, st store.Store, opts PCGWBundleFetchOptions) (BundleFetchResult, error) {
	settings, err := st.ListAdminSettings(ctx)
	if err != nil {
		return BundleFetchResult{}, err
	}
	if indexURL := store.PCGWBundleIndexURLFromSettings(settings); indexURL != "" {
		res, handled, idxErr := pcgwBundleFetchIndexed(ctx, st, settings, indexURL, opts)
		if handled {
			return res, idxErr
		}
		logx.Logger().Info().Str("index_url", indexURL).Msg("pcgw bundle index.json not found; using legacy bundle sync")
	}
	return pcgwBundleFetchLegacy(ctx, st, opts)
}

// pcgwBundleFetchLegacy fetches the full manifest bundle directly (no index.json)
// and merges it. It is used only when the publisher has not published an
// index.json. The conditional GET (If-None-Match) skips the download when the
// CDN reports the bundle is unchanged.
func pcgwBundleFetchLegacy(ctx context.Context, st store.Store, opts PCGWBundleFetchOptions) (BundleFetchResult, error) {
	settings, err := st.ListAdminSettings(ctx)
	if err != nil {
		return BundleFetchResult{}, err
	}

	url := store.PCGWBundleURLFromSettings(settings)
	storedETag := strings.TrimSpace(settings[store.AdminSettingPCGWBundleETag])
	if opts.ForceFull {
		storedETag = "" // force a download even if the ETag matches
	}
	data, respETag, notModified, fetchErr := fetchBundleHTTP(ctx, url, storedETag)
	if fetchErr != nil {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, fetchErr.Error())
		return BundleFetchResult{URL: url}, fetchErr
	}
	if notModified {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchedAt, nowRFC3339())
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, "")
		return BundleFetchResult{URL: url, NotModified: true, ETag: storedETag}, nil
	}

	return finishBundleImport(ctx, st, url, data, respETag)
}

func finishBundleImport(ctx context.Context, st store.Store, url string, data []byte, respETag string) (BundleFetchResult, error) {
	result, err := st.ImportPCGWManifestBundle(ctx, data, "merge_skip_unchanged")
	if err != nil {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, err.Error())
		return BundleFetchResult{URL: url}, err
	}

	now := nowRFC3339()
	if respETag != "" {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleETag, respETag)
	}
	if result.ExportedAt != "" {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastExportedAt, result.ExportedAt)
	}
	if result.FullExportedAt != "" {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleFullExportedAt, result.FullExportedAt)
	}
	_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchedAt, now)
	_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, "")

	return BundleFetchResult{
		URL:          url,
		ImportResult: result,
		ETag:         respETag,
	}, nil
}

func fetchBundleHTTP(ctx context.Context, url, ifNoneMatch string) ([]byte, string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", false, err
	}
	req.Header.Set("User-Agent", bundleUserAgent)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}

	client := &http.Client{Timeout: bundleHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, ifNoneMatch, true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", false, errBundleNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", false, fmt.Errorf("bundle fetch HTTP %d from %s", resp.StatusCode, url)
	}

	const maxBody = 256 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, "", false, err
	}
	etag := strings.TrimSpace(resp.Header.Get("ETag"))
	logx.Logger().Info().Str("url", url).Int("bytes", len(data)).Str("etag", etag).Msg("pcgw bundle fetched")
	return data, etag, false, nil
}
