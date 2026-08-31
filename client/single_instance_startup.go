package main

import (
	"errors"
	"log"
	"os"
	"time"
)

// postUpdateRelaunchWait bounds how long a freshly-updated instance waits for
// the outgoing one to let go of the single-instance lock.
const postUpdateRelaunchWait = 30 * time.Second

// singleInstanceRetryInterval is the poll interval while waiting for the lock.
const singleInstanceRetryInterval = 250 * time.Millisecond

// postUpdateRelaunchFlag is passed to the binary the updater starts, so the new
// instance knows to wait for the outgoing one instead of quitting on sight.
const postUpdateRelaunchFlag = "--post-update"

// isPostUpdateRelaunch reports whether this process was started by the updater.
func isPostUpdateRelaunch() bool {
	for _, a := range os.Args[1:] {
		if a == postUpdateRelaunchFlag {
			return true
		}
	}
	return false
}

// acquireSingleInstanceWait takes the single-instance lock, retrying until the
// timeout when another instance still holds it.
func acquireSingleInstanceWait(timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	for {
		release, err := tryAcquireSingleInstance()
		if err == nil || !errors.Is(err, errAlreadyRunning) {
			return release, err
		}
		if !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(singleInstanceRetryInterval)
	}
}

// acquireSingleInstanceOrExit is the startup path shared by every platform's
// tray and by console mode. It exits the process rather than returning on a
// duplicate launch.
//
// After an auto-update the updater starts the new binary without waiting for
// the old process to exit, so the new instance can arrive while the outgoing
// one still holds the lock. Quitting on sight there left the machine with no
// backup service running at all — no watching, no pushes — until the user
// noticed. A post-update relaunch therefore waits the old process out.
func acquireSingleInstanceOrExit() func() {
	wait := time.Duration(0)
	if isPostUpdateRelaunch() {
		wait = postUpdateRelaunchWait
	}
	release, err := acquireSingleInstanceWait(wait)
	if err == nil {
		return release
	}
	if errors.Is(err, errAlreadyRunning) {
		notifyAlreadyRunning()
		os.Exit(0)
	}
	// Could not determine whether another instance holds the lock. Refuse to
	// start rather than fail open into two concurrent sync loops contending on
	// the push-hash cache and the outbox.
	log.Fatalf("%v", err)
	return nil
}

// waitForPreviousInstance blocks until the instance that spawned this update
// helper has released the single-instance lock, so the binary swap and the
// relaunch do not race a still-live old process.
func waitForPreviousInstance() {
	release, err := acquireSingleInstanceWait(postUpdateRelaunchWait)
	if err != nil {
		log.Printf("update: proceeding without confirming the previous instance exited: %v", err)
		return
	}
	release()
}
