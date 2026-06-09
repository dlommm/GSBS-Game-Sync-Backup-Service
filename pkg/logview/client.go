package logview

import (
	"regexp"
	"strings"
)

var plainClientTimestamp = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}`)

// ParseClientLine parses a GSBS client log line (slog text or stdlib log.Printf).
func ParseClientLine(line string) Entry {
	if isSlogTextLine(line) {
		return parseSlogTextLine(line)
	}
	return parsePlainClientLine(line)
}

func isSlogTextLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "time=") ||
		(strings.Contains(trimmed, "level=") && strings.Contains(trimmed, "msg="))
}

func parseSlogTextLine(line string) Entry {
	entry := Entry{
		Raw:   line,
		Level: "info",
	}
	kv := parseKeyValuePairs(line)

	if ts := kv["time"]; ts != "" {
		entry.Timestamp = ts
	}
	if lvl := strings.ToLower(kv["level"]); lvl != "" {
		switch lvl {
		case "warning":
			entry.Level = "warn"
		default:
			entry.Level = lvl
		}
	}
	entry.Message = kv["msg"]
	entry.Event = kv["op"]
	if entry.Event == "" {
		entry.Event = entry.Message
	}
	entry.GameID = kv["game_id"]
	entry.PathKey = kv["path_key"]
	entry.Error = kv["error"]
	entry.Component = strings.ToLower(kv["component"])
	if entry.Component == "" {
		entry.Component = deriveClientComponent(entry, kv)
	}

	entry.Summary = buildClientSlogSummary(entry, kv)
	entry.Context = buildClientSlogContext(entry, kv)
	if entry.Message == "" {
		entry.Message = entry.Summary
	}
	return entry
}

func buildClientSlogSummary(entry Entry, kv map[string]string) string {
	if entry.Event != "" && entry.Event != entry.Message {
		parts := []string{entry.Event}
		if entry.GameID != "" {
			parts = append(parts, "game="+entry.GameID)
		}
		if entry.PathKey != "" {
			parts = append(parts, "path_key="+entry.PathKey)
		}
		if file := kv["file"]; file != "" {
			parts = append(parts, "file="+file)
		}
		if entry.Error != "" {
			parts = append(parts, entry.Error)
		}
		return strings.Join(parts, " ")
	}
	msg := entry.Message
	if entry.Error != "" && !strings.Contains(msg, entry.Error) {
		return msg + ": " + entry.Error
	}
	if msg != "" {
		return msg
	}
	return entry.Event
}

func buildClientSlogContext(entry Entry, kv map[string]string) string {
	pairs := make([]string, 0, 8)
	add := func(key, val string) {
		if strings.TrimSpace(val) != "" {
			pairs = append(pairs, key+"="+val)
		}
	}
	add("component", kv["component"])
	add("game_id", entry.GameID)
	add("path_key", entry.PathKey)
	add("file", kv["file"])
	add("error", entry.Error)
	for k, v := range kv {
		switch k {
		case "time", "level", "msg", "op", "component", "game_id", "path_key", "file", "error":
			continue
		default:
			add(k, v)
		}
	}
	return strings.Join(pairs, " ")
}

func parsePlainClientLine(line string) Entry {
	entry := Entry{
		Raw:     line,
		Level:   "info",
		Message: line,
		Summary: line,
	}
	if m := plainClientTimestamp.FindStringIndex(line); m != nil {
		entry.Timestamp = strings.TrimSpace(line[m[0]:m[1]])
		rest := strings.TrimSpace(line[m[1]:])
		entry.Message = rest
		entry.Summary = rest
	}

	lower := strings.ToLower(entry.Message)
	switch {
	case strings.Contains(lower, "warning:"):
		entry.Level = "warn"
	case strings.Contains(lower, "failed"), strings.Contains(lower, "error"):
		entry.Level = "error"
	default:
		entry.Level = "info"
	}

	if strings.HasPrefix(entry.Message, "client sync:") {
		entry.Event = "client.sync"
		entry.Component = "sync"
	} else if strings.HasPrefix(entry.Message, "client login:") {
		entry.Event = "client.login"
		entry.Component = "auth"
	} else if strings.HasPrefix(entry.Message, "tray:") {
		entry.Event = "tray"
		entry.Component = "tray"
	} else if strings.HasPrefix(entry.Message, "sync:") || strings.HasPrefix(entry.Message, "manifest:") {
		entry.Event = "sync"
		entry.Component = "sync"
	} else {
		entry.Event = entry.Message
		if len(entry.Event) > 60 {
			entry.Event = entry.Event[:57] + "..."
		}
	}

	return entry
}

func deriveClientComponent(entry Entry, kv map[string]string) string {
	if c := kv["component"]; c != "" {
		return strings.ToLower(c)
	}
	msg := strings.ToLower(entry.Message)
	switch {
	case strings.HasPrefix(msg, "tray:"):
		return "tray"
	case strings.HasPrefix(msg, "client login:"):
		return "auth"
	case strings.HasPrefix(msg, "client sync:"), strings.HasPrefix(msg, "sync:"), strings.HasPrefix(msg, "manifest:"):
		return "sync"
	case strings.Contains(msg, "setup"):
		return "setup"
	default:
		return "client"
	}
}

// parseKeyValuePairs parses slog text handler key=value pairs, including quoted values.
func parseKeyValuePairs(line string) map[string]string {
	out := make(map[string]string)
	i := 0
	for i < len(line) {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		if i >= len(line) {
			break
		}
		eq := strings.IndexByte(line[i:], '=')
		if eq < 0 {
			break
		}
		key := line[i : i+eq]
		i += eq + 1
		if i >= len(line) {
			out[key] = ""
			break
		}
		var val string
		if line[i] == '"' {
			i++
			var b strings.Builder
			for i < len(line) {
				if line[i] == '\\' && i+1 < len(line) {
					b.WriteByte(line[i+1])
					i += 2
					continue
				}
				if line[i] == '"' {
					i++
					break
				}
				b.WriteByte(line[i])
				i++
			}
			val = b.String()
		} else {
			start := i
			for i < len(line) && line[i] != ' ' {
				i++
			}
			val = line[start:i]
		}
		out[key] = val
	}
	return out
}
