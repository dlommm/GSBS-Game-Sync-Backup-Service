//go:build !windows

package sync

// isFileLockErrno is Windows-only; sharing violations do not exist elsewhere.
func isFileLockErrno(error) bool { return false }
