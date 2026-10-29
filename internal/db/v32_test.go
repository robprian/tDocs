package db

import (
	"path/filepath"
	"testing"
	"time"
)

func openV32TestDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(filepath.Join(t.TempDir(), "v32.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestV32_Tags(t *testing.T) {
	database := openV32TestDB(t)
	file, err := database.CreateFile(nil, "report.pdf", 100, "application/pdf", 1, "f1", "h1", "sha")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AddTag(file.ID, "Work Docs"); err != nil {
		t.Fatalf("AddTag failed: %v", err)
	}
	// Duplicate add is a no-op, not a second row.
	if err := database.AddTag(file.ID, "work-docs"); err != nil {
		t.Fatalf("duplicate AddTag failed: %v", err)
	}
	if err := database.AddTag(file.ID, "!!!"); err == nil {
		t.Fatal("expected invalid tag to fail")
	}
	tags, err := database.ListFileTags(file.ID)
	if err != nil || len(tags) != 1 || tags[0] != "work-docs" {
		t.Fatalf("ListFileTags = %v, err %v", tags, err)
	}
	all, err := database.ListAllTags()
	if err != nil || len(all) != 1 || all[0].Tag != "work-docs" || all[0].Count != 1 {
		t.Fatalf("ListAllTags = %v, err %v", all, err)
	}
	byTag, err := database.ListFilesByTag("work-docs", nil)
	if err != nil || len(byTag) != 1 || byTag[0].ID != file.ID {
		t.Fatalf("ListFilesByTag = %v, err %v", byTag, err)
	}
	other := "nope-folder"
	byTag, err = database.ListFilesByTag("work-docs", &other)
	if err != nil || len(byTag) != 0 {
		t.Fatalf("folder-scoped ListFilesByTag should be empty, got %v, err %v", byTag, err)
	}
	if err := database.RemoveTag(file.ID, "work-docs"); err != nil {
		t.Fatalf("RemoveTag failed: %v", err)
	}
	tags, err = database.ListFileTags(file.ID)
	if err != nil || len(tags) != 0 {
		t.Fatalf("tags after remove = %v, err %v", tags, err)
	}
}

func TestV32_FileRequests(t *testing.T) {
	database := openV32TestDB(t)
	fr, err := database.CreateFileRequest(nil, "Homework", nil)
	if err != nil {
		t.Fatal(err)
	}
	if fr.Token == "" {
		t.Fatal("token must not be empty")
	}
	got, err := database.GetFileRequestByToken(fr.Token)
	if err != nil || got.ID != fr.ID || got.Expired() {
		t.Fatalf("GetFileRequestByToken = %+v, err %v", got, err)
	}
	past := time.Now().Add(-time.Hour)
	expired, err := database.CreateFileRequest(nil, "Old", &past)
	if err != nil {
		t.Fatal(err)
	}
	if !expired.Expired() {
		t.Fatal("past-expiry request must report Expired")
	}
	if err := database.LinkRequestSession("sess-1", fr.Token); err != nil {
		t.Fatalf("LinkRequestSession failed: %v", err)
	}
	token, err := database.RequestTokenForSession("sess-1")
	if err != nil || token != fr.Token {
		t.Fatalf("RequestTokenForSession = %q, err %v", token, err)
	}
	if err := database.DeleteRequestSession("sess-1"); err != nil {
		t.Fatalf("DeleteRequestSession failed: %v", err)
	}
	if _, err := database.RequestTokenForSession("sess-1"); err == nil {
		t.Fatal("expected unlinked session lookup to fail")
	}
	if err := database.DeleteFileRequest(fr.ID); err != nil {
		t.Fatalf("DeleteFileRequest failed: %v", err)
	}
	if _, err := database.GetFileRequest(fr.ID); err == nil {
		t.Fatal("expected deleted request lookup to fail")
	}
}

func TestV32_Users(t *testing.T) {
	database := openV32TestDB(t)
	if _, err := database.CreateUser("ana", "hash", "superuser"); err == nil {
		t.Fatal("expected invalid role to fail")
	}
	user, err := database.CreateUser("ana", "hash-1", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateUser("ana", "hash-2", "viewer"); err == nil {
		t.Fatal("expected duplicate username to fail")
	}
	got, err := database.GetUserByUsername("ana")
	if err != nil || got.ID != user.ID || got.Role != "viewer" {
		t.Fatalf("GetUserByUsername = %+v, err %v", got, err)
	}
	if err := database.UpdateUserRole(user.ID, "admin"); err != nil {
		t.Fatalf("UpdateUserRole failed: %v", err)
	}
	if err := database.UpdateUserRole(user.ID, "nobody"); err == nil {
		t.Fatal("expected invalid role update to fail")
	}
	if err := database.UpdateUserPassword(user.ID, "hash-3"); err != nil {
		t.Fatalf("UpdateUserPassword failed: %v", err)
	}
	users, err := database.ListUsers()
	if err != nil || len(users) != 1 {
		t.Fatalf("ListUsers = %v, err %v", users, err)
	}
	if err := database.DeleteUser(user.ID); err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}
}

func TestV32_DiskFileRecord(t *testing.T) {
	database := openV32TestDB(t)
	file, err := database.CreateFileWithStorage(nil, "a.bin", 3, "application/octet-stream", "disk", "objects/x", "sha")
	if err != nil {
		t.Fatal(err)
	}
	if file.StorageKind != "disk" || file.LocalPath != "objects/x" {
		t.Fatalf("record = kind %q path %q", file.StorageKind, file.LocalPath)
	}
	if err := database.SetFileStorage(file.ID, "telegram", ""); err != nil {
		t.Fatalf("SetFileStorage failed: %v", err)
	}
	got, err := database.GetFile(file.ID)
	if err != nil || got.StorageKind != "telegram" {
		t.Fatalf("after SetFileStorage kind = %q, err %v", got.StorageKind, err)
	}
	// Telegram-path records created the old way still default to telegram.
	legacy, err := database.CreateFile(nil, "old.txt", 5, "text/plain", 2, "f2", "h2", "sha2")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.StorageKind != "telegram" {
		t.Fatalf("legacy record kind = %q, want telegram", legacy.StorageKind)
	}
}
