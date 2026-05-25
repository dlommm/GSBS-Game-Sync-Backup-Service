package sync

import (
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
