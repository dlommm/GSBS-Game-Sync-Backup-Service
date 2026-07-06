package store

import (
	"context"
	"fmt"
	"testing"
)

func TestSQLite_AuditLogPaging(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "alice", "h")
	for i := 0; i < 7; i++ {
		action := "run_job"
		if i%2 == 0 {
			action = "revoke_client"
		}
		_ = st.AppendAudit(ctx, u, "alice", action, fmt.Sprintf("target-%d", i), fmt.Sprintf("details %d", i))
	}

	total, err := st.CountAuditLog(ctx, AuditLogFilter{})
	if err != nil || total != 7 {
		t.Fatalf("CountAuditLog all: total=%d err=%v", total, err)
	}
	page, err := st.ListAuditLogPage(ctx, AuditLogFilter{}, 3, 3)
	if err != nil || len(page) != 3 {
		t.Fatalf("ListAuditLogPage limit=3 offset=3: len=%d err=%v", len(page), err)
	}
	tail, err := st.ListAuditLogPage(ctx, AuditLogFilter{}, 3, 6)
	if err != nil || len(tail) != 1 {
		t.Fatalf("ListAuditLogPage offset=6: len=%d err=%v", len(tail), err)
	}

	// Action filter.
	total, err = st.CountAuditLog(ctx, AuditLogFilter{Action: "run_job"})
	if err != nil || total != 3 {
		t.Fatalf("CountAuditLog run_job: total=%d err=%v", total, err)
	}
	rows, err := st.ListAuditLogPage(ctx, AuditLogFilter{Action: "run_job"}, 10, 0)
	if err != nil || len(rows) != 3 {
		t.Fatalf("ListAuditLogPage run_job: len=%d err=%v", len(rows), err)
	}
	for _, r := range rows {
		if r.Action != "run_job" {
			t.Errorf("action filter leaked: %+v", r)
		}
	}

	// Text filter matches details/target case-insensitively; LIKE
	// metacharacters in the query must not act as wildcards.
	total, err = st.CountAuditLog(ctx, AuditLogFilter{Text: "DETAILS 4"})
	if err != nil || total != 1 {
		t.Fatalf("CountAuditLog text: total=%d err=%v", total, err)
	}
	total, err = st.CountAuditLog(ctx, AuditLogFilter{Text: "details %"})
	if err != nil || total != 0 {
		t.Fatalf("CountAuditLog literal %%: total=%d err=%v", total, err)
	}

	actions, err := st.ListAuditActions(ctx)
	if err != nil || len(actions) != 2 {
		t.Fatalf("ListAuditActions: %v err=%v", actions, err)
	}
}

func TestSQLite_JobRunsPaging(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		id, err := st.LogJobStart(ctx, "pcgw_sync")
		if err != nil {
			t.Fatal(err)
		}
		status := "success"
		if i == 0 {
			status = "failed"
		}
		_ = st.LogJobFinish(ctx, id, status, "", i)
	}
	id, _ := st.LogJobStart(ctx, "backup")
	_ = st.LogJobFinish(ctx, id, "success", "", 1)

	total, err := st.CountJobRuns(ctx, "", "")
	if err != nil || total != 5 {
		t.Fatalf("CountJobRuns all: total=%d err=%v", total, err)
	}
	total, err = st.CountJobRuns(ctx, "pcgw_sync", "success")
	if err != nil || total != 3 {
		t.Fatalf("CountJobRuns pcgw success: total=%d err=%v", total, err)
	}
	rows, err := st.ListJobRunsPage(ctx, "pcgw_sync", "success", 2, 2)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListJobRunsPage: len=%d err=%v", len(rows), err)
	}
	names, err := st.ListJobNames(ctx)
	if err != nil || len(names) != 2 || names[0] != "backup" || names[1] != "pcgw_sync" {
		t.Fatalf("ListJobNames: %v err=%v", names, err)
	}
}

func TestSQLite_ManifestFetchesAndSnapshotsPaging(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = st.LogManifestFetch(ctx, "", "pc", "alice", 10+i)
	}
	total, err := st.CountManifestFetches(ctx)
	if err != nil || total != 3 {
		t.Fatalf("CountManifestFetches: total=%d err=%v", total, err)
	}
	rows, err := st.ListManifestFetchesPage(ctx, 2, 2)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListManifestFetchesPage: len=%d err=%v", len(rows), err)
	}

	for i := 0; i < 2; i++ {
		if err := st.AppendStatsSnapshot(ctx); err != nil {
			t.Fatal(err)
		}
	}
	stotal, err := st.CountStatsSnapshots(ctx)
	if err != nil || stotal != 2 {
		t.Fatalf("CountStatsSnapshots: total=%d err=%v", stotal, err)
	}
	srows, err := st.ListStatsSnapshotsPage(ctx, 1, 1)
	if err != nil || len(srows) != 1 {
		t.Fatalf("ListStatsSnapshotsPage: len=%d err=%v", len(srows), err)
	}
}
