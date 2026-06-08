package job

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gsbs/gsbs/server/store"
)

func TestTryRunPCGWSyncDuplicateGuard(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	r := NewRunner(st, nil, nil)
	r.mu.Lock()
	r.running["pcgw_sync"] = true
	r.mu.Unlock()

	started, err := r.TryRunPCGWSync(ctx)
	if started {
		t.Fatal("expected not started")
	}
	if !errors.Is(err, ErrJobAlreadyRunning) {
		t.Fatalf("err = %v, want ErrJobAlreadyRunning", err)
	}
}

func TestTryRunPCGWSyncDuplicateGuardDB(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	if _, err := st.LogJobStart(ctx, "pcgw_sync"); err != nil {
		t.Fatal(err)
	}

	r := NewRunner(st, nil, nil)
	started, err := r.TryRunPCGWSync(ctx)
	if started {
		t.Fatal("expected not started")
	}
	if !errors.Is(err, ErrJobAlreadyRunning) {
		t.Fatalf("err = %v, want ErrJobAlreadyRunning", err)
	}
}

func TestPCGWSyncCancelSetsCanceled(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	runID, err := st.StartPCGWSyncRun(ctx, "incremental")
	if err != nil {
		t.Fatal(err)
	}

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()

	_, syncErr := PCGWSyncEx(canceledCtx, st, nil, nil, nil, PCGWSyncOptions{
		SyncRunID:    runID,
		SkipStartRun: true,
	})
	if !errors.Is(syncErr, context.Canceled) {
		t.Fatalf("syncErr = %v, want context.Canceled", syncErr)
	}

	got, err := st.GetLatestPCGWSyncRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobCanceled {
		t.Fatalf("status = %q, want %q", got.Status, JobCanceled)
	}
}

func TestCancelPCGWSyncReturnsTrue(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	r := NewRunner(st, nil, nil)
	r.mu.Lock()
	r.cancelFuncs["pcgw_sync"] = func() {}
	r.mu.Unlock()

	if !r.CancelPCGWSync(context.Background()) {
		t.Fatal("expected cancel to succeed")
	}
}

func TestCancelPCGWSyncReturnsFalseWhenIdle(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	r := NewRunner(st, nil, nil)
	if r.CancelPCGWSync(context.Background()) {
		t.Fatal("expected cancel to fail when idle")
	}
}

func TestAutoCatchUpProgressError(t *testing.T) {
	backlog := phase2BacklogSnapshot{
		Missing:       3,
		TitleBackfill: 2,
		FailedPartial: 1,
		Total:         4,
	}
	if err := autoCatchUpProgressError(1, 2, backlog); err != nil {
		t.Fatalf("expected nil before limit, got %v", err)
	}
	err := autoCatchUpProgressError(2, 2, backlog)
	if err == nil {
		t.Fatal("expected no-progress error at limit")
	}
	got := err.Error()
	if got == "" || !containsAll(got, []string{"no backlog progress", "remaining backlog=4", "missing=3", "title_backfill=2", "failed_partial=1"}) {
		t.Fatalf("unexpected error message: %q", got)
	}
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
