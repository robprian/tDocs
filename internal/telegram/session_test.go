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

func TestWrongKeyPreservesSession(t *testing.T) {
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

	original, err := database.GetSetting("telegram_session")
	if err != nil {
		t.Fatal(err)
	}
	rotated := NewEncryptedSessionStorage(database, "different-secret")
	if _, err := rotated.LoadSession(ctx); err == nil || err == session.ErrNotFound {
		t.Fatalf("Expected a decryption error, not a missing session, got: %v", err)
	}
	if stored, err := database.GetSetting("telegram_session"); err != nil || stored != original {
		t.Fatal("Wrong key must not delete or replace the encrypted session")
	}
	if loaded, err := good.LoadSession(ctx); err != nil || string(loaded) != "real-mtproto-state" {
		t.Fatalf("Original key must still recover the session: %v", err)
	}
}

// A running server must not overwrite a session that `tdocs login` stored from
// a separate process: gotd re-saves its own (revoked) auth key, which used to
// destroy the fresh CLI pairing and send the dashboard back to setup.
func TestRunningServerDoesNotClobberCLISession(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.Open(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	server := NewEncryptedSessionStorage(database, "shared-secret")
	if err := server.StoreSession(ctx, []byte("revoked-key")); err != nil {
		t.Fatalf("StoreSession failed: %v", err)
	}
	if _, err := server.LoadSession(ctx); err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	// `tdocs login` in another process pairs and stores a fresh session.
	cli := NewEncryptedSessionStorage(database, "shared-secret")
	if err := cli.StoreSession(ctx, []byte("fresh-key")); err != nil {
		t.Fatalf("CLI StoreSession failed: %v", err)
	}

	if !server.replaced() {
		t.Fatal("Expected the server to notice the newer CLI session")
	}
	if err := server.StoreSession(ctx, []byte("stale-key")); err != nil {
		t.Fatalf("StoreSession failed: %v", err)
	}

	got, err := cli.LoadSession(ctx)
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}
	if string(got) != "fresh-key" {
		t.Fatalf("CLI session was clobbered: got %q, want %q", string(got), "fresh-key")
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

func TestSessionSharedAcrossCLIAndWebDatabaseHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tdocs.db")
	cliDB, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cliDB.Close()
	webDB, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer webDB.Close()
	ctx := context.Background()
	cli := NewEncryptedSessionStorage(cliDB, "shared-key")
	web := NewEncryptedSessionStorage(webDB, "shared-key")
	if err := cli.StoreSession(ctx, []byte("cli-pairing")); err != nil {
		t.Fatal(err)
	}
	if got, err := web.LoadSession(ctx); err != nil || string(got) != "cli-pairing" {
		t.Fatalf("web must load CLI pairing: %v", err)
	}
	if err := web.StoreSession(ctx, []byte("web-session-update")); err != nil {
		t.Fatal(err)
	}
	if got, err := cli.LoadSession(ctx); err != nil || string(got) != "web-session-update" {
		t.Fatalf("CLI must load web session: %v", err)
	}
}
