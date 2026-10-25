//go:build !unix

package main

import "os"

// ownerSuffix has no uid/gid concept outside Unix; permission hints simply
// omit ownership.
func ownerSuffix(os.FileInfo) string { return "" }
