package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateWriteUnderRoot(t *testing.T) {
	root := filepath.Join("/home", "user", "saves")

	t.Run("inside root", func(t *testing.T) {
		target := filepath.Join(root, "game", "save.sav")
		if err := ValidateWriteUnderRoot(target, root); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("escape rejected", func(t *testing.T) {
		target := filepath.Join(root, "..", "other", "save.sav")
		if err := ValidateWriteUnderRoot(target, root); err == nil {
			t.Fatal("expected escape error")
		}
	})

	t.Run("empty root skips", func(t *testing.T) {
		if err := ValidateWriteUnderRoot("/any/path", ""); err != nil {
			t.Fatalf("empty root should skip validation: %v", err)
		}
	})
}

func TestValidateWriteUnderRootSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, suffix := range []string{"save.sav", filepath.Join("new", "nested", "save.sav")} {
		if err := ValidateWriteUnderRoot(filepath.Join(link, suffix), root); err == nil {
			t.Fatalf("allowed symlink escape for %s", suffix)
		}
	}
	// A configured root may itself be a symlink; its descendants are valid.
	if err := ValidateWriteUnderRoot(filepath.Join(link, "new", "save.sav"), link); err != nil {
		t.Fatalf("rejected symlinked watch root: %v", err)
	}
	inside := filepath.Join(root, "inside")
	if err := os.Mkdir(inside, 0755); err != nil {
		t.Fatal(err)
	}
	internalLink := filepath.Join(root, "internal")
	if err := os.Symlink(inside, internalLink); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWriteUnderRoot(filepath.Join(internalLink, "save.sav"), root); err != nil {
		t.Fatalf("rejected internal symlink: %v", err)
	}
	broken := filepath.Join(root, "broken")
	if err := os.Symlink(filepath.Join(outside, "missing"), broken); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWriteUnderRoot(filepath.Join(broken, "save.sav"), root); err == nil {
		t.Fatal("allowed dangling symlink")
	}
}
