package main

import "testing"

func TestApplyPolicyOverrideForm(t *testing.T) {
	cfg := &config{ConflictPolicyOverrides: map[string]string{
		"g1": "keep_local",
		"g2": "keep_server",
	}}
	applyPolicyOverrideForm(cfg, map[string][]string{
		"override_policy::g1":  {"last_write_wins"}, // change
		"override_policy::g2":  {"remove"},          // delete
		"override_policy::g3":  {"bogus"},           // invalid policy ignored
		"override_policy::":    {"keep_local"},      // empty game ID ignored
		"override_add_game":    {"  g4  "},          // trimmed add
		"override_add_policy":  {"keep_server"},
		"unrelated_form_field": {"x"},
	})
	if got := cfg.ConflictPolicyOverrides["g1"]; got != "last_write_wins" {
		t.Errorf("g1 = %q, want last_write_wins", got)
	}
	if _, ok := cfg.ConflictPolicyOverrides["g2"]; ok {
		t.Error("g2 should be removed")
	}
	if _, ok := cfg.ConflictPolicyOverrides["g3"]; ok {
		t.Error("g3 with invalid policy should not be added")
	}
	if got := cfg.ConflictPolicyOverrides["g4"]; got != "keep_server" {
		t.Errorf("g4 = %q, want keep_server", got)
	}
	if len(cfg.ConflictPolicyOverrides) != 2 {
		t.Errorf("want 2 overrides, got %v", cfg.ConflictPolicyOverrides)
	}

	// Add into a nil map works.
	empty := &config{}
	applyPolicyOverrideForm(empty, map[string][]string{
		"override_add_game":   {"g9"},
		"override_add_policy": {"keep_local"},
	})
	if got := empty.ConflictPolicyOverrides["g9"]; got != "keep_local" {
		t.Errorf("nil-map add: g9 = %q, want keep_local", got)
	}
}
