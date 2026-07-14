package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// overrideVersion temporarily sets Version to a known value for tests.
func overrideVersion(t *testing.T, v string) {
	t.Helper()
	orig := Version
	Version = v
	t.Cleanup(func() { Version = orig })
}

// overrideGOOS makes the update eligibility check treat the process as running
// on a supported platform, necessary when tests run on darwin/arm64.
func overrideGOOS(t *testing.T, goos string) {
	t.Helper()
	orig := goosForUpdate
	goosForUpdate = func() string { return goos }
	t.Cleanup(func() { goosForUpdate = orig })
}

// overrideGOARCH mirrors overrideGOOS for the architecture half of the
// platform key / expected asset name.
func overrideGOARCH(t *testing.T, goarch string) {
	t.Helper()
	orig := goarchForUpdate
	goarchForUpdate = func() string { return goarch }
	t.Cleanup(func() { goarchForUpdate = orig })
}

// ghReleasePayload builds a minimal GitHub releases/latest JSON response.
func ghReleasePayload(tag string, assets []map[string]string) map[string]any {
	assetList := make([]map[string]any, 0, len(assets))
	for _, a := range assets {
		assetList = append(assetList, map[string]any{
			"name":                 a["name"],
			"browser_download_url": a["url"],
		})
	}
	return map[string]any{
		"tag_name": tag,
		"assets":   assetList,
	}
}

// installTestTransport replaces http.DefaultTransport for the duration of the
// test, redirecting all requests to the given httptest.Server. It uses the
// captured original transport for the actual TCP hop so there is no recursion.
func installTestTransport(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// Rewrite host to the test server; keep path/query intact.
		req.URL.Host = srv.Listener.Addr().String()
		req.URL.Scheme = "http"
		return orig.RoundTrip(req)
	})
	t.Cleanup(func() { http.DefaultTransport = orig })
}

// TestCheckForUpdate_APIError verifies that a 404 from the GitHub API returns
// Status=="api_error" with a non-empty Message.
func TestCheckForUpdate_APIError(t *testing.T) {
	overrideVersion(t, "1.0.0")
	overrideGOOS(t, "linux")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	installTestTransport(t, srv)

	result := CheckForUpdate("testowner/testrepo", false)

	if result.Status != "api_error" {
		t.Errorf("expected Status=api_error, got %q (message: %s)", result.Status, result.Message)
	}
	if result.Message == "" {
		t.Error("expected non-empty Message for api_error")
	}
	if result.Info != nil {
		t.Error("expected Info==nil for api_error")
	}
}

// TestCheckForUpdate_ManifestMissing verifies that a release with no
// latest-client.json asset (and no matching platform asset) offers a manual
// download — a newer release exists, the tray just can't verify a binary.
func TestCheckForUpdate_ManifestMissing(t *testing.T) {
	overrideVersion(t, "1.0.0")
	overrideGOOS(t, "linux")

	// Release has a newer tag but no latest-client.json and no platform asset.
	releasePayload := ghReleasePayload("v9.9.9", []map[string]string{
		{"name": "some-other-file.txt", "url": "http://example.com/some-other-file.txt"},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(releasePayload)
	}))
	defer srv.Close()
	installTestTransport(t, srv)

	result := CheckForUpdate("testowner/testrepo", false)

	if result.Status != "manual_download" {
		t.Errorf("expected Status=manual_download, got %q (message: %s)", result.Status, result.Message)
	}
	if result.Info == nil || !result.Info.Manual {
		t.Error("expected Info.Manual==true for manual_download")
	}
}

// A release that carries the platform binary but no latest-client.json must
// NOT report "available": DownloadUpdate refuses assets without a checksum,
// so that Install button would be a dead end. Manual download instead.
func TestCheckForUpdate_AssetWithoutManifestIsManual(t *testing.T) {
	overrideVersion(t, "1.0.0")
	overrideGOOS(t, "linux")
	overrideGOARCH(t, "amd64")

	releasePayload := ghReleasePayload("v9.9.9", []map[string]string{
		{"name": "gsbs-client-linux-amd64", "url": "http://example.com/gsbs-client-linux-amd64"},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(releasePayload)
	}))
	defer srv.Close()
	installTestTransport(t, srv)

	result := CheckForUpdate("testowner/testrepo", false)

	if result.Status != "manual_download" {
		t.Errorf("expected Status=manual_download, got %q (message: %s)", result.Status, result.Message)
	}
	if result.Info == nil || !result.Info.Manual {
		t.Error("expected Info.Manual==true when the asset has no manifest checksum")
	}
}

// expectedClientAssetName must be architecture-aware: linux/arm64 ships its
// own binary and must never be offered the amd64 asset.
func TestExpectedClientAssetName_ArchAware(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "gsbs-client-linux-amd64"},
		{"linux", "arm64", "gsbs-client-linux-arm64"},
		{"windows", "amd64", "gsbs-client-windows-amd64.exe"},
		{"darwin", "arm64", "gsbs-client-darwin-arm64"},
		{"darwin", "amd64", "gsbs-client-darwin-amd64"},
	}
	for _, tc := range cases {
		overrideGOOS(t, tc.goos)
		overrideGOARCH(t, tc.goarch)
		if got := expectedClientAssetName(); got != tc.want {
			t.Errorf("expectedClientAssetName(%s/%s) = %q, want %q", tc.goos, tc.goarch, got, tc.want)
		}
		if got := updatePlatformKey(); got != tc.goos+"-"+tc.goarch {
			t.Errorf("updatePlatformKey(%s/%s) = %q", tc.goos, tc.goarch, got)
		}
	}
}

// TestCheckForUpdate_UpToDate verifies that a release with an older tag
// returns Status=="up_to_date".
func TestCheckForUpdate_UpToDate(t *testing.T) {
	overrideVersion(t, "2.0.0")
	overrideGOOS(t, "linux")

	releasePayload := ghReleasePayload("v1.0.0", nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(releasePayload)
	}))
	defer srv.Close()
	installTestTransport(t, srv)

	result := CheckForUpdate("testowner/testrepo", false)

	if result.Status != "up_to_date" {
		t.Errorf("expected Status=up_to_date, got %q", result.Status)
	}
}

// roundTripFunc is an http.RoundTripper backed by a plain function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	return f(clone)
}
