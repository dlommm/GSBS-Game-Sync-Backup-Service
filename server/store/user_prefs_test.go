package store

import (
	"context"
	"testing"
)

func TestUserPrefsRoundTrip(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	uid, err := st.CreateUser(ctx, "prefuser", "h")
	if err != nil {
		t.Fatal(err)
	}

	if v, err := st.GetUserPref(ctx, uid, "appearance.design"); err != nil || v != "" {
		t.Fatalf("unset pref = %q, %v; want \"\", nil", v, err)
	}
	if err := st.SetUserPref(ctx, uid, "appearance.design", "hud"); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.GetUserPref(ctx, uid, "appearance.design"); v != "hud" {
		t.Fatalf("pref = %q, want hud", v)
	}
	// Upsert replaces.
	if err := st.SetUserPref(ctx, uid, "appearance.design", "crt"); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.GetUserPref(ctx, uid, "appearance.design"); v != "crt" {
		t.Fatalf("pref = %q, want crt", v)
	}
	// Keys are independent.
	if err := st.SetUserPref(ctx, uid, "appearance.layout", "dense"); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.GetUserPref(ctx, uid, "appearance.design"); v != "crt" {
		t.Fatalf("design pref clobbered by layout write: %q", v)
	}
	// Empty value deletes.
	if err := st.SetUserPref(ctx, uid, "appearance.design", ""); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.GetUserPref(ctx, uid, "appearance.design"); v != "" {
		t.Fatalf("pref after clear = %q, want \"\"", v)
	}
}
