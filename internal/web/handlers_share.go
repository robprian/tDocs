package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	appcrypto "tdocs/internal/crypto"
	"tdocs/internal/db"
	"tdocs/internal/telegram"
)

func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FileID       string  `json:"file_id"`
		Password     *string `json:"password"`
		ExpiryDays   *int    `json:"expiry_days"`
		MaxDownloads *int    `json:"max_downloads"`
		PreviewOnly  bool    `json:"preview_only"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.FileID == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if _, err := s.db.GetFile(body.FileID); err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	var pwdHash *string
	if body.Password != nil && *body.Password != "" {
		if len(*body.Password) > 128 {
			http.Error(w, "password too long", http.StatusBadRequest)
			return
		}
		hash, err := appcrypto.HashPassword(*body.Password)
		if err != nil {
			http.Error(w, "hash password failed", http.StatusInternalServerError)
			return
		}
		pwdHash = &hash
	}

	var expiresAt *time.Time
	if body.ExpiryDays != nil && *body.ExpiryDays > 0 {
		if *body.ExpiryDays > 3650 {
			http.Error(w, "expiry too far in the future", http.StatusBadRequest)
			return
		}
		exp := time.Now().Add(time.Duration(*body.ExpiryDays) * 24 * time.Hour)
		expiresAt = &exp
	}

	var maxDownloads *int
	if body.MaxDownloads != nil && *body.MaxDownloads > 0 {
		maxDownloads = body.MaxDownloads
	}

	share, err := s.db.CreateShareLink(body.FileID, pwdHash, expiresAt, maxDownloads, body.PreviewOnly)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "share_create", body.FileID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	base := s.cdnBaseURL(r)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"token":        share.Token,
		"page_url":     base + "/s/" + share.Token,
		"stream_url":   base + "/s/" + share.Token + "/stream",
		"download_url": base + "/s/" + share.Token + "/download",
	})
}

func (s *Server) handleShareLanding(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	share, err := s.db.GetShareLink(token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if share.ExpiresAt != nil && time.Now().After(*share.ExpiresAt) {
		http.Error(w, "This share link has expired", http.StatusGone)
		return
	}

	file, err := s.db.GetFile(share.FileID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	needPassword := share.PasswordHash != nil && !s.isTokenUnlocked(r, token)

	data := map[string]any{
		"Token":         token,
		"FileName":      file.Name,
		"FileSize":      file.Size,
		"FormattedSize": formatBytes(file.Size),
		"MimeType":      file.MimeType,
		"NeedPassword":  needPassword,
		"PreviewOnly":   share.PreviewOnly,
		"IsVideo":       strings.HasPrefix(file.MimeType, "video/"),
		"IsAudio":       strings.HasPrefix(file.MimeType, "audio/"),
		"IsImage":       strings.HasPrefix(file.MimeType, "image/"),
		"IsPDF":         file.MimeType == "application/pdf",
		"IsText":        strings.HasPrefix(file.MimeType, "text/") || strings.HasSuffix(file.Name, ".json") || strings.HasSuffix(file.Name, ".md") || strings.HasSuffix(file.Name, ".txt"),
	}

	_ = s.templates.ExecuteTemplate(w, "share.html", data)
}

func (s *Server) handleShareUnlock(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	share, err := s.db.GetShareLink(token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	pwd := r.FormValue("password")
	if share.PasswordHash != nil {
		// Dictionary-attack guard: 5 failures/min per IP+token.
		if !s.ratelimit.Allow("unlock:"+clientIP(r)+":"+token, 5, time.Minute) {
			http.Error(w, "Too many attempts, try again later", http.StatusTooManyRequests)
			return
		}
		if !appcrypto.VerifyPassword(*share.PasswordHash, pwd) {
			file, _ := s.db.GetFile(share.FileID)
			_ = s.templates.ExecuteTemplate(w, "share.html", map[string]any{
				"Token":        token,
				"FileName":     file.Name,
				"NeedPassword": true,
				"Error":        "Incorrect password",
			})
			return
		}
	}

	s.setTokenUnlocked(w, token)
	http.Redirect(w, r, fmt.Sprintf("/s/%s", token), http.StatusSeeOther)
}
func (s *Server) handleShareStream(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	share, err := s.db.GetShareLink(token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if share.PasswordHash != nil && !s.isTokenUnlocked(r, token) {
		http.Error(w, "Password required", http.StatusForbidden)
		return
	}

	file, err := s.db.GetFile(share.FileID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.telegramReady(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	docID, docHash := parseDocCoords(file.TelegramFileID, file.TelegramAccessHash)

	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Accept-Ranges", "bytes")

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
				fmt.Printf("[share-stream] %s (%s): %v\n", file.Name, file.ID, derr)
				return derr
			}
			return nil
		})
		return
	}

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	w.WriteHeader(http.StatusOK)
	_ = s.tg.Do(r.Context(), func(runCtx context.Context) error {
		if derr := s.tg.DownloadFullByMessage(runCtx, file.TelegramMessageID, docID, docHash, w); derr != nil {
			fmt.Printf("[share-stream] %s (%s): %v\n", file.Name, file.ID, derr)
			return derr
		}
		return nil
	})
}

func (s *Server) handleShareDownload(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	share, err := s.db.GetShareLink(token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if share.PasswordHash != nil && !s.isTokenUnlocked(r, token) {
		http.Error(w, "Password required", http.StatusForbidden)
		return
	}
	if share.MaxDownloads != nil && share.DownloadCount >= *share.MaxDownloads {
		http.Error(w, "This share link has reached its download limit", http.StatusGone)
		return
	}
	if share.PreviewOnly {
		http.Error(w, "This share link is preview-only: downloading is disabled by the owner", http.StatusForbidden)
		return
	}

	file, err := s.db.GetFile(share.FileID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.telegramReady(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	_ = s.db.IncrementShareDownload(token)

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
			fmt.Printf("[share-download] %s (%s): %v\n", file.Name, file.ID, derr)
			return derr
		}
		return nil
	})
}

func (s *Server) isTokenUnlocked(r *http.Request, token string) bool {
	cookie, err := r.Cookie(fmt.Sprintf("td_unlocked_%s", token))
	return err == nil && cookie.Value == "1"
}

func (s *Server) setTokenUnlocked(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     fmt.Sprintf("td_unlocked_%s", token),
		Value:    "1",
		Path:     fmt.Sprintf("/s/%s", token),
		HttpOnly: true,
		MaxAge:   86400, // 24 hours
	})
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	shares, err := s.db.ListShareLinks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if shares == nil {
		shares = []db.ShareLinkInfo{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(shares)
}

func (s *Server) handleDeleteShare(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing share id", http.StatusBadRequest)
		return
	}
	if err := s.db.DeleteShareLink(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "share_revoke", id)
	w.WriteHeader(http.StatusNoContent)
}
