package pcgw

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGoldenSubnautica2Sections(t *testing.T) {
	wiki := loadFixture(t, "subnautica2.wikitext")
	sections := SplitWikiSections(wiki)
	for _, key := range []string{"game_data", "availability", "video", "system_requirements"} {
		if _, ok := sections[key]; !ok {
			t.Errorf("missing section %q", key)
		}
	}
	infobox := ParseInfoboxGame(wiki)
	if infobox["developers"] == "" {
		t.Error("expected developers in infobox")
	}
	locs := ParseSaveLocationsFromWikitext(wiki, "999")
	if len(locs) < 2 {
		t.Fatalf("expected save locations, got %d", len(locs))
	}
	for _, l := range locs {
		for _, p := range l.Paths {
			if strings.Contains(p, "{{p") || strings.HasSuffix(p, "}}") && !strings.Contains(p, "<") {
				t.Errorf("malformed path %q for system %q", p, l.System)
			}
		}
	}
	for _, l := range locs {
		if strings.Contains(l.System, "Windows") {
			for _, p := range l.Paths {
				if strings.Contains(p, "Subnautica2") && !strings.HasPrefix(p, "%APPDATA%") {
					t.Errorf("expected normalized appdata path, got %q", p)
				}
			}
		}
	}
}

func TestGolden007FirstLightLinux(t *testing.T) {
	wiki := loadFixture(t, "007_first_light.wikitext")
	locs := ParseSaveLocationsFromWikitext(wiki, "888")
	hasLinux := false
	for _, l := range locs {
		if strings.Contains(l.System, "Linux") {
			hasLinux = true
		}
	}
	if !hasLinux {
		t.Fatal("expected Linux save path")
	}
	if !strings.Contains(NormalizePathTemplate("{{p|heroic}}/save"), "<Heroic-folder>") {
		t.Fatal("heroic placeholder not mapped")
	}
}

func TestIngestResiliencePartialSection(t *testing.T) {
	wiki := loadFixture(t, "subnautica2.wikitext")
	bundle := GameBundle{Sections: make(map[string]SectionResult)}
	rawSections := SplitWikiSections(wiki)
	for key, sec := range rawSections {
		sr := SectionResult{Key: key, SectionWikitext: sec.body, AllTemplates: ExtractAllTemplates(sec.body)}
		data, err := parseSectionStructured(key, sec.body, "1")
		if err != nil {
			sr.ParseError = err.Error()
		} else {
			sr.Data = data
		}
		bundle.Sections[key] = sr
	}
	if len(bundle.Sections) == 0 {
		t.Fatal("no sections")
	}
	for _, sr := range bundle.Sections {
		if sr.SectionWikitext == "" {
			t.Errorf("section %s missing raw wikitext", sr.Key)
		}
	}
}
