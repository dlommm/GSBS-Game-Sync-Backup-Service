package sync

import "io"

func closeIO(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}
