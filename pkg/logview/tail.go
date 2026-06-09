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
func LoadEntries(path, level, query string, limit int, parseFn ParseFunc) ([]Entry, error) {
	lines, err := ReadRecentLines(path, MaxReadBytes)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]Entry, 0, limit)
	for i := len(lines) - 1; i >= 0 && len(out) < limit; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		entry := parseFn(line)
		if level != "all" && entry.Level != level {
			continue
		}
		if q != "" && !MatchQuery(entry, q) {
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}
