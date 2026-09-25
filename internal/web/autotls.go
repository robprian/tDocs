package web

// Built-in HTTPS: when the operator enters a public domain in Settings, tDocs
// obtains and renews a Let's Encrypt certificate itself through ACME, so no
// external reverse proxy (Caddy/nginx) is required. Only the domain has to
// point at this server; ports 80 and 443 must be reachable.
//
// Design notes:
//   - autocert is a subpackage of golang.org/x/crypto, already a direct
//     dependency, so this adds no new module to the build.
//   - The plain HTTP listener on the dashboard port keeps working unchanged:
//     a certificate failure must never make the appliance unreachable.
//   - Port 80 serves the ACME HTTP-01 challenge and redirects everything else
//     to the HTTPS origin, which is what browsers and Let's Encrypt expect.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

// Settings keys owned by the domain configuration surface.
const (
	settingPublicDomain = "public_domain"
	settingACMEEmail    = "acme_email"
)

// Defaults for the ACME listeners. Overridable for hosts that run the
// dashboard behind another service or cannot bind privileged ports.
func httpChallengePort() string { return envPort("TDOCS_HTTP_PORT", "80") }
func httpsListenPort() string   { return envPort("TDOCS_HTTPS_PORT", "443") }

func envPort(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// ValidateDomain normalizes a user-entered public domain.
//
// Accepts a bare hostname ("files.example.com") and tolerates a pasted URL
// ("https://files.example.com/") by stripping scheme, path, port and trailing
// dot. Rejects anything ACME cannot issue for (IP literals, wildcards, hosts
// without a dot, invalid characters) with an actionable message.
func ValidateDomain(raw string) (string, error) {
	d := strings.TrimSpace(raw)
	if d == "" {
		return "", nil // clearing the setting is allowed: back to HTTP only
	}
	// A pasted URL is accepted as a convenience, but only the host part is
	// kept: certificate issuance never sees the scheme, path or query.
	if strings.Contains(d, "://") {
		rest := d[strings.Index(d, "://")+3:]
		if i := strings.IndexAny(rest, "/?#"); i >= 0 {
			rest = rest[:i]
		}
		d = rest
	} else if strings.ContainsAny(d, "/?#") {
		return "", errors.New("enter the domain only, without a path (e.g. files.example.com)")
	}
	if host, _, err := net.SplitHostPort(d); err == nil {
		d = host
	}
	d = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
	if d == "" {
		return "", errors.New("domain is empty after normalization")
	}
	if len(d) > 253 {
		return "", errors.New("domain is too long")
	}
	if strings.Contains(d, "*") {
		return "", errors.New("wildcard domains are not supported: enter the exact hostname")
	}
	if net.ParseIP(d) != nil {
		return "", errors.New("enter a domain name, not an IP address: certificates are issued to hostnames")
	}
	if !strings.Contains(d, ".") {
		return "", errors.New("enter a fully qualified domain, e.g. files.example.com")
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" {
			return "", errors.New("domain has an empty label (check for doubled dots)")
		}
		if len(label) > 63 {
			return "", errors.New("domain label is too long")
		}
		for i, r := range label {
			ok := r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z')
			if !ok {
				return "", fmt.Errorf("domain contains an invalid character %q", string(r))
			}
			if r == '-' && (i == 0 || i == len(label)-1) {
				return "", errors.New("domain labels cannot start or end with a hyphen")
			}
		}
	}
	return d, nil
}

// validateACMEEmail checks the optional Let's Encrypt contact address. It is
// not required to obtain a certificate, but it is how expiry warnings reach
// the operator, so an obviously malformed value is rejected.
func validateACMEEmail(raw string) (string, error) {
	e := strings.TrimSpace(raw)
	if e == "" {
		return "", nil
	}
	if len(e) > 254 || strings.Count(e, "@") != 1 {
		return "", errors.New("enter a valid contact email address")
	}
	local, domain, _ := strings.Cut(e, "@")
	if local == "" || domain == "" || !strings.Contains(domain, ".") {
		return "", errors.New("enter a valid contact email address")
	}
	return e, nil
}

// autoTLS holds the live ACME listeners. Guarded by Server.tlsMu.
type autoTLS struct {
	manager  *autocert.Manager
	domain   string
	email    string
	httpSrv  *http.Server
	httpsSrv *http.Server
	httpLn   net.Listener
	httpsLn  net.Listener
	started  time.Time
}

// tlsStatus is the honest state reported to the dashboard. "active" means the
// listeners are bound and the certificate is being served, never merely that a
// domain is configured.
type tlsStatus struct {
	Domain     string `json:"domain,omitempty"`
	Email      string `json:"email,omitempty"`
	Active     bool   `json:"active"`
	HTTPPort   string `json:"http_port"`
	HTTPSPort  string `json:"https_port"`
	CertExpiry string `json:"cert_expiry,omitempty"`
	CertSource string `json:"cert_source,omitempty"`
	LastError  string `json:"last_error,omitempty"`
	Hint       string `json:"hint,omitempty"`
}

// applyAutoTLS saves the domain settings and (re)starts the ACME listeners.
// An empty domain stops them, returning the appliance to plain HTTP.
func (s *Server) applyAutoTLS(ctx context.Context, domain, email string) error {
	if err := s.db.SetSetting(settingPublicDomain, domain); err != nil {
		return fmt.Errorf("save domain: %w", err)
	}
	if err := s.db.SetSetting(settingACMEEmail, email); err != nil {
		return fmt.Errorf("save contact email: %w", err)
	}
	s.restartAutoTLS(ctx, domain, email)
	return s.autoTLSError()
}

// restartAutoTLS tears down any running listeners and starts new ones when a
// domain is configured.
func (s *Server) restartAutoTLS(ctx context.Context, domain, email string) {
	s.stopAutoTLS(ctx)
	if domain == "" {
		return
	}

	cacheDir := s.acmeCacheDir()
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		s.setTLSError(fmt.Sprintf("cannot create certificate cache %s: %v", cacheDir, err))
		return
	}

	mgr := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(cacheDir),
		HostPolicy: autocert.HostWhitelist(domain),
	}
	if email != "" {
		mgr.Email = email
	}

	rt := &autoTLS{manager: mgr, domain: domain, email: email, started: time.Now()}

	// Port 80: ACME HTTP-01 challenge, everything else redirected to HTTPS.
	httpLn, err := net.Listen("tcp", net.JoinHostPort("", httpChallengePort()))
	if err != nil {
		s.setTLSError(fmt.Sprintf("cannot bind port %s for the certificate challenge: %v (grant CAP_NET_BIND_SERVICE or set TDOCS_HTTP_PORT)", httpChallengePort(), err))
		return
	}
	rt.httpLn = httpLn
	rt.httpSrv = &http.Server{
		Handler:           mgr.HTTPHandler(redirectToHTTPS(domain)),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Port 443: real HTTPS. Only the configured host answers: anything else
	// gets a closed connection instead of a misleading certificate error.
	httpsLn, err := net.Listen("tcp", net.JoinHostPort("", httpsListenPort()))
	if err != nil {
		_ = httpLn.Close()
		s.setTLSError(fmt.Sprintf("cannot bind port %s: %v (grant CAP_NET_BIND_SERVICE or set TDOCS_HTTPS_PORT)", httpsListenPort(), err))
		return
	}
	rt.httpsLn = httpsLn
	rt.httpsSrv = &http.Server{
		Handler: s,
		TLSConfig: &tls.Config{
			GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				if hello == nil || !strings.EqualFold(strings.TrimSuffix(hello.ServerName, "."), domain) {
					return nil, fmt.Errorf("no certificate for %q", hello.ServerName)
				}
				return mgr.GetCertificate(hello)
			},
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"h2", "http/1.1", "acme-tls/1"},
		},
		ReadHeaderTimeout: 15 * time.Second,
	}

	s.tlsMu.Lock()
	s.tls = rt
	s.tlsErr = ""
	s.tlsMu.Unlock()

	go func() {
		if err := rt.httpSrv.Serve(httpLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("acme http listener stopped: %v", err)
		}
	}()
	go func() {
		// Certificates are fetched lazily on the first handshake for the
		// configured host, which is also what surfaces ACME failures.
		if err := rt.httpsSrv.ServeTLS(httpsLn, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("https listener stopped: %v", err)
		}
	}()

	log.Printf("HTTPS enabled for %s (ACME, cache %s)", domain, cacheDir)
}

func redirectToHTTPS(domain string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := "https://" + domain + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
}

// stopAutoTLS shuts the ACME listeners down. Safe to call when none run.
func (s *Server) stopAutoTLS(ctx context.Context) {
	s.tlsMu.Lock()
	rt := s.tls
	s.tls = nil
	s.tlsMu.Unlock()
	if rt == nil {
		return
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if rt.httpSrv != nil {
		_ = rt.httpSrv.Shutdown(shutdownCtx)
	}
	if rt.httpsSrv != nil {
		_ = rt.httpsSrv.Shutdown(shutdownCtx)
	}
	log.Printf("HTTPS listeners stopped (was %s)", rt.domain)
}

func (s *Server) setTLSError(msg string) {
	s.tlsMu.Lock()
	s.tlsErr = msg
	s.tlsMu.Unlock()
	log.Printf("HTTPS setup failed: %s", msg)
}

func (s *Server) autoTLSError() error {
	s.tlsMu.Lock()
	defer s.tlsMu.Unlock()
	if s.tlsErr == "" {
		return nil
	}
	return errors.New(s.tlsErr)
}

// acmeCacheDir keeps certificates in the data directory so a restart reuses
// them instead of re-issuing (Let's Encrypt rate-limits duplicates).
func (s *Server) acmeCacheDir() string {
	dir := "."
	if s.cfg != nil && s.cfg.Paths != nil && s.cfg.Paths.DataDir != "" {
		dir = s.cfg.Paths.DataDir
	}
	return filepath.Join(dir, "acme")
}

// StartAutoTLSFromSettings resumes HTTPS on startup when a domain was saved.
func (s *Server) StartAutoTLSFromSettings(ctx context.Context) {
	domain, _ := s.db.GetSetting(settingPublicDomain)
	email, _ := s.db.GetSetting(settingACMEEmail)
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return
	}
	s.restartAutoTLS(ctx, domain, email)
	if err := s.autoTLSError(); err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ HTTPS for %s could not start: %v\n", domain, err)
	}
}

// autoTLSStatus reports the live state plus the cached certificate, so the UI
// can show whether a certificate was actually issued and when it expires.
func (s *Server) autoTLSStatus() tlsStatus {
	s.tlsMu.Lock()
	rt, lastErr := s.tls, s.tlsErr
	s.tlsMu.Unlock()

	st := tlsStatus{
		HTTPPort:  httpChallengePort(),
		HTTPSPort: httpsListenPort(),
		LastError: lastErr,
	}
	if rt != nil {
		st.Domain = rt.domain
		st.Email = rt.email
		st.Active = true
	} else {
		// Not running: report what is configured so the UI can distinguish
		// "never set up" from "set up but failed to bind".
		if d, err := s.db.GetSetting(settingPublicDomain); err == nil {
			st.Domain = strings.TrimSpace(d)
		}
		if e, err := s.db.GetSetting(settingACMEEmail); err == nil {
			st.Email = strings.TrimSpace(e)
		}
	}
	if st.Domain != "" {
		if expiry, source, ok := cachedCertInfo(s.acmeCacheDir(), st.Domain); ok {
			st.CertExpiry = expiry.UTC().Format(time.RFC3339)
			st.CertSource = source
		}
	}
	if st.Domain != "" && !st.Active && st.LastError == "" {
		st.Hint = "Domain saved but HTTPS is not running. Restart tDocs to apply it."
	}
	if st.Active && st.CertExpiry == "" {
		st.Hint = "Listening for HTTPS. The certificate is issued on the first visit to the domain."
	}
	return st
}

// cachedCertInfo finds the newest usable certificate for domain in the autocert
// cache directory. Filenames are an autocert implementation detail, so the
// directory is scanned and certificates are matched by their SANs instead.
func cachedCertInfo(dir, domain string) (time.Time, string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}, "", false
	}
	type found struct {
		expiry time.Time
		source string
	}
	var best found
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for rest := data; len(rest) > 0; {
			var block *pem.Block
			block, rest = pem.Decode(rest)
			if block == nil {
				break
			}
			if block.Type != "CERTIFICATE" {
				continue
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil || cert.NotAfter.Before(time.Now()) {
				continue
			}
			if cert.VerifyHostname(domain) != nil {
				continue
			}
			if cert.NotAfter.After(best.expiry) {
				best = found{expiry: cert.NotAfter, source: e.Name()}
			}
		}
	}
	if best.expiry.IsZero() {
		return time.Time{}, "", false
	}
	return best.expiry, best.source, true
}
