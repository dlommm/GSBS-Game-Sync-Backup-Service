package webui

import "testing"

func TestIsNumericGameID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"1", true},
		{"123456", true},
		{"123456789012", true},   // 12 digits: max allowed
		{"1234567890123", false}, // 13 digits: too long
		{"", false},
		{"12a4", false},
		{"-123", false},
		{"1.5", false},
		{"..", false},
		{"../etc/passwd", false},
		{"12 3", false},
	}
	for _, tc := range tests {
		if got := isNumericGameID(tc.id); got != tc.want {
			t.Errorf("isNumericGameID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestSanitizeFilenamePart(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"12345", "12345"},
		{`a/b\c`, "a_b_c"},
		{`quote"break`, "quote_break"},
		{"semi;colon", "semi_colon"},
		{"ctrl\x00\x1fchar", "ctrl__char"},
		{"plain-name_1.2", "plain-name_1.2"},
	}
	for _, tc := range tests {
		if got := sanitizeFilenamePart(tc.in); got != tc.want {
			t.Errorf("sanitizeFilenamePart(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
