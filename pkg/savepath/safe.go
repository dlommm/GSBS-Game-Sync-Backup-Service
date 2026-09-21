package savepath

import (
	"errors"
	"fmt"
	"path"
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
	// Reject rooted paths in either separator convention on any OS.
	if rel[0] == '/' || rel[0] == '\\' {
		return fmt.Errorf("%w: absolute path", ErrInvalidRelativePath)
	}
	// Reject Windows drive paths (e.g. C:\foo) on any OS.
	if len(rel) >= 2 && rel[1] == ':' && ((rel[0] >= 'A' && rel[0] <= 'Z') || (rel[0] >= 'a' && rel[0] <= 'z')) {
		return fmt.Errorf("%w: absolute path", ErrInvalidRelativePath)
	}
	// Saves cross operating systems: validate both separator conventions even
	// when the server itself runs on Unix.
	// Check slash-only semantics too: a backslash is a literal filename byte
	// on Unix, so normalizing it can hide a different traversal there.
	for _, clean := range []string{path.Clean(rel), path.Clean(strings.ReplaceAll(rel, `\`, "/"))} {
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("%w: path escapes", ErrInvalidRelativePath)
		}
	}
	return nil
}

// JoinUserGamePath resolves root/userID/gameID/relPath and ensures IDs cannot
// change the user or game directory.
func JoinUserGamePath(root, userID, gameID, relPath string) (absPath string, err error) {
	if err := ValidateRelativePath(relPath); err != nil {
		return "", err
	}
	for _, id := range []string{userID, gameID} {
		if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\\x00") || filepath.VolumeName(id) != "" {
			return "", fmt.Errorf("%w: invalid id", ErrInvalidRelativePath)
		}
	}
	jail := filepath.Clean(filepath.Join(root, userID))
	target := filepath.Clean(filepath.Join(jail, gameID, filepath.FromSlash(relPath)))
	if target != jail && !strings.HasPrefix(target, jail+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path escapes jail", ErrInvalidRelativePath)
	}
	return target, nil
}
