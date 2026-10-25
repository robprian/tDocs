package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tdocs/internal/telegram"
)

// cdnFile is the public catalog DTO for external consumers (e.g. a film site).
// Telegram object coordinates are intentionally never exposed.
type cdnFile struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	MimeType    string    `json:"mime_type"`
	FolderID    *string   `json:"folder_id"`
	CreatedAt   time.Time `json:"created_at"`
	StreamURL   string    `json:"stream_url"`
	DownloadURL string    `json:"download_url"`
}

func (s *Server) cdnBaseURL(r *http.Request) string {
	if s.cfg.CDNBaseURL != "" {
		return s.cfg.CDNBaseURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// Honor reverse-proxy headers so stream_url/download_url are reachable
	// from the client's network, not the proxy's internal address.
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = strings.Split(proto, ",")[0]
		scheme = strings.TrimSpace(scheme)
	}
	host := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
	if host == "" {
		host = r.Host
	}
	if host == "" {
		host = "localhost:" + s.cfg.Port
	}
	return scheme + "://" + host
}

func (s *Server) toCDNFile(r *http.Request, id, name string, size int64, mime string, folderID *string, created time.Time) cdnFile {
	base := s.cdnBaseURL(r)
	return cdnFile{
		ID:          id,
		Name:        name,
		Size:        size,
		MimeType:    mime,
		FolderID:    folderID,
		CreatedAt:   created,
		StreamURL:   base + "/cdn/" + id + "/stream",
		DownloadURL: base + "/cdn/" + id + "/download",
	}
}

// apiBearerOK accepts the dashboard session cookie, the admin password, or a
// live API token via "Authorization: Bearer ..." (or ?api_key=).
func (s *Server) apiBearerOK(r *http.Request) bool {
	if _, ok := s.cookieSession(r); ok {
		return true
	}
	return s.bearerTokenOK(r)
}

func (s *Server) requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		// Browser <fetch> with Authorization triggers a preflight that
		// carries no credentials; answer it before the auth check.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !s.apiBearerOK(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Range, Content-Type, Authorization")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges, ETag")
}

// handleCDNList serves the machine-readable catalog for external websites.
// GET /api/cdn/files?search=&mime=video/&limit=100
func (s *Server) handleCDNList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	files, err := s.db.ListAllFiles(r.URL.Query().Get("search"), r.URL.Query().Get("mime"), limit)
	if err != nil {
		http.Error(w, "catalog query failed", http.StatusInternalServerError)
		return
	}
	out := make([]cdnFile, 0, len(files))
	for _, f := range files {
		out = append(out, s.toCDNFile(r, f.ID, f.Name, f.Size, f.MimeType, f.FolderID, f.CreatedAt))
	}
	setCORS(w)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"files": out, "total": len(out)})
}

// handleCDNDetail serves one catalog entry.
// GET /api/cdn/files/{id}
func (s *Server) handleCDNDetail(w http.ResponseWriter, r *http.Request) {
	file, err := s.db.GetFile(r.PathValue("id"))
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	setCORS(w)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.toCDNFile(r, file.ID, file.Name, file.Size, file.MimeType, file.FolderID, file.CreatedAt))
}

// handleCDNStream is the embeddable <video>/<audio>/<img> URL with Range
// seeking, ETag caching and open CORS. Public by default (TELEDRIVE_CDN_PUBLIC);
// set it to "false" to require the admin Bearer key instead.
func (s *Server) handleCDNStream(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.cfg.CDNPublic && !s.apiBearerOK(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	file, err := s.db.GetFile(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	etag := fmt.Sprintf(`"%s-%d"`, file.ID, file.UpdatedAt.Unix())
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	docID, docHash := parseDocCoords(file.TelegramFileID, file.TelegramAccessHash)

	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "public, max-age=86400")

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := s.telegramReady(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	rangeHeader := r.Header.Get("Range")
	start, end, hasRange, err := telegram.ParseRange(rangeHeader, file.Size)
	if err != nil {
		http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if hasRange {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, file.Size))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_ = s.tg.Do(r.Context(), func(runCtx context.Context) error {
			if derr := s.tg.DownloadRangeByMessage(runCtx, file.TelegramMessageID, docID, docHash, start, end, w, s.limiter); derr != nil {
				fmt.Printf("[cdn-stream] %s (%s): %v\n", file.Name, file.ID, derr)
				return derr
			}
			return nil
		})
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	w.WriteHeader(http.StatusOK)
	_ = s.tg.Do(r.Context(), func(runCtx context.Context) error {
		if derr := s.tg.DownloadFullByMessage(runCtx, file.TelegramMessageID, docID, docHash, w); derr != nil {
			fmt.Printf("[cdn-stream] %s (%s): %v\n", file.Name, file.ID, derr)
			return derr
		}
		return nil
	})
}

// handleCDNDownload forces a save-as download of the same CDN object.
func (s *Server) handleCDNDownload(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.cfg.CDNPublic && !s.apiBearerOK(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	file, err := s.db.GetFile(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
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
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = s.tg.Do(r.Context(), func(runCtx context.Context) error {
		if derr := s.tg.DownloadFullByMessage(runCtx, file.TelegramMessageID, docID, docHash, w); derr != nil {
			fmt.Printf("[cdn-download] %s (%s): %v\n", file.Name, file.ID, derr)
			return derr
		}
		return nil
	})
}
