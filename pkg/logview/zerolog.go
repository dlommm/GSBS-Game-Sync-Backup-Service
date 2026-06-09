package logview

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ParseZerologLine parses a JSON (zerolog-style) log line into an Entry.
func ParseZerologLine(line string) Entry {
	entry := Entry{
		Level:   "raw",
		Message: line,
		Raw:     line,
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		entry.Summary = line
		entry.Message = line
		return entry
	}

	if ts, ok := payload["time"].(string); ok {
		entry.Timestamp = ts
	}
	if msg, ok := payload["message"].(string); ok && strings.TrimSpace(msg) != "" {
		entry.Message = msg
	}

	entry.Event = payloadString(payload, "event")
	entry.RequestID = payloadString(payload, "request_id")
	entry.Method = payloadString(payload, "method")
	entry.Path = payloadString(payload, "path")
	entry.Status = payloadStatus(payload, "status")
	entry.IP = payloadString(payload, "ip")
	entry.Duration = payloadDuration(payload, "duration")
	entry.UserID = payloadString(payload, "user_id")
	entry.Username = payloadString(payload, "username")
	entry.GameID = payloadString(payload, "game_id")
	entry.PathKey = payloadString(payload, "path_key")
	entry.ClientID = payloadString(payload, "client_id")
	entry.Error = payloadString(payload, "error")

	if lvl, ok := payload["level"].(string); ok {
		switch strings.ToLower(strings.TrimSpace(lvl)) {
		case "warning":
			entry.Level = "warn"
		case "debug", "info", "warn", "error":
			entry.Level = strings.ToLower(strings.TrimSpace(lvl))
		default:
			entry.Level = strings.ToLower(strings.TrimSpace(lvl))
		}
	}

	if entry.Event == "" {
		if isHTTPRequestLog(entry) {
			entry.Event = "http.request"
		} else {
			entry.Event = entry.Message
		}
	}

	entry.Summary = buildZerologSummary(entry, payload)
	entry.Context = buildZerologContext(entry, payload)
	entry.Message = entry.Summary
	return entry
}

func isHTTPRequestLog(entry Entry) bool {
	return entry.Message == "request" || entry.Event == "http.request" ||
		(entry.Method != "" && entry.Path != "" && entry.Status != "")
}

func buildZerologSummary(entry Entry, payload map[string]interface{}) string {
	if isHTTPRequestLog(entry) {
		parts := make([]string, 0, 4)
		if entry.Method != "" && entry.Path != "" {
			parts = append(parts, fmt.Sprintf("%s %s", entry.Method, entry.Path))
		} else if entry.Path != "" {
			parts = append(parts, entry.Path)
		}
		if entry.Status != "" {
			if len(parts) > 0 {
				parts[0] += " → " + entry.Status
			} else {
				parts = append(parts, "→ "+entry.Status)
			}
		}
		if entry.Duration != "" {
			parts = append(parts, "in "+entry.Duration)
		}
		if entry.IP != "" {
			parts = append(parts, "from "+entry.IP)
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}

	msg := entry.Message
	if strings.Contains(msg, "rate limit") {
		details := make([]string, 0, 3)
		if key := payloadString(payload, "key"); key != "" {
			details = append(details, "key="+key)
		}
		if limit := payloadAny(payload, "limit"); limit != "" {
			details = append(details, "limit="+limit)
		}
		if window := payloadAny(payload, "window"); window != "" {
			details = append(details, "window="+window)
		}
		if len(details) > 0 {
			return msg + " (" + strings.Join(details, ", ") + ")"
		}
		return msg
	}

	if entry.Error != "" {
		if msg != "" && !strings.Contains(msg, entry.Error) {
			return msg + ": " + entry.Error
		}
		if msg != "" {
			return msg
		}
		return entry.Error
	}

	if panicVal := payloadString(payload, "panic"); panicVal != "" {
		if msg != "" {
			return msg + ": " + panicVal
		}
		return panicVal
	}

	extras := make([]string, 0, 6)
	for _, pair := range []struct{ key, val string }{
		{"operation", payloadString(payload, "operation")},
		{"addr", payloadString(payload, "addr")},
		{"size", payloadAny(payload, "size")},
		{"entries", payloadAny(payload, "entries")},
		{"user_id", entry.UserID},
		{"game_id", entry.GameID},
	} {
		if pair.val != "" {
			extras = append(extras, pair.key+"="+pair.val)
		}
	}
	if len(extras) > 0 {
		if msg != "" {
			return msg + " (" + strings.Join(extras, ", ") + ")"
		}
		return strings.Join(extras, ", ")
	}
	if msg != "" {
		return msg
	}
	return entry.Event
}

func buildZerologContext(entry Entry, payload map[string]interface{}) string {
	pairs := make([]string, 0, 12)
	add := func(key, val string) {
		if strings.TrimSpace(val) != "" {
			pairs = append(pairs, key+"="+val)
		}
	}
	add("request_id", entry.RequestID)
	add("user_id", entry.UserID)
	add("username", entry.Username)
	add("game_id", entry.GameID)
	add("path_key", entry.PathKey)
	add("client_id", entry.ClientID)
	add("error", entry.Error)
	add("operation", payloadString(payload, "operation"))
	add("addr", payloadString(payload, "addr"))
	add("size", payloadAny(payload, "size"))
	add("entries", payloadAny(payload, "entries"))
	add("panic", payloadString(payload, "panic"))
	if stack := payloadString(payload, "stack"); stack != "" {
		if len(stack) > 120 {
			stack = stack[:117] + "..."
		}
		add("stack", stack)
	}
	return strings.Join(pairs, " ")
}

func payloadString(payload map[string]interface{}, key string) string {
	v, ok := payload[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return strings.TrimSpace(s)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func payloadAny(payload map[string]interface{}, key string) string {
	v, ok := payload[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case bool:
		return strconv.FormatBool(val)
	default:
		return strings.TrimSpace(fmt.Sprint(val))
	}
}

func payloadStatus(payload map[string]interface{}, key string) string {
	v, ok := payload[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case float64:
		return strconv.Itoa(int(val))
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case string:
		return strings.TrimSpace(val)
	default:
		return strings.TrimSpace(fmt.Sprint(val))
	}
}

func payloadDuration(payload map[string]interface{}, key string) string {
	v, ok := payload[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		s := strings.TrimSpace(val)
		if s == "" {
			return ""
		}
		if strings.HasSuffix(s, "ms") || strings.HasSuffix(s, "s") || strings.HasSuffix(s, "m") {
			return s
		}
		return s + "ms"
	case float64:
		ms := int64(val)
		if ms == 0 && val > 0 && val < 1 {
			ms = 1
		}
		return strconv.FormatInt(ms, 10) + "ms"
	case int:
		return strconv.Itoa(val) + "ms"
	case int64:
		return strconv.FormatInt(val, 10) + "ms"
	default:
		return strings.TrimSpace(fmt.Sprint(val))
	}
}
