package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsureSecretKeyPersistsAcrossCLIAndServer(t *testing.T) {
	for _, name := range []string{"TDOCS_SECRET_KEY", "ROBDOCS_SECRET_KEY", "TELEDRIVE_SECRET_KEY"} {
		t.Setenv(name, "")
	}
	path := filepath.Join(t.TempDir(), "tdocs.db")
	key, err := EnsureSecretKey(path, false)
	if err != nil || len(key) != 64 {
		t.Fatalf("expected persistent random key, got length %d, error %v", len(key), err)
	}
	if resolved := ResolveSecretKey(path); resolved != key {
		t.Fatal("server and CLI must resolve the same saved encryption key")
	}
	again, err := EnsureSecretKey(path, true)
	if err != nil || again != key {
		t.Fatal("restarting must not rotate the encryption key")
	}
	info, err := os.Stat(keyFilePath(path))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("encryption key permissions: got %o", info.Mode().Perm())
	}
}

func TestEnsureSecretKeyFailsWhenKeyCannotBeSaved(t *testing.T) {
	for _, name := range []string{"TDOCS_SECRET_KEY", "ROBDOCS_SECRET_KEY", "TELEDRIVE_SECRET_KEY"} {
		t.Setenv(name, "")
	}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if key, err := EnsureSecretKey(filepath.Join(blocked, "tdocs.db"), false); err == nil || key != "" {
		t.Fatal("must not authenticate with an encryption key that was never persisted")
	}
}

func TestEnsureSecretKeyDoesNotReplaceEmptyKey(t *testing.T) {
	for _, name := range []string{"TDOCS_SECRET_KEY", "ROBDOCS_SECRET_KEY", "TELEDRIVE_SECRET_KEY"} {
		t.Setenv(name, "")
	}
	path := filepath.Join(t.TempDir(), "tdocs.db")
	if err := os.WriteFile(keyFilePath(path), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureSecretKey(path, false); err == nil {
		t.Fatal("empty existing key must not be silently replaced")
	}
}
