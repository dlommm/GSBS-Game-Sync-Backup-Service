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
	Delta        bool
	NotModified  bool
	ImportResult store.PCGWImportResult
	ETag         string
}

// PCGWBundleFetchOptions controls bundle fetch behavior.
type PCGWBundleFetchOptions struct {
	ForceFull bool
}

// PCGWBundleFetch downloads and imports a manifest bundle from a remote URL.
func PCGWBundleFetch(ctx context.Context, st store.Store, opts PCGWBundleFetchOptions) (BundleFetchResult, error) {
	settings, err := st.ListAdminSettings(ctx)
	if err != nil {
		return BundleFetchResult{}, err
	}

	seeded, err := st.IsPCGWBundleSeeded(ctx)
	if err != nil {
		return BundleFetchResult{}, err
	}

	useDelta := !opts.ForceFull && seeded
	url := store.PCGWBundleURLFromSettings(settings)
	etagKey := store.AdminSettingPCGWBundleETag
	importMode := "merge_skip_unchanged"
	if useDelta {
		url = store.PCGWBundleDeltaURLFromSettings(settings)
		etagKey = store.AdminSettingPCGWBundleDeltaETag
		importMode = "delta"
		if ok, err := bundleDeltaApplicable(ctx, st, settings); err != nil {
			logx.Logger().Warn().Err(err).Msg("pcgw bundle meta check failed; falling back to full")
			useDelta = false
			url = store.PCGWBundleURLFromSettings(settings)
			etagKey = store.AdminSettingPCGWBundleETag
			importMode = "merge_skip_unchanged"
		} else if !ok {
			logx.Logger().Info().Msg("pcgw bundle delta gap detected; falling back to full")
			useDelta = false
			url = store.PCGWBundleURLFromSettings(settings)
			etagKey = store.AdminSettingPCGWBundleETag
			importMode = "merge_skip_unchanged"
		}
	}
	if opts.ForceFull {
		url = store.PCGWBundleURLFromSettings(settings)
		etagKey = store.AdminSettingPCGWBundleETag
		importMode = "merge_skip_unchanged"
		useDelta = false
	}

	storedETag := strings.TrimSpace(settings[etagKey])
	data, respETag, notModified, fetchErr := fetchBundleHTTP(ctx, url, storedETag)
	if fetchErr != nil {
		if useDelta && errors.Is(fetchErr, errBundleNotFound) {
			return fetchAndImportFull(ctx, st, settings, opts)
		}
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, fetchErr.Error())
		return BundleFetchResult{URL: url, Delta: useDelta}, fetchErr
	}
	if notModified {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchedAt, time.Now().UTC().Format(time.RFC3339))
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, "")
		return BundleFetchResult{URL: url, Delta: useDelta, NotModified: true, ETag: storedETag}, nil
	}

	return finishBundleImport(ctx, st, url, etagKey, useDelta, importMode, data, respETag)
}

func fetchAndImportFull(ctx context.Context, st store.Store, settings map[string]string, opts PCGWBundleFetchOptions) (BundleFetchResult, error) {
	url := store.PCGWBundleURLFromSettings(settings)
	storedETag := strings.TrimSpace(settings[store.AdminSettingPCGWBundleETag])
	data, respETag, notModified, err := fetchBundleHTTP(ctx, url, storedETag)
	if err != nil {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, err.Error())
		return BundleFetchResult{URL: url}, err
	}
	if notModified {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchedAt, time.Now().UTC().Format(time.RFC3339))
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, "")
		return BundleFetchResult{URL: url, NotModified: true, ETag: storedETag}, nil
	}
	return finishBundleImport(ctx, st, url, store.AdminSettingPCGWBundleETag, false, "merge_skip_unchanged", data, respETag)
}

func finishBundleImport(ctx context.Context, st store.Store, url, etagKey string, useDelta bool, importMode string, data []byte, respETag string) (BundleFetchResult, error) {
	result, err := st.ImportPCGWManifestBundle(ctx, data, importMode)
	if err != nil {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, err.Error())
		return BundleFetchResult{URL: url, Delta: useDelta}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if respETag != "" {
		_ = st.SetAdminSetting(ctx, etagKey, respETag)
	}
	if result.ExportedAt != "" {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastExportedAt, result.ExportedAt)
	}
	if !useDelta && result.FullExportedAt != "" {
		_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleFullExportedAt, result.FullExportedAt)
	} else if useDelta && result.FullExportedAt != "" {
		// Preserve anchor from delta bundle metadata when server has none yet.
		settings, _ := st.ListAdminSettings(ctx)
		if strings.TrimSpace(settings[store.AdminSettingPCGWBundleFullExportedAt]) == "" {
			_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleFullExportedAt, result.FullExportedAt)
		}
	}
	_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchedAt, now)
	_ = st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastFetchError, "")

	return BundleFetchResult{
		URL:          url,
		Delta:        useDelta,
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

func bundleDeltaApplicable(ctx context.Context, st store.Store, settings map[string]string) (bool, error) {
	metaURL := store.PCGWBundleMetaURLFromSettings(settings)
	if metaURL == "" {
		return false, fmt.Errorf("empty bundle meta URL")
	}
	data, _, _, err := fetchBundleHTTP(ctx, metaURL, "")
	if err != nil {
		return false, err
	}
	meta, err := store.ParsePCGWBundleMeta(data)
	if err != nil {
		return false, err
	}
	lastExported := strings.TrimSpace(settings[store.AdminSettingPCGWBundleLastExportedAt])
	anchor := strings.TrimSpace(meta.FullExportedAt)
	return store.CanApplyRemoteDelta(lastExported, anchor, meta), nil
}
