package store

import (
	"context"
	"testing"

	"github.com/gsbs/gsbs/pkg/types"
)

func TestAnalyticsQueries(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	userID, err := st.CreateUser(ctx, "analytics-user", "hash")
	if err != nil {
		t.Fatal(err)
	}

	if err := st.UpsertGameSaveLocations(ctx, []types.GameSaveLocation{
		{GameID: "730", GameTitle: "CS2", Platform: "windows", PathTemplate: "/save"},
	}); err != nil {
		t.Fatal(err)
	}

	meta := SaveMeta{ContentSize: 100}
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "730", "main", []byte("data"), &meta); err != nil {
		t.Fatal(err)
	}
	meta.ContentSize = 50
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "730", "cfg", []byte("more"), &meta); err != nil {
		t.Fatal(err)
	}

	if n, err := st.CountTotalSaves(ctx); err != nil || n != 2 {
		t.Fatalf("CountTotalSaves: %d %v", n, err)
	}
	if n, err := st.CountDistinctSaveGames(ctx); err != nil || n != 1 {
		t.Fatalf("CountDistinctSaveGames: %d %v", n, err)
	}

	top, err := st.ListTopSaveGames(ctx, 5)
	if err != nil || len(top) != 1 {
		t.Fatalf("ListTopSaveGames: %v len=%d", err, len(top))
	}
	if top[0].GameID != "730" || top[0].SaveCount != 2 || top[0].StorageBytes != 150 {
		t.Fatalf("top game: %+v", top[0])
	}

	if err := st.InsertPCGWParseFailure(ctx, &types.PCGWParseFailure{
		PageID: 42, Section: "save", ErrorMessage: "bad wikitext",
	}); err != nil {
		t.Fatal(err)
	}
	failures, err := st.ListRecentPCGWParseFailures(ctx, 10)
	if err != nil || len(failures) != 1 {
		t.Fatalf("ListRecentPCGWParseFailures: %v len=%d", err, len(failures))
	}
	if failures[0].PageID != 42 || failures[0].Section != "save" {
		t.Fatalf("failure: %+v", failures[0])
	}
}

func TestInsightsAggregates(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	userID, err := st.CreateUser(ctx, "insights-user", "hash")
	if err != nil {
		t.Fatal(err)
	}
	token, err := st.RegisterClient(ctx, userID, "Gaming-PC", "windows")
	if err != nil {
		t.Fatal(err)
	}
	_, clientID, _, _, err := st.ClientByToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}

	// Two versions today from the named client, one from an unknown device.
	meta := SaveMeta{ClientID: clientID}
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "730", "main", []byte("v1-data"), &meta); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "730", "main", []byte("v2-data-longer"), &meta); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "440", "slot", []byte("tf2"), &SaveMeta{Encrypted: true}); err != nil {
		t.Fatal(err)
	}

	if series, err := st.SyncVolumeByDayAll(ctx, 7); err != nil || len(series) != 7 || series[6].Count != 3 {
		t.Fatalf("SyncVolumeByDayAll: %v %+v", err, series)
	}
	if series, err := st.SyncBytesByDay(ctx, userID, 7); err != nil || len(series) != 7 || series[6].Bytes == 0 {
		t.Fatalf("SyncBytesByDay: %v %+v", err, series)
	}
	if series, err := st.SyncBytesByDay(ctx, "", 7); err != nil || series[6].Bytes == 0 {
		t.Fatalf("SyncBytesByDay all: %v %+v", err, series)
	}

	byClient, err := st.VersionsByClient(ctx, userID, 7)
	if err != nil || len(byClient) != 2 {
		t.Fatalf("VersionsByClient: %v %+v", err, byClient)
	}
	if byClient[0].ClientName != "Gaming-PC" || byClient[0].Versions != 2 {
		t.Fatalf("attribution: %+v", byClient[0])
	}

	if wd, err := st.ActivityByWeekday(ctx, userID, 30); err != nil || len(wd) != 7 || sumInts(wd) != 3 {
		t.Fatalf("ActivityByWeekday: %v %+v", err, wd)
	}
	if hh, err := st.ActivityByHour(ctx, userID, 30); err != nil || len(hh) != 24 || sumInts(hh) != 3 {
		t.Fatalf("ActivityByHour: %v %+v", err, hh)
	}

	active, err := st.MostActiveGames(ctx, userID, 7, 5)
	if err != nil || len(active) != 2 || active[0].GameID != "730" || active[0].SaveCount != 2 {
		t.Fatalf("MostActiveGames: %v %+v", err, active)
	}

	depth, err := st.VersionDepth(ctx, userID)
	if err != nil || depth.Slots != 2 || depth.Versions != 3 || depth.TopCount != 2 || depth.TopGameID != "730" {
		t.Fatalf("VersionDepth: %v %+v", err, depth)
	}

	enc, total, err := st.CountEncryptedSaves(ctx, userID)
	if err != nil || enc != 1 || total != 2 {
		t.Fatalf("CountEncryptedSaves: %d/%d %v", enc, total, err)
	}

	if vc, err := st.ClientVersionCounts(ctx); err != nil || len(vc) == 0 {
		t.Fatalf("ClientVersionCounts: %v %+v", err, vc)
	}
	if oc, err := st.ClientOSCounts(ctx); err != nil || len(oc) != 1 || oc[0].Label != "windows" {
		t.Fatalf("ClientOSCounts: %v %+v", err, oc)
	}

	if err := st.AppendAudit(ctx, userID, "insights-user", "enable_2fa", "", ""); err != nil {
		t.Fatal(err)
	}
	if ac, err := st.AuditActionCounts(ctx, 7, 5); err != nil || len(ac) != 1 || ac[0].Label != "enable_2fa" {
		t.Fatalf("AuditActionCounts: %v %+v", err, ac)
	}
	if av, err := st.AuditVolumeByDay(ctx, 7); err != nil || len(av) != 7 || av[6].Count != 1 {
		t.Fatalf("AuditVolumeByDay: %v %+v", err, av)
	}

	if mf, err := st.ManifestFetchByDay(ctx, 7); err != nil || len(mf) != 7 {
		t.Fatalf("ManifestFetchByDay: %v %+v", err, mf)
	}
	if au, err := st.ActiveUsersByDay(ctx, 7); err != nil || au[6].Count != 1 {
		t.Fatalf("ActiveUsersByDay: %v %+v", err, au)
	}
	if su, err := st.SignupsByMonth(ctx, 3); err != nil || len(su) != 3 || su[2].Count != 1 {
		t.Fatalf("SignupsByMonth: %v %+v", err, su)
	}

	ad, err := st.UserAdoptionStats(ctx)
	if err != nil || ad.Users != 1 || ad.TOTPEnabled != 0 {
		t.Fatalf("UserAdoptionStats: %v %+v", err, ad)
	}
	if err := st.SetTOTPEnabled(ctx, userID, true); err != nil {
		t.Fatal(err)
	}
	if ad, err = st.UserAdoptionStats(ctx); err != nil || ad.TOTPEnabled != 1 {
		t.Fatalf("UserAdoptionStats after 2FA: %v %+v", err, ad)
	}

	if js, err := st.JobRunStats(ctx); err != nil {
		t.Fatalf("JobRunStats: %v %+v", err, js)
	}
}

func TestRecoveryCodesStore(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	userID, err := st.CreateUser(ctx, "rc-user", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRecoveryCodes(ctx, userID, []string{"h1", "h2", "h3"}); err != nil {
		t.Fatal(err)
	}
	if n, err := st.CountRecoveryCodes(ctx, userID); err != nil || n != 3 {
		t.Fatalf("count: %d %v", n, err)
	}
	if ok, err := st.ConsumeRecoveryCode(ctx, userID, "h2"); err != nil || !ok {
		t.Fatalf("consume: %v %v", ok, err)
	}
	// A consumed code cannot be reused.
	if ok, _ := st.ConsumeRecoveryCode(ctx, userID, "h2"); ok {
		t.Fatal("reused a consumed recovery code")
	}
	if n, _ := st.CountRecoveryCodes(ctx, userID); n != 2 {
		t.Fatalf("count after consume: %d", n)
	}
	// Regeneration replaces the set.
	if err := st.SetRecoveryCodes(ctx, userID, []string{"n1"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.ConsumeRecoveryCode(ctx, userID, "h1"); ok {
		t.Fatal("old code survived regeneration")
	}
	if err := st.DeleteRecoveryCodes(ctx, userID); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.CountRecoveryCodes(ctx, userID); n != 0 {
		t.Fatalf("count after delete: %d", n)
	}
}

func sumInts(v []int) int {
	total := 0
	for _, n := range v {
		total += n
	}
	return total
}
