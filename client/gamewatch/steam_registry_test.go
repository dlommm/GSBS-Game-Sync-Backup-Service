package gamewatch

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRegistry(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "registry.vdf")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadRunningSteamAppID(t *testing.T) {
	dir := t.TempDir()
	running := writeRegistry(t, dir, `
"Registry"
{
	"HKCU"
	{
		"Software"
		{
			"Valve"
			{
				"Steam"
				{
					"RunningAppID"		"620"
				}
			}
		}
	}
}`)
	if got := ReadRunningSteamAppID([]string{running}); got != "620" {
		t.Fatalf("running = %q, want 620", got)
	}

	idle := writeRegistry(t, t.TempDir(), `"RunningAppID"		"0"`)
	if got := ReadRunningSteamAppID([]string{idle}); got != "" {
		t.Fatalf("idle = %q, want empty", got)
	}

	garbage := writeRegistry(t, t.TempDir(), "not a vdf at all")
	if got := ReadRunningSteamAppID([]string{garbage}); got != "" {
		t.Fatalf("garbage = %q, want empty", got)
	}

	if got := ReadRunningSteamAppID([]string{filepath.Join(dir, "missing.vdf")}); got != "" {
		t.Fatalf("missing file = %q, want empty", got)
	}

	// First readable file with a running app wins.
	if got := ReadRunningSteamAppID([]string{filepath.Join(dir, "missing.vdf"), running}); got != "620" {
		t.Fatalf("fallback = %q, want 620", got)
	}
}

// A Poller with no Detector but an ExtraRunning source (the Flatpak/Steam-
// registry configuration) must fire start/stop transitions with the same
// hysteresis as the process-scan path.
func TestPollerExtraRunningOnly(t *testing.T) {
	current := ""
	p := &Poller{
		ExtraRunning: func() map[string]bool {
			if current == "" {
				return nil
			}
			return map[string]bool{current: true}
		},
	}
	var started, stopped []string
	p.OnGameStart = func(id string) { started = append(started, id) }
	p.OnGameStop = func(id string) { stopped = append(stopped, id) }

	current = "elden-ring"
	p.scan()
	if len(started) != 1 || started[0] != "elden-ring" {
		t.Fatalf("started = %v, want [elden-ring]", started)
	}
	if !p.Running("elden-ring") {
		t.Fatal("game must be running after start")
	}

	// One absent scan must NOT stop it (hysteresis).
	current = ""
	p.scan()
	if len(stopped) != 0 {
		t.Fatalf("stopped too early: %v", stopped)
	}
	// Enough absent scans fire the stop.
	for i := 0; i < stopHysteresis; i++ {
		p.scan()
	}
	if len(stopped) != 1 || stopped[0] != "elden-ring" {
		t.Fatalf("stopped = %v, want [elden-ring]", stopped)
	}
	if p.Running("elden-ring") {
		t.Fatal("game must not be running after stop")
	}
}
