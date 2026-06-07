package savepath

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrInvalidRelativePath is returned when a relative path fails validation.
var ErrInvalidRelativePath = errors.New("invalid relative path")

// ValidateRelativePath rejects empty paths, NUL bytes, absolute paths, and ".." escapes.
func ValidateRelativePath(rel string) error {
	if rel == "" {
		return fmt.Errorf("%w: empty", ErrInvalidRelativePath)
	}
	if strings.Contains(rel, "\x00") {
		return fmt.Errorf("%w: nul byte", ErrInvalidRelativePath)
	}
	if filepath.IsAbs(rel) {
		return fmt.Errorf("%w: absolute path", ErrInvalidRelativePath)
	}
	// Reject Unix absolute paths on any OS (filepath.IsAbs misses these on Windows).
	if len(rel) > 0 && rel[0] == '/' {
		return fmt.Errorf("%w: absolute path", ErrInvalidRelativePath)
	}
	// Reject Windows drive paths (e.g. C:\foo) on any OS.
	if len(rel) >= 2 && rel[1] == ':' && ((rel[0] >= 'A' && rel[0] <= 'Z') || (rel[0] >= 'a' && rel[0] <= 'z')) {
		return fmt.Errorf("%w: absolute path", ErrInvalidRelativePath)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." {
		return fmt.Errorf("%w: path escapes", ErrInvalidRelativePath)
	}
	if strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: path escapes", ErrInvalidRelativePath)
	}
	return nil
}

// JoinUserGamePath resolves root/userID/gameID/relPath and ensures the result stays under root/userID.
func JoinUserGamePath(root, userID, gameID, relPath string) (absPath string, err error) {
	if err := ValidateRelativePath(relPath); err != nil {
		return "", err
	}
	if strings.Contains(userID, "\x00") || strings.Contains(gameID, "\x00") {
		return "", fmt.Errorf("%w: invalid id", ErrInvalidRelativePath)
	}
	jail := filepath.Clean(filepath.Join(root, userID))
	target := filepath.Clean(filepath.Join(jail, gameID, filepath.FromSlash(relPath)))
	if target != jail && !strings.HasPrefix(target, jail+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path escapes jail", ErrInvalidRelativePath)
	}
	return target, nil
}
