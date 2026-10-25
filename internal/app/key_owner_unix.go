//go:build unix

package app

import (
	"fmt"
	"os"
	"syscall"
)

func prepareKeyFile(keyPath, dbPath string) error {
	fd, err := syscall.Open(keyPath, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), keyPath)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("encryption key must be a regular file")
	}
	if os.Geteuid() == 0 {
		info, err := os.Stat(dbPath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			if owner, ok := info.Sys().(*syscall.Stat_t); ok {
				if err := file.Chown(int(owner.Uid), int(owner.Gid)); err != nil {
					return fmt.Errorf("match key ownership to database: %w", err)
				}
			}
		}
	}
	return file.Chmod(0o600)
}
