package paths

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// steamAppsDir is the name of the Steam apps directory (case varies by OS).
const steamAppsDir = "steamapps"

// libraryfoldersVDFName is the filename for library folders config.
const libraryfoldersVDFName = "libraryfolders.vdf"

// parseLibraryFoldersVDF reads libraryfolders.vdf at the given path and returns
// all "path" values (additional Steam library roots). Paths are returned as-is;
// the caller should validate they exist.
func parseLibraryFoldersVDF(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// VDF format: "path"		"C:\\Something" or "path" "value"
	pathRe := regexp.MustCompile(`^\s*"path"\s+"([^"]+)"`)
	var paths []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		m := pathRe.FindStringSubmatch(line)
		if len(m) != 2 {
			continue
		}
		val := m[1]
		val = strings.ReplaceAll(val, "\\\\", "\\")
		paths = append(paths, val)
	}
	return paths, scanner.Err()
}

// appendSteamLibrariesFromVDF appends to roots any library paths read from
// steamapps/libraryfolders.vdf under each existing root. Duplicates and
// non-existent paths are omitted.
func appendSteamLibrariesFromVDF(roots []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, r := range roots {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	for _, r := range out {
		vdfPath := filepath.Join(r, steamAppsDir, libraryfoldersVDFName)
		extra, err := parseLibraryFoldersVDF(vdfPath)
		if err != nil {
			continue
		}
		for _, p := range extra {
			if p == "" || seen[p] {
				continue
			}
			if _, err := os.Stat(p); err != nil {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
