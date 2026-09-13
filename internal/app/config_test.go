package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnv3Priority(t *testing.T) {
	t.Setenv("TDOCS_PORT", "9001")
	t.Setenv("ROBDOCS_PORT", "9002")
	t.Setenv("TELEDRIVE_PORT", "9003")
	if got := env3("TDOCS_PORT", "ROBDOCS_PORT", "TELEDRIVE_PORT", "8080"); got != "9001" {
		t.Fatalf("expected TDOCS_ to win, got %q", got)
	}

	os.Unsetenv("TDOCS_PORT")
	if got := env3("TDOCS_PORT", "ROBDOCS_PORT", "TELEDRIVE_PORT", "8080"); got != "9002" {
		t.Fatalf("expected ROBDOCS_ fallback, got %q", got)
	}

	os.Unsetenv("ROBDOCS_PORT")
	if got := env3("TDOCS_PORT", "ROBDOCS_PORT", "TELEDRIVE_PORT", "8080"); got != "9003" {
		t.Fatalf("expected TELEDRIVE_ fallback, got %q", got)
	}

	os.Unsetenv("TELEDRIVE_PORT")
	if got := env3("TDOCS_PORT", "ROBDOCS_PORT", "TELEDRIVE_PORT", "8080"); got != "8080" {
		t.Fatalf("expected default, got %q", got)
	}
}

func TestResolveDBPathPrefersNewestBrand(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if got := resolveDBPath(); got != "tdocs.db" {
		t.Fatalf("expected fresh default tdocs.db, got %q", got)
	}

	// Pre-rebrand robdocs.db is adopted when it is the only database present.
	if err := os.WriteFile(filepath.Join(dir, "robdocs.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := resolveDBPath(); got != "robdocs.db" {
		t.Fatalf("expected robdocs.db fallback, got %q", got)
	}

	// tdocs.db wins as soon as it exists.
	if err := os.WriteFile(filepath.Join(dir, "tdocs.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := resolveDBPath(); got != "tdocs.db" {
		t.Fatalf("expected tdocs.db to win, got %q", got)
	}
}

func TestKeyFilePathsOrder(t *testing.T) {
	paths := keyFilePaths("tdocs.db")
	want := []string{".tdocs.key", ".robdocs.key", ".teledrive.key"}
	if len(paths) != len(want) {
		t.Fatalf("expected %d key paths, got %v", len(want), paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("expected key path order %v, got %v", want, paths)
		}
	}
}
