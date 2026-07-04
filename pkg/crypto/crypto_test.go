package crypto

import "testing"

func TestEncryptDecrypt(t *testing.T) {
	plain := []byte("save game data")
	enc, err := Encrypt("test-passphrase", plain)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decrypt("test-passphrase", enc)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(plain) {
		t.Fatalf("got %q", out)
	}
}

func TestDecryptWrongPassphrase(t *testing.T) {
	enc, _ := Encrypt("right", []byte("data"))
	_, err := Decrypt("wrong", enc)
	if err == nil {
		t.Fatal("expected error")
	}
}

// Decrypt auto-detects both envelope formats: legacy (PBKDF2, no prefix) and
// v2 (gsbs2:, Argon2id). A mixed fleet reads everything.
func TestEncryptV2CrossFormat(t *testing.T) {
	plain := []byte("cross-format save data")

	v2, err := EncryptV2("pass", plain)
	if err != nil {
		t.Fatal(err)
	}
	if !IsV2(v2) {
		t.Fatalf("EncryptV2 output missing prefix: %q", v2[:12])
	}
	out, err := Decrypt("pass", v2)
	if err != nil || string(out) != string(plain) {
		t.Fatalf("v2 round-trip: %q err=%v", out, err)
	}

	v1, err := Encrypt("pass", plain)
	if err != nil {
		t.Fatal(err)
	}
	if IsV2(v1) {
		t.Fatal("legacy output must not carry the v2 prefix")
	}
	out, err = Decrypt("pass", v1)
	if err != nil || string(out) != string(plain) {
		t.Fatalf("legacy round-trip: %q err=%v", out, err)
	}

	if _, err := Decrypt("wrong", v2); err == nil {
		t.Fatal("v2 wrong passphrase must fail")
	}
	if _, err := Decrypt("pass", V2Prefix+"AAAA"); err == nil {
		t.Fatal("truncated v2 payload must fail")
	}
}
