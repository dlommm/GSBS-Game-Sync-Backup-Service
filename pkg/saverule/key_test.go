package saverule

import (
	"testing"

	"github.com/gsbs/gsbs/pkg/types"
)

// TestRuleKey_SlotLabel_SameAcrossOSes verifies that the same game_id + slot_label + is_config
// produces the same path_key regardless of Platform or Directory (OS-specific fields).
func TestRuleKey_SlotLabel_SameAcrossOSes(t *testing.T) {
	winRule := types.SaveRule{
		SlotLabel:       "0",
		Platform:        "windows",
		Directory:       `%APPDATA%\EldenRing\<user-id>`,
		IncludePatterns: []string{"*.sl2"},
		IsConfig:        false,
	}
	linRule := types.SaveRule{
		SlotLabel:       "0",
		Platform:        "linux",
		Directory:       `<SteamLibrary-folder>/steamapps/compatdata/1245620/pfx/drive_c/users/steamuser/AppData/Roaming/EldenRing/<user-id>`,
		IncludePatterns: []string{"*.sl2"},
		IsConfig:        false,
	}

	winKey := RuleKey("EldenRing", winRule)
	linKey := RuleKey("EldenRing", linRule)

	if winKey != linKey {
		t.Fatalf("expected identical key across OSes for same SlotLabel, got windows=%q linux=%q", winKey, linKey)
	}
	if len(winKey) != 16 {
		t.Fatalf("expected 16-char key, got %q (len %d)", winKey, len(winKey))
	}
}

// TestRuleKey_SlotLabel_IsConfigDistinct verifies that save and config slots with the same
// slot_label produce different keys.
func TestRuleKey_SlotLabel_IsConfigDistinct(t *testing.T) {
	save := types.SaveRule{SlotLabel: "0", IsConfig: false}
	cfg := types.SaveRule{SlotLabel: "0", IsConfig: true}

	saveKey := RuleKey("TestGame", save)
	cfgKey := RuleKey("TestGame", cfg)

	if saveKey == cfgKey {
		t.Fatalf("save and config with same slot_label must produce different keys, both got %q", saveKey)
	}
}

// TestRuleKey_SlotLabel_DifferentSlots verifies that different slot indices produce different keys.
func TestRuleKey_SlotLabel_DifferentSlots(t *testing.T) {
	r0 := types.SaveRule{SlotLabel: "0", IsConfig: false}
	r1 := types.SaveRule{SlotLabel: "1", IsConfig: false}

	k0 := RuleKey("TestGame", r0)
	k1 := RuleKey("TestGame", r1)

	if k0 == k1 {
		t.Fatalf("different slot_labels must produce different keys")
	}
}

// TestRuleKey_SlotLabel_DifferentGames verifies that the same slot_label on different games
// produces different keys.
func TestRuleKey_SlotLabel_DifferentGames(t *testing.T) {
	rule := types.SaveRule{SlotLabel: "0", IsConfig: false}

	k1 := RuleKey("GameA", rule)
	k2 := RuleKey("GameB", rule)

	if k1 == k2 {
		t.Fatalf("same slot_label on different game_ids must produce different keys")
	}
}

// TestRuleKey_EmptySlotLabel_BackwardCompat verifies that a rule with no SlotLabel still
// produces a valid, deterministic key (user-defined rule fallback).
func TestRuleKey_EmptySlotLabel_BackwardCompat(t *testing.T) {
	rule := types.SaveRule{
		Directory:       `%APPDATA%\MyGame`,
		Platform:        "windows",
		IncludePatterns: []string{"*.sav"},
		IsConfig:        false,
	}

	k1 := RuleKey("MyGame", rule)
	k2 := RuleKey("MyGame", rule)

	if k1 == "" {
		t.Fatal("expected non-empty key for user-defined rule")
	}
	if k1 != k2 {
		t.Fatal("user-defined rule key must be deterministic")
	}
	if len(k1) != 16 {
		t.Fatalf("expected 16-char key, got %q (len %d)", k1, len(k1))
	}
}

// TestRuleKey_EmptySlotLabel_OSChangesKey verifies that user-defined rules (no SlotLabel)
// produce DIFFERENT keys when Platform changes — confirming the legacy behavior is preserved.
func TestRuleKey_EmptySlotLabel_OSChangesKey(t *testing.T) {
	winRule := types.SaveRule{
		Directory:       `%APPDATA%\MyGame`,
		Platform:        "windows",
		IncludePatterns: []string{"*.sav"},
	}
	linRule := types.SaveRule{
		Directory:       `~/.config/mygame`,
		Platform:        "linux",
		IncludePatterns: []string{"*.sav"},
	}

	winKey := RuleKey("MyGame", winRule)
	linKey := RuleKey("MyGame", linRule)

	if winKey == linKey {
		t.Fatalf("user-defined rules with different directories/platforms should have different keys")
	}
}

// TestPathKeyForFile_StableWithSlotLabel verifies PathKeyForFile works correctly when
// the underlying rule key is derived from a slot_label.
func TestPathKeyForFile_StableWithSlotLabel(t *testing.T) {
	rule := types.SaveRule{SlotLabel: "0", IsConfig: false, Platform: "windows"}

	ruleKey := RuleKey("TestGame", rule)
	pk1 := PathKeyForFile(ruleKey, "save.sl2")
	pk2 := PathKeyForFile(ruleKey, "save.sl2")

	if pk1 != pk2 {
		t.Fatal("PathKeyForFile must be deterministic")
	}
	if len(pk1) != 16 {
		t.Fatalf("expected 16-char key, got %q", pk1)
	}

	// Different file → different key
	pk3 := PathKeyForFile(ruleKey, "other.sl2")
	if pk1 == pk3 {
		t.Fatal("different relative paths must produce different keys")
	}
}
