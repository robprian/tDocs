package web

import (
	"context"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"

	"tdocs/internal/app"
)

// ------------------------------------------------------------ update check ---
//
// Dashboard update banner (GET /api/update/status, superuser-only).
// The server checks GitHub at most once per 24h and never blocks a request
// on the network: a stale check triggers a background refresh while the
// cached (or "checking") snapshot is served immediately.

// updateCacheTTL bounds GitHub API traffic to one lookup per day per process.
const updateCacheTTL = 24 * time.Hour

type updateSnapshot struct {
	Current     string `json:"current"`
	Latest      string `json:"latest,omitempty"`
	Available   bool   `json:"available"`
	URL         string `json:"url,omitempty"`
	Hint        string `json:"hint,omitempty"`
	InstallKind string `json:"install_kind,omitempty"`
	CheckedAt   string `json:"checked_at,omitempty"`
	Checking    bool   `json:"checking,omitempty"`
}

// updateFetchFunc is a stub point for tests (defaults to the live lookup).
var updateFetchFunc = app.FetchLatestRelease

func (s *Server) currentVersion() string {
	if s.cfg != nil && s.cfg.Version != "" {
		return s.cfg.Version
	}
	return "dev"
}

func (s *Server) installKind() string {
	exe, err := os.Executable()
	if err != nil {
		return app.InstallUnknown
	}
	mode := ""
	if s.cfg != nil && s.cfg.Paths != nil {
		mode = s.cfg.Paths.Mode
	}
	return app.InstallKind(exe, mode)
}

// refreshUpdateCache contacts GitHub in the background and records the
// result. Only one refresh runs at a time; failures keep the old cache.
func (s *Server) refreshUpdateCache() {
	s.updateMu.Lock()
	if s.updateFlight {
		s.updateMu.Unlock()
		return
	}
	s.updateFlight = true
	s.updateMu.Unlock()

	go func() {
		defer func() {
			s.updateMu.Lock()
			s.updateFlight = false
			s.updateMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		rel, err := updateFetchFunc(ctx)
		s.updateMu.Lock()
		defer s.updateMu.Unlock()
		if err != nil {
			s.updateErr = err
			return
		}
		s.updateErr = nil
		s.updateLatest = rel
		s.updateCheckedAt = time.Now()
		if app.IsNewer(s.currentVersion(), rel.Tag) {
			log.Printf("Update available: tDocs %s (running %s) — %s",
				rel.Tag, s.currentVersion(), app.ReleaseURL(rel.Tag))
		}
	}()
}

func (s *Server) updateSnapshot() updateSnapshot {
	current := s.currentVersion()
	snap := updateSnapshot{Current: current, InstallKind: s.installKind()}

	s.updateMu.Lock()
	rel, checkedAt, flight := s.updateLatest, s.updateCheckedAt, s.updateFlight
	s.updateMu.Unlock()

	if rel != nil {
		snap.CheckedAt = checkedAt.UTC().Format(time.RFC3339)
		if app.IsNewer(current, rel.Tag) {
			// Latest is only advertised when it is genuinely newer: a release
			// equal to the running build must never surface as something to
			// install, on any client.
			snap.Latest = rel.Tag
			snap.Available = true
			snap.URL = app.ReleaseURL(rel.Tag)
			snap.Hint = app.UpgradeHint(snap.InstallKind, rel.Tag, runtime.GOARCH)
		}
	}
	if time.Since(checkedAt) > updateCacheTTL || (rel == nil && !flight) {
		snap.Checking = true
		go s.refreshUpdateCache()
	} else if flight {
		snap.Checking = true
	}
	return snap
}

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		s.updateMu.Lock()
		s.updateLatest = nil
		s.updateCheckedAt = time.Time{}
		s.updateMu.Unlock()
	}
	jsonOut(w, http.StatusOK, s.updateSnapshot())
}

// CheckForUpdatesAsync warms the update cache without blocking startup.
// Call once when the server comes up; the dashboard banner and journal logs
// pick up the result.
func (s *Server) CheckForUpdatesAsync() {
	go s.refreshUpdateCache()
}
