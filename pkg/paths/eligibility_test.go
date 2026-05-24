package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluatePullEligibility_EmptyPath(t *testing.T) {
	got := EvaluatePullEligibility("", "game1", PullContext{})
	if got != SkipNoAnchor {
		t.Fatalf("got %v, want SkipNoAnchor", got)
	}
}

func TestEvaluatePullEligibility_LegacyMode(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "save.sav")

	t.Run("existing save file", func(t *testing.T) {
		if err := os.WriteFile(savePath, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		got := EvaluatePullEligibility(savePath, "game1", PullContext{LegacyMode: true})
		if got != ApplyReady {
			t.Fatalf("got %v, want ApplyReady", got)
		}
	})

	t.Run("existing target dir", func(t *testing.T) {
		targetDir := filepath.Join(dir, "saves")
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			t.Fatal(err)
		}
		got := EvaluatePullEligibility(targetDir, "game1", PullContext{LegacyMode: true})
		if got != ApplyReady {
			t.Fatalf("got %v, want ApplyReady", got)
		}
	})

	t.Run("missing dir", func(t *testing.T) {
		missing := filepath.Join(dir, "missing", "save.sav")
		got := EvaluatePullEligibility(missing, "game1", PullContext{LegacyMode: true})
		if got != SkipNoAnchor {
			t.Fatalf("got %v, want SkipNoAnchor", got)
		}
	})
}

func TestEvaluatePullEligibility_InstallAware(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "saves", "save.sav")

	t.Run("game not installed", func(t *testing.T) {
		got := EvaluatePullEligibility(savePath, "missing-game", PullContext{
			InstalledGameIDs: map[string]bool{"other-game": true},
		})
		if got != SkipNotInstalled {
			t.Fatalf("got %v, want SkipNotInstalled", got)
		}
	})

	t.Run("game installed save exists", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Dir(savePath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(savePath, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		got := EvaluatePullEligibility(savePath, "game1", PullContext{
			InstalledGameIDs: map[string]bool{"game1": true},
		})
		if got != ApplyReady {
			t.Fatalf("got %v, want ApplyReady", got)
		}
	})

	t.Run("game installed parent missing uses anchor", func(t *testing.T) {
		missingSave := filepath.Join(dir, "nested", "save.sav")
		got := EvaluatePullEligibility(missingSave, "game1", PullContext{
			InstalledGameIDs: map[string]bool{"game1": true},
		})
		if got != ApplyCreateDir {
			t.Fatalf("got %v, want ApplyCreateDir", got)
		}
	})

	t.Run("empty installed map skips install check", func(t *testing.T) {
		if err := os.WriteFile(savePath, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		got := EvaluatePullEligibility(savePath, "game1", PullContext{
			InstalledGameIDs: map[string]bool{},
		})
		if got != ApplyReady {
			t.Fatalf("got %v, want ApplyReady", got)
		}
	})
}

func TestEvaluatePullEligibility_ApplyCreateDir_ProtonCompatdata(t *testing.T) {
	root := t.TempDir()
	appID := "123456"
	pfx := filepath.Join(root, "steamapps", "compatdata", appID, "pfx")
	if err := os.MkdirAll(pfx, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(pfx, "drive_c", "users", "steamuser", "AppData", "Local", "Game", "save.sav")

	got := EvaluatePullEligibility(savePath, "game1", PullContext{
		InstalledGameIDs:   map[string]bool{"game1": true},
		InstalledSteamApps: []string{appID},
	})
	if got != ApplyCreateDir {
		t.Fatalf("got %v, want ApplyCreateDir", got)
	}
}

func TestEvaluatePullEligibility_ApplyCreateDir_ParentChain(t *testing.T) {
	root := t.TempDir()
	anchor := filepath.Join(root, "Games", "Installed")
	if err := os.MkdirAll(anchor, 0755); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(anchor, "deep", "nested", "save.sav")

	got := EvaluatePullEligibility(savePath, "game1", PullContext{
		InstalledGameIDs: map[string]bool{"game1": true},
	})
	if got != ApplyCreateDir {
		t.Fatalf("got %v, want ApplyCreateDir", got)
	}
}

func TestEvaluatePullEligibility_SkipNoAnchor(t *testing.T) {
	root := t.TempDir()
	// Deep path so hasInstallAnchor parent walk (max 5) never reaches temp root.
	savePath := filepath.Join(root, "a", "b", "c", "d", "e", "f", "save.sav")

	got := EvaluatePullEligibility(savePath, "game1", PullContext{
		InstalledGameIDs: map[string]bool{"game1": true},
	})
	if got != SkipNoAnchor {
		t.Fatalf("got %v, want SkipNoAnchor", got)
	}
}

func TestEvaluatePullEligibility_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "save.sav")
	if err := os.WriteFile(savePath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	got := EvaluatePullEligibility(savePath, "game1", PullContext{LegacyMode: true})
	if got != ApplyReady {
		t.Fatalf("got %v, want ApplyReady", got)
	}
}

func TestHasInstallAnchor_Compatdata(t *testing.T) {
	root := t.TempDir()
	appID := "789012"
	pfx := filepath.Join(root, "compatdata", appID, "pfx")
	if err := os.MkdirAll(pfx, 0755); err != nil {
		t.Fatal(err)
	}

	t.Run("matching app with pfx anchor", func(t *testing.T) {
		path := filepath.Join(pfx, "drive_c", "save.sav")
		if !hasInstallAnchor(path, []string{appID}) {
			t.Fatal("expected anchor for matching compatdata app")
		}
	})

	t.Run("wrong app id deep path", func(t *testing.T) {
		// Deep enough that parent-chain walk does not reach pfx within 5 steps.
		path := filepath.Join(pfx, "a", "b", "c", "d", "e", "save.sav")
		if hasInstallAnchor(path, []string{"000000"}) {
			t.Fatal("expected no anchor for non-matching app id")
		}
	})

	t.Run("compatdata path without pfx dir", func(t *testing.T) {
		noPfx := filepath.Join(root, "compatdata", "999999", "a", "b", "c", "d", "e", "save.sav")
		if hasInstallAnchor(noPfx, []string{"999999"}) {
			t.Fatal("expected no anchor when pfx dir missing")
		}
	})
}

func TestHasInstallAnchor_ParentChain(t *testing.T) {
	root := t.TempDir()
	anchor := filepath.Join(root, "anchor")
	if err := os.MkdirAll(anchor, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(anchor, "a", "b", "c", "save.sav")
	if !hasInstallAnchor(path, nil) {
		t.Fatal("expected parent chain anchor within 5 levels")
	}
}

func TestWatchDirExists(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "save.sav")
	if err := os.WriteFile(savePath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if WatchDirExists("") {
		t.Fatal("empty path should be false")
	}
	if !WatchDirExists(savePath) {
		t.Fatal("parent dir exists for file path")
	}
	if WatchDirExists(filepath.Join(dir, "missing", "save.sav")) {
		t.Fatal("missing dir should be false")
	}
}
