package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMakePackageReadableForApt(t *testing.T) {
	tmp := t.TempDir()
	// t.TempDir is already 0700; simulate the updater's MkdirTemp + download.
	dir := filepath.Join(tmp, "tdocs-update-test")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(dir, "tdocs_3.1.2_amd64.deb")
	if err := os.WriteFile(pkg, []byte("fake deb"), 0o600); err != nil {
		t.Fatal(err)
	}
	makePackageReadableForApt(dir, pkg)
	if got, _ := os.Stat(dir); got.Mode().Perm() != 0o755 {
		t.Fatalf("dir perms = %o, want 755", got.Mode().Perm())
	}
	if got, _ := os.Stat(pkg); got.Mode().Perm() != 0o644 {
		t.Fatalf("file perms = %o, want 644", got.Mode().Perm())
	}
}

func TestMakePackageReadableForAptMissingFileDoesNotPanic(t *testing.T) {
	// A missing file must only warn on stderr, never crash the updater.
	makePackageReadableForApt(t.TempDir(), filepath.Join(t.TempDir(), "nope.deb"))
}
