package store

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	_ "github.com/mattn/go-sqlite3"
)

// openTestDB creates an in-memory SQLite database for testing, applies only the
// core-tables step (step 1 + all alters up to step 15) so that the schema matches
// the pre-step-16 state, and returns the *sqliteStore and a cleanup func.
func openTestDBUpTo15(t *testing.T) *sqliteStore {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=off")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	st := &sqliteStore{db: db, dbPath: ":memory:"}

	// Apply all steps up to (and including) 15 so the schema is fully built
	// but step 16 has not run yet.
	steps := st.migrationSteps()
	for _, step := range steps {
		if step.version > 15 {
			break
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("begin tx for step %d: %v", step.version, err)
		}
		if err := step.fn(tx); err != nil {
			_ = tx.Rollback()
			t.Fatalf("step %d: %v", step.version, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", step.version)); err != nil {
			_ = tx.Rollback()
			t.Fatalf("set user_version=%d: %v", step.version, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit step %d: %v", step.version, err)
		}
	}
	return st
}

// TestMergeMigration verifies that step 16 correctly collapses per-OS path_key
// rows into the OS-neutral slot key and archives the loser's content as a
// save_versions entry.
func TestMergeMigration(t *testing.T) {
	st := openTestDBUpTo15(t)

	const (
		gameID = "99999"
		userID = "user-1"
	)

	// Windows and Linux rules for the same logical save slot (slot_label = "0").
	winRule := types.SaveRule{
		Directory: `%USERPROFILE%\SavedGames\TestGame`,
		Platform:  "windows",
		IsConfig:  false,
		SlotLabel: "0",
	}
	linRule := types.SaveRule{
		Directory: `~/.local/share/testgame`,
		Platform:  "linux",
		IsConfig:  false,
		SlotLabel: "0",
	}

	// Pre-compute keys the way old clients did (SlotLabel was not set → legacy hash).
	oldWinKey := migLegacyKey(gameID, winRule)
	oldLinKey := migLegacyKey(gameID, linRule)

	// The expected new OS-neutral key.
	wantKey := migSlotKey(gameID, "0", false)

	if oldWinKey == wantKey {
		t.Fatal("test setup error: old Windows key already equals new key — remap would be a no-op")
	}
	if oldLinKey == wantKey {
		t.Fatal("test setup error: old Linux key already equals new key — remap would be a no-op")
	}
	if oldWinKey == oldLinKey {
		t.Fatal("test setup error: old Windows key equals old Linux key — merge would be pointless")
	}

	// ── Seed users table ─────────────────────────────────────────────────────
	if _, err := st.db.Exec(
		`INSERT INTO users (id, username, password_hash, created_at) VALUES (?,?,?,?)`,
		userID, "testuser", "hash", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// ── Seed game_save_locations with rules that have SlotLabel set ──────────
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := st.db.Exec(
		`INSERT INTO game_save_locations (id,game_id,pcgw_page_id,game_title,platform,path_template,is_config,updated_at,source,save_rules_json)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"loc-win", gameID, 99999, "TestGame", "windows", winRule.Directory, 0, now, "pcgw",
		encodeSaveRules([]types.SaveRule{winRule})); err != nil {
		t.Fatalf("insert windows location: %v", err)
	}
	if _, err := st.db.Exec(
		`INSERT INTO game_save_locations (id,game_id,pcgw_page_id,game_title,platform,path_template,is_config,updated_at,source,save_rules_json)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"loc-lin", gameID, 99999, "TestGame", "linux", linRule.Directory, 0, now, "pcgw",
		encodeSaveRules([]types.SaveRule{linRule})); err != nil {
		t.Fatalf("insert linux location: %v", err)
	}

	// ── Seed two saves rows with the old per-OS path_keys ───────────────────
	// Windows save is newer (should survive).
	winTime := "2025-06-05T12:00:00Z"
	linTime := "2025-06-04T12:00:00Z"
	winContent := []byte("windows save data")
	linContent := []byte("linux save data")

	if _, err := st.db.Exec(
		`INSERT INTO saves (user_id,game_id,path_key,content,updated_at,encrypted) VALUES (?,?,?,?,?,0)`,
		userID, gameID, oldWinKey, winContent, winTime); err != nil {
		t.Fatalf("insert win save: %v", err)
	}
	if _, err := st.db.Exec(
		`INSERT INTO saves (user_id,game_id,path_key,content,updated_at,encrypted) VALUES (?,?,?,?,?,0)`,
		userID, gameID, oldLinKey, linContent, linTime); err != nil {
		t.Fatalf("insert lin save: %v", err)
	}

	// ── Run step 16 ──────────────────────────────────────────────────────────
	step16 := st.migrationSteps()
	var fn func(*sql.Tx) error
	for _, s := range step16 {
		if s.version == 16 {
			fn = s.fn
			break
		}
	}
	if fn == nil {
		t.Fatal("step 16 not found in migrationSteps")
	}
	tx, err := st.db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("step 16 returned error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// ── Assertions ───────────────────────────────────────────────────────────

	// Only one saves row should remain for this (user, game).
	var saveCount int
	if err := st.db.QueryRow(
		`SELECT COUNT(*) FROM saves WHERE user_id=? AND game_id=?`, userID, gameID).Scan(&saveCount); err != nil {
		t.Fatalf("count saves: %v", err)
	}
	if saveCount != 1 {
		t.Errorf("saves count: got %d, want 1", saveCount)
	}

	// The surviving row must have the new OS-neutral path_key.
	var survivingKey string
	var survivingContent []byte
	if err := st.db.QueryRow(
		`SELECT path_key, content FROM saves WHERE user_id=? AND game_id=?`, userID, gameID).Scan(&survivingKey, &survivingContent); err != nil {
		t.Fatalf("query survivor: %v", err)
	}
	if survivingKey != wantKey {
		t.Errorf("survivor path_key: got %q, want %q", survivingKey, wantKey)
	}
	// Survivor content must be the Windows content (newer updated_at).
	if string(survivingContent) != string(winContent) {
		t.Errorf("survivor content: got %q, want %q", survivingContent, winContent)
	}

	// The loser's content must appear in save_versions under the new key.
	var versionCount int
	if err := st.db.QueryRow(
		`SELECT COUNT(*) FROM save_versions WHERE user_id=? AND game_id=? AND path_key=?`,
		userID, gameID, wantKey).Scan(&versionCount); err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if versionCount == 0 {
		t.Error("save_versions: expected at least one preserved entry for the loser, got 0")
	}

	// The loser (Linux) content must be in save_versions.
	var found bool
	rows, err := st.db.Query(
		`SELECT content FROM save_versions WHERE user_id=? AND game_id=? AND path_key=?`,
		userID, gameID, wantKey)
	if err != nil {
		t.Fatalf("query versions: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c []byte
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan version content: %v", err)
		}
		if string(c) == string(linContent) {
			found = true
		}
	}
	if !found {
		t.Error("save_versions: Linux (loser) content not found in preserved versions")
	}

	// No save_versions entries should remain under the old path_keys.
	for _, oldKey := range []string{oldWinKey, oldLinKey} {
		var n int
		if err := st.db.QueryRow(
			`SELECT COUNT(*) FROM save_versions WHERE user_id=? AND game_id=? AND path_key=?`,
			userID, gameID, oldKey).Scan(&n); err != nil {
			t.Fatalf("count old versions for key %s: %v", oldKey, err)
		}
		if n != 0 {
			t.Errorf("save_versions still has %d row(s) under old path_key %s", n, oldKey)
		}
	}

	// No saves rows should remain under the old path_keys.
	for _, oldKey := range []string{oldWinKey, oldLinKey} {
		var n int
		if err := st.db.QueryRow(
			`SELECT COUNT(*) FROM saves WHERE user_id=? AND game_id=? AND path_key=?`,
			userID, gameID, oldKey).Scan(&n); err != nil {
			t.Fatalf("count old saves for key %s: %v", oldKey, err)
		}
		if n != 0 {
			t.Errorf("saves still has %d row(s) under old path_key %s", n, oldKey)
		}
	}
}

// TestMergeMigration_NoOp verifies that step 16 does nothing when game_save_locations
// has no rows (e.g. PCGW data hasn't been synced yet).
func TestMergeMigration_NoOp(t *testing.T) {
	st := openTestDBUpTo15(t)

	const userID = "user-noop"
	if _, err := st.db.Exec(
		`INSERT INTO users (id, username, password_hash, created_at) VALUES (?,?,?,?)`,
		userID, "noopuser", "hash", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	// Insert a save with an arbitrary path_key; it must be untouched.
	if _, err := st.db.Exec(
		`INSERT INTO saves (user_id,game_id,path_key,content,updated_at,encrypted) VALUES (?,?,?,?,?,0)`,
		userID, "game-noop", "somekey", []byte("data"), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("insert save: %v", err)
	}

	// Run step 16 with empty game_save_locations (no remap possible).
	step16 := st.migrationSteps()
	var fn func(*sql.Tx) error
	for _, s := range step16 {
		if s.version == 16 {
			fn = s.fn
			break
		}
	}
	tx, _ := st.db.Begin()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("step 16 no-op case error: %v", err)
	}
	_ = tx.Commit()

	// Save must be unchanged.
	var key string
	if err := st.db.QueryRow(`SELECT path_key FROM saves WHERE user_id=?`, userID).Scan(&key); err != nil {
		t.Fatalf("query save: %v", err)
	}
	if key != "somekey" {
		t.Errorf("no-op: path_key changed unexpectedly to %q", key)
	}
}

// TestMergeMigration_SingleRowRekey checks that a single saves row with an old
// path_key (no collision) is simply re-keyed to the new path_key.
func TestMergeMigration_SingleRowRekey(t *testing.T) {
	st := openTestDBUpTo15(t)

	const (
		gameID = "77777"
		userID = "user-rekey"
	)

	winRule := types.SaveRule{
		Directory: `%APPDATA%\SomeGame`,
		Platform:  "windows",
		IsConfig:  false,
		SlotLabel: "0",
	}
	oldKey := migLegacyKey(gameID, winRule)
	newKey := migSlotKey(gameID, "0", false)

	if _, err := st.db.Exec(
		`INSERT INTO users (id, username, password_hash, created_at) VALUES (?,?,?,?)`,
		userID, "rekeyuser", "hash", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := st.db.Exec(
		`INSERT INTO game_save_locations (id,game_id,pcgw_page_id,game_title,platform,path_template,is_config,updated_at,source,save_rules_json)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"loc-rekey", gameID, 77777, "SomeGame", "windows", winRule.Directory, 0,
		time.Now().UTC().Format(time.RFC3339), "pcgw",
		encodeSaveRules([]types.SaveRule{winRule})); err != nil {
		t.Fatalf("insert location: %v", err)
	}
	if _, err := st.db.Exec(
		`INSERT INTO saves (user_id,game_id,path_key,content,updated_at,encrypted) VALUES (?,?,?,?,?,0)`,
		userID, gameID, oldKey, []byte("save data"), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("insert save: %v", err)
	}

	step16 := st.migrationSteps()
	var fn func(*sql.Tx) error
	for _, s := range step16 {
		if s.version == 16 {
			fn = s.fn
			break
		}
	}
	tx, _ := st.db.Begin()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("step 16 error: %v", err)
	}
	_ = tx.Commit()

	var key string
	if err := st.db.QueryRow(`SELECT path_key FROM saves WHERE user_id=? AND game_id=?`, userID, gameID).Scan(&key); err != nil {
		t.Fatalf("query save: %v", err)
	}
	if key != newKey {
		t.Errorf("re-key: got path_key %q, want %q", key, newKey)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM saves WHERE user_id=? AND game_id=? AND path_key=?`, userID, gameID, oldKey).Scan(&n); err != nil {
		t.Fatalf("count old saves: %v", err)
	}
	if n != 0 {
		t.Errorf("old path_key row still exists after re-key")
	}
}
