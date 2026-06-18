package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func idx(manifestVer, fullVer int) PCGWBundleIndex {
	return PCGWBundleIndex{
		ManifestVersion: manifestVer,
		Full:            PCGWBundleIndexEntry{Version: fullVer, URL: "https://x/full.json.gz", SHA256: "f"},
	}
}

func TestPlanBundleCatchup(t *testing.T) {
	tests := []struct {
		name      string
		merged    int
		index     PCGWBundleIndex
		wantKinds []string
		wantFinal int // merged version after the last step (0 if no steps)
	}{
		{"already current", 5, idx(5, 5), nil, 0},
		{"ahead (defensive)", 9, idx(5, 5), nil, 0},
		{"fresh server", 0, idx(5, 5), []string{"full"}, 5},
		{"behind one version", 4, idx(5, 5), []string{"full"}, 5},
		{"far behind a new baseline", 7, idx(12, 12), []string{"full"}, 12},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			steps := PlanBundleCatchup(tc.merged, tc.index)
			if len(steps) != len(tc.wantKinds) {
				t.Fatalf("got %d steps %v, want %v", len(steps), kinds(steps), tc.wantKinds)
			}
			for i, k := range tc.wantKinds {
				if steps[i].Kind != k {
					t.Fatalf("step %d kind = %q, want %q", i, steps[i].Kind, k)
				}
				if steps[i].Mode != "merge_skip_unchanged" {
					t.Fatalf("step %d mode = %q, want merge_skip_unchanged", i, steps[i].Mode)
				}
			}
			if len(steps) > 0 && steps[len(steps)-1].Version != tc.wantFinal {
				t.Fatalf("final version = %d, want %d", steps[len(steps)-1].Version, tc.wantFinal)
			}
		})
	}
}

func kinds(steps []BundleStep) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.Kind)
	}
	return out
}

func TestAdvanceBundleIndex(t *testing.T) {
	base := "https://example.com/manifest/"

	// v1: full.
	v1, err := AdvanceBundleIndex(PCGWBundleIndex{}, "sha-full-1", 100, base, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if v1.ManifestVersion != 1 || v1.Full.Version != 1 {
		t.Fatalf("v1 = %+v", v1)
	}
	// URLs carry a content-addressed ?h=<sha-prefix> cache-busting query.
	if !strings.HasPrefix(v1.Full.URL, base+"manifest.json.gz?h=") {
		t.Fatalf("v1 full url = %s", v1.Full.URL)
	}

	// Each subsequent publish increments the version and overwrites the full.
	v2, err := AdvanceBundleIndex(v1, "sha-full-2", 110, base, "t2")
	if err != nil {
		t.Fatal(err)
	}
	if v2.ManifestVersion != 2 || v2.Full.Version != 2 {
		t.Fatalf("v2 = %+v", v2)
	}

	v3, _ := AdvanceBundleIndex(v2, "sha-full-3", 120, base, "t3")
	if v3.ManifestVersion != 3 || v3.Full.Version != 3 {
		t.Fatalf("v3 = %+v", v3)
	}

	// A missing base URL is an error.
	if _, err := AdvanceBundleIndex(v3, "sha", 1, "", "t"); err == nil {
		t.Fatal("empty base URL should error")
	}

	// The produced indexes must all pass ParsePCGWBundleIndex validation.
	for i, ix := range []PCGWBundleIndex{v1, v2, v3} {
		b, _ := json.Marshal(ix)
		if _, err := ParsePCGWBundleIndex(b); err != nil {
			t.Errorf("index %d failed validation: %v", i+1, err)
		}
	}
}

func TestParsePCGWBundleIndex_Validation(t *testing.T) {
	good := `{"manifest_version":7,"full":{"version":7,"url":"u"}}`
	if _, err := ParsePCGWBundleIndex([]byte(good)); err != nil {
		t.Fatalf("good index rejected: %v", err)
	}

	// A legacy index.json that still carries a delta field parses fine; the
	// delta is simply ignored.
	legacy := `{"manifest_version":7,"full":{"version":5,"url":"u"},"delta":{"version":7,"base_version":5,"url":"d"}}`
	if _, err := ParsePCGWBundleIndex([]byte(legacy)); err != nil {
		t.Fatalf("legacy index with delta rejected: %v", err)
	}

	bad := []string{
		`{}`,                     // missing version
		`{"manifest_version":1}`, // missing full
		`{"manifest_version":0,"full":{"version":1,"url":"u"}}`, // version must be >= 1
	}
	for i, b := range bad {
		if _, err := ParsePCGWBundleIndex([]byte(b)); err == nil {
			t.Errorf("bad index %d accepted, want error", i)
		}
	}
}
