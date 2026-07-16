package main

import (
	"testing"
	"time"
)

func at(h, m int) time.Time {
	return time.Date(2026, 7, 15, h, m, 0, 0, time.Local)
}

func TestInQuietHours(t *testing.T) {
	cases := []struct {
		start, end string
		now        time.Time
		want       bool
	}{
		// Simple daytime window.
		{"09:00", "17:00", at(12, 0), true},
		{"09:00", "17:00", at(8, 59), false},
		{"09:00", "17:00", at(17, 0), false}, // end is exclusive
		// Midnight wrap.
		{"22:30", "07:00", at(23, 15), true},
		{"22:30", "07:00", at(3, 0), true},
		{"22:30", "07:00", at(7, 0), false},
		{"22:30", "07:00", at(12, 0), false},
		{"22:30", "07:00", at(22, 30), true},
		// Misconfigured / off.
		{"", "", at(12, 0), false},
		{"22:30", "", at(23, 0), false},
		{"junk", "07:00", at(23, 0), false},
		{"10:00", "10:00", at(10, 0), false}, // empty window = off
	}
	for _, tc := range cases {
		cfg := &config{QuietHoursStart: tc.start, QuietHoursEnd: tc.end}
		if got := inQuietHours(cfg, tc.now); got != tc.want {
			t.Errorf("inQuietHours(%q-%q at %s) = %v, want %v",
				tc.start, tc.end, tc.now.Format("15:04"), got, tc.want)
		}
	}
	if inQuietHours(nil, at(12, 0)) {
		t.Error("nil config must never be quiet")
	}
}

func TestGameSnoozeExpiry(t *testing.T) {
	SnoozeGame("g1", time.Now().Add(time.Hour))
	SnoozeGame("g2", time.Now().Add(-time.Minute)) // already expired
	t.Cleanup(func() {
		SnoozeGame("g1", time.Time{})
		SnoozeGame("g2", time.Time{})
	})

	if !gameSnoozed("g1") {
		t.Error("g1 should be snoozed")
	}
	if gameSnoozed("g2") {
		t.Error("expired snooze must lazily clear")
	}
	expired := expiredSnoozes()
	for _, id := range expired {
		if id == "g1" {
			t.Error("active snooze must not report expired")
		}
	}
	// Clearing works.
	SnoozeGame("g1", time.Time{})
	if gameSnoozed("g1") {
		t.Error("cleared snooze must not report snoozed")
	}
}

func TestNotifyAllowedMatrix(t *testing.T) {
	defer SetNotifyPrefs("all", true)
	cases := []struct {
		level     string
		perUpload bool
		kind      notifyKind
		want      bool
	}{
		{"all", true, notifyInfo, true},
		{"all", true, notifyProblem, true},
		{"all", true, notifyEssential, true},
		{"errors", true, notifyInfo, false},
		{"errors", true, notifyProblem, true},
		{"errors", true, notifyEssential, true},
		{"silent", true, notifyInfo, false},
		{"silent", true, notifyProblem, false},
		{"silent", true, notifyEssential, true},
	}
	for _, tc := range cases {
		SetNotifyPrefs(tc.level, tc.perUpload)
		if got := notifyAllowed(tc.kind); got != tc.want {
			t.Errorf("notifyAllowed(level=%s, kind=%d) = %v, want %v", tc.level, tc.kind, got, tc.want)
		}
	}
	// Per-upload toggle gates independently of level.
	SetNotifyPrefs("all", false)
	if perUploadToastsEnabled() {
		t.Error("per-upload toasts must respect the disabled toggle")
	}
	SetNotifyPrefs("all", true)
	if !perUploadToastsEnabled() {
		t.Error("per-upload toasts enabled by setting")
	}
}
