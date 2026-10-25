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

// serviceState is the pre-update state of the system tdocs.service.
// It MUST be captured BEFORE the package install: the .deb prerm (pre
// v3.1.4) runs `systemctl disable --now tdocs.service` on upgrade, so a
// check taken after apt always sees a stopped service — even one that was
// running fine before the update — and the updater then wrongly concludes
// "no restart needed" while leaving the service down and disabled.
type serviceState struct {
	managed bool // systemctl exists
	active  bool // is-active at capture time
	enabled bool // is-enabled at capture time
}

func captureServiceState() serviceState {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return serviceState{}
	}
	return serviceState{
		managed: true,
		active:  exec.Command("systemctl", "is-active", "--quiet", "tdocs.service").Run() == nil,
		enabled: exec.Command("systemctl", "is-enabled", "--quiet", "tdocs.service").Run() == nil,
	}
}

// decideServiceAction maps a captured state to what restore must do:
// re-enable exactly when it was enabled, restart exactly when it was
// active. A stopped-but-enabled service is re-enabled but NOT started;
// a never-managed service is left alone.
func decideServiceAction(s serviceState) (enable, restart bool) {
	return s.enabled, s.active
}

// restoreServiceAfterUpdate brings the service back to its pre-update
// state. It never fails the update: failures are reported as warnings
// with the manual command.
func restoreServiceAfterUpdate(s serviceState) {
	if !s.managed {
		fmt.Println("  ○ Restart the service if running: sudo systemctl restart tdocs")
		return
	}
	enable, restart := decideServiceAction(s)
	if !enable && !restart {
		fmt.Println("  ○ tdocs service was not running before the update — left stopped.")
		return
	}
	if enable {
		name, args := "systemctl", []string{"enable", "tdocs.service"}
		if os.Geteuid() != 0 {
			name, args = "sudo", []string{"systemctl", "enable", "tdocs.service"}
		}
		if err := exec.Command(name, args...).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  ○ Could not re-enable tdocs service: %v\n    Enable manually: sudo systemctl enable tdocs\n", err)
		}
	}
	if !restart {
		fmt.Println("  ✓ tdocs service re-enabled (it was stopped before the update).")
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
