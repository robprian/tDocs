package web

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"html/template"

	"tdocs/internal/crypto"
	"tdocs/internal/db"
	"tdocs/internal/telegram"
)

// v3.2.0 handlers: tags, file requests (admin + public upload), folder ZIP,
// send-to-Telegram-chat, mirror schedule, dashboard users, storage settings.

// ------------------------------------------------------------------ tags --

func (s *Server) handleListFileTags(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.db.GetFile(id); err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	tags, err := s.db.ListFileTags(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) handleAddFileTag(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Tag string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid payload: need {tag}", http.StatusBadRequest)
		return
	}
	if err := s.db.AddTag(id, body.Tag); err != nil {
		if strings.Contains(err.Error(), "invalid tag") {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	tags, _ := s.db.ListFileTags(id)
	writeJSON(w, http.StatusCreated, tags)
}

func (s *Server) handleRemoveFileTag(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tag := r.PathValue("tag")
	if _, err := s.db.GetFile(id); err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if err := s.db.RemoveTag(id, tag); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListAllTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.db.ListAllTags()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tags == nil {
		tags = []db.TagCount{}
	}
	writeJSON(w, http.StatusOK, tags)
}

// ---------------------------------------------------------- file requests --

func (s *Server) handleCreateFileRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FolderID  *string `json:"folder_id"`
		Name      string  `json:"name"`
		ExpiresAt *string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		http.Error(w, "invalid payload: need {name, folder_id?, expires_at?}", http.StatusBadRequest)
		return
	}
	name, err := validName(body.Name, 128)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.FolderID != nil && strings.TrimSpace(*body.FolderID) == "" {
		body.FolderID = nil
	}
	var expiresAt *time.Time
	if body.ExpiresAt != nil && strings.TrimSpace(*body.ExpiresAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*body.ExpiresAt))
		if err != nil {
			http.Error(w, "expires_at must be RFC3339", http.StatusBadRequest)
			return
		}
		expiresAt = &t
	}
	fr, err := s.db.CreateFileRequest(body.FolderID, name, expiresAt)
	if err != nil {
		if strings.Contains(err.Error(), "target folder not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "file_request_create", fr.Name)
	writeJSON(w, http.StatusCreated, fr)
}

func (s *Server) handleListFileRequests(w http.ResponseWriter, r *http.Request) {
	frs, err := s.db.ListFileRequests()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if frs == nil {
		frs = []db.FileRequest{}
	}
	writeJSON(w, http.StatusOK, frs)
}

func (s *Server) handleDeleteFileRequest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.db.GetFileRequest(id); err != nil {
		http.Error(w, "file request not found", http.StatusNotFound)
		return
	}
	if err := s.db.DeleteFileRequest(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "file_request_delete", id)
	w.WriteHeader(http.StatusNoContent)
}

// validateRequestToken loads a request by its public token. Expired
// requests answer 410 so upload clients can tell expiry from a bad link.
func (s *Server) validateRequestToken(w http.ResponseWriter, r *http.Request) (*db.FileRequest, bool) {
	token := r.PathValue("token")
	fr, err := s.db.GetFileRequestByToken(token)
	if err != nil {
		http.Error(w, "file request not found", http.StatusNotFound)
		return nil, false
	}
	if fr.Expired() {
		http.Error(w, "file request expired", http.StatusGone)
		return nil, false
	}
	return fr, true
}

func (s *Server) handlePublicFileRequest(w http.ResponseWriter, r *http.Request) {
	fr, ok := s.validateRequestToken(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":       fr.Name,
		"expires_at": fr.ExpiresAt,
		"expired":    false,
	})
}

func (s *Server) handleRequestUploadInit(w http.ResponseWriter, r *http.Request) {
	fr, ok := s.validateRequestToken(w, r)
	if !ok {
		return
	}
	var body struct {
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		MimeType string `json:"mime_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" || body.Size <= 0 {
		http.Error(w, "invalid upload init payload: need {name, size>0, mime_type?}", http.StatusBadRequest)
		return
	}
	if body.Size > 4<<30 {
		http.Error(w, "file too large: max 4 GB per file (Telegram Premium limit)", http.StatusBadRequest)
		return
	}
	if body.MimeType == "" {
		body.MimeType = "application/octet-stream"
	}
	name, err := validName(body.Name, 255)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.storageBackendKind() != "disk" {
		if err := s.telegramPreflight(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	session, err := s.newUploadSession(fr.FolderID, strings.TrimSpace(name), body.Size, body.MimeType)
	if err != nil {
		if strings.Contains(err.Error(), "target folder not found") {
			http.Error(w, "target folder no longer exists", http.StatusGone)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.db.LinkRequestSession(session.ID, fr.Token); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           session.ID,
		"session_id":   session.ID,
		"chunk_size":   ClientChunkSize,
		"storage_kind": session.StorageKind,
		"total_chunks": int((body.Size + ClientChunkSize - 1) / ClientChunkSize),
	})
}

// readUploadChunkForm parses one chunked-upload multipart body. It returns
// an HTTP status to answer with when err != nil.
func readUploadChunkForm(w http.ResponseWriter, r *http.Request) (sessionID string, chunkIndex int, chunkData []byte, status int, err error) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxClientChunk+(1<<20))
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		return "", 0, nil, http.StatusBadRequest, fmt.Errorf("parse multipart form error: chunk must be <= 5 MB (field `chunk`)")
	}
	sessionID = strings.TrimSpace(r.FormValue("session_id"))
	chunkIndex, err = strconv.Atoi(strings.TrimSpace(r.FormValue("chunk_index")))
	if err != nil || sessionID == "" || chunkIndex < 0 {
		return "", 0, nil, http.StatusBadRequest, fmt.Errorf("invalid session_id/chunk_index")
	}
	file, _, err := r.FormFile("chunk")
	if err != nil {
		return "", 0, nil, http.StatusBadRequest, fmt.Errorf("chunk payload missing: multipart field `chunk` required")
	}
	defer file.Close()
	chunkData, err = io.ReadAll(file)
	if err != nil {
		return "", 0, nil, http.StatusInternalServerError, fmt.Errorf("read chunk data failed")
	}
	if len(chunkData) == 0 {
		return "", 0, nil, http.StatusBadRequest, fmt.Errorf("empty chunk: client sent 0 bytes")
	}
	if len(chunkData) > MaxClientChunk {
		return "", 0, nil, http.StatusBadRequest, fmt.Errorf("chunk too large: max 5 MB per chunk")
	}
	return sessionID, chunkIndex, chunkData, 0, nil
}

func (s *Server) handleRequestUploadChunk(w http.ResponseWriter, r *http.Request) {
	fr, ok := s.validateRequestToken(w, r)
	if !ok {
		return
	}
	sessionID, chunkIndex, chunkData, status, err := readUploadChunkForm(w, r)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	session, err := s.db.GetUploadSession(sessionID)
	if err != nil {
		http.Error(w, "upload session not found or expired: re-init upload", http.StatusNotFound)
		return
	}
	mapped, err := s.db.RequestTokenForSession(sessionID)
	if err != nil || mapped != fr.Token {
		http.Error(w, "session does not belong to this file request", http.StatusForbidden)
		return
	}
	if session.StorageKind != "disk" {
		if err := s.telegramReady(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	if err := s.writeUploadChunk(r.Context(), session, chunkIndex, chunkData); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "chunk_index": chunkIndex})
}

func (s *Server) handleRequestUploadComplete(w http.ResponseWriter, r *http.Request) {
	fr, ok := s.validateRequestToken(w, r)
	if !ok {
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.SessionID) == "" {
		http.Error(w, "invalid payload: need {session_id}", http.StatusBadRequest)
		return
	}
	session, err := s.db.GetUploadSession(strings.TrimSpace(body.SessionID))
	if err != nil {
		http.Error(w, "upload session not found", http.StatusNotFound)
		return
	}
	mapped, err := s.db.RequestTokenForSession(session.ID)
	if err != nil || mapped != fr.Token {
		http.Error(w, "session does not belong to this file request", http.StatusForbidden)
		return
	}
	if session.StorageKind != "disk" {
		if err := s.telegramReady(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	file, shaHex, err := s.finishUpload(r.Context(), session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.db.DeleteRequestSession(session.ID)
	s.afterUploadComplete(w, r, file, shaHex)
}

// ------------------------------------------------------------------- zip --

type zipEntry struct {
	rel  string
	file db.File
}

func (s *Server) collectZipEntries(folderID *string, prefix string, out *[]zipEntry) error {
	folders, err := s.db.ListFolders(folderID)
	if err != nil {
		return err
	}
	for _, fo := range folders {
		next := fo.ID
		if err := s.collectZipEntries(&next, prefix+fo.Name+"/", out); err != nil {
			return err
		}
	}
	files, err := s.db.ListFiles(folderID)
	if err != nil {
		return err
	}
	for _, f := range files {
		// Folder/file names are validated at creation, but never let a
		// crafted name escape the archive (zip slip).
		rel := prefix + f.Name
		if strings.Contains(rel, "..") {
			continue
		}
		*out = append(*out, zipEntry{rel: rel, file: f})
	}
	return nil
}

// handleFolderZip streams a folder (recursively) as one ZIP archive.
// Bytes stream file by file: nothing is buffered whole in memory or on
// disk, and a mid-archive backend error aborts the stream (logged, as
// with the single-file stream handlers, since headers are already sent).
func (s *Server) handleFolderZip(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	folder, err := s.db.GetFolder(id)
	if err != nil {
		http.Error(w, "folder not found", http.StatusNotFound)
		return
	}
	var entries []zipEntry
	if err := s.collectZipEntries(&id, "", &entries); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	safeName := strings.ReplaceAll(folder.Name, `"`, "")
	safeName = strings.Map(func(ch rune) rune {
		if ch < 32 {
			return -1
		}
		return ch
	}, safeName)
	if safeName == "" {
		safeName = "folder"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", safeName+".zip"))
	zw := zip.NewWriter(w)
	defer zw.Close()
	for _, e := range entries {
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: e.rel, Method: zip.Deflate})
		if err != nil {
			fmt.Printf("[zip] %s: %v\n", e.rel, err)
			return
		}
		if err := s.writeFileBytes(r, fw, &e.file); err != nil {
			fmt.Printf("[zip] %s: %v\n", e.rel, err)
			return
		}
	}
}

// writeFileBytes streams one catalog file's bytes to w, from whichever
// backend holds it.
func (s *Server) writeFileBytes(r *http.Request, w io.Writer, file *db.File) error {
	if file.StorageKind == "disk" {
		abs, ok := s.diskObjectAbs(file.LocalPath)
		if !ok {
			return fmt.Errorf("invalid local path")
		}
		f, err := os.Open(abs)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	}
	if err := s.telegramReady(); err != nil {
		return err
	}
	docID, docHash := parseDocCoords(file.TelegramFileID, file.TelegramAccessHash)
	return s.tg.Do(r.Context(), func(runCtx context.Context) error {
		return s.tg.DownloadFullByMessage(runCtx, file.TelegramMessageID, docID, docHash, w)
	})
}

// ------------------------------------------------------- telegram sending --

func (s *Server) handleTelegramSend(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	file, err := s.db.GetFile(id)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	var body struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Target) == "" {
		http.Error(w, "invalid payload: need {target} (an @username or numeric user ID)", http.StatusBadRequest)
		return
	}
	target := strings.TrimSpace(body.Target)

	tmp, err := os.CreateTemp("", "tdocs-send-")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	// Stage the bytes once: Telegram has no server-side copy between
	// arbitrary peers, so sending re-uploads from these bytes.
	if file.StorageKind == "disk" {
		abs, ok := s.diskObjectAbs(file.LocalPath)
		if !ok {
			http.Error(w, "invalid local path", http.StatusInternalServerError)
			return
		}
		src, err := os.Open(abs)
		if err != nil {
			http.Error(w, "local file not found", http.StatusNotFound)
			return
		}
		if _, err := io.Copy(tmp, src); err != nil {
			src.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		src.Close()
	} else {
		if err := s.telegramReady(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		docID, docHash := parseDocCoords(file.TelegramFileID, file.TelegramAccessHash)
		if err := s.tg.Do(r.Context(), func(runCtx context.Context) error {
			return s.tg.DownloadFullByMessage(runCtx, file.TelegramMessageID, docID, docHash, tmp)
		}); err != nil {
			http.Error(w, "download from Telegram failed: "+err.Error(), http.StatusBadGateway)
			return
		}
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var msgID int
	if err := s.tg.Do(r.Context(), func(runCtx context.Context) error {
		var sendErr error
		msgID, sendErr = s.tg.SendFileToChat(runCtx, target, tmp, file.Size, file.Name, file.MimeType, s.limiter)
		return sendErr
	}); err != nil {
		http.Error(w, "send to Telegram failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	s.audit(r, "telegram_send", fmt.Sprintf("%s -> %s (msg %d)", file.Name, target, msgID))
	writeJSON(w, http.StatusOK, map[string]any{"message_id": msgID, "target": target})
}

// --------------------------------------------------------- mirror schedule --

func (s *Server) mirrorSchedule() (enabled bool, intervalHours int) {
	enabled = false
	if raw, err := s.db.GetSetting("mirror_schedule_enabled"); err == nil && raw == "1" {
		enabled = true
	}
	intervalHours = 24
	if raw, err := s.db.GetSetting("mirror_schedule_interval_hours"); err == nil {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 && n <= 168 {
			intervalHours = n
		}
	}
	return enabled, intervalHours
}

func (s *Server) handleGetMirrorSchedule(w http.ResponseWriter, r *http.Request) {
	enabled, interval := s.mirrorSchedule()
	lastRun, _ := s.db.GetSetting("mirror_schedule_last_run")
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":        enabled,
		"interval_hours": interval,
		"last_run":       lastRun,
	})
}

func (s *Server) handleSetMirrorSchedule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled       bool `json:"enabled"`
		IntervalHours int  `json:"interval_hours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid payload: need {enabled, interval_hours}", http.StatusBadRequest)
		return
	}
	if body.IntervalHours < 1 || body.IntervalHours > 168 {
		http.Error(w, "interval_hours must be between 1 and 168", http.StatusBadRequest)
		return
	}
	enabled := "0"
	if body.Enabled {
		enabled = "1"
	}
	if err := s.db.SetSetting("mirror_schedule_enabled", enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.db.SetSetting("mirror_schedule_interval_hours", strconv.Itoa(body.IntervalHours)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "mirror_schedule_set", fmt.Sprintf("enabled=%v interval=%dh", body.Enabled, body.IntervalHours))
	s.handleGetMirrorSchedule(w, r)
}

// StartMirrorScheduler runs the scheduled mirror pass: every 15 minutes it
// checks whether a configured interval elapsed since the last run, and if
// so starts a full missing-files backup pass per ready account. Running
// jobs are never duplicated: startBackupJobAuto marks a second trigger as
// pending instead, so the pass re-runs when the current one finishes.
func (s *Server) StartMirrorScheduler(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				s.runMirrorSchedulePass()
			}
		}
	}()
}

func (s *Server) runMirrorSchedulePass() {
	enabled, intervalHours := s.mirrorSchedule()
	if !enabled {
		return
	}
	if raw, err := s.db.GetSetting("mirror_schedule_last_run"); err == nil && raw != "" {
		if last, err := time.Parse(time.RFC3339, raw); err == nil {
			if time.Since(last) < time.Duration(intervalHours)*time.Hour {
				return
			}
		}
	}
	reg, ok := s.requireAccounts()
	if !ok {
		return
	}
	accounts, err := telegram.LoadTelegramAccounts(s.db)
	if err != nil {
		return
	}
	_ = s.db.SetSetting("mirror_schedule_last_run", time.Now().Format(time.RFC3339))
	for _, acct := range accounts {
		if !reg.HasSession(acct) || reg.ChannelID(acct) == 0 {
			continue
		}
		s.startBackupJobAuto(acct, "")
	}
}

// ------------------------------------------------------------------- users --

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.db.ListUsers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if users == nil {
		users = []db.User{}
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid payload: need {username, password, role}", http.StatusBadRequest)
		return
	}
	if len(body.Password) < 8 {
		http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	hash, err := crypto.HashPassword(body.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	user, err := s.db.CreateUser(body.Username, hash, body.Role)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			http.Error(w, "username already exists", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "user_create", user.Username+" ("+user.Role+")")
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.db.GetUser(id); err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if err := s.db.DeleteUser(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "user_delete", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.db.GetUser(id); err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	var body struct {
		Role     *string `json:"role"`
		Password *string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if body.Role != nil {
		if err := s.db.UpdateUserRole(id, *body.Role); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if body.Password != nil {
		if len(*body.Password) < 8 {
			http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
			return
		}
		hash, err := crypto.HashPassword(*body.Password)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := s.db.UpdateUserPassword(id, hash); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	user, _ := s.db.GetUser(id)
	s.audit(r, "user_update", id)
	writeJSON(w, http.StatusOK, user)
}

// -------------------------------------------------------- storage settings --

func (s *Server) handleGetStorageSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"backend":   s.storageBackendKind(),
		"disk_path": s.diskRoot(),
	})
}

func (s *Server) handleSetStorageSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Backend  string  `json:"backend"`
		DiskPath *string `json:"disk_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid payload: need {backend, disk_path?}", http.StatusBadRequest)
		return
	}
	if body.Backend != "telegram" && body.Backend != "disk" {
		http.Error(w, "backend must be telegram or disk", http.StatusBadRequest)
		return
	}
	if body.DiskPath != nil && strings.TrimSpace(*body.DiskPath) != "" {
		path := strings.TrimSpace(*body.DiskPath)
		if err := os.MkdirAll(path, 0o755); err != nil {
			http.Error(w, "disk_path is not writable: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.db.SetSetting("disk_storage_path", path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := s.db.SetSetting("storage_backend", body.Backend); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "storage_settings_set", "backend="+body.Backend)
	s.handleGetStorageSettings(w, r)
}

// ------------------------------------------------------- public request page --

// handleRequestPage serves the no-login upload page for a file request:
// GET /r/{token}. Unknown tokens are 404, expired ones 410 — the same
// semantics as the JSON endpoint, so a dead link never renders a form.
func (s *Server) handleRequestPage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	req, err := s.db.GetFileRequestByToken(token)
	if err != nil {
		http.Error(w, "file request not found", http.StatusNotFound)
		return
	}
	if req.Expired() {
		http.Error(w, "this file request has expired", http.StatusGone)
		return
	}
	tokenJSON, err := json.Marshal(req.Token)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "request.html", map[string]any{
		"Name":      req.Name,
		"TokenJSON": template.JS(tokenJSON),
	})
}
