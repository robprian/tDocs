package db

import (
	"path/filepath"
	"testing"
)

func openFeatureDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(filepath.Join(t.TempDir(), "feat.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestFeatures_Favorites(t *testing.T) {
	database := openFeatureDB(t)
	f, err := database.CreateFile(nil, "star.mp4", 100, "video/mp4", 1, "1", "1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AddFavorite(f.ID); err != nil {
		t.Fatal(err)
	}
	ok, _ := database.IsFavorite(f.ID)
	if !ok {
		t.Fatalf("expected favorite")
	}
	list, err := database.ListFavoriteFiles()
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 favorite, got %d (%v)", len(list), err)
	}
	if err := database.RemoveFavorite(f.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := database.IsFavorite(f.ID); ok {
		t.Fatalf("expected unfavorited")
	}
}

func TestFeatures_TrashRestorePurge(t *testing.T) {
	database := openFeatureDB(t)
	root, _ := database.CreateFolder("R", nil)
	sub, _ := database.CreateFolder("S", &root.ID)
	f1, _ := database.CreateFile(&sub.ID, "a.txt", 10, "text/plain", 1, "1", "1", "")
	f2, _ := database.CreateFile(nil, "b.txt", 20, "text/plain", 2, "2", "2", "")

	// Trash single file: hidden from lists, visible in trash.
	if err := database.TrashFile(f2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetFile(f2.ID); err == nil {
		t.Fatalf("trashed file must be hidden from GetFile")
	}
	trash, err := database.ListTrashedFiles()
	if err != nil || len(trash) != 1 {
		t.Fatalf("expected 1 trashed file, got %d", len(trash))
	}
	if err := database.RestoreFile(f2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetFile(f2.ID); err != nil {
		t.Fatalf("restored file must be visible: %v", err)
	}

	// Trash folder cascades to subtree files.
	if err := database.TrashFolder(root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetFolder(root.ID); err == nil {
		t.Fatalf("trashed folder must be hidden")
	}
	if _, err := database.GetFile(f1.ID); err == nil {
		t.Fatalf("file inside trashed folder must be hidden")
	}
	if err := database.RestoreFolder(root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetFile(f1.ID); err != nil {
		t.Fatalf("file must reappear after folder restore: %v", err)
	}

	// Permanent delete removes the row.
	if err := database.DeleteFile(f1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetFileAny(f1.ID); err == nil {
		t.Fatalf("purged file must be gone")
	}
}

func TestFeatures_Versions(t *testing.T) {
	database := openFeatureDB(t)
	f, _ := database.CreateFile(nil, "v.txt", 10, "text/plain", 1, "doc1", "h1", "sha1")
	v, err := database.ArchiveVersion(f)
	if err != nil {
		t.Fatal(err)
	}
	if v.TelegramFileID != "doc1" {
		t.Fatalf("version must snapshot coordinates, got %+v", v)
	}
	if err := database.UpdateFileCoords(f.ID, "v.txt", 20, "text/plain", 2, "doc2", "h2", "sha2"); err != nil {
		t.Fatal(err)
	}
	cur, _ := database.GetFile(f.ID)
	if cur.TelegramFileID != "doc2" || cur.Size != 20 {
		t.Fatalf("coords swap failed: %+v", cur)
	}
	vs, _ := database.ListVersions(f.ID)
	if len(vs) != 1 {
		t.Fatalf("expected 1 version, got %d", len(vs))
	}
	if err := database.DeleteVersion(v.ID); err != nil {
		t.Fatal(err)
	}
	if vs, _ := database.ListVersions(f.ID); len(vs) != 0 {
		t.Fatalf("expected 0 versions after delete")
	}
}

func TestFeatures_Tokens(t *testing.T) {
	database := openFeatureDB(t)
	id, prefix, secret, err := database.NewAPIToken("ci")
	if err != nil {
		t.Fatal(err)
	}
	if len(prefix) == 0 || len(secret) < 40 {
		t.Fatalf("weak token material: %q", secret)
	}
	ok, _ := database.VerifyAPIToken(secret)
	if !ok {
		t.Fatalf("fresh token must verify")
	}
	if ok, _ := database.VerifyAPIToken(secret + "x"); ok {
		t.Fatalf("tampered token must not verify")
	}
	// Secret must be stored hashed, never plaintext.
	var stored string
	_ = database.QueryRow("SELECT token_hash FROM api_tokens WHERE id = ?", id).Scan(&stored)
	if stored == secret || stored == "" {
		t.Fatalf("token stored in recoverable form")
	}
	if err := database.RevokeAPIToken(id); err != nil {
		t.Fatal(err)
	}
	if ok, _ := database.VerifyAPIToken(secret); ok {
		t.Fatalf("revoked token must not verify")
	}
}

func TestFeatures_AuditAndSyncRuns(t *testing.T) {
	database := openFeatureDB(t)
	if err := database.Audit("admin", "login", "d", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	entries, err := database.ListAudit("", 10, 0)
	if err != nil || len(entries) != 1 || entries[0].Action != "login" {
		t.Fatalf("audit list broken: %v %+v", err, entries)
	}
	runID, err := database.StartSyncRun("manual")
	if err != nil || runID == 0 {
		t.Fatal(err)
	}
	if err := database.FinishSyncRun(runID, 5, 2, 3, ""); err != nil {
		t.Fatal(err)
	}
	runs, err := database.ListSyncRuns(10)
	if err != nil || len(runs) != 1 || runs[0].Scanned != 5 || runs[0].Trigger != "manual" {
		t.Fatalf("sync runs broken: %v %+v", err, runs)
	}
}

func TestFeatures_DuplicatesAndStats(t *testing.T) {
	database := openFeatureDB(t)
	_, _ = database.CreateFile(nil, "a.bin", 100, "application/octet-stream", 1, "1", "1", "deadbeef")
	_, _ = database.CreateFile(nil, "b.bin", 200, "application/octet-stream", 2, "2", "2", "deadbeef")
	_, _ = database.CreateFile(nil, "c.jpg", 50, "image/jpeg", 3, "3", "3", "unique")
	groups, err := database.ListDuplicateGroups()
	if err != nil || len(groups) != 1 || groups[0].Count != 2 || groups[0].Bytes != 300 {
		t.Fatalf("duplicate groups broken: %v %+v", err, groups)
	}
	same, _ := database.ListFilesBySHA("deadbeef")
	if len(same) != 2 {
		t.Fatalf("expected 2 files by sha, got %d", len(same))
	}
	total, _ := database.TotalBytes()
	if total != 350 {
		t.Fatalf("expected 350 total bytes, got %d", total)
	}
	stats, _ := database.MimeBreakdown()
	cats := map[string]int{}
	for _, m := range stats {
		cats[m.Category] = m.Files
	}
	if cats["image"] != 1 || cats["other"] != 2 {
		t.Fatalf("mime breakdown broken: %+v", cats)
	}
	recent, _ := database.RecentFiles(2)
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent files, got %d", len(recent))
	}
	if _, err := database.FindByNameInFolder(nil, "a.bin"); err != nil {
		t.Fatalf("FindByNameInFolder must locate live file: %v", err)
	}
}
