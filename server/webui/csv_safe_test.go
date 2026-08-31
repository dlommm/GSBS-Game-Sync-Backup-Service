package webui

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
)

// Exported CSVs carry attacker-influenced values (wiki-sourced game titles and
// save paths, client-supplied log context). A field starting with a formula
// character executes when an admin opens the file in a spreadsheet.
func TestNeutralizeCSVField(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"Portal 2", "Portal 2"},
		{"=cmd|'/c calc'!A1", "'=cmd|'/c calc'!A1"},
		{"+1+1", "'+1+1"},
		{"-2+3", "'-2+3"},
		{"@SUM(A1)", "'@SUM(A1)"},
		{"\tleading tab", "'\tleading tab"},
		{"\rleading cr", "'\rleading cr"},
		{"a=b", "a=b"}, // only a LEADING formula char matters
	} {
		if got := neutralizeCSVField(tc.in); got != tc.want {
			t.Errorf("neutralizeCSVField(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWriteCSVRowNeutralizesEveryField(t *testing.T) {
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if err := writeCSVRow(cw, "ok", "=DANGER()", "+2"); err != nil {
		t.Fatal(err)
	}
	cw.Flush()
	got := strings.TrimSpace(buf.String())
	want := `ok,'=DANGER(),'+2`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
