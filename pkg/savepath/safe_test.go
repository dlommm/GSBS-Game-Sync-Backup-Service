package savepath

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestValidateRelativePath(t *testing.T) {
	tests := []struct {
		rel   string
		valid bool
	}{
		{"saves/file.dat", true},
		{"game/slot1/save.bin", true},
		{"", false},
		{".", false},
		{"..", false},
		{"../etc/passwd", false},
		{"foo/../../bar", false},
		{"/absolute/path", false},
		{"C:\\Windows\\file", false},
		{"file\x00name", false},
	}
	for _, tt := range tests {
		err := ValidateRelativePath(tt.rel)
		if tt.valid && err != nil {
			t.Errorf("ValidateRelativePath(%q) = %v, want nil", tt.rel, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("ValidateRelativePath(%q) = nil, want error", tt.rel)
		}
		if !tt.valid && err != nil && !errors.Is(err, ErrInvalidRelativePath) {
			t.Errorf("ValidateRelativePath(%q) error = %v, want ErrInvalidRelativePath", tt.rel, err)
		}
	}
}

func TestJoinUserGamePath(t *testing.T) {
	root := t.TempDir()
	userID := "user-abc"
	gameID := "game-1"
	rel := "saves/slot.dat"

	abs, err := JoinUserGamePath(root, userID, gameID, rel)
	if err != nil {
		t.Fatalf("JoinUserGamePath: %v", err)
	}
	want := filepath.Join(root, userID, gameID, "saves", "slot.dat")
	if abs != want {
		t.Errorf("got %q want %q", abs, want)
	}

	_, err = JoinUserGamePath(root, userID, gameID, "../escape")
	if err == nil {
		t.Error("expected error for escape attempt")
	}
}
