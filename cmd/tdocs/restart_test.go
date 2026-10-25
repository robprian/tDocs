package main

import (
	"reflect"
	"testing"
)

func TestRestartCommand(t *testing.T) {
	for _, tc := range []struct {
		name    string
		user    bool
		uid     int
		command string
		args    []string
	}{
		{"system as root", false, 0, "systemctl", []string{"restart", "tdocs.service"}},
		{"system as user", false, 1000, "sudo", []string{"systemctl", "restart", "tdocs.service"}},
		{"user service", true, 1000, "systemctl", []string{"--user", "restart", "tdocs.service"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name, args := restartCommand(tc.user, tc.uid)
			if name != tc.command || !reflect.DeepEqual(args, tc.args) {
				t.Fatalf("got %s %v, want %s %v", name, args, tc.command, tc.args)
			}
		})
	}
}

func TestRestartRejectsUnknownArguments(t *testing.T) {
	if err := runRestart([]string{"--force"}); err == nil {
		t.Fatal("expected invalid argument to fail without restarting anything")
	}
}
