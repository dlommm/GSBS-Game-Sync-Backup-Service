package pcgw

import (
	"strings"
)

// ParseInfoboxGame extracts all key=value rows from {{Infobox game|...}} wikitext.
func ParseInfoboxGame(wikitext string) map[string]string {
	out := make(map[string]string)
	start := strings.Index(wikitext, "{{Infobox game")
	if start < 0 {
		start = strings.Index(wikitext, "{{Infobox_game")
	}
	if start < 0 {
		return out
	}
	end := findTemplateEnd(wikitext, start+2)
	if end < 0 {
		return out
	}
	body := wikitext[start+2 : end-2]
	for _, part := range splitTemplateArgs(body) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if k, v, ok := splitInfoboxRow(part); ok {
			out[k] = v
		}
	}
	return out
}

func splitTemplateArgs(body string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(body); i++ {
		if i+1 < len(body) && body[i] == '{' && body[i+1] == '{' {
			depth++
			i++
			continue
		}
		if i+1 < len(body) && body[i] == '}' && body[i+1] == '}' {
			depth--
			i++
			continue
		}
		if body[i] == '|' && depth == 0 {
			parts = append(parts, body[start:i])
			start = i + 1
		}
	}
	parts = append(parts, body[start:])
	return parts
}

func splitInfoboxRow(part string) (key, value string, ok bool) {
	eq := strings.Index(part, "=")
	if eq < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(part[:eq])
	value = strings.TrimSpace(part[eq+1:])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}
