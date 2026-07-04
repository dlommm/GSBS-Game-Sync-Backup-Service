package store

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func payload(b byte, n int) []byte { return bytes.Repeat([]byte{b}, n) }

// StorageUsage counts current saves plus retained version history;
// UserStorageBytes keeps its saves-only semantics.
func TestStorageUsage_CountsVersions(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	userID, err := st.CreateUser(ctx, "u", "h")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", payload('a', 100), &SaveMeta{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", payload('b', 150), &SaveMeta{}); err != nil {
		t.Fatal(err)
	}

	saves, err := st.UserStorageBytes(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if saves != 150 {
		t.Fatalf("UserStorageBytes = %d, want 150 (current save only)", saves)
	}
	usage, err := st.StorageUsage(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	// current save (150) + versions v1 (100) + v2 (150)
	if usage != 400 {
		t.Fatalf("StorageUsage = %d, want 400 (saves + version history)", usage)
	}
	total, err := st.TotalStorageUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if total != usage {
		t.Fatalf("TotalStorageUsage = %d, want %d", total, usage)
	}
}

// A push whose projected usage (including the new version row) exceeds the
// quota is rolled back completely: same hash, same version count.
func TestQuota_BlocksGrowthInTx(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	userID, err := st.CreateUser(ctx, "u", "h")
	if err != nil {
		t.Fatal(err)
	}

	first := payload('a', 100)
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", first, &SaveMeta{QuotaBytes: 250}); err != nil {
		t.Fatalf("first push within quota: %v", err) // usage 200
	}
	_, err = st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", payload('b', 100), &SaveMeta{QuotaBytes: 250})
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err) // projected 300 > 250
	}

	blob, err := st.GetSave(ctx, userID, "g1", "pk1")
	if err != nil || !bytes.Equal(blob.Content, first) {
		t.Fatalf("save must be unchanged after rollback: err=%v", err)
	}
	versions, err := st.ListSaveVersions(ctx, userID, "g1", "pk1", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("version count = %d, want 1 (rolled back)", len(versions))
	}
}

// Users already over quota may shrink/replace (usage not growing) but not grow.
func TestQuota_GrandfatherAllowsShrink(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	userID, err := st.CreateUser(ctx, "u", "h")
	if err != nil {
		t.Fatal(err)
	}

	// Seed without a quota: usage = 200 (saves) + 200 (v1) = 400.
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", payload('a', 200), &SaveMeta{}); err != nil {
		t.Fatal(err)
	}

	// Now a tiny quota is imposed. Shrinking replacement: post-usage
	// 50 + (200+50) = 300 <= pre 400 -> allowed despite quota 100.
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", payload('b', 50), &SaveMeta{QuotaBytes: 100}); err != nil {
		t.Fatalf("shrinking push must be grandfathered: %v", err)
	}

	// Growth while over quota: blocked.
	_, err = st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", payload('c', 300), &SaveMeta{QuotaBytes: 100})
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("growth over quota must fail, got %v", err)
	}
}

// Per-game retention overrides win over the global version retention.
func TestRetentionOverridePerGame(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	userID, err := st.CreateUser(ctx, "u", "h")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAdminSetting(ctx, AdminSettingRetentionOverrides, `{"g-short": 2}`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := st.UpsertSaveWithMeta(ctx, userID, "g-short", "pk", payload(byte('a'+i), 10+i), &SaveMeta{}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpsertSaveWithMeta(ctx, userID, "g-normal", "pk", payload(byte('a'+i), 10+i), &SaveMeta{}); err != nil {
			t.Fatal(err)
		}
	}
	short, err := st.ListSaveVersions(ctx, userID, "g-short", "pk", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(short) != 2 {
		t.Fatalf("override game kept %d versions, want 2", len(short))
	}
	normal, err := st.ListSaveVersions(ctx, userID, "g-normal", "pk", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(normal) != 4 {
		t.Fatalf("default game kept %d versions, want 4 (under default retention)", len(normal))
	}
}

// Two concurrent pushes near the limit: exactly one wins, the other gets
// ErrQuotaExceeded, and final usage stays within quota. Uses a file-backed DB
// so both goroutines run real concurrent transactions.
func TestQuota_ConcurrentPushesNeverExceed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "q.db")
	st, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	userID, err := st.CreateUser(ctx, "u", "h")
	if err != nil {
		t.Fatal(err)
	}

	const quota = 250 // fits one 100-byte push (usage 200), not two (400)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			slot := []string{"pk-a", "pk-b"}[i]
			_, errs[i] = st.UpsertSaveWithMeta(ctx, userID, "g1", slot, payload(byte('a'+i), 100), &SaveMeta{QuotaBytes: quota})
		}(i)
	}
	wg.Wait()

	var ok, quotaErr int
	for _, e := range errs {
		switch {
		case e == nil:
			ok++
		case errors.Is(e, ErrQuotaExceeded):
			quotaErr++
		default:
			t.Fatalf("unexpected error: %v", e)
		}
	}
	if ok != 1 || quotaErr != 1 {
		t.Fatalf("got %d successes and %d quota errors, want exactly 1 and 1", ok, quotaErr)
	}
	usage, err := st.StorageUsage(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if usage > quota {
		t.Fatalf("usage %d exceeds quota %d", usage, quota)
	}
}
