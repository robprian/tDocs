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
