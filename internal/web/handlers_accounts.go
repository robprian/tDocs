package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"tdocs/internal/db"
	"tdocs/internal/telegram"
)

// ------------------------------------------------------- telegram accounts ---
//
// Multi-Telegram accounts: additional phone numbers registered as backup
// storage targets. Routes (all superuser-only):
//   GET    /api/telegram/accounts                  list primary + additional
//   POST   /api/telegram/accounts                  {name, phone} → pending account
//   DELETE /api/telegram/accounts/{id}             remove record + session keys
//   POST   /api/telegram/accounts/{id}/wizard/start {phone} → wizard id
//          (status/submit/cancel reuse the global /api/telegram/wizard/*
//          endpoints with the returned wizard id)
//   POST   /api/telegram/accounts/{id}/refresh     re-read bound channel id
//   POST   /api/telegram/accounts/{id}/backup      {file_id?} → background job
//   GET    /api/telegram/accounts/{id}/backup/status
//
// Backup semantics: bytes are streamed primary → backup channel
// (io.Pipe, no temp file) and recorded in backup_copies. The local
// catalog is never merged with a backup channel, so message IDs from
// different channels can never collide. A file already copied is
// skipped on re-runs; deleting its backup_copies row re-queues it.

type backupJob struct {
	Running   bool      `json:"running"`
	Total     int       `json:"total"`
	Done      int       `json:"done"`
	Failed    int       `json:"failed"`
	LastError string    `json:"last_error,omitempty"`
	StartedAt time.Time `json:"started_at"`
	// Pending is set by auto-mirror when a new upload arrives mid-job;
	// the job then re-runs a full missing-files pass instead of
	// dropping the file. Not part of the API contract beyond this file.
	Pending bool `json:"-"`
}

func (s *Server) requireAccounts() (*telegram.AccountRegistry, bool) {
	return s.accounts, s.accounts != nil
}

func (s *Server) findAccount(id string) (telegram.TelegramAccount, error) {
	accounts, err := telegram.LoadTelegramAccounts(s.db)
	if err != nil {
		return telegram.TelegramAccount{}, err
	}
	for _, a := range accounts {
		if a.ID == id {
			return a, nil
		}
	}
	return telegram.TelegramAccount{}, fmt.Errorf("telegram account %s not found", id)
}

func (s *Server) accountView(reg *telegram.AccountRegistry, a telegram.TelegramAccount) map[string]any {
	status := "pending_login"
	if reg.HasSession(a) {
		status = "paired"
		if reg.ChannelID(a) != 0 {
			status = "ready"
		}
	}
	copies, _ := s.db.CountBackupCopies(a.ID)
	last := ""
	_ = s.db.QueryRow(`SELECT MAX(created_at) FROM backup_copies WHERE account_id = ?`, a.ID).Scan(&last)
	return map[string]any{
		"id":             a.ID,
		"name":           a.Name,
		"phone":          a.Phone,
		"status":         status,
		"channel_id":     reg.ChannelID(a),
		"backup_copies":  copies,
		"files":          copies,
		"last_synced_at": last,
		"created_at":     a.CreatedAt,
	}
}

// lastPrimarySyncAt returns the most recent catalog sync run start, or "".
func (s *Server) lastPrimarySyncAt() string {
	var ts string
	if err := s.db.QueryRow(`SELECT started_at FROM sync_runs ORDER BY id DESC LIMIT 1`).Scan(&ts); err != nil {
		return ""
	}
	return ts
}

func (s *Server) handleTelegramAccounts(w http.ResponseWriter, r *http.Request) {
	reg, ok := s.requireAccounts()
	if !ok {
		jsonOut(w, http.StatusServiceUnavailable, map[string]any{"error": "telegram accounts not configured: server started without Telegram app credentials"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		accounts, err := telegram.LoadTelegramAccounts(s.db)
		if err != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		totalFiles, _ := s.db.CountFiles()
		primary := map[string]any{"id": "primary", "name": "Primary", "phone": "", "status": "unknown", "channel_id": int64(0), "backup_copies": 0, "files": totalFiles, "last_synced_at": s.lastPrimarySyncAt()}
		if s.tg != nil {
			if mgr, isMgr := s.tg.(*telegram.ClientManager); isMgr && mgr != nil {
				primary["status"] = "paired"
				if mgr.IsAuthorized() {
					primary["status"] = "ready"
				}
				if raw, err := s.db.GetSetting("storage_channel_id"); err == nil {
					var id int64
					_, _ = fmt.Sscanf(raw, "%d", &id)
					primary["channel_id"] = id
				}
			}
		}
		out := []any{primary}
		for _, a := range accounts {
			out = append(out, s.accountView(reg, a))
		}
		jsonOut(w, http.StatusOK, map[string]any{"accounts": out})
	case http.MethodPost:
		var body struct {
			Name  string `json:"name"`
			Phone string `json:"phone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonOut(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		body.Phone = strings.TrimSpace(body.Phone)
		if body.Name == "" || body.Phone == "" {
			jsonOut(w, http.StatusBadRequest, map[string]any{"error": "name and phone are required"})
			return
		}
		acct, err := telegram.RegisterTelegramAccount(s.db, body.Name, body.Phone)
		if err != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		_ = s.db.Audit("admin", "telegram_account_add", acct.ID+" "+acct.Phone, clientIP(r))
		jsonOut(w, http.StatusCreated, s.accountView(reg, acct))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTelegramAccountDelete(w http.ResponseWriter, r *http.Request) {
	reg, ok := s.requireAccounts()
	if !ok {
		jsonOut(w, http.StatusServiceUnavailable, map[string]any{"error": "telegram accounts not configured"})
		return
	}
	id := r.PathValue("id")
	if id == "" || id == "primary" {
		jsonOut(w, http.StatusBadRequest, map[string]any{"error": "the primary account cannot be deleted here; use `tdocs logout` on the server"})
		return
	}
	if _, err := s.findAccount(id); err != nil {
		jsonOut(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	reg.Stop(id)
	reg.Remove(id)
	if err := telegram.DeleteTelegramAccount(s.db, id); err != nil {
		jsonOut(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	_ = s.db.Audit("admin", "telegram_account_delete", id, clientIP(r))
	jsonOut(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) waitAccountRunning(mgr *telegram.ClientManager, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if mgr.IsRunning() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("telegram account runner did not start within %s", timeout)
}

func (s *Server) handleTelegramAccountWizardStart(w http.ResponseWriter, r *http.Request) {
	reg, ok := s.requireAccounts()
	if !ok {
		jsonOut(w, http.StatusServiceUnavailable, map[string]any{"error": "telegram accounts not configured"})
		return
	}
	acct, err := s.findAccount(r.PathValue("id"))
	if err != nil {
		jsonOut(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	var body struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	phone := strings.TrimSpace(body.Phone)
	if phone == "" {
		phone = acct.Phone
	}
	if phone == "" {
		jsonOut(w, http.StatusBadRequest, map[string]any{"error": "phone is required"})
		return
	}
	reg.Start(acct)
	mgr := reg.ManagerFor(acct)
	if err := s.waitAccountRunning(mgr, 10*time.Second); err != nil {
		jsonOut(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	id, err := telegram.WizardStart(r.Context(), mgr, phone)
	if err != nil {
		jsonOut(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	_ = s.db.Audit("admin", "telegram_account_wizard_start", acct.ID+" "+phone, clientIP(r))
	snap := telegram.WizardSnapshotJSON(id)
	jsonOut(w, http.StatusOK, map[string]any{"id": id, "phase": snap.Phase, "success": snap.Success, "error": snap.Error})
}

func (s *Server) handleTelegramAccountRefresh(w http.ResponseWriter, r *http.Request) {
	reg, ok := s.requireAccounts()
	if !ok {
		jsonOut(w, http.StatusServiceUnavailable, map[string]any{"error": "telegram accounts not configured"})
		return
	}
	acct, err := s.findAccount(r.PathValue("id"))
	if err != nil {
		jsonOut(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	if ch := reg.ChannelID(acct); ch != 0 {
		_ = telegram.UpdateTelegramAccountChannel(s.db, acct.ID, ch)
		acct.ChannelID = ch
	}
	jsonOut(w, http.StatusOK, s.accountView(reg, acct))
}

func (s *Server) copyFileToBackup(ctx context.Context, mgr *telegram.ClientManager, file db.File) (int, error) {
	if s.tg == nil {
		return 0, fmt.Errorf("primary telegram backend is not configured")
	}
	docID, docHash := parseDocCoords(file.TelegramFileID, file.TelegramAccessHash)
	pr, pw := io.Pipe()
	dlDone := make(chan error, 1)
	go func() {
		err := s.tg.Do(ctx, func(runCtx context.Context) error {
			return s.tg.DownloadFullByMessage(runCtx, file.TelegramMessageID, docID, docHash, pw)
		})
		if err != nil {
			_ = pw.CloseWithError(err)
		} else {
			_ = pw.Close()
		}
		dlDone <- err
	}()
	var backupMsg int
	upErr := mgr.Do(ctx, func(runCtx context.Context) error {
		msg, _, _, _, err := mgr.UploadFromReader(runCtx, pr, file.Size, file.Name, file.MimeType, s.limiter, nil)
		if err != nil {
			return err
		}
		backupMsg = msg
		return nil
	})
	if upErr != nil {
		_ = pr.Close()
	}
	dlErr := <-dlDone
	if upErr != nil {
		return 0, upErr
	}
	if dlErr != nil {
		return 0, dlErr
	}
	return backupMsg, nil
}

func (s *Server) runBackupJob(acct telegram.TelegramAccount, fileID string, job *backupJob) {
	defer func() {
		s.backupMu.Lock()
		job.Running = false
		s.backupMu.Unlock()
	}()
	s.runBackupPasses(acct, fileID, job)
}

func (s *Server) runBackupPasses(acct telegram.TelegramAccount, fileID string, job *backupJob) {
	for {
		s.runBackupPass(acct, fileID, job)
		s.backupMu.Lock()
		pending := job.Pending
		job.Pending = false
		s.backupMu.Unlock()
		if !pending {
			return
		}
	}
}

func (s *Server) runBackupPass(acct telegram.TelegramAccount, fileID string, job *backupJob) {

	var files []db.File
	if fileID != "" {
		f, err := s.db.GetFile(fileID)
		if err != nil {
			s.backupMu.Lock()
			job.Failed = 1
			job.LastError = "file not found"
			s.backupMu.Unlock()
			return
		}
		files = []db.File{*f}
	} else {
		rows, err := s.db.Query(`SELECT id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, created_at, updated_at FROM files WHERE deleted_at IS NULL ORDER BY created_at DESC`)
		if err != nil {
			s.backupMu.Lock()
			job.LastError = err.Error()
			s.backupMu.Unlock()
			return
		}
		defer rows.Close()
		for rows.Next() {
			var f db.File
			if err := rows.Scan(&f.ID, &f.FolderID, &f.Name, &f.Size, &f.MimeType, &f.TelegramMessageID, &f.TelegramFileID, &f.TelegramAccessHash, &f.SHA256, &f.CreatedAt, &f.UpdatedAt); err != nil {
				continue
			}
			files = append(files, f)
		}
		if err := rows.Err(); err != nil {
			s.backupMu.Lock()
			job.LastError = err.Error()
			s.backupMu.Unlock()
			return
		}
	}

	reg, _ := s.requireAccounts()
	mgr := reg.ManagerFor(acct)
	ctx := context.Background()
	for _, f := range files {
		has, _ := s.db.HasBackupCopy(f.ID, acct.ID)
		if has {
			continue
		}
		s.backupMu.Lock()
		job.Total++
		s.backupMu.Unlock()
		msg, err := s.copyFileToBackup(ctx, mgr, f)
		s.backupMu.Lock()
		if err != nil {
			job.Failed++
			job.LastError = f.Name + ": " + err.Error()
		} else {
			_ = s.db.RecordBackupCopy(f.ID, acct.ID, msg)
			job.Done++
		}
		s.backupMu.Unlock()
	}
	_ = s.db.Audit("admin", "telegram_backup_done", fmt.Sprintf("%s: %d copied, %d failed", acct.ID, job.Done, job.Failed), "")
}

func (s *Server) handleTelegramAccountBackup(w http.ResponseWriter, r *http.Request) {
	reg, ok := s.requireAccounts()
	if !ok {
		jsonOut(w, http.StatusServiceUnavailable, map[string]any{"error": "telegram accounts not configured"})
		return
	}
	acct, err := s.findAccount(r.PathValue("id"))
	if err != nil {
		jsonOut(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	if !reg.HasSession(acct) {
		jsonOut(w, http.StatusBadRequest, map[string]any{"error": "account has no Telegram session yet; pair it with the wizard first"})
		return
	}
	if s.tg == nil {
		jsonOut(w, http.StatusServiceUnavailable, map[string]any{"error": "primary telegram backend is not configured"})
		return
	}
	var body struct {
		FileID string `json:"file_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	s.backupMu.Lock()
	if job, exists := s.backupJobs[acct.ID]; exists && job.Running {
		s.backupMu.Unlock()
		jsonOut(w, http.StatusConflict, map[string]any{"error": "a backup job is already running for this account", "job": job})
		return
	}
	s.backupMu.Unlock()

	reg.Start(acct)
	mgr := reg.ManagerFor(acct)
	if err := s.waitAccountRunning(mgr, 10*time.Second); err != nil {
		jsonOut(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	// Bind the backup channel now (account keys): without it the upload
	// has nowhere to go. A second run is a no-op once bound.
	if err := mgr.Do(r.Context(), func(runCtx context.Context) error {
		return mgr.EnsureStorageChannel(runCtx)
	}); err != nil {
		jsonOut(w, http.StatusServiceUnavailable, map[string]any{"error": "backup storage channel unavailable: " + err.Error()})
		return
	}
	if ch := reg.ChannelID(acct); ch != 0 {
		_ = telegram.UpdateTelegramAccountChannel(s.db, acct.ID, ch)
	}

	job := &backupJob{Running: true, StartedAt: time.Now()}
	s.backupMu.Lock()
	s.backupJobs[acct.ID] = job
	s.backupMu.Unlock()
	go s.runBackupJob(acct, strings.TrimSpace(body.FileID), job)
	_ = s.db.Audit("admin", "telegram_backup_start", acct.ID, clientIP(r))
	jsonOut(w, http.StatusAccepted, map[string]any{"ok": true, "message": "backup job started; poll backup/status"})
}

func (s *Server) handleTelegramAccountBackupStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAccounts(); !ok {
		jsonOut(w, http.StatusServiceUnavailable, map[string]any{"error": "telegram accounts not configured"})
		return
	}
	if _, err := s.findAccount(r.PathValue("id")); err != nil {
		jsonOut(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	s.backupMu.Lock()
	job, exists := s.backupJobs[r.PathValue("id")]
	s.backupMu.Unlock()
	if !exists {
		jsonOut(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"job": job})
}

// ------------------------------------------------------- mirror setting ---
//
// "Multi-account Mirror" on the Overview rail: when enabled, every newly
// uploaded file is automatically copied to all ready backup accounts in
// the background (same backup job machinery as manual Backup Now, with a
// pending re-run so a file arriving mid-job is not missed). Manual Backup
// Now always works regardless of this setting.
func (s *Server) handleTelegramMirror(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		raw, _ := s.db.GetSetting("multi_account_mirror")
		jsonOut(w, http.StatusOK, map[string]any{"enabled": raw == "1"})
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonOut(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
			return
		}
		val := "0"
		if body.Enabled {
			val = "1"
		}
		if err := s.db.SetSetting("multi_account_mirror", val); err != nil {
			jsonOut(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		_ = s.db.Audit("admin", "telegram_mirror_toggle", val, clientIP(r))
		jsonOut(w, http.StatusOK, map[string]any{"enabled": body.Enabled})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// maybeMirrorNewFile queues a background backup of one newly uploaded file
// to every ready backup account when the mirror toggle is on. It never
// blocks or fails the upload: any error is recorded on the job.
func (s *Server) maybeMirrorNewFile(file db.File) {
	raw, _ := s.db.GetSetting("multi_account_mirror")
	if raw != "1" {
		return
	}
	reg, ok := s.requireAccounts()
	if !ok || s.tg == nil {
		return
	}
	accounts, err := telegram.LoadTelegramAccounts(s.db)
	if err != nil {
		return
	}
	for _, acct := range accounts {
		if !reg.HasSession(acct) || reg.ChannelID(acct) == 0 {
			continue
		}
		s.startBackupJobAuto(acct, file.ID)
	}
}

// startBackupJobAuto starts a single-file backup job, or marks the running
// job pending so it re-runs a full missing-files pass at the end.
func (s *Server) startBackupJobAuto(acct telegram.TelegramAccount, fileID string) {
	s.backupMu.Lock()
	if job, exists := s.backupJobs[acct.ID]; exists && job.Running {
		job.Pending = true
		s.backupMu.Unlock()
		return
	}
	job := &backupJob{Running: true, StartedAt: time.Now()}
	s.backupJobs[acct.ID] = job
	s.backupMu.Unlock()
	go s.runBackupJob(acct, fileID, job)
}
