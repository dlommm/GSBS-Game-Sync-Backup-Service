package saverule

import "testing"

func TestMatchInclude_SyncAll(t *testing.T) {
	if !MatchInclude("subdir/save.dat", nil, true) {
		t.Fatal("syncAll with no patterns should match all files")
	}
	if MatchInclude("save.dat", nil, false) {
		t.Fatal("no patterns and not syncAll should not match")
	}
}

func TestMatchInclude_Patterns(t *testing.T) {
	patterns := []string{"*.sav", "profile.dat"}
	if !MatchInclude("game.sav", patterns, false) {
		t.Fatal("expected basename glob match")
	}
	if !MatchInclude("subdir/profile.dat", patterns, false) {
		t.Fatal("expected full relative path match")
	}
	if MatchInclude("game.tmp", patterns, false) {
		t.Fatal("expected non-match")
	}
}
