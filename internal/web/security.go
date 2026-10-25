package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"tdocs/internal/crypto"
)

// This file implements the real (non-placeholder) web security layer:
// random session tokens, CSRF protection, per-IP rate limiting and secure
// HTTP headers.

// ---------------------------------------------------------------- sessions ---

type WebSession struct {
	Token     string
	CSRF      string
	CreatedAt time.Time
	LastSeen  time.Time
	IP        string
	UserAgent string
}

type SessionStore struct {
	mu    sync.Mutex
	items map[string]*WebSession
	ttl   time.Duration
}

func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{items: map[string]*WebSession{}, ttl: ttl}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *SessionStore) Create(ip, ua string) (*WebSession, error) {
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	csrf, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	sess := &WebSession{Token: token, CSRF: csrf, CreatedAt: time.Now(), LastSeen: time.Now(), IP: ip, UserAgent: ua}
	s.mu.Lock()
	s.items[token] = sess
	// Opportunistic expiry sweep.
	for k, v := range s.items {
		if time.Since(v.LastSeen) > s.ttl {
			delete(s.items, k)
		}
	}
	s.mu.Unlock()
	return sess, nil
}

func (s *SessionStore) Get(token string) (*WebSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.items[token]
	if !ok || time.Since(sess.LastSeen) > s.ttl {
		delete(s.items, token)
		return nil, false
	}
	sess.LastSeen = time.Now()
	return sess, true
}

// ShortID is the non-secret prefix used to identify a session in listings.
func (s *WebSession) ShortID() string {
	if len(s.Token) >= 16 {
		return s.Token[:16]
	}
	return s.Token
}

func (s *SessionStore) Revoke(token string) {
	s.mu.Lock()
	delete(s.items, token)
	s.mu.Unlock()
}

func (s *SessionStore) List() []WebSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]WebSession, 0, len(s.items))
	for _, v := range s.items {
		if time.Since(v.LastSeen) <= s.ttl {
			out = append(out, *v)
		}
	}
	return out
}

// -------------------------------------------------------------------- CSRF ---

// csrfExempt lists cookie-authed mutation routes that cannot carry a token
// (public share unlock form). Everything else mutating requires X-CSRF-Token.
func csrfExempt(path string) bool {
	return strings.HasPrefix(path, "/s/") && strings.HasSuffix(path, "/unlock")
}

func isMutation(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	}
	return false
}

// ---------------------------------------------------------------- rate limit ---

type RateLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{hits: map[string][]time.Time{}}
}

// Allow reports whether key may proceed under limit events per window.
func (l *RateLimiter) Allow(key string, limit int, window time.Duration) bool {
	now := time.Now()
	cutoff := now.Add(-window)
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := make([]time.Time, 0, len(l.hits[key])+1)
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	// Bound memory: drop quiet keys opportunistically.
	if len(l.hits) > 10000 {
		for k, v := range l.hits {
			if len(v) == 0 || v[len(v)-1].Before(cutoff) {
				delete(l.hits, k)
			}
		}
	}
	return true
}

// Reset clears every recorded hit for key. Used after a successful login so a
// handful of earlier typos cannot leave the operator locked out for the rest
// of the window (the 429 page is easy to mistake for a broken login).
func (l *RateLimiter) Reset(key string) {
	l.mu.Lock()
	delete(l.hits, key)
	l.mu.Unlock()
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if ip, _, err := net.SplitHostPort(strings.TrimSpace(strings.Split(fwd, ",")[0]) + ":0"); err == nil {
			return ip
		}
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ------------------------------------------------------------------- headers ---

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		h.Set("Content-Security-Policy", strings.Join([]string{
			"default-src 'self'",
			"script-src 'self' 'unsafe-inline' https://unpkg.com",
			"style-src 'self' 'unsafe-inline' https://unpkg.com",
			"font-src 'self'",
			"img-src 'self' data: https:",
			"connect-src 'self'",
			"frame-src 'self'",
			"frame-ancestors 'self'",
			"form-action 'self'",
			"base-uri 'self'",
			"object-src 'none'",
		}, "; "))
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// ------------------------------------------------------------ admin auth ---

// adminPasswordOK verifies against a DB-stored Argon2id hash when an admin
// override exists, otherwise against the configured password (constant time).
func (s *Server) adminPasswordOK(pw string) bool {
	if h, err := s.db.GetSetting("admin_password_hash"); err == nil && h != "" {
		return crypto.VerifyPassword(h, pw)
	}
	if pw == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(pw), []byte(s.cfg.AdminPassword)) == 1
}

// bearerTokenOK accepts the admin password or any live API token.
func (s *Server) bearerTokenOK(r *http.Request) bool {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		secret := strings.TrimPrefix(h, "Bearer ")
		if s.adminPasswordOK(secret) {
			return true
		}
		if ok, _ := s.db.VerifyAPIToken(secret); ok {
			return true
		}
	}
	if q := r.URL.Query().Get("api_key"); q != "" {
		if s.adminPasswordOK(q) {
			return true
		}
		if ok, _ := s.db.VerifyAPIToken(q); ok {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------- superuser ---

// isSuperuser reports whether the request comes from the human administrator.
//
// tDocs is a single-admin appliance, but the credential classes are not
// equivalent: a browser session minted by the login form, or a Bearer carrying
// the literal admin password, proves operator intent. Machine API tokens do
// not — they exist to embed files and must never be able to reconfigure the
// storage backend or drive an MTProto login. That asymmetry is what makes the
// Telegram configuration surface superuser-only.
func (s *Server) isSuperuser(r *http.Request) bool {
	if _, ok := s.cookieSession(r); ok {
		return true
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return s.adminPasswordOK(strings.TrimPrefix(h, "Bearer "))
	}
	// Deliberately no ?api_key= path: superuser actions must not be performed
	// with a secret that ends up in URLs, logs and browser history.
	return false
}

// superuserOnly guards the Telegram storage configuration surface. It enforces
// both the superuser check and (for cookie-authenticated mutations) the same
// CSRF requirement as authMiddleware, so it is a strict superset of it.
// Signed download tickets, share guests and API tokens are all rejected.
func (s *Server) superuserOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isSuperuser(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":   "superuser_required",
				"message": "Telegram storage configuration is restricted to the administrator.",
			})
			return
		}
		if sess, ok := s.cookieSession(r); ok && isMutation(r) && !csrfExempt(r.URL.Path) {
			got := r.Header.Get("X-CSRF-Token")
			if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRF)) != 1 {
				http.Error(w, "CSRF token missing or invalid", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

// cookieSession returns the live web session when the request carries a valid
// session cookie (current or pre-rebrand name).
func (s *Server) cookieSession(r *http.Request) (*WebSession, bool) {
	for _, name := range []string{"tdocs_session", "robdocs_session", "teledrive_session"} {
		// Pre-rebrand static cookies ("authenticated") are no longer honored.
		c, err := r.Cookie(name)
		if err != nil || c.Value == "" || c.Value == "authenticated" {
			continue
		}
		if sess, ok := s.sessions.Get(c.Value); ok {
			return sess, true
		}
	}
	return nil, false
}
