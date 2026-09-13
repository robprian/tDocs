package web

import (
	"context"
	"crypto/subtle"
	"embed"
	"fmt"
	"hash"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"tdocs/internal/app"
	"tdocs/internal/db"
	"tdocs/internal/storage"
	"tdocs/internal/telegram"
)

//go:embed templates/* static/*
var contentFS embed.FS

type Server struct {
	cfg       *app.Config
	db        *db.DB
	tg        storage.Backend
	limiter   *telegram.SafeLimiter
	ratelimit *RateLimiter
	sessions  *SessionStore
	mux       *http.ServeMux
	templates *template.Template

	// Concurrency safety for hot-restore and snapshot operations
	dbMu       sync.RWMutex
	snapshotMu sync.Mutex

	startedAt time.Time

	// In-memory SHA-256 accumulators for in-flight web uploads
	// (single-process single binary: no cross-instance resume needed).
	uploadHashMu sync.Mutex
	uploadHash   map[string]*hashEntry

	// In-memory unlock cache for password-protected share tokens
	unlockedMu     sync.RWMutex
	unlockedTokens map[string]time.Time

	// Last verified Telegram backend state (never inferred from config alone).
	tgHealthMu sync.Mutex
	tgHealth   tgHealthSnapshot
}

func NewServer(cfg *app.Config, database *db.DB, tgManager *telegram.ClientManager) (*Server, error) {
	// Compile-time proof that the Telegram client satisfies the storage
	// abstraction the web layer programs against.
	var _ storage.Backend = tgManager

	// Keep the interface truly nil when no manager is configured: a typed-nil
	// pointer inside the interface would defeat every s.tg == nil guard.
	var backend storage.Backend
	if tgManager != nil {
		backend = tgManager
	}
	tmpl, err := template.ParseFS(contentFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse embedded templates: %w", err)
	}

	s := &Server{
		cfg:            cfg,
		db:             database,
		tg:             backend,
		limiter:        telegram.NewSafeLimiter(),
		ratelimit:      NewRateLimiter(),
		sessions:       NewSessionStore(30 * 24 * time.Hour),
		mux:            http.NewServeMux(),
		templates:      tmpl,
		unlockedTokens: make(map[string]time.Time),
		startedAt:      time.Now(),
		uploadHash:     make(map[string]*hashEntry),
	}

	s.routes()
	return s, nil
}

func (s *Server) routes() {
	// Embedded static assets
	staticFS, _ := fs.Sub(contentFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Authentication
	s.mux.HandleFunc("GET /login", s.handleLoginPage)
	s.mux.HandleFunc("POST /login", s.handleLoginSubmit)
	s.mux.HandleFunc("GET /logout", s.handleLogout)

	// Protected Dashboard
	s.mux.HandleFunc("GET /", s.authMiddleware(s.handleDashboard))

	// Machine-readable API index (auth: cookie or Bearer, see /docs)
	s.mux.HandleFunc("GET /api", s.requireAPIKey(s.handleAPIIndex))
	s.mux.HandleFunc("OPTIONS /api", s.requireAPIKey(s.handleAPIIndex))
	s.mux.HandleFunc("GET /api/status", s.authMiddleware(s.handleStatus))
	s.mux.HandleFunc("POST /api/sync", s.authMiddleware(s.handleSyncChannel))

	// Protected Drive API (session cookie OR Bearer admin password / API token)
	s.mux.HandleFunc("GET /api/folders", s.authMiddleware(s.handleListFolders))
	s.mux.HandleFunc("GET /api/folders/stats", s.authMiddleware(s.handleFolderStats))
	s.mux.HandleFunc("POST /api/folders", s.authMiddleware(s.handleCreateFolder))
	s.mux.HandleFunc("PUT /api/folders/{id}", s.authMiddleware(s.handleUpdateFolder))
	s.mux.HandleFunc("DELETE /api/folders/{id}", s.authMiddleware(s.handleDeleteFolder))

	s.mux.HandleFunc("GET /api/files", s.authMiddleware(s.handleListFiles))
	s.mux.HandleFunc("GET /api/files/{id}", s.authMiddleware(s.handleGetFile))
	s.mux.HandleFunc("GET /api/files/{id}/meta", s.authMiddleware(s.handleFileMeta))
	s.mux.HandleFunc("PUT /api/files/{id}", s.authMiddleware(s.handleUpdateFile))
	s.mux.HandleFunc("DELETE /api/files/{id}", s.authMiddleware(s.handleDeleteFile))
	s.mux.HandleFunc("GET /api/files/{id}/stream", s.authMiddleware(s.handleFileStream))
	s.mux.HandleFunc("HEAD /api/files/{id}/stream", s.authMiddleware(s.handleFileStream))
	s.mux.HandleFunc("GET /api/files/{id}/download", s.authMiddleware(s.handleFileDownload))
	s.mux.HandleFunc("HEAD /api/files/{id}/download", s.authMiddleware(s.handleFileDownload))
	s.mux.HandleFunc("POST /api/files/{id}/ticket", s.authMiddleware(s.handleMintTicket))
	s.mux.HandleFunc("POST /api/files/{id}/copy", s.authMiddleware(s.handleCopyFile))
	s.mux.HandleFunc("POST /api/files/{id}/favorite", s.authMiddleware(s.handleAddFavorite))
	s.mux.HandleFunc("DELETE /api/files/{id}/favorite", s.authMiddleware(s.handleRemoveFavorite))
	s.mux.HandleFunc("GET /api/files/{id}/versions", s.authMiddleware(s.handleListVersions))
	s.mux.HandleFunc("POST /api/files/{id}/versions/{vid}/restore", s.authMiddleware(s.handleRestoreVersion))
	s.mux.HandleFunc("DELETE /api/files/{id}/versions/{vid}", s.authMiddleware(s.handleDeleteVersion))
	s.mux.HandleFunc("POST /api/files/bulk", s.authMiddleware(s.handleBulkFiles))

	// Resumable Chunked Upload API
	s.mux.HandleFunc("POST /api/upload/init", s.authMiddleware(s.handleUploadInit))
	s.mux.HandleFunc("POST /api/upload/chunk", s.authMiddleware(s.handleUploadChunk))
	s.mux.HandleFunc("POST /api/upload/complete", s.authMiddleware(s.handleUploadComplete))

	// Library views
	s.mux.HandleFunc("GET /api/favorites", s.authMiddleware(s.handleListFavorites))
	s.mux.HandleFunc("GET /api/duplicates", s.authMiddleware(s.handleListDuplicates))
	s.mux.HandleFunc("GET /api/stats", s.authMiddleware(s.handleStats))
	s.mux.HandleFunc("GET /api/notifications", s.authMiddleware(s.handleNotifications))

	// Trash
	s.mux.HandleFunc("GET /api/trash", s.authMiddleware(s.handleListTrash))
	s.mux.HandleFunc("POST /api/trash/restore", s.authMiddleware(s.handleRestoreTrash))
	s.mux.HandleFunc("DELETE /api/trash/{kind}/{id}", s.authMiddleware(s.handlePurgeTrash))
	s.mux.HandleFunc("POST /api/trash/empty", s.authMiddleware(s.handleEmptyTrash))

	// Admin: audit, tokens, sessions, settings, health, sync history
	s.mux.HandleFunc("GET /api/audit", s.authMiddleware(s.handleListAudit))
	s.mux.HandleFunc("GET /api/tokens", s.authMiddleware(s.handleListTokens))
	s.mux.HandleFunc("POST /api/tokens", s.authMiddleware(s.handleCreateToken))
	s.mux.HandleFunc("DELETE /api/tokens/{id}", s.authMiddleware(s.handleRevokeToken))
	s.mux.HandleFunc("DELETE /api/tokens/{id}/purge", s.authMiddleware(s.handleDeleteToken))
	s.mux.HandleFunc("GET /api/sessions", s.authMiddleware(s.handleListSessions))
	s.mux.HandleFunc("DELETE /api/sessions/{id}", s.authMiddleware(s.handleRevokeSession))
	s.mux.HandleFunc("POST /api/settings/password", s.authMiddleware(s.handleChangePassword))
	s.mux.HandleFunc("GET /api/health", s.authMiddleware(s.handleHealth))
	s.mux.HandleFunc("GET /api/sync/runs", s.authMiddleware(s.handleListSyncRuns))
	s.mux.HandleFunc("POST /api/storage/verify", s.authMiddleware(s.handleVerifyStorage))

	// Share Links
	s.mux.HandleFunc("POST /api/share", s.authMiddleware(s.handleCreateShare))
	s.mux.HandleFunc("GET /api/shares", s.authMiddleware(s.handleListShares))
	s.mux.HandleFunc("DELETE /api/shares/{id}", s.authMiddleware(s.handleDeleteShare))
	s.mux.HandleFunc("GET /s/{token}", s.handleShareLanding)
	s.mux.HandleFunc("POST /s/{token}/unlock", s.handleShareUnlock)
	s.mux.HandleFunc("GET /s/{token}/stream", s.handleShareStream)
	s.mux.HandleFunc("HEAD /s/{token}/stream", s.handleShareStream)
	s.mux.HandleFunc("GET /s/{token}/download", s.handleShareDownload)
	s.mux.HandleFunc("HEAD /s/{token}/download", s.handleShareDownload)

	// Database Snapshots & Point-in-Time Recovery
	s.mux.HandleFunc("GET /api/snapshots", s.authMiddleware(s.handleListSnapshots))
	s.mux.HandleFunc("POST /api/snapshots", s.authMiddleware(s.handleCreateSnapshot))
	s.mux.HandleFunc("POST /api/snapshots/{id}/restore", s.authMiddleware(s.handleRestoreSnapshot))
	s.mux.HandleFunc("GET /api/snapshots/{id}/download", s.authMiddleware(s.handleDownloadSnapshot))
	s.mux.HandleFunc("DELETE /api/snapshots/{id}", s.authMiddleware(s.handleDeleteSnapshot))
	s.mux.HandleFunc("POST /api/snapshots/upload-restore", s.authMiddleware(s.handleUploadRestoreSnapshot))

	// Public CDN for external consumers (film site embeds). Catalog stays
	// key-protected; stream/download are open CORS by default.
	s.mux.HandleFunc("GET /api/cdn/files", s.requireAPIKey(s.handleCDNList))
	s.mux.HandleFunc("OPTIONS /api/cdn/files", s.requireAPIKey(s.handleCDNList))
	s.mux.HandleFunc("GET /api/cdn/files/{id}", s.requireAPIKey(s.handleCDNDetail))
	s.mux.HandleFunc("OPTIONS /api/cdn/files/{id}", s.requireAPIKey(s.handleCDNDetail))
	s.mux.HandleFunc("GET /cdn/{id}/stream", s.handleCDNStream)
	s.mux.HandleFunc("HEAD /cdn/{id}/stream", s.handleCDNStream)
	s.mux.HandleFunc("OPTIONS /cdn/{id}/stream", s.handleCDNStream)
	s.mux.HandleFunc("GET /cdn/{id}/download", s.handleCDNDownload)
	s.mux.HandleFunc("HEAD /cdn/{id}/download", s.handleCDNDownload)
	s.mux.HandleFunc("OPTIONS /cdn/{id}/download", s.handleCDNDownload)

	// Browser-based Telegram auth wizard (Cloudy setup wizard). Every one of
	// these endpoints can drive an MTProto login or reconfigure the storage
	// backend, so they are superuser-only — API tokens and share guests get
	// 403 regardless of what the UI happens to render.
	s.mux.HandleFunc("POST /api/telegram/wizard/start", s.superuserOnly(s.handleTelegramWizardStart))
	s.mux.HandleFunc("GET /api/telegram/wizard/status", s.superuserOnly(s.handleTelegramWizardStatus))
	s.mux.HandleFunc("POST /api/telegram/wizard/submit", s.superuserOnly(s.handleTelegramWizardSubmit))
	s.mux.HandleFunc("POST /api/telegram/wizard/discard", s.superuserOnly(s.handleTelegramWizardDiscard))
	s.mux.HandleFunc("GET /api/telegram/wizard/needed", s.superuserOnly(s.handleTelegramWizardNeeded))
	s.mux.HandleFunc("POST /api/telegram/onboard", s.superuserOnly(s.handleTelegramWizardStart)) // alias
	// Verified backend state + explicit live connection test (superuser only).
	s.mux.HandleFunc("GET /api/telegram/state", s.superuserOnly(s.handleTelegramState))
	s.mux.HandleFunc("POST /api/telegram/test", s.superuserOnly(s.handleTelegramTest))

	// API contract
	s.mux.HandleFunc("GET /api/openapi.json", s.handleOpenAPIJSON)
	s.mux.HandleFunc("GET /docs", s.handleDocs)
}

// hashEntry accumulates a running SHA-256 over the chunks of one upload.
type hashEntry struct {
	h  hash.Hash
	at time.Time
}

// dbFileSize returns the on-disk size of the SQLite database (0 when unknown).
func dbFileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Global per-IP rate limiting for machine surfaces (generous). Login is
	// throttled inside handleLoginSubmit instead: the limiter must punish
	// wrong guesses, not reject the correct password. Blocking before the
	// credential check is what made a few typos look like a broken login —
	// the right password kept returning 429 for the rest of the window.
	ip := clientIP(r)
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/cdn/") {
		if !s.ratelimit.Allow("api:"+ip, 600, time.Minute) {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}
	secureHeaders(s.mux).ServeHTTP(w, r)
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Signed expiring download tickets bypass login (verified below).
		if s.validFileTicket(r) {
			next(w, r)
			return
		}
		sess, cookieOK := s.cookieSession(r)
		bearerOK := s.bearerTokenOK(r)
		if !cookieOK && !bearerOK {
			if r.Header.Get("X-Requested-With") == "XMLHttpRequest" || r.URL.Path != "/" {
				http.Error(w, "Unauthorized: login session or Authorization: Bearer <admin password / API token> required", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		// CSRF: cookie-authed mutations must present the session token.
		// Bearer/API-token callers are exempt (no ambient authority).
		if cookieOK && !bearerOK && isMutation(r) && !csrfExempt(r.URL.Path) {
			got := r.Header.Get("X-CSRF-Token")
			if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRF)) != 1 {
				http.Error(w, "CSRF token missing or invalid", http.StatusForbidden)
				return
			}
		}
		next(w, r)
		return
	}
}

// telegramReady guards handlers that need MTProto. Request-scoped MTProto
// work must go through s.tg.Do, which dispatches to the single long-lived
// gotd session started by ClientManager.Run (gotd's Run is not re-entrant).
func (s *Server) telegramReady() error {
	if s.tg == nil {
		return fmt.Errorf("telegram client not initialized (run `tdocs login` first)")
	}
	return nil
}

// telegramPreflight fails fast when an upload cannot possibly succeed:
// Telegram never authorized on this server, or no bound Storage Channel yet.
// Without this the failure surfaces cryptically halfway through the chunks.
func (s *Server) telegramPreflight() error {
	if err := s.telegramReady(); err != nil {
		return err
	}
	if _, err := s.db.GetSetting("telegram_authorized"); err != nil {
		return fmt.Errorf("telegram belum login di server ini — jalankan `tdocs login`, lalu ulangi upload")
	}
	if _, err := s.db.GetSetting("storage_channel_id"); err != nil {
		return fmt.Errorf("storage channel belum terikat — restart server agar discovery jalan, lalu ulangi upload")
	}
	return nil
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	_ = s.templates.ExecuteTemplate(w, "login.html", nil)
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	pwd := r.FormValue("password")
	if s.adminPasswordOK(pwd) {
		sess, err := s.sessions.Create(clientIP(r), r.UserAgent())
		if err != nil {
			http.Error(w, "Could not create session", http.StatusInternalServerError)
			return
		}
		secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
		http.SetCookie(w, &http.Cookie{
			Name:     "tdocs_session",
			Value:    sess.Token,
			Path:     "/",
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400 * 30, // 30 days
		})
		// CSRF token cookie is readable by JS (the secret stays server-side
		// inside the session record).
		http.SetCookie(w, &http.Cookie{
			Name:     "tdocs_csrf",
			Value:    sess.CSRF,
			Path:     "/",
			HttpOnly: false,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400 * 30,
		})
		_ = s.db.Audit("admin", "login", "dashboard login from "+clientIP(r), clientIP(r))
		// A correct password clears the failed-attempt window: the operator is
		// demonstrably not an attacker, and stale typos must not cause a later
		// 429 that reads like a broken login.
		s.ratelimit.Reset("login:" + clientIP(r))
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Wrong credential: consume the brute-force budget and explain a lockout
	// on the real page rather than emitting a bare 429.
	if !s.ratelimit.Allow("login:"+clientIP(r), 15, time.Minute) {
		w.Header().Set("Retry-After", "60")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = s.templates.ExecuteTemplate(w, "login.html", map[string]any{
			"Error": "Too many failed attempts. Wait a minute, then try again.",
		})
		return
	}

	_ = s.templates.ExecuteTemplate(w, "login.html", map[string]any{
		"Error": "Invalid admin password",
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Revoke the server-side session so the token cannot be replayed.
	if c, err := r.Cookie("tdocs_session"); err == nil {
		s.sessions.Revoke(c.Value)
	}
	// Clear the current cookie, the CSRF cookie and all legacy cookies.
	for _, name := range []string{"tdocs_session", "tdocs_csrf", "robdocs_session", "teledrive_session"} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			MaxAge:   -1,
		})
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	_ = s.templates.ExecuteTemplate(w, "index.html", nil)
}

// StartPeriodicBackup starts a background scheduler that creates and uploads
// a Database Snapshot every 24 hours (or configured interval) with automatic rolling retention.
func (s *Server) StartPeriodicBackup(ctx context.Context) {
	interval := 24 * time.Hour
	if envInterval := os.Getenv("TDOCS_BACKUP_INTERVAL"); envInterval != "" {
		if d, err := time.ParseDuration(envInterval); err == nil && d >= time.Minute {
			interval = d
		}
	}

	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				if s.tg == nil {
					continue
				}
				if err := s.PerformAutomatedSnapshot(ctx); err != nil {
					fmt.Printf("[Periodic Backup] Warning: automated snapshot failed: %v\n", err)
				} else {
					fmt.Println("[Periodic Backup] Automated snapshot successfully created and retention applied")
				}
			}
		}
	}()
}

// PerformAutomatedSnapshot creates a point-in-time snapshot, uploads it to Telegram, and prunes older snapshots.
func (s *Server) PerformAutomatedSnapshot(ctx context.Context) error {
	if s.tg == nil {
		return fmt.Errorf("telegram client manager is not initialized")
	}

	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()

	s.dbMu.RLock()
	gzPath, err := s.db.CreateSnapshot()
	s.dbMu.RUnlock()
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	defer os.Remove(gzPath)

	return s.tg.Do(ctx, func(runCtx context.Context) error {
		if err := s.tg.EnsureStorageChannel(runCtx); err != nil {
			return err
		}
		_, err := s.tg.UploadSnapshot(runCtx, gzPath, s.limiter)
		return err
	})
}
