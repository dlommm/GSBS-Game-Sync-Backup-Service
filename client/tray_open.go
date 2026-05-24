package main

import (
	"github.com/skratchdot/open-golang/open"
)

func osExecOpen(url string) error {
	return open.Run(url)
}
