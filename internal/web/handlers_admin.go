package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	appcrypto "tdocs/internal/crypto"
	"tdocs/internal/db"
)

// Admin APIs: notifications, audit, API tokens, sessions, settings,
// signed download tickets, sync history, health.

// ------------------------------------------------------------ notifications ---

func notifyKind(action string) string {
	switch {
	case strings.HasPrefix(action, "login"):
		return "auth"
	case strings.HasPrefix(action, "upload"):
		return "upload"
	case strings.HasPrefix(action, "sync"):
		return "sync"
	case strings.HasPrefix(action, "snapshot"), strings.HasPrefix(action, "restore"):
		return "snapshot"
	case strings.HasPrefix(action, "share"):
		return "share"
	case strings.HasPrefix(action, "trash"), strings.HasPrefix(action, "purge"),
		strings.HasPrefix(action, "version"), strings.HasPrefix(action, "folder"),
		strings.HasPrefix(action, "file_"), strings.HasPrefix(action, "bulk"):
		return "file"
	case strings.HasPrefix(action, "token"), strings.HasPrefix(action, "password"),
		strings.HasPrefix(action, "session"), strings.HasPrefix(action, "favorite"):
		return "security"
	default:
		return "system"
	}
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	entries, err := s.db.ListAudit("", 25, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"id": e.ID, "time": e.CreatedAt, "kind": notifyKind(e.Action),
			"title": e.Action, "detail": e.Detail,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// -------------------------------------------------------------------- audit ---

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	entries, err := s.db.ListAudit(r.URL.Query().Get("action"), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []db.AuditEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// ------------------------------------------------------------------- tokens ---

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.db.ListAPITokens()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tokens == nil {
		tokens = []db.APIToken{}
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid payload: need {name}", http.StatusBadRequest)
		return
	}
	name, err := validName(body.Name, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, prefix, secret, err := s.db.NewAPIToken(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "token_create", name+" ("+prefix+")")
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "name": name, "prefix": prefix,
		// Shown exactly once: the secret is only stored as a hash.
		"token": secret,
	})
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.db.RevokeAPIToken(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "token_revoke", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.db.DeleteAPIToken(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "token_delete", id)
	w.WriteHeader(http.StatusNoContent)
}

// ----------------------------------------------------------------- sessions ---

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	var current string
	if c, err := r.Cookie("tdocs_session"); err == nil {
		current = c.Value
	}
	list := s.sessions.List()
	out := make([]map[string]any, 0, len(list))
	for _, sess := range list {
		out = append(out, map[string]any{
			"id": sess.ShortID(), "current": sess.Token == current,
			"created_at": sess.CreatedAt, "last_seen": sess.LastSeen,
			"ip": sess.IP, "user_agent": sess.UserAgent,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	revoked := false
	for _, sess := range s.sessions.List() {
		if strings.HasPrefix(sess.Token, id) {
			s.sessions.Revoke(sess.Token)
			revoked = true
		}
	}
	if !revoked {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	s.audit(r, "session_revoke", id)
	w.WriteHeader(http.StatusNoContent)
}

// ----------------------------------------------------------------- settings ---

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid payload: need {current, new}", http.StatusBadRequest)
		return
	}
	if !s.adminPasswordOK(body.Current) {
		http.Error(w, "current password is incorrect", http.StatusForbidden)
		return
	}
	if len(body.New) < 8 {
		http.Error(w, "new password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	hash, err := appcrypto.HashPassword(body.New)
	if err != nil {
		http.Error(w, "hash password failed", http.StatusInternalServerError)
		return
	}
	if err := s.db.SetSetting("admin_password_hash", hash); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r, "password_change", "admin password updated")
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	_, sessErr := s.db.GetSetting("telegram_session")
	_, authErr := s.db.GetSetting("telegram_authorized")
	_, chanIDErr := s.db.GetSetting("storage_channel_id")
	writeJSON(w, http.StatusOK, map[string]any{
		"version":             "2.0.0",
		"uptime_seconds":      int64(time.Since(s.startedAt).Seconds()),
		"goroutines":          runtime.NumGoroutine(),
		"memory_alloc_bytes":  mem.Alloc,
		"db_bytes":            dbFileSize(s.cfg.DBPath),
		"telegram_session":    sessErr == nil,
		"telegram_authorized": authErr == nil,
		"storage_channel":     chanIDErr == nil,
		"sessions":            len(s.sessions.List()),
	})
}

// ------------------------------------------------------------------ tickets ---

// Signed, expiring download URLs let dashboard downloads work without ambient
// cookies and power "copy download link" features.
func (s *Server) ticketKey() []byte {
	return appcrypto.DeriveKey("ticket-v1:" + s.cfg.SecretKey)
}

func (s *Server) mintTicket(fileID string, ttl time.Duration) string {
	if ttl <= 0 || ttl > 24*time.Hour {
		ttl = time.Hour
	}
	exp := time.Now().Add(ttl).Unix()
	payload := fmt.Sprintf("%d.%s", exp, fileID)
	mac := hmac.New(sha256.New, s.ticketKey())
	mac.Write([]byte(payload))
	raw := payload + "." + hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func (s *Server) verifyTicket(ticket, fileID string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(ticket)
	if err != nil {
		return false
	}
	parts := strings.Split(string(raw), ".")
	if len(parts) != 3 || parts[1] != fileID {
		return false
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	mac := hmac.New(sha256.New, s.ticketKey())
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(parts[2]), []byte(want)) == 1
}

// validFileTicket allows ticket-authenticated access to a file's stream and
// download routes without a login session.
func (s *Server) validFileTicket(r *http.Request) bool {
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" {
		return false
	}
	path := r.URL.Path
	var id string
	switch {
	case strings.HasPrefix(path, "/api/files/") && (strings.HasSuffix(path, "/stream") || strings.HasSuffix(path, "/download")):
		mid := strings.TrimPrefix(path, "/api/files/")
		mid = strings.TrimSuffix(strings.TrimSuffix(mid, "/stream"), "/download")
		id = mid
	default:
		return false
	}
	if id == "" || strings.Contains(id, "/") {
		return false
	}
	return s.verifyTicket(ticket, id)
}

func (s *Server) handleMintTicket(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.db.GetFile(id); err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	var body struct {
		ExpiresIn int64 `json:"expires_in"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ttl := time.Duration(body.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	if ttl > 24*time.Hour {
		ttl = 24 * time.Hour
	}
	exp := time.Now().Add(ttl)
	base := s.cdnBaseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"url":        base + "/api/files/" + id + "/download?ticket=" + s.mintTicket(id, ttl),
		"stream_url": base + "/api/files/" + id + "/stream?ticket=" + s.mintTicket(id, ttl),
		"expires_at": exp,
	})
}

// ------------------------------------------------------ storage verification ---

type verifyFailure struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error"`
}

// handleVerifyStorage re-resolves up to N recent files against the Storage
// Channel and reports which objects are still retrievable. Read-only.
func (s *Server) handleVerifyStorage(w http.ResponseWriter, r *http.Request) {
	if !s.requireTelegram(w, r) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	files, err := s.db.RecentFiles(limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	checked, okCount := 0, 0
	var failed []verifyFailure
	err = s.tg.Do(r.Context(), func(runCtx context.Context) error {
		for _, f := range files {
			checked++
			if _, rerr := s.tg.ResolveChannelDocument(runCtx, f.TelegramMessageID); rerr != nil {
				failed = append(failed, verifyFailure{ID: f.ID, Name: f.Name, Error: rerr.Error()})
			} else {
				okCount++
			}
		}
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "telegram_storage_unavailable",
			"state":   string(telegramUnavailableState(err)),
			"message": classifyHealth(err),
		})
		return
	}
	if failed == nil {
		failed = []verifyFailure{}
	}
	s.audit(r, "storage_verify", fmt.Sprintf("checked=%d ok=%d failed=%d", checked, okCount, len(failed)))
	writeJSON(w, http.StatusOK, map[string]any{
		"checked": checked, "ok": okCount, "failed": failed,
	})
}

// -------------------------------------------------------------- sync runs ---

func (s *Server) handleListSyncRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.db.ListSyncRuns(limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []db.SyncRun{}
	}
	writeJSON(w, http.StatusOK, runs)
}
