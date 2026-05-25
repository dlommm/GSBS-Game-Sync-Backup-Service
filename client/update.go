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

// CheckForUpdate compares embedded Version against the latest GitHub release.
// Returns nil when up to date or the check fails silently.
func CheckForUpdate(repo string) *UpdateInfo {
	if repo == "" {
		repo = defaultUpdateRepo
	}
	current := normalizeVersion(Version)
	if current == "" {
		return nil
	}
	if shouldSkipUpdateCheck() {
		return nil
	}

	rel, err := fetchLatestRelease(repo)
	if err != nil {
		log.Printf("update: release fetch: %v", err)
		return nil
	}
	if rel.TagName == "" {
		return nil
	}
	if !isNewerVersion(Version, rel.TagName) {
		return nil
	}

	if info := updateFromManifest(rel); info != nil {
		return info
	}
	return updateFromAssetNames(rel)
}

func shouldSkipUpdateCheck() bool {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		return true
	}
	cfg, err := loadConfig()
	if err != nil || cfg == nil {
		return false
	}
	if cfg.UpdateCheckEnabled != nil && !*cfg.UpdateCheckEnabled {
		return true
	}
	if cfg.SkipSyncWhenMetered && IsMeteredConnection() {
		return true
	}
	return false
}

type ghRelease struct {
	TagName string
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

func updateFromManifest(rel *ghRelease) *UpdateInfo {
	var manifestURL string
	for _, a := range rel.Assets {
		if a.Name == "latest-client.json" {
			manifestURL = a.BrowserDownloadURL
			break
		}
	}
	if manifestURL == "" {
		return nil
	}
	client := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, manifestURL, nil)
	req.Header.Set("User-Agent", updateUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("update: manifest download: %v", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var manifest ClientManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		log.Printf("update: manifest parse: %v", err)
		return nil
	}
	if !isNewerVersion(Version, manifest.Version) {
		return nil
	}
	asset, ok := manifest.Assets[updatePlatformKey()]
	if !ok || asset.Name == "" {
		return nil
	}
	dlURL := ""
	for _, a := range rel.Assets {
		if a.Name == asset.Name {
			dlURL = a.BrowserDownloadURL
			break
		}
	}
	if dlURL == "" {
		return nil
	}
	return &UpdateInfo{
		Tag:         rel.TagName,
		Version:     manifest.Version,
		AssetName:   asset.Name,
		DownloadURL: dlURL,
		SHA256:      strings.ToLower(asset.SHA256),
	}
}

func expectedClientAssetName() string {
	switch runtime.GOOS {
	case "windows":
		return "gsbs-client-windows-amd64.exe"
	case "linux":
		return "gsbs-client-linux-amd64"
	default:
		return ""
	}
}

func updateFromAssetNames(rel *ghRelease) *UpdateInfo {
	want := expectedClientAssetName()
	if want == "" {
		return nil
	}
	for _, a := range rel.Assets {
		if a.Name == want {
			return &UpdateInfo{
				Tag:         rel.TagName,
				Version:     strings.TrimPrefix(rel.TagName, "v"),
				AssetName:   a.Name,
				DownloadURL: a.BrowserDownloadURL,
			}
		}
	}
	return nil
}

// DownloadUpdate saves the release asset under ClientDataDir()/updates/.
func DownloadUpdate(info *UpdateInfo) (string, error) {
	if info == nil || info.DownloadURL == "" {
		return "", fmt.Errorf("invalid update info")
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
