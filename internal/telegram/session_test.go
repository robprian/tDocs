package telegram

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gotd/td/session"
	"tdocs/internal/db"
)

func TestEncryptedSessionStorage(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	defer database.Close()

	secret := "test-secret-key-for-session"
	storage := NewEncryptedSessionStorage(database, secret)

	ctx := context.Background()

	// 1. Initially, session should return session.ErrNotFound
	_, err = storage.LoadSession(ctx)
	if err != session.ErrNotFound {
		t.Fatalf("Expected ErrNotFound, got: %v", err)
	}

	// 2. Store session
	sampleData := []byte("mtproto-auth-key-binary-data-simulation")
	if err := storage.StoreSession(ctx, sampleData); err != nil {
		t.Fatalf("StoreSession failed: %v", err)
	}

	// 3. Load session and verify
	loaded, err := storage.LoadSession(ctx)
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if string(loaded) != string(sampleData) {
		t.Fatalf("Loaded session mismatch: got %q, want %q", string(loaded), string(sampleData))
	}

	// 4. Verify raw DB contains encrypted text, not raw session
	rawSetting, err := database.GetSetting("telegram_session")
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	if rawSetting == string(sampleData) {
		t.Fatalf("Session was stored in plaintext, expected encrypted ciphertext!")
	}
}

func TestCorruptSessionHealsToNotFound(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	good := NewEncryptedSessionStorage(database, "correct-secret")
	if err := good.StoreSession(ctx, []byte("real-mtproto-state")); err != nil {
		t.Fatalf("StoreSession failed: %v", err)
	}

	// Wrong secret (e.g. rotated TELEDRIVE_SECRET_KEY) must heal to
	// ErrNotFound so login/server start a fresh auth flow instead of
	// failing with "decrypt session: cipher: message authentication failed".
	rotated := NewEncryptedSessionStorage(database, "different-secret")
	if _, err := rotated.LoadSession(ctx); err != session.ErrNotFound {
		t.Fatalf("Expected ErrNotFound for undecryptable session, got: %v", err)
	}
	if _, err := database.GetSetting("telegram_session"); err == nil {
		t.Fatal("Expected corrupt session row to be deleted after heal")
	}
}

func TestLegacyDefaultKeyFallback(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	legacy := NewEncryptedSessionStorage(database, "teledrive-default-local-secret-32b")
	if err := legacy.StoreSession(ctx, []byte("legacy-session")); err != nil {
		t.Fatalf("StoreSession failed: %v", err)
	}

	// A newly configured secret must still read pre-migration sessions.
	migrated := NewEncryptedSessionStorage(database, "new-random-secret")
	loaded, err := migrated.LoadSession(ctx)
	if err != nil {
		t.Fatalf("LoadSession with legacy fallback failed: %v", err)
	}
	if string(loaded) != "legacy-session" {
		t.Fatalf("Loaded session mismatch: got %q", string(loaded))
	}
}
