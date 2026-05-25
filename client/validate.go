package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gsbs/gsbs/pkg/paths"
)

// ValidateConfig runs optional checks (server reachable, token valid, watch_paths resolvable) and returns warning messages.
// Run in background; do not block startup.
func ValidateConfig(cfg *config) []string {
	var warnings []string
	if cfg == nil {
		return warnings
	}
	if cfg.ServerURL == "" {
		return warnings
	}
	baseURL := strings.TrimSuffix(cfg.ServerURL, "/")

	// Server reachable (v2 preferred, v1 fallback)
	resp, err := pingManifestHealth(baseURL, cfg.Token)
	if err != nil {
		warnings = append(warnings, "Server unreachable: "+err.Error())
		return warnings
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && cfg.Token != "" {
		warnings = append(warnings, "Token may be expired or invalid (server returned 401).")
		return warnings
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		warnings = append(warnings, "Server returned "+resp.Status)
	}

	// Watch paths: check that path_templates can be resolved (optional)
	resolver := paths.NewResolver()
	resolver.UbisoftConnect = cfg.UbisoftConnectFolder
	resolver.GOGGalaxy = cfg.GOGGalaxyFolder
	resolver.EpicGames = cfg.EpicGamesFolder
	resolver.XboxApp = cfg.XboxAppFolder
	resolver.UserID = cfg.LauncherUserID
	currentOS := paths.CurrentOS()
	for i, wp := range cfg.WatchPaths {
		for _, t := range wp.PathTemplates {
			abs := resolver.Resolve(t, currentOS)
			if len(abs) == 0 || (len(abs) == 1 && abs[0] == "") {
				warnings = append(warnings, fmt.Sprintf("watch_paths[%d] path_template could not be resolved: %s", i, t))
				break
			}
		}
	}
	return warnings
}
