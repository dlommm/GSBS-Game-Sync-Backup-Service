package job

import (
	"context"
	"errors"
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
