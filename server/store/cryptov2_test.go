package store

import (
	"context"
	"testing"
	"time"
)

func TestVersionMajor(t *testing.T) {
	cases := map[string]int{
		"4.0.0": 4, "v4.1.2": 4, "4.0.0-dev": 4, "10.2": 10,
		"": 0, "abc": 0, "3.2.4": 3, "v3": 3,
	}
	for in, want := range cases {
		if got := versionMajor(in); got != want {
			t.Errorf("versionMajor(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestCryptoV2Ready(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := st.(*sqliteStore)
	ctx := context.Background()
	userID, err := st.CreateUser(ctx, "u", "h")
	if err != nil {
		t.Fatal(err)
	}

	clientID := func(token string) string {
		_, id, _, _, err := st.ClientByToken(ctx, token)
		if err != nil {
			t.Fatalf("ClientByToken: %v", err)
		}
		return id
	}

	// Single up-to-date device: ready.
	tokA, err := st.RegisterClient(ctx, userID, "desktop", "linux")
	if err != nil {
		t.Fatal(err)
	}
	idA := clientID(tokA)
	if err := st.UpdateClientLastSeen(ctx, idA, "4.0.0"); err != nil {
		t.Fatal(err)
	}
	if ready, err := st.CryptoV2Ready(ctx, userID); err != nil || !ready {
		t.Fatalf("single v4 device: ready=%v err=%v, want true", ready, err)
	}

	// A second, legacy device holds the fleet back.
	tokB, err := st.RegisterClient(ctx, userID, "laptop", "windows")
	if err != nil {
		t.Fatal(err)
	}
	idB := clientID(tokB)
	if err := st.UpdateClientLastSeen(ctx, idB, "3.2.4"); err != nil {
		t.Fatal(err)
	}
	if ready, _ := st.CryptoV2Ready(ctx, userID); ready {
		t.Fatal("legacy device present: want not ready")
	}

	// Empty version (pre-4.0 clients send no header) also counts as legacy.
	if err := st.UpdateClientLastSeen(ctx, idB, ""); err != nil {
		t.Fatal(err)
	}
	if ready, _ := st.CryptoV2Ready(ctx, userID); ready {
		t.Fatal("versionless device present: want not ready")
	}

	// The legacy device upgrades: ready again.
	if err := st.UpdateClientLastSeen(ctx, idB, "4.1.0"); err != nil {
		t.Fatal(err)
	}
	if ready, _ := st.CryptoV2Ready(ctx, userID); !ready {
		t.Fatal("all devices v4+: want ready")
	}

	// A stale legacy device (last seen >30 days ago) doesn't hold the fleet back.
	stale := time.Now().UTC().AddDate(0, 0, -40).Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE clients SET app_version = '3.1.0', last_seen = ? WHERE id = ?`, stale, idB); err != nil {
		t.Fatal(err)
	}
	if ready, _ := st.CryptoV2Ready(ctx, userID); !ready {
		t.Fatal("stale legacy device: want ready")
	}
}
