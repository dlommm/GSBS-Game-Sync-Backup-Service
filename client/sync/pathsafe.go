package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateWriteUnderRoot ensures absPath stays within watchRoot (no directory escape).
func ValidateWriteUnderRoot(absPath, watchRoot string) error {
	if watchRoot == "" {
		return nil
	}
	cleanAbs := filepath.Clean(absPath)
	cleanRoot := filepath.Clean(watchRoot)
	rel, err := filepath.Rel(cleanRoot, cleanAbs)
	if err != nil {
		return fmt.Errorf("path outside watch root: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes watch root: %s", absPath)
	}
	// A lexical descendant may still escape through an existing symlink.
	// Resolve the nearest existing ancestor so new nested save directories
	// are checked before MkdirAll or backup creation as well.
	resolvedRoot, err := resolveWritePath(cleanRoot)
	if err != nil {
		return fmt.Errorf("resolve watch root: %w", err)
	}
	resolvedPath, err := resolveWritePath(cleanAbs)
	if err != nil {
		return fmt.Errorf("resolve save path: %w", err)
	}
	rel, err = filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes watch root through symlink: %s", absPath)
	}
	return nil
}

func resolveWritePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	ancestor := abs
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", err
			}
			rel, err := filepath.Rel(ancestor, abs)
			if err != nil {
				return "", err
			}
			return filepath.Join(resolved, rel), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", err
		}
		ancestor = parent
	}
}
