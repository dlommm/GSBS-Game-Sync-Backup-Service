package saverule

import (
	"strings"
	"testing"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/stretchr/testify/assert"
)

func TestValidateRule_RejectsEmptyDirectory(t *testing.T) {
	assert.NotEmpty(t, ValidateRule(types.SaveRule{Directory: ""}, "linux"))
	assert.NotEmpty(t, ValidateRule(types.SaveRule{Directory: "."}, "linux"))
}

func TestValidateRule_RejectsInvalidGlob(t *testing.T) {
	reason := ValidateRule(types.SaveRule{
		Directory:       "/tmp",
		IncludePatterns: []string{"["},
	}, "linux")
	assert.Contains(t, reason, "invalid glob")
}

func TestValidateTemplate_RejectsTraversal(t *testing.T) {
	traversalCases := []string{
		`%APPDATA%/../../../etc/passwd`,
		`../secret`,
		`%USERPROFILE%/../../Windows`,
		`foo/../../bar`,
	}
	for _, raw := range traversalCases {
		if err := ValidateTemplate(raw); err == nil {
			t.Errorf("ValidateTemplate(%q) expected error, got nil", raw)
		}
	}
}

func TestValidateTemplate_AcceptsValidPaths(t *testing.T) {
	validCases := []string{
		`%APPDATA%/Game/saves`,
		`%USERPROFILE%/Documents/My Games`,
		`<SteamLibrary-folder>/userdata/12345/remote`,
	}
	for _, raw := range validCases {
		if err := ValidateTemplate(raw); err != nil {
			t.Errorf("ValidateTemplate(%q) unexpected error: %v", raw, err)
		}
	}
}

func TestValidateRule_RejectsTraversal(t *testing.T) {
	reason := ValidateRule(types.SaveRule{
		Directory: `%APPDATA%/../../../etc/passwd`,
		SyncAll:   true,
	}, "windows")
	if reason == "" {
		t.Fatal("expected non-empty reason for traversal path")
	}
	if !strings.Contains(reason, "..") {
		t.Errorf("reason %q should mention '..'", reason)
	}
}

func TestParseSaveRules_TraversalExcluded(t *testing.T) {
	raw := `%APPDATA%/Game/../../secret`
	got := ParseSaveRules(raw, "windows", false, identityNormalize)
	if len(got) != 0 {
		t.Errorf("traversal path should be excluded, got %+v", got)
	}
}

func TestFilterValidRules(t *testing.T) {
	rules := []types.SaveRule{
		{Directory: "/good", SyncAll: true, Platform: "linux"},
		{Directory: "", SyncAll: true},
	}
	valid, skips := FilterValidRules(rules, "linux")
	assert.Len(t, valid, 1)
	assert.Len(t, skips, 1)
}
