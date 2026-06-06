package paths

import (
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var (
	steamLoginUserIDRe = regexp.MustCompile(`"(7656119\d+)"`)
	steamMostRecentRe  = regexp.MustCompile(`"MostRecent"\s+"1"`)
)

// DetectSteamUserID returns the SteamID64 of the most recently used Steam account,
// read from config/loginusers.vdf under the given Steam library roots.
// Falls back to the first account found, then to a single userdata folder when VDF is missing.
func DetectSteamUserID(steamLibraries []string) string {
	for _, root := range steamLibraries {
		if root == "" {
			continue
		}
		vdfPath := filepath.Join(root, "config", "loginusers.vdf")
		data, err := os.ReadFile(vdfPath)
		if err != nil {
			continue
		}
		if id := parseLoginUsersVDF(string(data)); id != "" {
			return id
		}
	}
	for _, root := range steamLibraries {
		if id := detectSteamUserFromUserdata(root); id != "" {
			return id
		}
	}
	return ""
}

func parseLoginUsersVDF(text string) string {
	locs := steamLoginUserIDRe.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		return ""
	}
	fallback := text[locs[0][2]:locs[0][3]]
	for i, loc := range locs {
		id := text[loc[2]:loc[3]]
		start := loc[0]
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		if steamMostRecentRe.MatchString(text[start:end]) {
			return id
		}
	}
	return fallback
}

func detectSteamUserFromUserdata(steamRoot string) string {
	userdata := filepath.Join(steamRoot, "userdata")
	entries, err := os.ReadDir(userdata)
	if err != nil {
		return ""
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if steamLoginUserIDRe.MatchString(`"` + name + `"`) {
			ids = append(ids, name)
		}
	}
	if len(ids) == 1 {
		return ids[0]
	}
	if len(ids) > 1 {
		// When multiple accounts exist and no MostRecent flag was found in the VDF,
		// pick the account whose userdata/<id> directory was most recently modified.
		var bestID string
		var bestTime time.Time
		for _, id := range ids {
			info, err := os.Stat(filepath.Join(userdata, id))
			if err != nil {
				continue
			}
			if info.ModTime().After(bestTime) {
				bestTime = info.ModTime()
				bestID = id
			}
		}
		return bestID
	}
	return ""
}
