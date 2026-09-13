package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotAndRestore(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "original.db")

	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open original failed: %v", err)
	}

	// Insert data
	folder, err := database.CreateFolder("Important Documents", nil)
	if err != nil {
		t.Fatalf("CreateFolder failed: %v", err)
	}
	_, err = database.CreateFile(&folder.ID, "passport.pdf", 512000, "application/pdf", 101, "tg_file_1", "access_hash_1", "sha_1")
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	// 1. Create snapshot
	gzPath, err := database.CreateSnapshot()
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}
	defer os.Remove(gzPath)

	database.Close()

	// 2. Restore to new location
	restoredPath := filepath.Join(tempDir, "restored.db")
	if err := RestoreFromGzip(gzPath, restoredPath); err != nil {
		t.Fatalf("RestoreFromGzip failed: %v", err)
	}

	// 3. Verify restored database has identical records
	restoredDB, err := Open(restoredPath)
	if err != nil {
		t.Fatalf("Open restored db failed: %v", err)
	}
	defer restoredDB.Close()

	folders, err := restoredDB.ListFolders(nil)
	if err != nil || len(folders) != 1 || folders[0].Name != "Important Documents" {
		t.Fatalf("Restored folder verification failed: %v", folders)
	}

	files, err := restoredDB.ListFiles(&folders[0].ID)
	if err != nil || len(files) != 1 || files[0].Name != "passport.pdf" {
		t.Fatalf("Restored file verification failed: %v", files)
	}
}
