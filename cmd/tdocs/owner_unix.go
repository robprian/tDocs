//go:build unix

package main

import (
	"fmt"
	"os"
	"os/user"
	"syscall"
)

// ownerSuffix describes who owns a file and its mode, for permission errors.
// Unix-only: syscall.Stat_t does not exist on Windows.
func ownerSuffix(fi os.FileInfo) string {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	if u, err := user.LookupId(fmt.Sprint(st.Uid)); err == nil {
		return fmt.Sprintf(" (milik %s, mode %#o)", u.Username, fi.Mode().Perm())
	}
	return fmt.Sprintf(" (milik uid %d, mode %#o)", st.Uid, fi.Mode().Perm())
}
