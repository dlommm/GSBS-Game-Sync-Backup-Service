package sync

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isFileLockErrno reports whether err carries a Windows sharing/lock errno.
// Checking the errno works on localized Windows where the error text is not
// English.
func isFileLockErrno(err error) bool {
	var errno windows.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == windows.ERROR_SHARING_VIOLATION || errno == windows.ERROR_LOCK_VIOLATION
}
