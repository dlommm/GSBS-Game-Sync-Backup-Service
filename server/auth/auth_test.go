package auth

import (
	"context"
	"testing"
	"time"

	"github.com/gsbs/gsbs/server/store"
)

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" || hash == "password123" {
		t.Error("expected non-empty hash different from plaintext")
	}
}

func TestCheckPassword(t *testing.T) {
	hash, _ := HashPassword("secret")
	if err := CheckPassword("secret", hash); err != nil {
		t.Errorf("expected match: %v", err)
	}
	if err := CheckPassword("wrong", hash); err != ErrBadCredentials {
		t.Errorf("expected ErrBadCredentials, got %v", err)
	}
}

func TestRegisterAndLogin(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	svc := NewService(st)

	userID, err := svc.RegisterUser(ctx, "alice", "password1234")
	if err != nil {
		t.Fatal(err)
	}
	if userID == "" {
		t.Error("expected non-empty user ID")
	}

	_, _, err = svc.Login(ctx, "alice", "wrong", "client1", "windows")
	if err != ErrBadCredentials {
		t.Errorf("expected ErrBadCredentials on wrong password, got %v", err)
	}

	uid, token, err := svc.Login(ctx, "alice", "password1234", "client1", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if uid != userID {
		t.Errorf("user ID mismatch: %s != %s", uid, userID)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}

	validUID, validCID, err := svc.ValidateToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if validUID != userID {
		t.Errorf("ValidateToken user ID: %s != %s", validUID, userID)
	}
	if validCID == "" {
		t.Error("expected non-empty client ID")
	}

	_, _, err = svc.ValidateToken(ctx, "invalid-token")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestAuthenticate(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	svc := NewService(st)

	_, err = svc.RegisterUser(ctx, "bob", "bobpass99")
	if err != nil {
		t.Fatal(err)
	}

	uid, err := svc.Authenticate(ctx, "bob", "bobpass99")
	if err != nil {
		t.Fatal(err)
	}
	if uid == "" {
		t.Error("expected non-empty user ID")
	}

	_, err = svc.Authenticate(ctx, "bob", "wrong")
	if err != ErrBadCredentials {
		t.Errorf("expected ErrBadCredentials, got %v", err)
	}
}

// An unknown username must cost the same bcrypt work as a real one: returning
// before any comparison made response timing a clean account-existence oracle
// despite the deliberately identical error bodies.
func TestLoginTimingDoesNotRevealUnknownUsernames(t *testing.T) {
	if testing.Short() {
		t.Skip("timing measurement")
	}
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	svc := NewService(st)

	if _, err := svc.RegisterUser(ctx, "realuser", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}

	measure := func(username string) time.Duration {
		const runs = 5
		start := time.Now()
		for i := 0; i < runs; i++ {
			_, _, _ = svc.Login(ctx, username, "wrong-password", "dev", "linux")
		}
		return time.Since(start) / runs
	}

	real := measure("realuser")
	unknown := measure("nosuchuser")

	// Both paths do one bcrypt compare, so they land in the same order of
	// magnitude. The pre-fix gap was ~1000x (no bcrypt at all vs a full compare).
	if unknown*4 < real {
		t.Errorf("unknown-username login is far faster than a real one (%v vs %v): timing oracle", unknown, real)
	}
}
