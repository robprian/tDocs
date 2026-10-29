package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"tdocs/internal/db"
)

// Local-disk storage (v3.2.0) is a per-file alternative to Telegram, not a
// global backend switch: switching the setting only affects NEW uploads.
// Existing files keep their storage_kind, so a Telegram catalog can never
// be orphaned by flipping the setting. Mirror/snapshots still cover
// Telegram files only; disk files live and die with this server's disk.

// storageBackendKind returns "telegram" (default) or "disk" for new uploads.
func (s *Server) storageBackendKind() string {
	if raw, err := s.db.GetSetting("storage_backend"); err == nil && raw == "disk" {
		return "disk"
	}
	return "telegram"
}

// diskRoot resolves the disk storage root: an explicit setting wins, then
// <db-dir>/files, then ./tdocs-files (tests use :memory: databases).
func (s *Server) diskRoot() string {
	if raw, err := s.db.GetSetting("disk_storage_path"); err == nil && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	if s.cfg != nil && s.cfg.DBPath != "" && s.cfg.DBPath != ":memory:" {
		return filepath.Join(filepath.Dir(s.cfg.DBPath), "files")
	}
	return "./tdocs-files"
}

// diskObjectAbs resolves a stored relative path (objects/<id>) to an
// absolute path, refusing anything that escapes the storage root.
func (s *Server) diskObjectAbs(rel string) (string, bool) {
	root, err := filepath.Abs(s.diskRoot())
	if err != nil {
		return "", false
	}
	clean := filepath.Clean("/" + rel)
	abs := filepath.Join(root, clean)
	if rel, err := filepath.Rel(root, abs); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return abs, true
}

func (s *Server) diskStagingPath(sessionID string) string {
	return filepath.Join(s.diskRoot(), ".staging", sessionID)
}

func (s *Server) diskObjectsDir() string {
	return filepath.Join(s.diskRoot(), "objects")
}

// serveDiskFile streams a disk-backed file, with Range support via
// http.ServeContent. Attachment adds a download disposition.
func (s *Server) serveDiskFile(w http.ResponseWriter, r *http.Request, file *db.File, attachment bool) {
	abs, ok := s.diskObjectAbs(file.LocalPath)
	if !ok {
		http.Error(w, "invalid local path", http.StatusInternalServerError)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		http.Error(w, "local file not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	if attachment {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.Name))
		w.Header().Set("Content-Type", "application/octet-stream")
	} else {
		w.Header().Set("Content-Type", file.MimeType)
		w.Header().Set("Accept-Ranges", "bytes")
	}
	http.ServeContent(w, r, file.Name, file.UpdatedAt, f)
}

// deleteDiskObject removes a disk object's bytes. Missing files are fine:
// the catalog row is already gone or going.
func (s *Server) deleteDiskObject(file *db.File) {
	if file.StorageKind != "disk" || file.LocalPath == "" {
		return
	}
	if abs, ok := s.diskObjectAbs(file.LocalPath); ok {
		_ = os.Remove(abs)
	}
}

// copyDiskObject duplicates a disk object's bytes to a new object path and
// returns that relative path.
func (s *Server) copyDiskObject(src *db.File, newID string) (string, error) {
	srcAbs, ok := s.diskObjectAbs(src.LocalPath)
	if !ok {
		return "", fmt.Errorf("invalid local path")
	}
	rel := filepath.Join("objects", newID)
	dstAbs, ok := s.diskObjectAbs(rel)
	if !ok {
		return "", fmt.Errorf("invalid local path")
	}
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		return "", err
	}
	in, err := os.Open(srcAbs)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.Create(dstAbs)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dstAbs)
		return "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dstAbs)
		return "", err
	}
	return rel, nil
}
