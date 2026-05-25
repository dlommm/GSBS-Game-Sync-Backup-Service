package pcgw

import "strings"

// ExtractAllTemplates returns every top-level {{...}} invocation in wikitext.
func ExtractAllTemplates(wikitext string) []string {
	var out []string
	seen := make(map[string]bool)
	i := 0
	for i < len(wikitext) {
		start := strings.Index(wikitext[i:], "{{")
		if start < 0 {
			break
		}
		start += i
		end := findTemplateEnd(wikitext, start+2)
		if end < 0 {
			break
		}
		tmpl := wikitext[start:end]
		if !seen[tmpl] {
			seen[tmpl] = true
			out = append(out, tmpl)
		}
		i = end
	}
	return out
}

// findTemplateEnd returns the index past the closing }} for a template whose body starts at pathStart.
// pathStart is the index immediately after "{{".
func findTemplateEnd(s string, pathStart int) int {
	depth := 1
	for i := pathStart; i < len(s)-1; i++ {
		if s[i] == '{' && s[i+1] == '{' {
			depth++
			i++
			continue
		}
		if s[i] == '}' && s[i+1] == '}' {
			depth--
			if depth == 0 {
				return i + 2
			}
			i++
		}
	}
	return -1
}
