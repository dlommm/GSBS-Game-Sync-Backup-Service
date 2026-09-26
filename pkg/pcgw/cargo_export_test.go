package pcgw

import (
	"strings"
	"testing"
)

func TestDecodeCargoExportResponse(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantRows int
		wantErr  string
	}{
		{
			name:     "rows",
			body:     `[{"PageID":5,"Title":"Titan Quest"},{"PageID":7,"Title":"Left 4 Dead"}]`,
			wantRows: 2,
		},
		{
			// Special:CargoExport pads its JSON with newlines in some responses.
			name:     "leading whitespace",
			body:     "\n\t  [{\"PageID\":5}]",
			wantRows: 1,
		},
		{
			name:     "no matching rows",
			body:     `[]`,
			wantRows: 0,
		},
		{
			// An empty body is "nothing matched", not a malformed response —
			// the catalog scan uses a zero-length page as its stop condition.
			name:     "empty body",
			body:     "",
			wantRows: 0,
		},
		{
			// The failure that made the outage hard to read: an unknown table
			// is a plain-text page served under HTTP 200.
			name:    "plain text error",
			body:    "Error: Table Infobox_game not found.",
			wantErr: "Table Infobox_game not found",
		},
		{
			name:    "html error page",
			body:    "<!DOCTYPE html><html><body><p>Error: no field named Foo</p></body></html>",
			wantErr: "no field named Foo",
		},
		{
			name:    "truncated json",
			body:    `[{"PageID":5`,
			wantErr: "decode",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := decodeCargoExportResponse(strings.NewReader(tc.body))
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got rows=%v", tc.wantErr, rows)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err=%v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(rows) != tc.wantRows {
				t.Fatalf("len(rows)=%d want %d", len(rows), tc.wantRows)
			}
		})
	}
}

// TestParseCargoValuesAcrossBackends pins that both response shapes normalise
// to the same strings: Special:CargoExport sends typed JSON, action=cargoquery
// sends everything as strings.
func TestParseCargoValuesAcrossBackends(t *testing.T) {
	t.Run("export types", func(t *testing.T) {
		if got := parseCargoSingleValue(float64(5)); got != "5" {
			t.Errorf("number: got %q want \"5\"", got)
		}
		if got := parseCargoMultiValue([]interface{}{float64(4540), float64(4550)}); len(got) != 2 || got[0] != "4540" {
			t.Errorf("number array: %v", got)
		}
		// The reason the export backend is the default: a value containing a
		// comma survives, where the string path would split it.
		got := parseCargoMultiValue([]interface{}{"Bandai Namco, Inc.", "Sega"})
		if len(got) != 2 || got[0] != "Bandai Namco, Inc." {
			t.Errorf("comma-bearing value: %v", got)
		}
		// Cargo serialises an empty List-of column as [""].
		if got := parseCargoMultiValue([]interface{}{""}); len(got) != 0 {
			t.Errorf("empty list: %v want none", got)
		}
		// A single-valued read of a list takes the first non-empty entry.
		if got := parseCargoSingleValue([]interface{}{"", "123"}); got != "123" {
			t.Errorf("single from list: %q", got)
		}
	})

	t.Run("cargoquery types", func(t *testing.T) {
		if got := parseCargoSingleValue("5"); got != "5" {
			t.Errorf("string: %q", got)
		}
		if got := parseCargoMultiValue("4540,4550"); len(got) != 2 || got[1] != "4550" {
			t.Errorf("comma string: %v", got)
		}
		if got := parseCargoSingleValue(map[string]interface{}{"fulltext": " Titan Quest "}); got != "Titan Quest" {
			t.Errorf("fulltext: %q", got)
		}
	})

	t.Run("absent", func(t *testing.T) {
		if got := parseCargoSingleValue(nil); got != "" {
			t.Errorf("nil: %q", got)
		}
		if got := parseCargoMultiValue(nil); got != nil {
			t.Errorf("nil multi: %v", got)
		}
	})
}
