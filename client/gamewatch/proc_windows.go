//go:build windows

package gamewatch

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// NewDetector returns the Toolhelp32-based process detector.
func NewDetector() Detector {
	return &toolhelpDetector{}
}

type toolhelpDetector struct{}

func (d *toolhelpDetector) Snapshot() ([]ProcessInfo, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil, err
	}
	var out []ProcessInfo
	for {
		if path := queryProcessImagePath(entry.ProcessID); path != "" {
			out = append(out, ProcessInfo{PID: int(entry.ProcessID), ExePath: path})
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return out, nil
}

// queryProcessImagePath resolves a PID's full executable path; empty on any
// failure (system processes we cannot open are never games).
func queryProcessImagePath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}
