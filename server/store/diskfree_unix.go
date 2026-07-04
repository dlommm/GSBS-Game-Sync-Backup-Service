//go:build !windows

package store

import "golang.org/x/sys/unix"

// freeDiskBytes reports the bytes available to unprivileged writes on the
// volume containing path.
func freeDiskBytes(path string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
