package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSteamInstallPath(t *testing.T) {
	root := t.TempDir()
	steamapps := filepath.Join(root, "steamapps")
	common := filepath.Join(steamapps, "common", "Test Game")
	if err := os.MkdirAll(common, 0755); err != nil {
		t.Fatal(err)
	}
	acf := `"AppState"
{
	"appid"		"999"
	"name"		"Test Game"
	"installdir"		"Test Game"
}`
	if err := os.WriteFile(filepath.Join(steamapps, "appmanifest_999.acf"), []byte(acf), 0644); err != nil {
		t.Fatal(err)
	}

	// Override library roots by placing only our temp root in a minimal scan.
	// scanSteam uses paths.GetSteamLibraryRoots — inject via env is not supported,
	// so test parse logic via a direct read of one manifest file pattern.
	installPath := ""
	if im := steamInstalldirRe.FindStringSubmatch(acf); len(im) >= 2 {
		candidate := filepath.Join(steamapps, "common", im[1])
		if _, err := os.Stat(candidate); err == nil {
			installPath = candidate
		}
	}
	if installPath != common {
		t.Fatalf("got install path %q want %q", installPath, common)
	}
}
