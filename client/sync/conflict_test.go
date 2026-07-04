package sync

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDecidePull(t *testing.T) {
	serverTime := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	localNewer := serverTime.Add(time.Hour)
	localOlder := serverTime.Add(-time.Hour)

	tests := []struct {
		name        string
		localExists bool
		localHash   string
		localMtime  time.Time
		serverHash  string
		policy      string
		want        PullDecision
	}{
		{"no local", false, "", time.Time{}, "abc", "last_write_wins", PullApply},
		{"hash match", true, "abc", localNewer, "abc", "last_write_wins", PullSkip},
		{"lww local newer", true, "a", localNewer, "b", "last_write_wins", PullSkip},
		{"lww server newer", true, "a", localOlder, "b", "last_write_wins", PullApply},
		{"lww equal mtime", true, "a", serverTime, "b", "last_write_wins", PullConflict},
		{"keep_local newer", true, "a", localNewer, "b", "keep_local", PullSkip},
		{"keep_local older", true, "a", localOlder, "b", "keep_local", PullApply},
		{"keep_server server wins", true, "a", localOlder, "b", "keep_server", PullApply},
		{"keep_server local wins", true, "a", localNewer, "b", "keep_server", PullConflict},
		// Legacy server rows without a hash must not blind-overwrite local files.
		{"empty server hash no local", false, "", time.Time{}, "", "last_write_wins", PullApply},
		{"empty server hash lww local newer", true, "a", localNewer, "", "last_write_wins", PullSkip},
		{"empty server hash lww server newer", true, "a", localOlder, "", "last_write_wins", PullApply},
		{"empty server hash keep_local newer", true, "a", localNewer, "", "keep_local", PullSkip},
		{"empty server hash keep_server", true, "a", localOlder, "", "keep_server", PullApply},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DecidePull(tc.localExists, tc.localHash, tc.localMtime, tc.serverHash, serverTime, tc.policy)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestConflictPersistence(t *testing.T) {
	dir := t.TempDir()
	SetConflictsPathForTest(filepath.Join(dir, "conflicts.json"))
	defer SetConflictsPathForTest("")

	RecordConflict(ConflictRecord{GameID: "g1", PathKey: "p1", FilePath: "/tmp/save"})
	list := ListConflicts()
	if len(list) != 1 {
		t.Fatalf("want 1 conflict got %d", len(list))
	}
	RecordConflict(ConflictRecord{GameID: "g1", PathKey: "p1", FilePath: "/tmp/save2"})
	list = ListConflicts()
	if len(list) != 1 || list[0].FilePath != "/tmp/save2" {
		t.Fatalf("dedup failed: %+v", list)
	}
	ClearConflict("g1", "p1")
	if len(ListConflicts()) != 0 {
		t.Fatal("expected cleared")
	}
}

func TestIsRetryablePullError(t *testing.T) {
	if !isRetryablePullError(errStatus(503), 503) {
		t.Fatal("503 retryable")
	}
	if isRetryablePullError(errStatus(401), 401) {
		t.Fatal("401 not retryable")
	}
}

type statusErr string

func (e statusErr) Error() string { return string(e) }

func errStatus(code int) error {
	return statusErr("status " + itoa(code))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
