package store

import (
	"context"
	"testing"
	"time"
)

func TestPruneHistory(t *testing.T) {
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

	old := time.Now().UTC().AddDate(0, 0, -400).Format(time.RFC3339)
	fresh := time.Now().UTC().Format(time.RFC3339)

	mustExec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := s.db.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// Two old + one fresh row per history table.
	mustExec(`INSERT INTO audit_log (id, at, actor_user_id, actor_username, action) VALUES ('a1',?,?, 'u','login'),('a2',?,?,'u','login'),('a3',?,?,'u','login')`,
		old, userID, old, userID, fresh, userID)
	mustExec(`INSERT INTO manifest_fetches (id, username, entries_count, fetched_at) VALUES ('m1','u',1,?),('m2','u',1,?),('m3','u',1,?)`,
		old, old, fresh)
	mustExec(`INSERT INTO stats_snapshots (id, at, user_count, client_count, save_count, storage_bytes) VALUES ('s1',?,1,1,1,1),('s2',?,1,1,1,1)`,
		old, fresh)

	// Five versions of one slot, all ancient: the newest 3 must survive
	// age-based pruning (newest-N floor).
	for v := 1; v <= 5; v++ {
		mustExec(`INSERT INTO save_versions (user_id, game_id, path_key, version, content, updated_at, content_hash) VALUES (?,?,?,?,?,?,?)`,
			userID, "g1", "pk1", v, []byte("x"), old, "h")
	}

	pc, err := st.PruneHistory(ctx, 180, 30, 730, 90)
	if err != nil {
		t.Fatalf("PruneHistory: %v", err)
	}
	if pc.Audit != 2 {
		t.Errorf("Audit pruned = %d, want 2", pc.Audit)
	}
	if pc.ManifestFetches != 2 {
		t.Errorf("ManifestFetches pruned = %d, want 2", pc.ManifestFetches)
	}
	// stats: old row is 400 days old, window 730 -> kept.
	if pc.Stats != 0 {
		t.Errorf("Stats pruned = %d, want 0 (400d < 730d window)", pc.Stats)
	}
	if pc.SaveVersions != 2 {
		t.Errorf("SaveVersions pruned = %d, want 2 (5 ancient, floor keeps newest 3)", pc.SaveVersions)
	}

	var survivors []int
	rows, err := s.db.Query(`SELECT version FROM save_versions WHERE user_id=? ORDER BY version`, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		survivors = append(survivors, v)
	}
	if len(survivors) != 3 || survivors[0] != 3 || survivors[2] != 5 {
		t.Fatalf("surviving versions = %v, want [3 4 5]", survivors)
	}

	// Disabled windows (0) prune nothing.
	pc2, err := st.PruneHistory(ctx, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pc2.Total() != 0 {
		t.Fatalf("disabled prune removed %d rows", pc2.Total())
	}
}
