//go:build unix

package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareKeyFileRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "unrelated-file")
	if err := os.WriteFile(target, []byte("not a session key"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".tdocs.key")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := prepareKeyFile(link, filepath.Join(dir, "tdocs.db")); err == nil {
		t.Fatal("must not change permissions or ownership through a symlink")
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatal("unrelated file permissions changed")
	}
}
