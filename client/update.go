package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// CheckForUpdate compares embedded Version against GitHub latest release tag.
// Returns newer version string or empty if up to date or check failed.
func CheckForUpdate(repo string) string {
	if repo == "" {
		repo = "dlomm/GSBS-Game-Sync-Backup-Service"
	}
	client := &http.Client{Timeout: 15 * time.Second}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gsbs-client")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if json.NewDecoder(resp.Body).Decode(&rel) != nil || rel.TagName == "" {
		return ""
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	current := strings.TrimPrefix(Version, "v")
	if latest != "" && latest != current && latest > current {
		return rel.TagName
	}
	return ""
}
