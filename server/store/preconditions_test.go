package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentSavePreconditions(t *testing.T) {
	for _, mode := range []string{"if_hash", "if_absent"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("GSBS_SAVE_ROOT", t.TempDir())
			st, err := NewSQLite(filepath.Join(t.TempDir(), "saves.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = st.Close() })
			ctx := context.Background()
			user, err := st.CreateUser(ctx, "u", "h")
			if err != nil {
				t.Fatal(err)
			}
			meta := SaveMeta{RelativePath: "slot.dat", IfAbsent: true}
			if mode == "if_hash" {
				if _, err := st.UpsertSaveWithMeta(ctx, user, "g", "p", []byte("initial"), &meta); err != nil {
					t.Fatal(err)
				}
				meta.IfAbsent = false
				meta.IfHash = hashContent([]byte("initial"))
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for _, value := range []string{"first", "second"} {
				wg.Add(1)
				go func(value string) {
					defer wg.Done()
					<-start
					_, err := st.UpsertSaveWithMeta(ctx, user, "g", "p", []byte(value), &meta)
					results <- err
				}(value)
			}
			close(start)
			wg.Wait()
			close(results)
			accepted, rejected := 0, 0
			for err := range results {
				var conflict *SaveConflictError
				switch {
				case err == nil:
					accepted++
				case errors.As(err, &conflict):
					rejected++
				default:
					t.Fatalf("unexpected push error: %v", err)
				}
			}
			if accepted != 1 || rejected != 1 {
				t.Fatalf("accepted=%d conflicts=%d; want one each", accepted, rejected)
			}
			blob, err := st.GetSave(ctx, user, "g", "p")
			if err != nil || blob == nil {
				t.Fatalf("read winner: %v", err)
			}
			hash, err := st.GetSaveHash(ctx, user, "g", "p")
			if err != nil || hash != hashContent(blob.Content) {
				t.Fatalf("winner bytes and hash disagree: %v", err)
			}
		})
	}
}

func TestSavePreconditionCheckedBeforeDedup(t *testing.T) {
	t.Setenv("GSBS_SAVE_ROOT", "")
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	user, err := st.CreateUser(ctx, "u", "h")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("current")
	if _, err := st.UpsertSaveWithMeta(ctx, user, "g", "p", body, &SaveMeta{}); err != nil {
		t.Fatal(err)
	}
	_, err = st.UpsertSaveWithMeta(ctx, user, "g", "p", body, &SaveMeta{IfHash: "stale"})
	var conflict *SaveConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale precondition bypassed by dedup: %v", err)
	}
	if skipped, err := st.UpsertSaveWithMeta(ctx, user, "g", "p", body, &SaveMeta{IfAbsent: true}); err != nil || !skipped {
		t.Fatalf("identical first push should dedup: skipped=%v err=%v", skipped, err)
	}
}
