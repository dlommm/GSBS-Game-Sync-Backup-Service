//go:build windows

package store

import "golang.org/x/sys/windows"

// freeDiskBytes reports the bytes available to unprivileged writes on the
// volume containing path.
func freeDiskBytes(path string) (int64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var freeToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &total, &totalFree); err != nil {
		return 0, err
	}
	return int64(freeToCaller), nil
}
