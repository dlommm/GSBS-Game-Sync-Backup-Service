//go:build darwin

package gamewatch

import (
	"bytes"
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// NewDetector returns the sysctl-based process detector.
func NewDetector() Detector {
	return &sysctlDetector{}
}

// sysctlDetector lists PIDs via kern.proc.all and resolves each executable
// path from kern.procargs2 (readable unprivileged for the user's own
// processes — which is exactly the set games run in).
type sysctlDetector struct{}

func (d *sysctlDetector) Snapshot() ([]ProcessInfo, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	out := make([]ProcessInfo, 0, len(procs))
	for i := range procs {
		pid := int(procs[i].Proc.P_pid)
		if pid <= 0 {
			continue
		}
		exe := procExecPath(pid)
		if exe == "" {
			continue
		}
		out = append(out, ProcessInfo{PID: pid, ExePath: exe})
	}
	return out, nil
}

// procExecPath extracts the executable path from KERN_PROCARGS2: the buffer
// starts with a 4-byte argc followed by the NUL-terminated exec path.
func procExecPath(pid int) string {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(raw) <= 4 {
		return ""
	}
	_ = binary.LittleEndian.Uint32(raw[:4]) // argc (unused)
	rest := raw[4:]
	if i := bytes.IndexByte(rest, 0); i > 0 {
		return string(rest[:i])
	}
	return ""
}
