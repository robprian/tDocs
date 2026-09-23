package main

import (
	"fmt"
	"runtime"
)

// Build-time metadata injected via -ldflags "-X main.Version=... -X main.Commit=... -X main.BuildDate=...".
// Development builds leave these at their defaults and print "dev".
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// displayVersion returns the human-facing version string:
// "dev" for development builds, "v2.1.0" for tagged releases.
func displayVersion() string {
	if Version == "" || Version == "dev" {
		return "dev"
	}
	if Version[0] == 'v' {
		return Version
	}
	return "v" + Version
}

// packageVersion returns the bare version without a leading v (for packages/scripts).
func packageVersion() string {
	v := displayVersion()
	if v == "dev" {
		return "dev"
	}
	return v[1:]
}

func printVersion() {
	fmt.Print(banner)
	fmt.Printf("  tDocs %s — Telegram MTProto personal cloud\n", displayVersion())
	fmt.Printf("  commit:    %s\n", Commit)
	fmt.Printf("  build date: %s\n", BuildDate)
	fmt.Printf("  go:        %s\n", runtime.Version())
	fmt.Printf("  platform:  %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println("  Created by Robby Aprianto (https://github.com/robprian/tDocs)")
	fmt.Print("  Licensed under the MIT License\n")
}
