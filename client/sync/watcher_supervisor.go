package sync

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/gsbs/gsbs/pkg/retry"
)

// WatcherHealthy reports whether the file watcher supervisor is running normally.
var WatcherHealthy func() bool

// RunWatcherSupervisor runs the watcher with auto-restart on failure until ctx is cancelled.
func RunWatcherSupervisor(ctx context.Context, w *Watcher, getPaths func() []WatchPath, onHealthy func(bool)) {
	healthy := true
	setHealthy := func(v bool) {
		if healthy == v {
			return
		}
		healthy = v
		if onHealthy != nil {
			onHealthy(v)
		}
	}
	setHealthy(true)
	bo := retry.DefaultBackoff()
	bo.Initial = 2 * time.Second
	for {
		select {
		case <-ctx.Done():
			setHealthy(false)
			return
		default:
		}
		if paths := getPaths(); len(paths) > 0 {
			w.RemoveStalePaths(paths)
			_ = w.AddPaths(paths)
		}
		err := w.Run(ctx)
		if ctx.Err() != nil {
			setHealthy(false)
			return
		}
		if err == nil {
			continue
		}
		setHealthy(false)
		log.Printf("watcher supervisor: %v; restarting", err)
		if recErr := w.Recreate(); recErr != nil {
			log.Printf("watcher supervisor: recreate failed: %v", recErr)
		}
		delay := bo.Next()
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		setHealthy(true)
		bo.Reset()
	}
}

// IsRetriableWatcherError reports whether the supervisor should restart the watcher.
func IsRetriableWatcherError(err error) bool {
	return errors.Is(err, errWatcherClosed)
}
