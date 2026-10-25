package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"tdocs/internal/db"
	"tdocs/internal/telegram"
)

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	var parentID *string
	if p := r.URL.Query().Get("folder_id"); p != "" {
		parentID = &p
	}

	folders, err := s.db.ListFolders(parentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Clients expect an array; never encode null.
	if folders == nil {
		folders = []db.Folder{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(folders)
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string  `json:"name"`
		ParentID *string `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "invalid folder data", http.StatusBadRequest)
		return
	}
	name, err := validName(body.Name, 128)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	folder, err := s.db.CreateFolder(name, body.ParentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(folder)
}

func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name     *string `json:"name"`
		ParentID *string `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if body.Name != nil && *body.Name != "" {
		name, verr := validName(*body.Name, 128)
		if verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
		if err := s.db.RenameFolder(id, name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if body.ParentID != nil {
		if err := s.db.MoveFolder(id, body.ParentID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.db.GetFolderAny(id); err != nil {
		http.Error(w, "folder not found", http.StatusNotFound)
		return
	}
	// Soft delete: recoverable from Trash. Permanent purge lives under /api/trash.
	if err := s.db.TrashFolder(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "folder_trash", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := q.Get("search")
	mime := q.Get("mime")
	minSize, _ := strconv.ParseInt(q.Get("min_size"), 10, 64)
	maxSize, _ := strconv.ParseInt(q.Get("max_size"), 10, 64)
	since := q.Get("since") // YYYY-MM-DD prefix filter on updated_at
	if search != "" || mime != "" || minSize > 0 || maxSize > 0 || since != "" {
		if since != "" && len(since) == 10 {
			since += " 00:00:00"
		}
		files, err := s.db.SearchFilesFiltered(search, mime, minSize, maxSize, since)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if files == nil {
			files = []db.File{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(files)
		return
	}

	var folderID *string
	if f := r.URL.Query().Get("folder_id"); f != "" {
		folderID = &f
	}

	files, err := s.db.ListFiles(folderID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if files == nil {
		files = []db.File{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(files)
}

// handleGetFile returns one file record for API clients.
// GET /api/files/{id}
func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	file, err := s.db.GetFile(r.PathValue("id"))
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(file)
}

func (s *Server) handleUpdateFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name     *string `json:"name"`
		FolderID *string `json:"folder_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if body.Name != nil && *body.Name != "" {
		name, verr := validName(*body.Name, 255)
		if verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}
		if err := s.db.RenameFile(id, name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if body.FolderID != nil {
		if err := s.db.MoveFile(id, body.FolderID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.db.GetFile(id); err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	// Soft delete: recoverable from Trash. Permanent purge lives under /api/trash.
	if err := s.db.TrashFile(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "file_trash", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFileStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	file, err := s.db.GetFile(id)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if err := s.telegramReady(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	docID, docHash := parseDocCoords(file.TelegramFileID, file.TelegramAccessHash)

	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Accept-Ranges", "bytes")
	// A stable validator per file revision lets the browser reuse already
	// fetched byte ranges (seeking, replay) instead of re-requesting them.
	// It is a weak validator because the bytes may be re-fetched from the
	// backend; "private" because the response is behind authentication.
	etag := fmt.Sprintf(`W/"%s-%d-%d"`, file.ID, file.Size, file.UpdatedAt.Unix())
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) && r.Header.Get("Range") == "" {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	rangeHeader := r.Header.Get("Range")
	start, end, hasRange, err := telegram.ParseRange(rangeHeader, file.Size)
	if err != nil {
		http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// Coordinates are resolved fresh from the channel message so recovered or
	// long-stored files keep working after reference rotation. Headers are
	// sent first; a mid-stream Telegram error can only abort the body, so it
	// is logged server-side for diagnosis via ./manage.sh logs.
	if hasRange {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, file.Size))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		// Wrap response writer in bufio so partial writes don't get truncated
		// by the underlying chunked writer, then flush in defer.
		bw := bufio.NewWriter(w)
		derr := s.tg.Do(r.Context(), func(runCtx context.Context) error {
			return s.tg.DownloadRangeByMessage(runCtx, file.TelegramMessageID, docID, docHash, start, end, bw, s.limiter)
		})
		if fErr := bw.Flush(); fErr != nil && derr == nil {
			derr = fErr
		}
		if derr != nil {
			fmt.Printf("[stream] %s (%s): %v\n", file.Name, file.ID, derr)
		}
		return
	}

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	w.WriteHeader(http.StatusOK)
	bw := bufio.NewWriter(w)
	derr := s.tg.Do(r.Context(), func(runCtx context.Context) error {
		return s.tg.DownloadFullByMessage(runCtx, file.TelegramMessageID, docID, docHash, bw)
	})
	if fErr := bw.Flush(); fErr != nil && derr == nil {
		derr = fErr
	}
	if derr != nil {
		fmt.Printf("[stream] %s (%s): %v\n", file.Name, file.ID, derr)
	}
}

func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	file, err := s.db.GetFile(id)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if err := s.telegramReady(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	docID, docHash := parseDocCoords(file.TelegramFileID, file.TelegramAccessHash)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.Name))
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = s.tg.Do(r.Context(), func(runCtx context.Context) error {
		if derr := s.tg.DownloadFullByMessage(runCtx, file.TelegramMessageID, docID, docHash, w); derr != nil {
			fmt.Printf("[download] %s (%s): %v\n", file.Name, file.ID, derr)
			return derr
		}
		return nil
	})
}

// parseDocCoords decodes the stored Telegram document coordinates. Unparseable
// values yield 0, which the ByMessage download helpers treat as "no fallback".
func parseDocCoords(fileID, accessHash string) (int64, int64) {
	docID, _ := strconv.ParseInt(fileID, 10, 64)
	docHash, _ := strconv.ParseInt(accessHash, 10, 64)
	return docID, docHash
}
