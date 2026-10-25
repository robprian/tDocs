package crypto

import (
	"bytes"
	"testing"
)

func TestAES256GCM_RoundTrip(t *testing.T) {
	secret := "my-very-secret-passphrase-12345"
	key := DeriveKey(secret)
	original := []byte("tDocs MTProto Session auth_key token string payload")

	encrypted, err := Encrypt(original, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(original, decrypted) {
		t.Fatalf("Decrypted payload %q does not match original %q", string(decrypted), string(original))
	}
}

func TestAES256GCM_WrongKey(t *testing.T) {
	key1 := DeriveKey("key-1")
	key2 := DeriveKey("key-2")
	original := []byte("sensitive-data")

	encrypted, err := Encrypt(original, key1)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = Decrypt(encrypted, key2)
	if err == nil {
		t.Fatalf("Expected decryption failure with wrong key, but succeeded")
	}
}
