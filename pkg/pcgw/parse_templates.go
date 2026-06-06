package pcgw

import "strings"

// ExtractAllTemplates returns every top-level {{...}} invocation in wikitext.
// An unterminated {{ (malformed template) is skipped so that valid templates
// appearing later on the same page are still extracted.
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
			// Unterminated {{: advance past the opener and continue.
			i = start + 2
			continue
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
