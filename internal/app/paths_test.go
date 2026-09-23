package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePathsDevMode(t *testing.T) {
	// This package lives inside the source tree (go.mod upward), so the
	// default detection must pick dev mode with CWD as config/data root.
	t.Setenv("TDOCS_MODE", "")
	t.Setenv("TDOCS_CONFIG_DIR", "")
	t.Setenv("TDOCS_DATA_DIR", "")
	t.Setenv("ROBDOCS_CONFIG_DIR", "")
	t.Setenv("ROBDOCS_DATA_DIR", "")

	p := ResolvePaths()
	if p.Mode != "dev" {
		t.Fatalf("expected dev mode inside source tree, got %q", p.Mode)
	}
	wd, _ := os.Getwd()
	if p.ConfigDir != wd || p.DataDir != wd {
		t.Fatalf("dev mode should use CWD: config=%q data=%q wd=%q", p.ConfigDir, p.DataDir, wd)
	}
	if p.EnvFile != filepath.Join(wd, ".env") {
		t.Fatalf("env file = %q", p.EnvFile)
	}
}

func TestResolvePathsUserMode(t *testing.T) {
	t.Setenv("TDOCS_MODE", "user")
	t.Setenv("TDOCS_CONFIG_DIR", "")
	t.Setenv("TDOCS_DATA_DIR", "")
	t.Setenv("ROBDOCS_CONFIG_DIR", "")
	t.Setenv("ROBDOCS_DATA_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")

	p := ResolvePaths()
	if p.Mode != "user" {
		t.Fatalf("mode = %q", p.Mode)
	}
	if p.ConfigDir != "/tmp/xdg-config/tdocs" {
		t.Fatalf("config = %q", p.ConfigDir)
	}
	if p.DataDir != "/tmp/xdg-data/tdocs" {
		t.Fatalf("data = %q", p.DataDir)
	}
}

func TestResolvePathsExplicitDirsWin(t *testing.T) {
	t.Setenv("TDOCS_CONFIG_DIR", "/custom/cfg")
	t.Setenv("TDOCS_DATA_DIR", "/custom/data")

	p := ResolvePaths()
	if p.ConfigDir != "/custom/cfg" || p.DataDir != "/custom/data" {
		t.Fatalf("explicit dirs ignored: %+v", p)
	}
	if p.EnvFile != "/custom/cfg/.env" {
		t.Fatalf("env = %q", p.EnvFile)
	}
}

func TestResolvePathsSystemMode(t *testing.T) {
	t.Setenv("TDOCS_MODE", "system")
	t.Setenv("TDOCS_CONFIG_DIR", "")
	t.Setenv("TDOCS_DATA_DIR", "")
	t.Setenv("ROBDOCS_CONFIG_DIR", "")
	t.Setenv("ROBDOCS_DATA_DIR", "")

	p := ResolvePaths()
	if p.ConfigDir != "/etc/tdocs" {
		t.Fatalf("system config = %q", p.ConfigDir)
	}
	if p.DataDir != "/var/lib/tdocs" {
		t.Fatalf("system data = %q", p.DataDir)
	}
}

func TestEnvFilesPrecedenceOrder(t *testing.T) {
	p := &Paths{CwdEnvFile: ".env", EnvFile: "/etc/tdocs/.env"}
	files := p.EnvFiles()
	if len(files) != 2 || files[0] != ".env" || files[1] != "/etc/tdocs/.env" {
		t.Fatalf("files = %v", files)
	}
}

func TestEnsureDataDirPermissions(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "state")
	p := &Paths{DataDir: data}
	if err := p.EnsureDataDir(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(data)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("data dir must not be group/world accessible, got %#o", perm)
	}
}

func TestIsDevTreeFindsGoMod(t *testing.T) {
	if !isDevTree() {
		t.Fatal("expected isDevTree() true under repository")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if isDevTree() {
		t.Fatal("expected isDevTree() false outside the repository")
	}
}

func TestDefaultConfigDirIgnoresEmptyXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home")
	}
	got := defaultConfigDir("user")
	if !strings.HasPrefix(got, home) {
		t.Fatalf("expected home-based config dir, got %q", got)
	}
}
