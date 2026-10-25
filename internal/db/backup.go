package db

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// CreateSnapshot forces a WAL checkpoint, executes VACUUM INTO to create a consistent
// point-in-time snapshot, and compresses it with gzip.
func (d *DB) CreateSnapshot() (string, error) {
	// 1. Flush WAL to main database file
	if _, err := d.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
		return "", fmt.Errorf("wal checkpoint failed: %w", err)
	}

	tempDir := os.TempDir()
	timestamp := time.Now().Format("20060102-150405")
	rawSnapshotPath := filepath.Join(tempDir, fmt.Sprintf("tdocs-raw-%s.db", timestamp))
	defer os.Remove(rawSnapshotPath)

	// 2. Safe online backup via VACUUM INTO
	vacuumQuery := fmt.Sprintf("VACUUM INTO '%s';", rawSnapshotPath)
	if _, err := d.Exec(vacuumQuery); err != nil {
		return "", fmt.Errorf("vacuum into failed: %w", err)
	}

	// 3. Compress with gzip
	rawFile, err := os.Open(rawSnapshotPath)
	if err != nil {
		return "", fmt.Errorf("open raw snapshot: %w", err)
	}
	defer rawFile.Close()

	gzPath := filepath.Join(tempDir, fmt.Sprintf("tdocs-backup-%s.db.gz", timestamp))
	gzFile, err := os.Create(gzPath)
	if err != nil {
		return "", fmt.Errorf("create gzip snapshot: %w", err)
	}
	defer gzFile.Close()

	gzWriter := gzip.NewWriter(gzFile)
	if _, err := io.Copy(gzWriter, rawFile); err != nil {
		return "", fmt.Errorf("gzip compress snapshot: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		return "", fmt.Errorf("close gzip writer: %w", err)
	}

	return gzPath, nil
}

// RestoreFromGzip decompresses a gzipped database snapshot to the destination DB path.
func RestoreFromGzip(gzPath, destDBPath string) error {
	gzFile, err := os.Open(gzPath)
	if err != nil {
		return fmt.Errorf("open gzip file: %w", err)
	}
	defer gzFile.Close()

	gzReader, err := gzip.NewReader(gzFile)
	if err != nil {
		return fmt.Errorf("init gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destDBPath), 0755); err != nil {
		return fmt.Errorf("mkdir for dest: %w", err)
	}

	// Remove existing files and WAL/SHM files
	_ = os.Remove(destDBPath)
	_ = os.Remove(destDBPath + "-wal")
	_ = os.Remove(destDBPath + "-shm")

	destFile, err := os.OpenFile(destDBPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create dest db file: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, gzReader); err != nil {
		return fmt.Errorf("decompress to dest: %w", err)
	}

	return nil
}
