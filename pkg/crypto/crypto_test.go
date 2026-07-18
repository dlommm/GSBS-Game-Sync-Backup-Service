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

// TestGoldenV1BlobStillDecrypts freezes the legacy v1 format. The v1 envelope
// (base64(salt||nonce||ciphertext)) carries NO KDF-parameter field, so changing
// iter/keyLen/saltLen/nonceLen or the PBKDF2 hash would silently make every
// existing v1 save undecryptable — data loss. This hard-coded blob was produced
// at iter=100000; if it ever fails to decrypt, a parameter was changed and must
// be reverted (introduce a new versioned format instead — see V2Prefix).
func TestGoldenV1BlobStillDecrypts(t *testing.T) {
	const goldenV1 = "kILTwGOGBD672Dp1k6FGDmtppo3E98P+yAudW4bxxVDJIfh/OgQSeS4Th2pPFB6MWzc242AU+yaD2+XcLOYsMo13hQ=="
	const passphrase = "correct-horse-battery-staple"
	want := "golden v1 plaintext ✓"
	if IsV2(goldenV1) {
		t.Fatal("golden blob must be legacy v1 (no gsbs2: prefix)")
	}
	got, err := Decrypt(passphrase, goldenV1)
	if err != nil {
		t.Fatalf("golden v1 blob no longer decrypts — a KDF parameter was changed and must be reverted: %v", err)
	}
	if string(got) != want {
		t.Fatalf("golden v1 decrypt = %q, want %q", got, want)
	}
}
