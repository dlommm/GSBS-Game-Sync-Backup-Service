//go:build windows

package atomicio

func syscallUmask(mask int) int { return 0 }
