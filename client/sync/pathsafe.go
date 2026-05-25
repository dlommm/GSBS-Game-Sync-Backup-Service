package sync

import (
	"fmt"
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
	return nil
}
