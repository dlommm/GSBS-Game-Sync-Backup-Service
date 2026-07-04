package gamewatch

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestRunningGames_Matching(t *testing.T) {
	sep := string(filepath.Separator)
	root := filepath.Join(sep+"games", "SteamLibrary", "steamapps", "common", "Elden Ring")
	other := filepath.Join(sep+"games", "SteamLibrary", "steamapps", "common", "Other Game")
	roots := map[string][]string{
		"elden-ring": {root},
		"other":      {other},
	}

	procs := []ProcessInfo{
		{PID: 1, ExePath: filepath.Join(sep+"usr", "bin", "bash")},
		{PID: 2, ExePath: filepath.Join(root, "Game", "eldenring.exe")},
	}
	got := RunningGames(procs, roots)
	if !got["elden-ring"] || got["other"] {
		t.Fatalf("got %v, want only elden-ring", got)
	}

	// A sibling directory sharing the prefix must NOT match ("Elden Ring 2").
	procs = []ProcessInfo{{PID: 3, ExePath: root + " 2" + sep + "game.exe"}}
	if got := RunningGames(procs, roots); got != nil {
		t.Fatalf("prefix sibling matched: %v", got)
	}

	// The root itself is not a process inside the game.
	procs = []ProcessInfo{{PID: 4, ExePath: root}}
	if got := RunningGames(procs, roots); got != nil {
		t.Fatalf("root itself matched: %v", got)
	}

	if RunningGames(nil, roots) != nil || RunningGames(procs, nil) != nil {
		t.Fatal("empty inputs must return nil")
	}
}

func TestRunningGames_CaseFolding(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("case-sensitive filesystem semantics on linux")
	}
	root := `C:\Games\MyGame`
	exe := `c:\games\mygame\BIN\game.EXE`
	if runtime.GOOS != "windows" {
		root = "/Games/MyGame"
		exe = "/games/mygame/BIN/game.APP"
	}
	roots := map[string][]string{"g": {root}}
	procs := []ProcessInfo{{PID: 1, ExePath: exe}}
	if got := RunningGames(procs, roots); !got["g"] {
		t.Fatalf("case-insensitive match failed: %v", got)
	}
}

// fakeDetector returns scripted snapshots in sequence, repeating the last.
type fakeDetector struct {
	mu    sync.Mutex
	steps [][]ProcessInfo
	i     int
}

func (f *fakeDetector) Snapshot() ([]ProcessInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.steps[f.i]
	if f.i < len(f.steps)-1 {
		f.i++
	}
	return s, nil
}

func TestPoller_HysteresisAndTransitions(t *testing.T) {
	sep := string(filepath.Separator)
	root := filepath.Join(sep+"lib", "game")
	exe := filepath.Join(root, "bin", "game")
	roots := map[string][]string{"g1": {root}}

	det := &fakeDetector{steps: [][]ProcessInfo{
		{{PID: 9, ExePath: exe}}, // scan 1: running -> start
		{},                       // scan 2: absent (1) — hysteresis holds
		{},                       // scan 3: absent (2) -> stop
		{},
	}}

	var mu sync.Mutex
	var events []string
	p := &Poller{
		Detector: det,
		Roots:    func() map[string][]string { return roots },
		OnGameStart: func(g string) {
			mu.Lock()
			events = append(events, "start:"+g)
			mu.Unlock()
		},
		OnGameStop: func(g string) {
			mu.Lock()
			events = append(events, "stop:"+g)
			mu.Unlock()
		},
	}

	p.scan() // 1
	if !p.Running("g1") || !p.AnyRunning() || p.RunningCount() != 1 {
		t.Fatal("game should be running after first scan")
	}
	p.scan() // 2: absent once — still running (hysteresis)
	if !p.Running("g1") {
		t.Fatal("single missed scan must not stop the game (launcher flap)")
	}
	p.scan() // 3: absent twice — stopped
	if p.Running("g1") || p.AnyRunning() {
		t.Fatal("game should have stopped after hysteresis")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0] != "start:g1" || events[1] != "stop:g1" {
		t.Fatalf("events = %v, want [start:g1 stop:g1]", events)
	}
}

func TestPoller_RunHonorsContext(t *testing.T) {
	det := &fakeDetector{steps: [][]ProcessInfo{{}}}
	p := &Poller{Detector: det, Interval: 5 * time.Millisecond, Roots: func() map[string][]string { return nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop with context")
	}
}
