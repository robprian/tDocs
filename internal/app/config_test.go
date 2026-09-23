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
	t.Setenv("TDOCS_DB_PATH", "")
	t.Setenv("ROBDOCS_DB_PATH", "")
	t.Setenv("TELEDRIVE_DB_PATH", "")
	t.Setenv("TDOCS_MODE", "user")
	t.Setenv("TDOCS_DATA_DIR", filepath.Join(dir, "prod-data"))

	// Fresh production install: database lives under the data directory.
	got := resolveDBPath(ResolvePaths())
	want := filepath.Join(dir, "prod-data", "tdocs.db")
	if got != want {
		t.Fatalf("expected production default %q, got %q", want, got)
	}

	// Pre-rebrand robdocs.db in the CWD is adopted when it is the only database present.
	if err := os.WriteFile(filepath.Join(dir, "robdocs.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := resolveDBPath(ResolvePaths()); got != "robdocs.db" {
		t.Fatalf("expected robdocs.db fallback, got %q", got)
	}

	// tdocs.db in the CWD wins as soon as it exists.
	if err := os.WriteFile(filepath.Join(dir, "tdocs.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := resolveDBPath(ResolvePaths()); got != "tdocs.db" {
		t.Fatalf("expected tdocs.db to win, got %q", got)
	}
}

func TestResolveDBPathEnvOverride(t *testing.T) {
	t.Setenv("TDOCS_DB_PATH", "/tmp/custom.db")
	if got := resolveDBPath(ResolvePaths()); got != "/tmp/custom.db" {
		t.Fatalf("env override ignored: %q", got)
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
