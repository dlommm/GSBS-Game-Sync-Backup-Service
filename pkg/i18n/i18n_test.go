package i18n

import "testing"

func TestT_LookupAndFallback(t *testing.T) {
	if got := T("en", "nav.dashboard"); got != "Dashboard" {
		t.Fatalf("en lookup = %q", got)
	}
	// Unknown locale falls back to English.
	if got := T("xx", "nav.dashboard"); got != "Dashboard" {
		t.Fatalf("unknown-locale fallback = %q", got)
	}
	// Unknown key falls back to the key itself (never blank).
	if got := T("en", "does.not.exist"); got != "does.not.exist" {
		t.Fatalf("unknown-key fallback = %q", got)
	}
}

func TestInterpolate(t *testing.T) {
	// The catalog has no placeholder strings yet, so test the mechanism directly.
	catalogsForTest()["en"]["_test.greet"] = "Hello {0}, you have {1} messages"
	if got := T("en", "_test.greet", "Alex", "3"); got != "Hello Alex, you have 3 messages" {
		t.Fatalf("interpolate = %q", got)
	}
	// Out-of-range index is left as-is.
	if got := T("en", "_test.greet", "Alex"); got != "Hello Alex, you have {1} messages" {
		t.Fatalf("partial interpolate = %q", got)
	}
}

func TestNegotiate(t *testing.T) {
	cases := []struct {
		pref, accept, want string
	}{
		{"en", "", "en"},
		{"", "en-US,en;q=0.9", "en"},
		{"", "de-DE,de;q=0.9,en;q=0.8", "en"}, // de has no catalog → falls to en
		{"xx", "", "en"},                      // invalid pref ignored
		{"", "", "en"},
	}
	for _, c := range cases {
		if got := Negotiate(c.pref, c.accept); got != c.want {
			t.Errorf("Negotiate(%q,%q) = %q, want %q", c.pref, c.accept, got, c.want)
		}
	}
}

// TestEnglishIsComplete guards the invariant that English is the source of
// truth: every key referenced by any other catalog must exist in en.json, so a
// translation can never introduce a key that English lacks.
func TestEnglishIsComplete(t *testing.T) {
	load()
	en := catalogs[DefaultLocale]
	if len(en) == 0 {
		t.Fatal("English catalog is empty")
	}
	for locale, cat := range catalogs {
		if locale == DefaultLocale {
			continue
		}
		for key := range cat {
			if _, ok := en[key]; !ok {
				t.Errorf("locale %q defines key %q that is missing from English (en.json)", locale, key)
			}
		}
	}
}

// catalogsForTest exposes the loaded catalogs for the interpolation test.
func catalogsForTest() map[string]map[string]string {
	load()
	return catalogs
}
