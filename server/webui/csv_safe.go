package webui

import (
	"encoding/csv"
	"strings"
)

// csvFormulaPrefixes are the characters a spreadsheet treats as the start of a
// formula rather than text.
const csvFormulaPrefixes = "=+-@\t\r"

// neutralizeCSVField prefixes a leading formula character with a single quote,
// which spreadsheets read as "this cell is text".
//
// Exported CSVs carry attacker-influenced values — game titles and save paths
// come from a public wiki, log context and audit details from client-supplied
// data. A field beginning with =, +, -, @, tab, or CR is executed as a formula
// when an admin opens the export in Excel, LibreOffice, or Sheets, which is a
// remote-content and data-exfiltration vector aimed squarely at an admin.
func neutralizeCSVField(v string) string {
	if v == "" {
		return v
	}
	if strings.ContainsRune(csvFormulaPrefixes, rune(v[0])) {
		return "'" + v
	}
	return v
}

// writeCSVRow writes one record with every field neutralized.
func writeCSVRow(cw *csv.Writer, fields ...string) error {
	safe := make([]string, len(fields))
	for i, f := range fields {
		safe[i] = neutralizeCSVField(f)
	}
	return cw.Write(safe)
}
