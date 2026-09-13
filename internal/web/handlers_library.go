package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"tdocs/internal/db"
)

// Library APIs: favorites, trash, versions, bulk ops, duplicates, stats.

// ------------------------------------------------------------------ helpers ---

func (s *Server) audit(r *http.Request, action, detail string) {
	_ = s.db.Audit("admin", action, detail, clientIP(r))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------- favorites ---

func (s *Server) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.db.GetFile(id); err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if err := s.db.AddFavorite(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "favorite_add", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.db.RemoveFavorite(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "favorite_remove", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	files, err := s.db.ListFavoriteFiles()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if files == nil {
		files = []db.File{}
	}
	writeJSON(w, http.StatusOK, files)
}

// -------------------------------------------------------------------- trash ---

func (s *Server) handleListTrash(w http.ResponseWriter, r *http.Request) {
	files, err := s.db.ListTrashedFiles()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	folders, err := s.db.ListTrashedFolders()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if files == nil {
		files = []db.TrashedFile{}
	}
	if folders == nil {
		folders = []db.Folder{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "folders": folders})
}

func (s *Server) handleRestoreTrash(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, "invalid payload: need {kind: file|folder, id}", http.StatusBadRequest)
		return
	}
	switch body.Kind {
	case "file":
		if _, err := s.db.GetFileAny(body.ID); err != nil {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		if err := s.db.RestoreFile(body.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "folder":
		if _, err := s.db.GetFolderAny(body.ID); err != nil {
			http.Error(w, "folder not found", http.StatusNotFound)
			return
		}
		if err := s.db.RestoreFolder(body.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "kind must be file or folder", http.StatusBadRequest)
		return
	}
	s.audit(r, "trash_restore", body.Kind+":"+body.ID)
	w.WriteHeader(http.StatusOK)
}

// purgeFile permanently deletes a file record, its versions, and its Telegram
// message (best-effort).
func (s *Server) purgeFile(r *http.Request, id string) error {
	f, err := s.db.GetFileAny(id)
	if err != nil {
		return fmt.Errorf("file not found")
	}
	if f.TelegramMessageID > 0 && s.tg != nil {
		_ = s.tg.Do(r.Context(), func(runCtx context.Context) error {
			if derr := s.tg.DeleteChannelMessage(runCtx, f.TelegramMessageID); derr != nil {
				fmt.Printf("[purge] telegram delete msg %d: %v\n", f.TelegramMessageID, derr)
			}
			return nil
		})
	}
	if vs, _ := s.db.ListVersions(id); len(vs) > 0 {
		for _, v := range vs {
			_ = s.db.DeleteVersion(v.ID)
		}
	}
	return s.db.DeleteFile(id)
}

func (s *Server) handlePurgeTrash(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	id := r.PathValue("id")
	switch kind {
	case "file":
		if err := s.purgeFile(r, id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
	case "folder":
		folders, _ := s.db.DescendantFolderIDs(id)
		for _, fid := range folders {
			files, _ := s.db.ListFiles(&fid)
			for _, f := range files {
				_ = s.purgeFile(r, f.ID)
			}
		}
		if err := s.db.DeleteFolder(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "kind must be file or folder", http.StatusBadRequest)
		return
	}
	s.audit(r, "trash_purge", kind+":"+id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	trashed, _ := s.db.ListTrashedFiles()
	for _, f := range trashed {
		_ = s.purgeFile(r, f.ID)
	}
	folders, _ := s.db.ListTrashedFolders()
	for _, fo := range folders {
		_ = s.db.DeleteFolder(fo.ID)
	}
	_, _, _ = s.db.EmptyTrash()
	s.audit(r, "trash_empty", fmt.Sprintf("purged files=%d folders=%d", len(trashed), len(folders)))
	w.WriteHeader(http.StatusNoContent)
}

// ----------------------------------------------------------------- versions ---

func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.db.GetFile(id); err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	vs, err := s.db.ListVersions(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if vs == nil {
		vs = []db.FileVersion{}
	}
	writeJSON(w, http.StatusOK, vs)
}

func (s *Server) handleRestoreVersion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	vid := r.PathValue("vid")
	cur, err := s.db.GetFile(id)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	v, err := s.db.GetVersion(vid)
	if err != nil || v.FileID != id {
		http.Error(w, "version not found", http.StatusNotFound)
		return
	}
	// Push current coordinates down as a version, then promote the snapshot.
	if _, err := s.db.ArchiveVersion(cur); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.db.UpdateFileCoords(id, v.Name, v.Size, v.MimeType, v.TelegramMessageID, v.TelegramFileID, v.TelegramAccessHash, v.SHA256); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.db.DeleteVersion(vid)
	s.audit(r, "version_restore", id+":"+vid)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeleteVersion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	vid := r.PathValue("vid")
	v, err := s.db.GetVersion(vid)
	if err != nil || v.FileID != id {
		http.Error(w, "version not found", http.StatusNotFound)
		return
	}
	if v.TelegramMessageID > 0 && s.tg != nil {
		_ = s.tg.Do(r.Context(), func(runCtx context.Context) error {
			return s.tg.DeleteChannelMessage(runCtx, v.TelegramMessageID)
		})
	}
	_ = s.db.DeleteVersion(vid)
	s.audit(r, "version_delete", id+":"+vid)
	w.WriteHeader(http.StatusNoContent)
}

// --------------------------------------------------------------------- bulk ---

func (s *Server) handleBulkFiles(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action   string   `json:"action"`
		IDs      []string `json:"ids"`
		FolderID *string  `json:"folder_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		http.Error(w, "invalid payload: need {action, ids[]}", http.StatusBadRequest)
		return
	}
	if len(body.IDs) > 500 {
		http.Error(w, "too many ids (max 500)", http.StatusBadRequest)
		return
	}
	done, failed := 0, 0
	switch body.Action {
	case "trash":
		for _, id := range body.IDs {
			if err := s.db.TrashFile(id); err != nil {
				failed++
			} else {
				done++
			}
		}
	case "restore":
		for _, id := range body.IDs {
			if err := s.db.RestoreFile(id); err != nil {
				failed++
			} else {
				done++
			}
		}
	case "purge":
		for _, id := range body.IDs {
			if err := s.purgeFile(r, id); err != nil {
				failed++
			} else {
				done++
			}
		}
	case "move":
		if body.FolderID != nil {
			if _, err := s.db.GetFolder(*body.FolderID); err != nil {
				http.Error(w, "target folder not found", http.StatusNotFound)
				return
			}
		}
		for _, id := range body.IDs {
			if err := s.db.MoveFile(id, body.FolderID); err != nil {
				failed++
			} else {
				done++
			}
		}
	case "favorite":
		for _, id := range body.IDs {
			if err := s.db.AddFavorite(id); err != nil {
				failed++
			} else {
				done++
			}
		}
	default:
		http.Error(w, "action must be trash|restore|purge|move|favorite", http.StatusBadRequest)
		return
	}
	s.audit(r, "bulk_"+body.Action, fmt.Sprintf("done=%d failed=%d", done, failed))
	writeJSON(w, http.StatusOK, map[string]any{"done": done, "failed": failed})
}

// --------------------------------------------------------------- duplicates ---

func (s *Server) handleListDuplicates(w http.ResponseWriter, r *http.Request) {
	if sha := r.URL.Query().Get("sha"); sha != "" {
		files, err := s.db.ListFilesBySHA(sha)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if files == nil {
			files = []db.File{}
		}
		writeJSON(w, http.StatusOK, files)
		return
	}
	groups, err := s.db.ListDuplicateGroups()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if groups == nil {
		groups = []db.DuplicateGroup{}
	}
	writeJSON(w, http.StatusOK, groups)
}

// -------------------------------------------------------------------- stats ---

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 8
	}
	files, _ := s.db.CountFiles()
	folders, _ := s.db.CountFolders()
	bytes, _ := s.db.TotalBytes()
	breakdown, _ := s.db.MimeBreakdown()
	recent, _ := s.db.RecentFiles(limit)
	largest, _ := s.db.LargestFiles(5)
	trashed, _ := s.db.CountTrashedFiles()
	versions, _ := s.db.CountVersions()
	favs, _ := s.db.ListFavoriteFiles()
	if breakdown == nil {
		breakdown = []db.MimeStat{}
	}
	if recent == nil {
		recent = []db.File{}
	}
	if largest == nil {
		largest = []db.File{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"files": files, "folders": folders, "bytes": bytes,
		"breakdown": breakdown, "recent": recent, "largest": largest,
		"trashed_files": trashed, "versions": versions, "favorites": len(favs),
	})
}

// -------------------------------------------------------------------- copy ---

// handleCopyFile duplicates a catalog record pointing at the same Telegram
// object (no re-upload). Useful for "Save a copy" flows and versioning.
func (s *Server) handleCopyFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	src, err := s.db.GetFile(id)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	var body struct {
		FolderID *string `json:"folder_id"`
		Name     *string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	name := src.Name + " (copy)"
	if body.Name != nil && strings.TrimSpace(*body.Name) != "" {
		name = strings.TrimSpace(*body.Name)
	}
	name, verr := validName(name, 255)
	if verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	folderID := src.FolderID
	if body.FolderID != nil {
		if *body.FolderID == "" {
			folderID = nil
		} else {
			if _, err := s.db.GetFolder(*body.FolderID); err != nil {
				http.Error(w, "target folder not found", http.StatusNotFound)
				return
			}
			folderID = body.FolderID
		}
	}
	cp, err := s.db.CreateFile(folderID, name, src.Size, src.MimeType,
		src.TelegramMessageID, src.TelegramFileID, src.TelegramAccessHash, src.SHA256)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "file_copy", id+" -> "+cp.ID)
	writeJSON(w, http.StatusCreated, cp)
}

// ------------------------------------------------------------- folder stats ---

func (s *Server) handleFolderStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.FolderStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if stats == nil {
		stats = []db.FolderStat{}
	}
	writeJSON(w, http.StatusOK, stats)
}

// ---------------------------------------------------------------- file meta ---

func (s *Server) handleFileMeta(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	file, err := s.db.GetFile(id)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	fav, _ := s.db.IsFavorite(id)
	versions, _ := s.db.ListVersions(id)
	shares, _ := s.db.ListShareLinks()
	shareCount := 0
	for _, sh := range shares {
		if sh.FileID == id {
			shareCount++
		}
	}
	dupCount := 0
	if file.SHA256 != "" {
		if same, _ := s.db.ListFilesBySHA(file.SHA256); len(same) > 1 {
			dupCount = len(same) - 1
		}
	}
	if versions == nil {
		versions = []db.FileVersion{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"file": file, "favorite": fav,
		"versions": versions, "shares": shareCount, "duplicates": dupCount,
	})
}

// validName enforces safe object names: trimmed, bounded, no path tricks.
func validName(name string, maxLen int) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name must not be empty")
	}
	if len(name) > maxLen {
		return "", fmt.Errorf("name too long (max %d characters)", maxLen)
	}
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return "", fmt.Errorf("name must not contain path separators")
	}
	for _, c := range name {
		if c < 0x20 || c == 0x7f {
			return "", fmt.Errorf("name contains control characters")
		}
	}
	return name, nil
}
