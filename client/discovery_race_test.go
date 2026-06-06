package main

import (
	"sync"
	"testing"
)

// TestDiscoveryStateRace spawns concurrent readers (activeGameIDSet, isGameDisabled)
// and writers (toggleDiscoveredGame via direct discoveryState mutation) to verify
// that discoveryMu prevents concurrent map read/write crashes. Run with -race.
func TestDiscoveryStateRace(t *testing.T) {
	initDiscoveryState()

	// Seed some initial state.
	discoveryMu.Lock()
	discoveryState.MatchedGameIDs["game-a"] = true
	discoveryState.MatchedGameIDs["game-b"] = true
	discoveryMu.Unlock()

	const workers = 8
	var wg sync.WaitGroup

	// Writers: repeatedly update DisabledGameIDs (simulating toggleDiscoveredGame).
	for i := 0; i < workers/2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				discoveryMu.Lock()
				if j%2 == 0 {
					discoveryState.DisabledGameIDs["game-a"] = true
				} else {
					delete(discoveryState.DisabledGameIDs, "game-a")
				}
				discoveryMu.Unlock()
			}
		}(i)
	}

	// Readers: repeatedly call activeGameIDSet and isGameDisabled.
	for i := 0; i < workers/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = activeGameIDSet()
				_ = isGameDisabled("game-a")
				_ = isGameDisabled("game-b")
			}
		}()
	}

	wg.Wait()
}
