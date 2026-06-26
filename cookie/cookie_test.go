package cookie

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	blob, err := Encrypt("session=abc", "BRWSR\x01", "test-key")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !IsEncrypted(blob, "BRWSR\x01") {
		t.Fatal("expected encrypted magic")
	}
	plain, err := Decrypt(blob, "BRWSR\x01", "test-key")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plain != "session=abc" {
		t.Fatalf("got %q", plain)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	blob, err := Encrypt("secret", "BRWSR\x01", "right")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := Decrypt(blob, "BRWSR\x01", "wrong"); err == nil {
		t.Fatal("expected decrypt failure")
	}
}
