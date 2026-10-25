package db

import (
	"path/filepath"
	"testing"
)

func TestDB_FolderAndFileOperations(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer database.Close()

	// 1. Create root folder
	root, err := database.CreateFolder("Work", nil)
	if err != nil {
		t.Fatalf("CreateFolder root failed: %v", err)
	}

	// 2. Create subfolder
	sub, err := database.CreateFolder("Projects", &root.ID)
	if err != nil {
		t.Fatalf("CreateFolder sub failed: %v", err)
	}

	// 3. Test cycle prevention (cannot move root into its subfolder)
	err = database.MoveFolder(root.ID, &sub.ID)
	if err == nil {
		t.Fatalf("Expected error when moving parent into its child, got nil")
	}

	// 4. Create file in subfolder
	file, err := database.CreateFile(&sub.ID, "report.pdf", 1048576, "application/pdf", 1234, "file_xyz", "access_hash_abc", "dummy_sha256")
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	// 5. Search file
	results, err := database.SearchFiles("report")
	if err != nil || len(results) != 1 {
		t.Fatalf("SearchFiles failed, expected 1 result, got %d (err: %v)", len(results), err)
	}
	if results[0].ID != file.ID {
		t.Fatalf("SearchFiles returned wrong file ID %s, expected %s", results[0].ID, file.ID)
	}

	// 6. Test share link creation
	share, err := database.CreateShareLink(file.ID, nil, nil, nil, false)
	if err != nil {
		t.Fatalf("CreateShareLink failed: %v", err)
	}

	loadedShare, err := database.GetShareLink(share.Token)
	if err != nil || loadedShare.FileID != file.ID {
		t.Fatalf("GetShareLink failed or returned mismatch")
	}

	// 6b. List and Delete ShareLink
	shares, err := database.ListShareLinks()
	if err != nil || len(shares) != 1 {
		t.Fatalf("ListShareLinks failed, expected 1, got %d: %v", len(shares), err)
	}
	if shares[0].FileName != "report.pdf" {
		t.Fatalf("ListShareLinks expected FileName 'report.pdf', got '%s'", shares[0].FileName)
	}
	if err := database.DeleteShareLink(share.ID); err != nil {
		t.Fatalf("DeleteShareLink failed: %v", err)
	}
	shares, err = database.ListShareLinks()
	if err != nil || len(shares) != 0 {
		t.Fatalf("Expected 0 shares after delete, got %d", len(shares))
	}

	// 7. Delete folder (cascading check)
	err = database.DeleteFolder(root.ID)
	if err != nil {
		t.Fatalf("DeleteFolder failed: %v", err)
	}

	_, err = database.GetFile(file.ID)
	// Foreign key set NULL or file remains if on delete set null
	// In schema: ON DELETE SET NULL for files(folder_id)
	if err != nil {
		t.Fatalf("File query after folder deletion failed: %v", err)
	}
}

func TestDB_UpsertFileFromChannelIsIdempotent(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "channel.db")

	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer database.Close()

	// First sync inserts a recovered file record.
	inserted, err := database.UpsertFileFromChannel(nil, "film.mp4", 4200000000, "video/mp4", 501, "9001", "77")
	if err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}
	if !inserted {
		t.Fatalf("expected first upsert to insert a new row")
	}

	// Second sync for the same message must update, never duplicate.
	inserted, err = database.UpsertFileFromChannel(nil, "film-renamed.mp4", 4200000000, "video/mp4", 501, "9001", "77")
	if err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}
	if inserted {
		t.Fatalf("expected second upsert to update the existing row")
	}

	all, err := database.ListAllFiles("", "", 100)
	if err != nil {
		t.Fatalf("list all files: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected exactly 1 file after re-sync, got %d", len(all))
	}
	if all[0].Name != "film-renamed.mp4" {
		t.Fatalf("expected refreshed name, got %q", all[0].Name)
	}
	if all[0].TelegramFileID != "9001" || all[0].TelegramAccessHash != "77" {
		t.Fatalf("telegram coordinates not persisted: %+v", all[0])
	}
}
