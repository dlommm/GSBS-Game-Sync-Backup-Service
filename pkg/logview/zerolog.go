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
	entry.Component = deriveComponent(entry, payload)

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
		entry.Event = deriveEvent(entry, payload)
	}

	entry.Summary = buildZerologSummary(entry, payload)
	entry.Context = buildZerologContext(entry, payload)
	entry.Message = entry.Summary
	return entry
}

func deriveComponent(entry Entry, payload map[string]interface{}) string {
	if c := payloadString(payload, "component"); c != "" {
		return strings.ToLower(c)
	}
	msg := strings.ToLower(strings.TrimSpace(entry.Message))
	switch {
	case isHTTPRequestLog(entry):
		return "http"
	case strings.HasPrefix(msg, "sse:"):
		return "sse"
	case strings.Contains(msg, "pcgw") || strings.Contains(msg, "catalog scan"):
		return "pcgw"
	case strings.HasPrefix(msg, "job"):
		return "job"
	case strings.Contains(msg, "cron:"):
		return "cron"
	case strings.Contains(msg, "migrate") || strings.Contains(msg, "gsbs migrate"):
		return "migration"
	case strings.Contains(msg, "reconcile:") || strings.Contains(msg, "database:"):
		return "store"
	case strings.Contains(msg, "webui"):
		return "webui"
	case strings.HasPrefix(msg, "api ") || strings.Contains(msg, "api "):
		return "api"
	case msg == "listening" || strings.Contains(msg, "file logging") || strings.Contains(msg, "metrics:"):
		return "server"
	default:
		return "server"
	}
}

func deriveEvent(entry Entry, payload map[string]interface{}) string {
	if e := payloadString(payload, "event"); e != "" {
		return e
	}
	if isHTTPRequestLog(entry) {
		return "http.request"
	}
	msg := strings.TrimSpace(entry.Message)
	if msg == "" {
		return "log"
	}
	// Normalize common operational messages into stable event ids.
	lower := strings.ToLower(msg)
	switch {
	case strings.HasPrefix(lower, "catalog scan:"):
		return "pcgw.catalog.scan"
	case strings.HasPrefix(lower, "pcgw sync phase2:"):
		return "pcgw.sync.phase2"
	case strings.HasPrefix(lower, "pcgw sync:"):
		return "pcgw.sync"
	case strings.HasPrefix(lower, "job runner:"):
		return "job.runner"
	case strings.HasPrefix(lower, "job:"):
		return "job.lifecycle"
	case strings.HasPrefix(lower, "sse:"):
		slug := strings.TrimSpace(strings.TrimPrefix(lower, "sse:"))
		return "sse." + strings.ReplaceAll(slug, " ", ".")
	case strings.HasPrefix(lower, "cron:"):
		return "cron." + slugToken(msg)
	}
	return slugToken(msg)
}

func slugToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '.'
	}, s)
	for strings.Contains(s, "..") {
		s = strings.ReplaceAll(s, "..", ".")
	}
	return strings.Trim(s, ".")
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

	if entry.Component == "pcgw" || entry.Component == "job" || strings.Contains(msg, "pcgw") || strings.HasPrefix(msg, "job") {
		if enriched := buildDomainSummary(entry, payload); enriched != "" {
			return enriched
		}
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

func buildDomainSummary(entry Entry, payload map[string]interface{}) string {
	parts := make([]string, 0, 8)
	msg := strings.TrimSpace(entry.Message)
	if msg != "" {
		parts = append(parts, msg)
	}
	addKV := func(key, val string) {
		if val != "" {
			parts = append(parts, key+"="+val)
		}
	}
	addKV("job", payloadString(payload, "job"))
	addKV("run_id", payloadString(payload, "run_id"))
	addKV("phase", payloadString(payload, "phase"))
	addKV("mode", payloadString(payload, "mode"))
	addKV("queue", payloadAny(payload, "queue"))
	addKV("missing", payloadAny(payload, "missing"))
	addKV("remote", payloadAny(payload, "remote"))
	addKV("catalog_rows", payloadAny(payload, "catalog_rows"))
	addKV("remote_total", payloadAny(payload, "remote_total"))
	addKV("upserted", payloadAny(payload, "upserted"))
	addKV("ok", payloadAny(payload, "ok"))
	addKV("partial", payloadAny(payload, "partial"))
	addKV("failed", payloadAny(payload, "failed"))
	addKV("skipped", payloadAny(payload, "skipped"))
	addKV("budget", payloadAny(payload, "budget"))
	addKV("cursor", payloadAny(payload, "cursor"))
	addKV("entries", payloadAny(payload, "entries"))
	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts, " · ")
}

func buildZerologContext(entry Entry, payload map[string]interface{}) string {
	pairs := make([]string, 0, 16)
	add := func(key, val string) {
		if strings.TrimSpace(val) != "" {
			pairs = append(pairs, key+"="+val)
		}
	}
	add("component", entry.Component)
	add("request_id", entry.RequestID)
	add("user_id", entry.UserID)
	add("username", entry.Username)
	add("game_id", entry.GameID)
	add("path_key", entry.PathKey)
	add("client_id", entry.ClientID)
	add("error", entry.Error)
	add("job", payloadString(payload, "job"))
	add("run_id", payloadString(payload, "run_id"))
	add("phase", payloadString(payload, "phase"))
	add("queue", payloadAny(payload, "queue"))
	add("remote", payloadAny(payload, "remote"))
	add("catalog_rows", payloadAny(payload, "catalog_rows"))
	add("remote_total", payloadAny(payload, "remote_total"))
	add("upserted", payloadAny(payload, "upserted"))
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
