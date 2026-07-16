package main

import (
	"strconv"
	"strings"
	"time"
)

// parseClock parses "HH:MM" (24h). ok is false for anything malformed.
func parseClock(s string) (minuteOfDay int, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// quietHoursConfigured reports whether both bounds parse.
func quietHoursConfigured(cfg *config) bool {
	if cfg == nil {
		return false
	}
	_, ok1 := parseClock(cfg.QuietHoursStart)
	_, ok2 := parseClock(cfg.QuietHoursEnd)
	return ok1 && ok2
}

// inQuietHours reports whether now (local clock) falls inside the configured
// window. Pure minute-of-day comparison — a window like 22:30–07:00 wraps
// midnight; start == end means the feature is effectively off (empty window).
func inQuietHours(cfg *config, now time.Time) bool {
	if cfg == nil {
		return false
	}
	start, ok1 := parseClock(cfg.QuietHoursStart)
	end, ok2 := parseClock(cfg.QuietHoursEnd)
	if !ok1 || !ok2 || start == end {
		return false
	}
	cur := now.Hour()*60 + now.Minute()
	if start < end {
		return cur >= start && cur < end
	}
	// Wraps midnight: e.g. 22:30–07:00.
	return cur >= start || cur < end
}
