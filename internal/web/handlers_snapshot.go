package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"tdocs/internal/db"
	"tdocs/internal/telegram"
)

func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	// Snapshots live in the Telegram Storage Channel: without a verified
	// backend there is nothing to list, and reporting an empty list would
	// pretend the feature is working. Return the honest state instead.
	if !s.requireTelegram(w, r) {
		return
	}

	var snapshots []telegram.SnapshotInfo
	err := s.tg.Do(r.Context(), func(runCtx context.Context) error {
		if err := s.tg.EnsureStorageChannel(runCtx); err != nil {
			return err
		}
		var err error
		snapshots, err = s.tg.ListSnapshots(runCtx)
		return err
	})

	if err != nil {
		s.recordTelegramHealth(telegramUnavailableState(err), telegram.ClassifyHealth(err))
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "telegram_storage_unavailable",
			"state":   string(telegramUnavailableState(err)),
			"message": telegram.ClassifyHealth(err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if snapshots == nil {
		snapshots = []telegram.SnapshotInfo{}
	}
	_ = json.NewEncoder(w).Encode(snapshots)
}

func (s *Server) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.requireTelegram(w, r) {
		return
	}

	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()

	s.dbMu.RLock()
	gzPath, err := s.db.CreateSnapshot()
	s.dbMu.RUnlock()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create snapshot: %v", err), http.StatusInternalServerError)
		return
	}
	defer os.Remove(gzPath)

	var msgID int
	err = s.tg.Do(r.Context(), func(runCtx context.Context) error {
		if err := s.tg.EnsureStorageChannel(runCtx); err != nil {
			return err
		}
		var err error
		msgID, err = s.tg.UploadSnapshot(runCtx, gzPath, s.limiter)
		return err
	})

	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "telegram_storage_unavailable",
			"state":   string(telegramUnavailableState(err)),
			"message": telegram.ClassifyHealth(err),
		})
		return
	}

	s.audit(r, "snapshot_create", fmt.Sprintf("message=%d", msgID))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message_id": msgID,
		"message":    "Snapshot created, uploaded to Storage Channel, and retention applied",
	})
}

func (s *Server) handleRestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	msgID, err := strconv.Atoi(idStr)
	if err != nil || msgID <= 0 {
		http.Error(w, "Invalid snapshot ID", http.StatusBadRequest)
		return
	}

	if !s.requireTelegram(w, r) {
		return
	}

	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()

	s.dbMu.Lock()
	defer s.dbMu.Unlock()

	// Drain and close active SQLite handles
	if s.db != nil {
		_ = s.db.Close()
	}

	err = s.tg.Do(r.Context(), func(runCtx context.Context) error {
		if err := s.tg.EnsureStorageChannel(runCtx); err != nil {
			return err
		}
		return s.tg.RestoreSnapshotByID(runCtx, msgID, s.cfg.DBPath)
	})

	// Reopen database connection
	newDB, openErr := db.Open(s.cfg.DBPath)
	if openErr != nil {
		http.Error(w, fmt.Sprintf("Restore succeeded but failed to reopen DB: %v", openErr), http.StatusInternalServerError)
		return
	}
	s.db = newDB
	if s.tg != nil {
		s.tg.SetDB(newDB)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to restore snapshot: %v", err), http.StatusInternalServerError)
		return
	}

	s.audit(r, "snapshot_restore", fmt.Sprintf("message=%d", msgID))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": fmt.Sprintf("Database successfully restored from snapshot message %d", msgID),
	})
}

func (s *Server) handleDownloadSnapshot(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	msgID, err := strconv.Atoi(idStr)
	if err != nil || msgID <= 0 {
		http.Error(w, "Invalid snapshot ID", http.StatusBadRequest)
		return
	}

	if !s.requireTelegram(w, r) {
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"tdocs-snapshot-%d.db.gz\"", msgID))

	err = s.tg.Do(r.Context(), func(runCtx context.Context) error {
		if err := s.tg.EnsureStorageChannel(runCtx); err != nil {
			return err
		}
		return s.tg.DownloadSnapshotByID(runCtx, msgID, w)
	})

	if err != nil {
		// If headers were already written, http.Error won't do much, but log error
		fmt.Printf("Error downloading snapshot %d: %v\n", msgID, err)
	}
}

func (s *Server) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	msgID, err := strconv.Atoi(idStr)
	if err != nil || msgID <= 0 {
		http.Error(w, "Invalid snapshot ID", http.StatusBadRequest)
		return
	}

	if !s.requireTelegram(w, r) {
		return
	}

	err = s.tg.Do(r.Context(), func(runCtx context.Context) error {
		if err := s.tg.EnsureStorageChannel(runCtx); err != nil {
			return err
		}
		return s.tg.DeleteSnapshotByID(runCtx, msgID)
	})

	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "telegram_storage_unavailable",
			"state":   string(telegramUnavailableState(err)),
			"message": telegram.ClassifyHealth(err),
		})
		return
	}

	s.audit(r, "snapshot_delete", fmt.Sprintf("message=%d", msgID))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUploadRestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	// 50 MB max
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, "File too large or invalid multipart form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("snapshot")
	if err != nil {
		http.Error(w, "Missing 'snapshot' file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	tempGz := filepath.Join(os.TempDir(), fmt.Sprintf("uploaded-restore-%d.db.gz", os.Getpid()))
	defer os.Remove(tempGz)

	dst, err := os.Create(tempGz)
	if err != nil {
		http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		http.Error(w, "Failed to save uploaded snapshot", http.StatusInternalServerError)
		return
	}
	dst.Close()

	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()

	s.dbMu.Lock()
	defer s.dbMu.Unlock()

	if s.db != nil {
		_ = s.db.Close()
	}

	if err := db.RestoreFromGzip(tempGz, s.cfg.DBPath); err != nil {
		// Attempt recover
		s.db, _ = db.Open(s.cfg.DBPath)
		http.Error(w, fmt.Sprintf("Decompress and restore failed: %v", err), http.StatusInternalServerError)
		return
	}

	newDB, err := db.Open(s.cfg.DBPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to reopen restored database: %v", err), http.StatusInternalServerError)
		return
	}
	s.db = newDB
	if s.tg != nil {
		s.tg.SetDB(newDB)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "Local database snapshot restored successfully",
	})
}
