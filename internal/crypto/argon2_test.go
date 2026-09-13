package crypto

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestArgon2Roundtrip(t *testing.T) {
	hash, err := HashPassword("correct-horse-123")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("expected argon2id encoding, got %q", hash)
	}
	if !VerifyPassword(hash, "correct-horse-123") {
		t.Fatalf("correct password did not verify")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Fatalf("wrong password verified")
	}
	// Distinct salts per hash.
	hash2, _ := HashPassword("correct-horse-123")
	if hash == hash2 {
		t.Fatalf("expected unique salts, got identical hashes")
	}
}

func TestVerifyLegacyBcrypt(t *testing.T) {
	legacy, err := bcrypt.GenerateFromPassword([]byte("old-share-pass"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(string(legacy), "old-share-pass") {
		t.Fatalf("legacy bcrypt hash must keep verifying")
	}
	if VerifyPassword(string(legacy), "nope") {
		t.Fatalf("wrong password verified against bcrypt")
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "plaintext", "$argon2id$bogus", "$2a$short"} {
		if VerifyPassword(bad, "x") {
			t.Fatalf("garbage hash %q verified", bad)
		}
	}
	if _, err := HashPassword(""); err == nil {
		t.Fatalf("expected error hashing empty password")
	}
}
