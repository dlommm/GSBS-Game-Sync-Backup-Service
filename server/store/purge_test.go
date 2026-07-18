package store

import (
	"context"
	"testing"
)

func TestPurgeSavesForGameAllUsers(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	u1, _ := st.CreateUser(ctx, "u1", "h")
	u2, _ := st.CreateUser(ctx, "u2", "h")

	for _, u := range []string{u1, u2} {
		if _, err := st.UpsertSaveWithMeta(ctx, u, "bad", "pk", []byte("x"), &SaveMeta{}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpsertSaveWithMeta(ctx, u, "good", "pk", []byte("y"), &SaveMeta{}); err != nil {
			t.Fatal(err)
		}
	}

	users, saves, err := st.PurgeSavesForGameAllUsers(ctx, "bad")
	if err != nil {
		t.Fatal(err)
	}
	if users != 2 || saves != 2 {
		t.Fatalf("purge = users %d saves %d, want 2/2", users, saves)
	}

	for _, u := range []string{u1, u2} {
		if b, _ := st.GetSave(ctx, u, "bad", "pk"); b != nil {
			t.Fatalf("bad save still present for %s", u)
		}
		if b, _ := st.GetSave(ctx, u, "good", "pk"); b == nil {
			t.Fatalf("good save wrongly removed for %s", u)
		}
	}
}
