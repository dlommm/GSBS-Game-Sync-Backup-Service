package main

// DetectedLauncherPaths holds paths that can be auto-detected (Windows).
type DetectedLauncherPaths struct {
	UbisoftConnect string `json:"ubisoft_connect_folder,omitempty"`
	GOGGalaxy      string `json:"gog_galaxy_folder,omitempty"`
	EpicGames      string `json:"epic_games_folder,omitempty"`
	XboxApp        string `json:"xbox_app_folder,omitempty"`
}
