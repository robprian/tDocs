package main

import (
	"fmt"
	"os"
	"os/exec"
)

func restartCommand(userMode bool, uid int) (string, []string) {
	if userMode {
		return "systemctl", []string{"--user", "restart", "tdocs.service"}
	}
	if uid == 0 {
		return "systemctl", []string{"restart", "tdocs.service"}
	}
	return "sudo", []string{"systemctl", "restart", "tdocs.service"}
}

// restartServiceIfRunning restarts the system tdocs.service when it is
// currently active, and reports what it did. It never fails the update:
// a failed restart is reported as a warning with the manual command, and
// a service that is not running is simply left alone.
func restartServiceIfRunning() {
	if _, err := exec.LookPath("systemctl"); err != nil {
		fmt.Println("  ○ Restart the service if running: sudo systemctl restart tdocs")
		return
	}
	if exec.Command("systemctl", "is-active", "--quiet", "tdocs.service").Run() != nil {
		fmt.Println("  ○ tdocs service is not running — no restart needed.")
		return
	}
	name, args := restartCommand(false, os.Geteuid())
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "  ○ Automatic restart failed: %v\n    Restart manually: sudo systemctl restart tdocs\n", err)
		return
	}
	fmt.Println("  ✓ tdocs service restarted automatically")
}

// makePackageReadableForApt loosens a MkdirTemp directory (0700) and a
// downloaded package file (0600) so apt's _apt sandbox user can read them.
// Without this, `apt install` on a root-owned temp file prints
// "Download is performed unsandboxed as root ... couldn't be accessed by
// user '_apt' (13: Permission denied)". The install still succeeds, but
// the notice is alarming and the sandbox is bypassed. Failures here are
// warnings only: the install may still succeed unsandboxed.
func makePackageReadableForApt(dir, pkgPath string) {
	if err := os.Chmod(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "  ○ Could not relax temp dir permissions: %v\n", err)
	}
	if err := os.Chmod(pkgPath, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "  ○ Could not relax package permissions: %v\n", err)
	}
}

func runRestart(args []string) error {
	userMode := false
	for _, arg := range args {
		if arg != "--user" {
			return fmt.Errorf("usage: tdocs restart [--user]")
		}
		userMode = true
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("restart requires an installed systemd service; for a foreground server, stop it with Ctrl+C and run `tdocs server` again")
	}
	name, commandArgs := restartCommand(userMode, os.Geteuid())
	cmd := exec.Command(name, commandArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restart failed: %w; check that tdocs.service is installed and you have permission to restart it", err)
	}
	fmt.Println("tDocs service restarted. Telegram pairing and stored files are preserved.")
	return nil
}
