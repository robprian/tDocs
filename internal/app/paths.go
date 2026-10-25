package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Paths describes where tDocs keeps configuration and mutable production data.
// Nothing mutable is ever written next to the installed binary (/usr/bin, /opt).
type Paths struct {
	// Mode is "dev" (source checkout), "user" (XDG per-user install), or
	// "system" (package install with /etc/tdocs + /var/lib/tdocs).
	Mode string
	// ConfigDir holds the production .env (and future config files).
	ConfigDir string
	// DataDir holds the SQLite database, encryption key, and other state.
	DataDir string
	// EnvFile is the canonical production environment file path.
	EnvFile string
	// CwdEnvFile is the legacy relative .env next to the process working
	// directory (kept for source-tree and pre-2.1 installs).
	CwdEnvFile string
}

// Env config-file candidates in precedence order (after real environment
// variables, which always win because loadDotEnv never overrides them):
//
//  1. ./.env               — legacy / source-tree location
//  2. ConfigDir/.env       — production config location
func (p *Paths) EnvFiles() []string {
	return []string{p.CwdEnvFile, p.EnvFile}
}

// ResolvePaths determines config/data locations for the current process.
//
// Overrides (highest first for directory selection):
//
//	TDOCS_CONFIG_DIR / TDOCS_DATA_DIR
//	mode defaults:
//	  dev    → process working directory
//	  user   → $XDG_CONFIG_HOME/tdocs, $XDG_DATA_HOME/tdocs
//	           (fallback ~/.config/tdocs, ~/.local/share/tdocs)
//	  system → /etc/tdocs, /var/lib/tdocs
//
// Mode detection:
//
//	TDOCS_MODE=dev|user|system  (explicit)
//	else dev   if a go.mod is found upward from the working directory
//	else system if the executable lives under /usr or /opt, or root is
//	           running with /etc/tdocs present
//	else user
func ResolvePaths() *Paths {
	mode := detectMode()
	p := &Paths{
		Mode:       mode,
		CwdEnvFile: ".env",
	}

	p.ConfigDir = firstNonEmpty(
		os.Getenv("TDOCS_CONFIG_DIR"),
		os.Getenv("ROBDOCS_CONFIG_DIR"),
		defaultConfigDir(mode),
	)
	p.DataDir = firstNonEmpty(
		os.Getenv("TDOCS_DATA_DIR"),
		os.Getenv("ROBDOCS_DATA_DIR"),
		defaultDataDir(mode),
	)
	p.EnvFile = filepath.Join(p.ConfigDir, ".env")
	return p
}

func detectMode() string {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("TDOCS_MODE"))); v == "dev" || v == "user" || v == "system" {
		return v
	}
	if isDevTree() {
		return "dev"
	}
	if isSystemBinary() || (os.Geteuid() == 0 && dirExists("/etc/tdocs")) {
		return "system"
	}
	return "user"
}

// isDevTree reports whether the working directory sits inside a source
// checkout (a go.mod exists on the path upward).
func isDevTree() bool {
	dir, err := os.Getwd()
	if err != nil {
		return false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// isSystemBinary reports whether the running executable was installed under
// system prefixes (/usr, /opt). Executable path is best-effort; failure to
// resolve falls back to argv0 on PATH.
func isSystemBinary() bool {
	exe, err := os.Executable()
	if err != nil {
		exe, err = exec.LookPath(os.Args[0])
		if err != nil {
			return false
		}
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	exe = filepath.Clean(exe)
	for _, prefix := range []string{"/usr/", "/opt/"} {
		if strings.HasPrefix(exe, prefix) {
			return true
		}
	}
	return false
}

func defaultConfigDir(mode string) string {
	switch mode {
	case "system":
		return "/etc/tdocs"
	case "dev":
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
		return "."
	default: // user
		if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			return filepath.Join(x, "tdocs")
		}
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ".tdocs-config"
		}
		return filepath.Join(home, ".config", "tdocs")
	}
}

func defaultDataDir(mode string) string {
	switch mode {
	case "system":
		return "/var/lib/tdocs"
	case "dev":
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
		return "."
	default: // user
		if x := os.Getenv("XDG_DATA_HOME"); x != "" {
			return filepath.Join(x, "tdocs")
		}
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ".tdocs-data"
		}
		return filepath.Join(home, ".local", "share", "tdocs")
	}
}

// EnsureDataDir creates the data directory with owner-only permissions when
// missing. Safe to call repeatedly.
func (p *Paths) EnsureDataDir() error {
	if p.DataDir == "" || p.DataDir == "." {
		return nil
	}
	return os.MkdirAll(p.DataDir, 0o700)
}

// EnsureConfigDir creates the config directory with owner-only permissions.
func (p *Paths) EnsureConfigDir() error {
	if p.ConfigDir == "" || p.ConfigDir == "." {
		return nil
	}
	return os.MkdirAll(p.ConfigDir, 0o700)
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// HostOS returns a stable runtime.GOOS string (helper for doctor output).
func HostOS() string { return runtime.GOOS }
