//go:build darwin

package main

import (
	"runtime"
	"testing"
)

func TestAppBundleRoot(t *testing.T) {
	cases := []struct {
		exe  string
		want string
	}{
		{"/Applications/GSBS.app/Contents/MacOS/gsbs-client", "/Applications/GSBS.app"},
		{"/Users/u/Applications/GSBS.app/Contents/MacOS/gsbs-client", "/Users/u/Applications/GSBS.app"},
		{"/usr/local/bin/gsbs-client", ""},                  // bare binary, no bundle
		{"/tmp/Contents/MacOS/gsbs-client", ""},             // Contents/MacOS but no .app root
		{"/Applications/GSBS.app/Contents/gsbs-client", ""}, // not under MacOS/
	}
	for _, c := range cases {
		if got := appBundleRoot(c.exe); got != c.want {
			t.Errorf("appBundleRoot(%q) = %q, want %q", c.exe, got, c.want)
		}
	}
}

func TestExpectedClientAssetNameDarwin(t *testing.T) {
	want := "gsbs-client-darwin-" + runtime.GOARCH
	if got := expectedClientAssetName(); got != want {
		t.Errorf("expectedClientAssetName() = %q, want %q", got, want)
	}
}

// A newer release with no darwin binary (pre-4.2 releases) must offer a
// manual download instead of reporting a mismatch, so the tray can open the
// GitHub releases page.
func TestUpdateFromAssetNames_DarwinManualFallback(t *testing.T) {
	rel := &ghRelease{TagName: "v99.0.0"}
	rel.Assets = append(rel.Assets, struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	}{Name: "gsbs-client-windows-amd64.exe", BrowserDownloadURL: "https://example.invalid/x.exe"})

	res := updateFromAssetNames(rel)
	if res.Status != "manual_download" {
		t.Fatalf("Status = %q, want manual_download (%s)", res.Status, res.Message)
	}
	if res.Info == nil || !res.Info.Manual || res.Info.Tag != "v99.0.0" {
		t.Fatalf("Info = %+v, want Manual=true Tag=v99.0.0", res.Info)
	}
}
