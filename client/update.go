package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	defaultUpdateRepo = "dlommm/GSBS--Game-Sync---Backup-Service-"
	updateUserAgent   = "gsbs-client"
	maxUpdateBytes    = 128 * 1024 * 1024 // 128 MiB
)

// UpdateInfo describes an available client update from a GitHub release.
type UpdateInfo struct {
	Tag         string
	Version     string
	AssetName   string
	DownloadURL string
	SHA256      string
}

// UpdateCheckResult carries the outcome of CheckForUpdate.
// Status values: "available", "up_to_date", "disabled", "metered_skip",
// "network_error", "api_error", "manifest_mismatch", "unsupported_arch".
type UpdateCheckResult struct {
	Info    *UpdateInfo // non-nil only when Status == "available"
	Status  string
	Message string // human-readable detail for logs/UI
}

// ClientManifest is the latest-client.json release asset.
type ClientManifest struct {
	Version    string                         `json:"version"`
	ReleasedAt string                         `json:"released_at"`
	Assets     map[string]ClientManifestAsset `json:"assets"`
}

// ClientManifestAsset describes one platform binary in the manifest.
type ClientManifestAsset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// goosForUpdate returns the OS string used for update-eligibility checks.
// Tests may override this to simulate a supported platform on CI.
var goosForUpdate = func() string { return runtime.GOOS }

func updatePlatformKey() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if v == "" || v == "dev" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return ""
	}
	return v
}

func isNewerVersion(current, latest string) bool {
	cur := normalizeVersion(current)
	lat := normalizeVersion(latest)
	if cur == "" || lat == "" {
		return false
	}
	return semver.Compare(lat, cur) > 0
}

// CheckForUpdate compares the embedded Version against the latest GitHub release.
// manual=true bypasses the metered-connection skip but still honours update_check_enabled=false.
// The returned Status is always set; Info is non-nil only when Status=="available".
func CheckForUpdate(repo string, manual bool) UpdateCheckResult {
	if repo == "" {
		repo = defaultUpdateRepo
	}

	if goos := goosForUpdate(); goos != "windows" && goos != "linux" {
		return UpdateCheckResult{
			Status:  "unsupported_arch",
			Message: fmt.Sprintf("auto-update not supported on %s/%s", runtime.GOOS, runtime.GOARCH),
		}
	}

	current := normalizeVersion(Version)
	if current == "" {
		return UpdateCheckResult{
			Status:  "unsupported_arch",
			Message: "dev or unknown build version; update check skipped",
		}
	}

	cfg, _ := loadConfig()
	if cfg != nil && cfg.UpdateCheckEnabled != nil && !*cfg.UpdateCheckEnabled {
		return UpdateCheckResult{Status: "disabled", Message: "update checks are disabled (update_check_enabled=false)"}
	}
	if !manual && cfg != nil && cfg.SkipSyncWhenMetered && IsMeteredConnection() {
		return UpdateCheckResult{Status: "metered_skip", Message: "update check skipped on metered connection"}
	}

	rel, err := fetchLatestRelease(repo)
	if err != nil {
		log.Printf("update: release fetch: %v", err)
		status := "network_error"
		if strings.Contains(err.Error(), "github API status") {
			status = "api_error"
		}
		return UpdateCheckResult{Status: status, Message: err.Error()}
	}
	if rel.TagName == "" {
		return UpdateCheckResult{Status: "api_error", Message: "empty release tag in GitHub API response"}
	}
	if !isNewerVersion(Version, rel.TagName) {
		return UpdateCheckResult{
			Status:  "up_to_date",
			Message: fmt.Sprintf("current %s is not older than latest %s", Version, rel.TagName),
		}
	}

	res := updateFromManifest(rel)
	if res.Status != "" {
		return res
	}
	return updateFromAssetNames(rel)
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	}
}

func fetchLatestRelease(repo string) (*ghRelease, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", updateUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// updateFromManifest tries the latest-client.json manifest approach.
// Returns a result with Status=="" when the manifest is not present or our
// platform is not listed, signalling that the caller should fall through to
// updateFromAssetNames. Any other non-empty Status is final.
func updateFromManifest(rel *ghRelease) UpdateCheckResult {
	var manifestURL string
	for _, a := range rel.Assets {
		if a.Name == "latest-client.json" {
			manifestURL = a.BrowserDownloadURL
			break
		}
	}
	if manifestURL == "" {
		log.Printf("update: latest-client.json not found in release assets")
		return UpdateCheckResult{} // fall through to asset-name heuristic
	}

	client := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, manifestURL, nil)
	req.Header.Set("User-Agent", updateUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("update: manifest download: %v", err)
		return UpdateCheckResult{Status: "network_error", Message: "manifest download: " + err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("manifest download status %d", resp.StatusCode)
		log.Printf("update: %s", msg)
		return UpdateCheckResult{Status: "api_error", Message: msg}
	}

	var manifest ClientManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		log.Printf("update: manifest parse: %v", err)
		return UpdateCheckResult{Status: "api_error", Message: "manifest parse: " + err.Error()}
	}
	if !isNewerVersion(Version, manifest.Version) {
		return UpdateCheckResult{
			Status:  "up_to_date",
			Message: fmt.Sprintf("manifest version %s is not newer than current %s", manifest.Version, Version),
		}
	}

	platformKey := updatePlatformKey()
	asset, ok := manifest.Assets[platformKey]
	if !ok || asset.Name == "" {
		log.Printf("update: platform key %s not found in manifest", platformKey)
		return UpdateCheckResult{} // fall through; asset-name heuristic may still work
	}

	dlURL := ""
	for _, a := range rel.Assets {
		if a.Name == asset.Name {
			dlURL = a.BrowserDownloadURL
			break
		}
	}
	if dlURL == "" {
		log.Printf("update: asset %s not found in release", asset.Name)
		return UpdateCheckResult{} // fall through
	}

	return UpdateCheckResult{
		Status: "available",
		Info: &UpdateInfo{
			Tag:         rel.TagName,
			Version:     manifest.Version,
			AssetName:   asset.Name,
			DownloadURL: dlURL,
			SHA256:      strings.ToLower(asset.SHA256),
		},
	}
}

func expectedClientAssetName() string {
	switch goosForUpdate() {
	case "windows":
		return "gsbs-client-windows-amd64.exe"
	case "linux":
		return "gsbs-client-linux-amd64"
	default:
		return ""
	}
}

func updateFromAssetNames(rel *ghRelease) UpdateCheckResult {
	want := expectedClientAssetName()
	if want == "" {
		return UpdateCheckResult{
			Status:  "unsupported_arch",
			Message: fmt.Sprintf("no expected asset name for %s/%s", runtime.GOOS, runtime.GOARCH),
		}
	}
	for _, a := range rel.Assets {
		if a.Name == want {
			return UpdateCheckResult{
				Status: "available",
				Info: &UpdateInfo{
					Tag:         rel.TagName,
					Version:     strings.TrimPrefix(rel.TagName, "v"),
					AssetName:   a.Name,
					DownloadURL: a.BrowserDownloadURL,
				},
			}
		}
	}
	log.Printf("update: asset %s not found in release %s", want, rel.TagName)
	return UpdateCheckResult{
		Status:  "manifest_mismatch",
		Message: fmt.Sprintf("asset %s not found in release %s", want, rel.TagName),
	}
}

// DownloadUpdate saves the release asset under ClientDataDir()/updates/.
func DownloadUpdate(info *UpdateInfo) (string, error) {
	if info == nil || info.DownloadURL == "" {
		return "", fmt.Errorf("invalid update info")
	}
	if info.SHA256 == "" {
		return "", fmt.Errorf("update manifest is missing SHA256 checksum; refusing to apply unverified update")
	}
	dir := filepath.Join(ClientDataDir(), "updates")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, info.AssetName)
	tmp := dest + ".part"

	client := &http.Client{Timeout: 10 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, info.DownloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", updateUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download status %d", resp.StatusCode)
	}
	if resp.ContentLength > maxUpdateBytes {
		return "", fmt.Errorf("update too large (%d bytes)", resp.ContentLength)
	}

	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	w := io.MultiWriter(out, hasher)
	n, err := io.Copy(w, io.LimitReader(resp.Body, maxUpdateBytes+1))
	if closeErr := out.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if n > maxUpdateBytes {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("update exceeded size limit")
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	if info.SHA256 != "" && !strings.EqualFold(sum, info.SHA256) {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("checksum mismatch")
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	log.Printf("update: downloaded %s (%d bytes)", dest, n)
	return dest, nil
}

// ApplyUpdate replaces the running binary and restarts the client.
func ApplyUpdate(stagedPath string) error {
	return applyUpdatePlatform(stagedPath)
}

// ParseApplyUpdateFlag returns the path from --apply-update=PATH if present.
func ParseApplyUpdateFlag() string {
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--apply-update=") {
			return strings.TrimPrefix(a, "--apply-update=")
		}
	}
	return ""
}

// RunApplyUpdateMode handles early startup when applying a staged update on Linux.
func RunApplyUpdateMode(stagedPath string) error {
	return applyStagedBinary(stagedPath)
}
