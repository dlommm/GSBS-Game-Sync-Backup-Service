package store

import (
	"context"
	"testing"
)

func TestReconcileStaleJobRuns(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	runID, err := st.LogJobStart(ctx, "pcgw_sync")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReconcileStaleJobRuns(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetLatestJobRun(ctx, "pcgw_sync")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "interrupted" {
		t.Fatalf("status = %q, want interrupted", got.Status)
	}
	if got.FinishedAt == "" {
		t.Fatal("expected finished_at set")
	}
	if got.ErrorMessage == "" {
		t.Fatal("expected error_message set")
	}
	if st.HasRunningJob(ctx, "pcgw_sync") {
		t.Fatal("should not have running job after reconcile")
	}
	_ = runID
}

func TestReconcileStalePCGWSyncRuns(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	runID, err := st.StartPCGWSyncRun(ctx, "incremental")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdatePCGWSyncRunCheckpoint(ctx, runID, 50, PCGWSyncRunStats{GamesTotal: 50}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReconcileStalePCGWSyncRuns(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetLatestPCGWSyncRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "interrupted" {
		t.Fatalf("status = %q, want interrupted", got.Status)
	}
	if got.CheckpointOffset != 50 {
		t.Fatalf("checkpoint = %d, want 50", got.CheckpointOffset)
	}
	if st.HasRunningPCGWSync(ctx) {
		t.Fatal("should not have running pcgw sync after reconcile")
	}
}

func TestGetResumablePCGWSyncRun(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	runID, err := st.StartPCGWSyncRun(ctx, "incremental")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdatePCGWSyncRunCheckpoint(ctx, runID, 100, PCGWSyncRunStats{}); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishPCGWSyncRun(ctx, runID, "failed", "timeout", PCGWSyncRunStats{}); err != nil {
		t.Fatal(err)
	}

	resumable, err := st.GetResumablePCGWSyncRun(ctx, "incremental")
	if err != nil {
		t.Fatal(err)
	}
	if resumable == nil || resumable.CheckpointOffset != 100 {
		t.Fatalf("resumable = %+v", resumable)
	}

	// success runs are not resumable
	runID2, _ := st.StartPCGWSyncRun(ctx, "incremental")
	_ = st.UpdatePCGWSyncRunCheckpoint(ctx, runID2, 200, PCGWSyncRunStats{})
	_ = st.FinishPCGWSyncRun(ctx, runID2, "success", "", PCGWSyncRunStats{})
	if r, _ := st.GetResumablePCGWSyncRun(ctx, "incremental"); r == nil || r.ID != runID {
		t.Fatalf("expected first failed run, got %+v", r)
	}

	// canceled runs must not be resumable
	runID3, _ := st.StartPCGWSyncRun(ctx, "incremental")
	_ = st.UpdatePCGWSyncRunPhase2Progress(ctx, runID3, 0, 10)
	_ = st.FinishPCGWSyncRun(ctx, runID3, "canceled", "context canceled", PCGWSyncRunStats{})
	if r, _ := st.GetResumablePCGWSyncRun(ctx, "incremental"); r != nil && r.ID == runID3 {
		t.Fatalf("canceled run should not be resumable, got %+v", r)
	}
}

func TestStartPCGWSyncRunWithResume(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	prevID, err := st.StartPCGWSyncRun(ctx, "incremental")
	if err != nil {
		t.Fatal(err)
	}
	_ = st.FinishPCGWSyncRun(ctx, prevID, "interrupted", "restart", PCGWSyncRunStats{})

	newID, err := st.StartPCGWSyncRunWithResume(ctx, "incremental", prevID, "resumed after restart")
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetLatestPCGWSyncRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != newID || got.ResumedFromRunID != prevID || got.Notes != "resumed after restart" {
		t.Fatalf("got %+v", got)
	}
}
