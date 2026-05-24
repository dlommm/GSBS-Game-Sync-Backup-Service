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
