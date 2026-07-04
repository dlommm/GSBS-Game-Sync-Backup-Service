// Package i18n provides message catalogs for the GSBS server and client.
//
// English (locales/en.json) is the source of truth and always complete; other
// locales are partial overlays and fall back to English per-key, so a missing
// translation shows English rather than a blank or a raw key. Translators add
// a locale by dropping a locales/<lang>.json file with the same keys — see
// docs/wiki/Contributing.md.
package i18n

import (
	"embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

//go:embed locales/*.json
var catalogFS embed.FS

// DefaultLocale is the fallback and source-of-truth locale.
const DefaultLocale = "en"

var (
	loadOnce  sync.Once
	catalogs  map[string]map[string]string // locale -> key -> message
	available []string                     // sorted locale codes
)

func load() {
	loadOnce.Do(func() {
		catalogs = map[string]map[string]string{}
		entries, err := catalogFS.ReadDir("locales")
		if err != nil {
			catalogs[DefaultLocale] = map[string]string{}
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			code := strings.TrimSuffix(e.Name(), ".json")
			data, rerr := catalogFS.ReadFile("locales/" + e.Name())
			if rerr != nil {
				continue
			}
			var m map[string]string
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			catalogs[code] = m
			available = append(available, code)
		}
		sort.Strings(available)
	})
}

// AvailableLocales returns the locale codes that have a catalog (sorted).
func AvailableLocales() []string {
	load()
	out := make([]string, len(available))
	copy(out, available)
	return out
}

// HasLocale reports whether a catalog exists for the code.
func HasLocale(code string) bool {
	load()
	_, ok := catalogs[code]
	return ok
}

// T returns the message for key in locale, falling back to English and then to
// the key itself. args replace ordered "{0}", "{1}", … placeholders.
func T(locale, key string, args ...string) string {
	load()
	msg, ok := catalogs[locale][key]
	if !ok || msg == "" {
		if msg, ok = catalogs[DefaultLocale][key]; !ok || msg == "" {
			msg = key
		}
	}
	if len(args) > 0 {
		msg = interpolate(msg, args)
	}
	return msg
}

func interpolate(msg string, args []string) string {
	var b strings.Builder
	for i := 0; i < len(msg); i++ {
		if msg[i] == '{' {
			if j := strings.IndexByte(msg[i:], '}'); j > 0 {
				idx := 0
				valid := true
				for _, c := range msg[i+1 : i+j] {
					if c < '0' || c > '9' {
						valid = false
						break
					}
					idx = idx*10 + int(c-'0')
				}
				if valid && idx < len(args) {
					b.WriteString(args[idx])
					i += j
					continue
				}
			}
		}
		b.WriteByte(msg[i])
	}
	return b.String()
}

// Negotiate picks the best available locale for an explicit preference and/or
// an Accept-Language header. An explicit non-empty pref with a catalog wins;
// otherwise each Accept-Language entry is matched exactly then by base
// language (e.g. "de-DE" → "de"); otherwise English.
func Negotiate(pref, acceptLanguage string) string {
	load()
	if pref != "" && HasLocale(pref) {
		return pref
	}
	for _, part := range strings.Split(acceptLanguage, ",") {
		part = strings.TrimSpace(part)
		if i := strings.IndexByte(part, ';'); i >= 0 {
			part = part[:i]
		}
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if HasLocale(part) {
			return part
		}
		if i := strings.IndexByte(part, '-'); i > 0 {
			if base := part[:i]; HasLocale(base) {
				return base
			}
		}
	}
	return DefaultLocale
}
