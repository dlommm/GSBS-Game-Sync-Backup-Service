//go:build !windows

package atomicio

import "syscall"

func syscallUmask(mask int) int { return syscall.Umask(mask) }
