package main

// Production operations: migrate, status, systemd service, self-update, uninstall.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"tdocs/internal/app"
	"tdocs/internal/db"
)

// ---------------------------------------------------------------------------
// tdocs migrate
// ---------------------------------------------------------------------------

func runMigrate(cfg *app.Config) {
	fmt.Println("  ◈ tDocs · migrate")
	fmt.Println("  ──────────────────")
	fmt.Printf("  Database: %s\n", cfg.DBPath)

	if cfg.Paths != nil && cfg.Paths.DataDir != "" && cfg.Paths.DataDir != "." {
		_ = cfg.Paths.EnsureDataDir()
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ Migration failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "    Existing data was not modified beyond a failed open.")
		fmt.Fprintln(os.Stderr, "    Restore a backup (tdocs restore) or upgrade tDocs if the schema is newer.")
		os.Exit(1)
	}
	defer database.Close()

	ver, err := database.SchemaVersion()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ Could not read schema version: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  ✓ Schema at version %d (this build supports %d)\n", ver, db.SchemaVersion)
	if ver < db.SchemaVersion {
		fmt.Println("  ○ Re-open stamped the database to the current version.")
	} else if ver > db.SchemaVersion {
		fmt.Println("  ✕ Database is newer than this binary — upgrade tDocs.")
		os.Exit(1)
	}
	fmt.Println("  ✓ Migration complete — no data removed.")
}

// ---------------------------------------------------------------------------
// tdocs status
// ---------------------------------------------------------------------------

func runStatus(cfg *app.Config) {
	fmt.Println("  ◈ tDocs · status")
	fmt.Printf("  version   tDocs %s (%s/%s)\n", displayVersion(), runtime.GOOS, runtime.GOARCH)
	if cfg.Paths != nil {
		fmt.Printf("  mode      %s\n", cfg.Paths.Mode)
		fmt.Printf("  config    %s\n", cfg.Paths.ConfigDir)
		fmt.Printf("  data      %s\n", cfg.Paths.DataDir)
	}
	fmt.Printf("  database  %s", cfg.DBPath)
	if fi, err := os.Stat(cfg.DBPath); err == nil {
		fmt.Printf(" (%d bytes)", fi.Size())
	} else {
		fmt.Printf(" (not created yet)")
	}
	fmt.Println()

	port := cfg.Port
	if port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%s/api/health", port))
	if err != nil {
		fmt.Printf("  server    not responding on port %s\n", port)
	} else {
		_ = resp.Body.Close()
		fmt.Printf("  server    http://127.0.0.1:%s (HTTP %d)\n", port, resp.StatusCode)
	}

	if cfg.Paths != nil && cfg.Paths.DataDir != "" && cfg.Paths.DataDir != "." {
		_ = cfg.Paths.EnsureDataDir()
	}
	if database, err := db.Open(cfg.DBPath); err == nil {
		if ver, err := database.SchemaVersion(); err == nil {
			fmt.Printf("  schema    v%d\n", ver)
		}
		if _, err := database.GetSetting("telegram_authorized"); err == nil {
			fmt.Println("  telegram  paired")
		} else {
			fmt.Println("  telegram  not paired (run `tdocs login`)")
		}
		database.Close()
	}
}

// ---------------------------------------------------------------------------
// tdocs service install|remove
// ---------------------------------------------------------------------------

func runService(cfg *app.Config, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "  Usage: tdocs service install [--user] | tdocs service remove [--user]")
		os.Exit(1)
	}
	userMode := false
	for _, a := range args[1:] {
		if a == "--user" {
			userMode = true
		}
	}
	switch args[0] {
	case "install":
		serviceInstall(cfg, userMode)
	case "remove", "uninstall":
		serviceRemove(userMode)
	default:
		fmt.Fprintf(os.Stderr, "  Unknown service subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func serviceInstall(cfg *app.Config, userMode bool) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		fmt.Fprintln(os.Stderr, "  ✕ systemctl not available — use your init system or run `tdocs server` directly.")
		os.Exit(1)
	}

	exe, err := os.Executable()
	if err != nil {
		exe = "tdocs"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	configDir := "."
	dataDir := "."
	mode := "user"
	if cfg.Paths != nil {
		configDir = cfg.Paths.ConfigDir
		dataDir = cfg.Paths.DataDir
		if !userMode && cfg.Paths.Mode == "system" {
			mode = "system"
		}
	}
	if userMode {
		mode = "user"
	}

	unit := fmt.Sprintf(`[Unit]
Description=tDocs — Telegram MTProto personal cloud
Documentation=https://github.com/robprian/tDocs
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s server
WorkingDirectory=%s
Environment=TDOCS_NO_BROWSER=1
Environment=TDOCS_MODE=%s
Environment=TDOCS_CONFIG_DIR=%s
Environment=TDOCS_DATA_DIR=%s
EnvironmentFile=-%s
Restart=on-failure
RestartSec=5
TimeoutStopSec=30
KillSignal=SIGTERM

# Hardening (ignored gracefully on older systemd)
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=read-only
RestrictSUIDSGID=true
LockPersonality=true

[Install]
WantedBy=%s
`, exe, dataDir, mode, configDir, dataDir, cfg.Paths.EnvFile,
		map[bool]string{true: "default.target", false: "multi-user.target"}[userMode])

	var unitPath string
	if userMode {
		home, _ := os.UserHomeDir()
		dir := filepath.Join(home, ".config", "systemd", "user")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "  ✕ %v\n", err)
			os.Exit(1)
		}
		unitPath = filepath.Join(dir, "tdocs.service")
		if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "  ✕ %v\n", err)
			os.Exit(1)
		}
		_ = runCapture("systemctl", "--user", "daemon-reload")
		_ = runCapture("systemctl", "--user", "enable", "--now", "tdocs.service")
		fmt.Println("  ✓ User service installed & started: systemctl --user status tdocs")
		return
	}

	// System unit: prefer packaging path when writable (root), else /etc/systemd/system.
	candidates := []string{
		"/etc/systemd/system/tdocs.service",
		"/usr/lib/systemd/system/tdocs.service",
		"/lib/systemd/system/tdocs.service",
	}
	unitPath = candidates[0]
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		// Fall back to generating under a temp path and sudo mv.
		tmp := filepath.Join(os.TempDir(), "tdocs.service")
		if err := os.WriteFile(tmp, []byte(unit), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "  ✕ Cannot write systemd unit (need root?): %v\n", err)
			os.Exit(1)
		}
		cmd := exec.Command("sudo", "mv", tmp, unitPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  ✕ Installing unit failed: %v\n", err)
			os.Exit(1)
		}
	}
	if err := runCapture("sudo", "systemctl", "daemon-reload"); err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ daemon-reload failed: %v\n", err)
		os.Exit(1)
	}
	_ = runCapture("sudo", "systemctl", "enable", "tdocs.service")
	fmt.Println("  ✓ System service installed.")
	fmt.Println("      sudo systemctl start tdocs")
	fmt.Println("      sudo systemctl status tdocs")
	fmt.Println("      sudo journalctl -u tdocs -f")
}

func serviceRemove(userMode bool) {
	if userMode {
		_ = runCapture("systemctl", "--user", "disable", "--now", "tdocs.service")
		home, _ := os.UserHomeDir()
		_ = os.Remove(filepath.Join(home, ".config", "systemd", "user", "tdocs.service"))
		_ = runCapture("systemctl", "--user", "daemon-reload")
		fmt.Println("  ✓ User service removed (data untouched).")
		return
	}
	_ = runCapture("sudo", "systemctl", "disable", "--now", "tdocs.service")
	for _, p := range []string{
		"/etc/systemd/system/tdocs.service",
		"/usr/lib/systemd/system/tdocs.service",
		"/lib/systemd/system/tdocs.service",
	} {
		_ = os.Remove(p)
	}
	_ = runCapture("sudo", "systemctl", "daemon-reload")
	fmt.Println("  ✓ System service removed (data untouched).")
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func runCapture(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ---------------------------------------------------------------------------
// tdocs update
// ---------------------------------------------------------------------------

func runUpdate(cfg *app.Config, args []string) {
	checkOnly := false
	for _, a := range args {
		if a == "--check" || a == "-n" {
			checkOnly = true
		}
	}

	fmt.Printf("  ◈ tDocs · update (current %s)\n", displayVersion())
	latest, err := app.FetchLatestRelease(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ Could not query GitHub releases: %v\n", err)
		fmt.Fprintln(os.Stderr, "    Upgrade manually from https://github.com/robprian/tDocs/releases")
		os.Exit(1)
	}
	// Same version: nothing to advertise, so no release line and no command.
	if !app.IsNewer(displayVersion(), latest.Tag) {
		fmt.Println("  ✓ Already on the latest release.")
		return
	}
	fmt.Printf("  Latest release: %s (%s)\n", latest.Tag, latest.PublishedAt.Format("2006-01-02"))
	if checkOnly {
		fmt.Println("  ○ Update available — run `tdocs update` to install.")
		return
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ Cannot resolve current binary path: %v\n", err)
		os.Exit(1)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	mode := ""
	if cfg.Paths != nil {
		mode = cfg.Paths.Mode
	}
	if kind := app.InstallKind(exe, mode); kind == app.InstallDeb || kind == app.InstallRPM {
		if name, ok := app.PackageName(kind, latest.Tag, runtime.GOARCH); ok {
			if url := latest.AssetURL(name); url != "" {
				tmp, tErr := os.MkdirTemp("", "tdocs-update-")
				if tErr == nil {
					defer os.RemoveAll(tmp)
					pkgPath := filepath.Join(tmp, name)
					sumsURL := latest.AssetURL("SHA256SUMS")
					fmt.Printf("  ↓ Downloading %s …\n", name)
					if dErr := downloadFile(url, pkgPath); dErr != nil {
						fmt.Fprintf(os.Stderr, "  ✕ Download failed: %v\n", dErr)
						os.Exit(1)
					}
					if sumsURL != "" {
						sumsPath := filepath.Join(tmp, "SHA256SUMS")
						if dErr := downloadFile(sumsURL, sumsPath); dErr == nil {
							if want, lErr := lookupSHA256(sumsPath, name); lErr == nil {
								if got, hErr := fileSHA256(pkgPath); hErr == nil && got == want {
									fmt.Println("  ✓ SHA256 checksum verified")
								} else {
									fmt.Fprintf(os.Stderr, "  ✕ Checksum mismatch (want %s got %s)\n", want, got)
									os.Exit(1)
								}
							}
						}
					}
					// Auto-install via package manager (no second download).
					if kind == app.InstallDeb {
						fmt.Printf("  → Installing %s via apt …\n", name)
						if err := exec.Command("sudo", "apt", "update").Run(); err != nil {
							fmt.Fprintf(os.Stderr, "  ○ apt update warning: %v\n", err)
						}
						cmd := exec.Command("sudo", append([]string{"apt", "install", "-y"}, pkgPath)...)
						cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
						if err := cmd.Run(); err != nil {
							fmt.Fprintf(os.Stderr, "  ✕ Install failed: %v\n  Hint: %s\n  Release: %s\n", err, app.UpgradeHint(kind, latest.Tag, runtime.GOARCH), app.ReleaseURL(latest.Tag))
							os.Exit(1)
						}
					} else {
						bin, args := app.PackageManager(kind)
						fmt.Printf("  → Installing %s via %s …\n", name, bin)
						cmd := exec.Command("sudo", append([]string{bin}, append(args, pkgPath)...)...)
						cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
						if err := cmd.Run(); err != nil {
							fmt.Fprintf(os.Stderr, "  ✕ Install failed: %v\n  Hint: %s\n  Release: %s\n", err, app.UpgradeHint(kind, latest.Tag, runtime.GOARCH), app.ReleaseURL(latest.Tag))
							os.Exit(1)
						}
					}
					fmt.Printf("  ✓ Updated to %s via %s package\n", latest.Tag, kind)
					fmt.Println("  ○ Restart the service if running: sudo systemctl restart tdocs")
					return
				}
			}
		}
		fmt.Printf("  ○ Package install (%s) detected — upgrade via the package manager:\n", kind)
		fmt.Printf("    %s\n", app.UpgradeHint(kind, latest.Tag, runtime.GOARCH))
		fmt.Printf("    Release notes: %s\n", app.ReleaseURL(latest.Tag))
		return
	}
	if runtime.GOOS != "linux" {
		fmt.Fprintln(os.Stderr, "  ✕ Automatic update supports linux builds only. Download from GitHub Releases.")
		os.Exit(1)
	}

	goarch := runtime.GOARCH
	asset, ok := app.TarballName(latest.Tag, goarch)
	if !ok {
		fmt.Fprintf(os.Stderr, "  ✕ No published tarball for %s/%s.\n", runtime.GOOS, goarch)
		os.Exit(1)
	}

	sumsURL := latest.AssetURL("SHA256SUMS")
	assetURL := latest.AssetURL(asset)
	if assetURL == "" {
		fmt.Fprintf(os.Stderr, "  ✕ Asset %s not found in release %s.\n", asset, latest.Tag)
		os.Exit(1)
	}

	tmp, err := os.MkdirTemp("", "tdocs-update-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	tarPath := filepath.Join(tmp, asset)
	fmt.Printf("  ↓ Downloading %s …\n", asset)
	if err := downloadFile(assetURL, tarPath); err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ Download failed: %v\n", err)
		os.Exit(1)
	}

	if sumsURL != "" {
		sumsPath := filepath.Join(tmp, "SHA256SUMS")
		if err := downloadFile(sumsURL, sumsPath); err != nil {
			fmt.Fprintf(os.Stderr, "  ✕ Checksum download failed: %v\n", err)
			os.Exit(1)
		}
		want, err := lookupSHA256(sumsPath, asset)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ✕ %v\n", err)
			os.Exit(1)
		}
		got, err := fileSHA256(tarPath)
		if err != nil || got != want {
			fmt.Fprintf(os.Stderr, "  ✕ Checksum mismatch (want %s got %s)\n", want, got)
			os.Exit(1)
		}
		fmt.Println("  ✓ SHA256 checksum verified")
	} else {
		fmt.Println("  ○ SHA256SUMS not published for this release — aborting (unsafe).")
		os.Exit(1)
	}

	// Extract binary
	if err := exec.Command("tar", "-xzf", tarPath, "-C", tmp).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ Extract failed: %v\n", err)
		os.Exit(1)
	}
	newBin := filepath.Join(tmp, "tdocs", "tdocs")
	if _, err := os.Stat(newBin); err != nil {
		newBin = filepath.Join(tmp, "tdocs")
	}
	if _, err := os.Stat(newBin); err != nil {
		fmt.Fprintln(os.Stderr, "  ✕ Extracted archive does not contain a tdocs binary.")
		os.Exit(1)
	}

	backup := exe + ".bak"
	if err := copyFile(exe, backup); err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ Backup of current binary failed: %v\n", err)
		os.Exit(1)
	}
	if err := copyFile(newBin, exe); err != nil {
		// roll back
		_ = copyFile(backup, exe)
		fmt.Fprintf(os.Stderr, "  ✕ Replace failed (rolled back): %v\n", err)
		os.Exit(1)
	}
	_ = os.Chmod(exe, 0o755)
	fmt.Printf("  ✓ Updated %s → %s (backup: %s)\n", exe, latest.Tag, backup)
	fmt.Println("  ○ Restart the service if running: sudo systemctl restart tdocs")
	fmt.Println("  ○ Database and configuration were not modified.")
}

func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func lookupSHA256(sumsPath, name string) (string, error) {
	data, err := os.ReadFile(sumsPath)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && (fields[1] == name || strings.HasSuffix(fields[1], "/"+name)) {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("no SHA256 entry for %s", name)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// ---------------------------------------------------------------------------
// tdocs uninstall
// ---------------------------------------------------------------------------

func runUninstall(cfg *app.Config, args []string) {
	purge := false
	yes := false
	for _, a := range args {
		switch a {
		case "--purge":
			purge = true
		case "--yes", "-y":
			yes = true
		}
	}

	fmt.Println("  ◈ tDocs · uninstall")
	fmt.Println("  ────────────────────")
	fmt.Println("  Application files:")
	exe, _ := os.Executable()
	if exe != "" {
		fmt.Printf("    binary    %s\n", exe)
	}
	if cfg.Paths != nil {
		fmt.Printf("    config    %s\n", cfg.Paths.ConfigDir)
		fmt.Printf("    data      %s\n", cfg.Paths.DataDir)
	}
	fmt.Printf("    database  %s\n", cfg.DBPath)
	fmt.Println()
	fmt.Println("  User data (database, Telegram session, files metadata) is NEVER")
	fmt.Println("  deleted unless you pass --purge AND confirm.")

	reader := bufio.NewReader(os.Stdin)

	// Stop/disable service if present (best-effort).
	if _, err := exec.LookPath("systemctl"); err == nil {
		_ = runCapture("systemctl", "is-active", "--quiet", "tdocs.service")
	}

	if !purge {
		if !yes {
			fmt.Print("  Remove application binary/service only? [y/N]: ")
			line, _ := reader.ReadString('\n')
			if !strings.EqualFold(strings.TrimSpace(line), "y") && !strings.EqualFold(strings.TrimSpace(line), "yes") {
				fmt.Println("  ○ Cancelled.")
				return
			}
		}
		// Remove unit
		if _, err := exec.LookPath("systemctl"); err == nil {
			_ = runCapture("sudo", "systemctl", "disable", "--now", "tdocs.service")
		}
		removed := false
		for _, p := range []string{
			"/etc/systemd/system/tdocs.service",
			"/usr/lib/systemd/system/tdocs.service",
			"/lib/systemd/system/tdocs.service",
		} {
			if err := os.Remove(p); err == nil {
				removed = true
			}
		}
		if removed {
			_ = runCapture("sudo", "systemctl", "daemon-reload")
		}
		fmt.Println("  ✓ Service unit removed (if present).")
		fmt.Printf("  ○ Remove the binary manually when ready: rm %s\n", exe)
		fmt.Println("  ○ Config and data were left in place.")
		fmt.Println("    Use `tdocs uninstall --purge` to also delete them (with confirmation).")
		return
	}

	// Purge path — explicit confirmation for every destructive step.
	if !yes {
		fmt.Printf("  DELETE configuration directory %s ? [y/N]: ", cfg.Paths.ConfigDir)
		line, _ := reader.ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(line), "y") {
			fmt.Println("  ○ Config kept.")
		} else {
			if err := os.RemoveAll(cfg.Paths.ConfigDir); err != nil {
				fmt.Fprintf(os.Stderr, "  ✕ %v\n", err)
			} else {
				fmt.Println("  ✓ Config removed.")
			}
		}

		fmt.Printf("  DELETE data directory %s (database + Telegram session + keys) ? [y/N]: ", cfg.Paths.DataDir)
		line, _ = reader.ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(line), "y") {
			fmt.Println("  ○ Data kept — nothing else will be deleted.")
			return
		}
		if err := os.RemoveAll(cfg.Paths.DataDir); err != nil {
			fmt.Fprintf(os.Stderr, "  ✕ %v\n", err)
			os.Exit(1)
		}
		fmt.Println("  ✓ Data removed.")
		if cfg.DBPath != "" && !strings.HasPrefix(cfg.DBPath, cfg.Paths.DataDir) {
			_ = os.Remove(cfg.DBPath)
		}
	}
	fmt.Println("  ✓ Purge complete.")
}
