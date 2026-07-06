package logview

import (
	"io"
	"os"
	"strings"
)

// ParseFunc converts a raw log line into an Entry.
type ParseFunc func(line string) Entry

// ReadRecentLines reads up to maxBytes from the end of path and returns lines.
func ReadRecentLines(path string, maxBytes int64) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	start := int64(0)
	if size > maxBytes {
		start = size - maxBytes
	}
	if _, err := f.Seek(start, 0); err != nil {
		return nil, err
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.ReplaceAll(string(buf), "\r\n", "\n"), "\n"), nil
}

// LoadEntries reads recent lines from path, parses them, and returns newest-first matches.
func LoadEntries(path string, q Query, parseFn ParseFunc) ([]Entry, error) {
	out, _, err := LoadEntriesPage(path, q, parseFn)
	return out, err
}

// LoadEntriesPage is LoadEntries plus paging support: it scans the whole
// tail window, returns the matches in [q.Offset, q.Offset+q.Limit) newest
// first, and the total number of matches in the window (so callers can
// render Newer/Older paging controls).
func LoadEntriesPage(path string, q Query, parseFn ParseFunc) ([]Entry, int, error) {
	lines, err := ReadRecentLines(path, MaxReadBytes)
	if err != nil {
		return nil, 0, err
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	out := make([]Entry, 0, q.Limit)
	total := 0
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		entry := parseFn(line)
		if q.Level != "all" && entry.Level != q.Level {
			continue
		}
		if q.HideHTTPNoise && IsRoutineHTTPNoise(entry) {
			continue
		}
		if !MatchComponent(entry, q.Component) {
			continue
		}
		if q.Text != "" && !MatchQuery(entry, q.Text) {
			continue
		}
		if total >= offset && len(out) < q.Limit {
			out = append(out, entry)
		}
		total++
	}
	return out, total, nil
}

// LoadEntriesLegacy is a thin wrapper for callers that only need level/text/limit filters.
func LoadEntriesLegacy(path, level, query string, limit int, parseFn ParseFunc) ([]Entry, error) {
	if strings.TrimSpace(level) == "" {
		level = "all"
	}
	return LoadEntries(path, Query{Level: level, Text: query, Limit: limit, Component: "all", HideHTTPNoise: true}, parseFn)
}
