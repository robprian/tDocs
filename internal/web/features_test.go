package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"tdocs/internal/app"
	"tdocs/internal/db"
)

type featureFixture struct {
	server *Server
	db     *db.DB
	cookie *http.Cookie
	csrf   string
}

func newFeatureFixture(t *testing.T) *featureFixture {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "feat.db"))
	if err != nil {
		t.Fatalf("Open db failed: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	cfg := &app.Config{
		Port: "8080", DBPath: ":memory:", SecretKey: "test-secret-key-32b-long-enough",
		AdminPassword: "adminpass123",
	}
	server, err := NewServer(cfg, database, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	sess, err := server.sessions.Create("test", "test")
	if err != nil {
		t.Fatal(err)
	}
	return &featureFixture{
		server: server, db: database,
		cookie: &http.Cookie{Name: "tdocs_session", Value: sess.Token},
		csrf:   sess.CSRF,
	}
}

func (f *featureFixture) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-CSRF-Token", f.csrf)
	req.AddCookie(f.cookie)
	rec := httptest.NewRecorder()
	f.server.ServeHTTP(rec, req)
	return rec
}

func TestFeatures_TrashFlow(t *testing.T) {
	f := newFeatureFixture(t)
	file, err := f.db.CreateFile(nil, "gone.txt", 42, "text/plain", 9, "9", "9", "")
	if err != nil {
		t.Fatal(err)
	}

	// DELETE now trashes instead of destroying.
	if rec := f.do(t, "DELETE", "/api/files/"+file.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 trash, got %d", rec.Code)
	}
	if rec := f.do(t, "GET", "/api/files/"+file.ID, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("trashed file must 404, got %d", rec.Code)
	}
	rec := f.do(t, "GET", "/api/trash", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "gone.txt") {
		t.Fatalf("trash must list the file, got %d %s", rec.Code, rec.Body.String())
	}
	// Restore brings it back.
	if rec := f.do(t, "POST", "/api/trash/restore", map[string]string{"kind": "file", "id": file.ID}); rec.Code != http.StatusOK {
		t.Fatalf("restore failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, "GET", "/api/files/"+file.ID, nil); rec.Code != http.StatusOK {
		t.Fatalf("restored file must be visible, got %d", rec.Code)
	}
	// Purge destroys it.
	if rec := f.do(t, "DELETE", "/api/files/"+file.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("re-trash failed: %d", rec.Code)
	}
	if rec := f.do(t, "DELETE", "/api/trash/file/"+file.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("purge failed: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := f.db.GetFileAny(file.ID); err == nil {
		t.Fatalf("purged file must be gone from DB")
	}
}

func TestFeatures_ShareDownloadLimit(t *testing.T) {
	f := newFeatureFixture(t)
	file, err := f.db.CreateFile(nil, "limited.mp4", 100, "video/mp4", 7, "7", "7", "")
	if err != nil {
		t.Fatal(err)
	}
	max := 1
	share, err := f.db.CreateShareLink(file.ID, nil, nil, &max, false)
	if err != nil {
		t.Fatal(err)
	}
	// Limit not yet reached (telegram is nil -> 503 downstream, but NOT 410).
	req := httptest.NewRequest("GET", "/s/"+share.Token+"/download", nil)
	rec := httptest.NewRecorder()
	f.server.ServeHTTP(rec, req)
	if rec.Code == http.StatusGone {
		t.Fatalf("fresh link must not be Gone, got %d", rec.Code)
	}
	// Simulate the single allowed download completing.
	if err := f.db.IncrementShareDownload(share.Token); err != nil {
		t.Fatal(err)
	}
	// The link is now exhausted.
	req = httptest.NewRequest("GET", "/s/"+share.Token+"/download", nil)
	rec = httptest.NewRecorder()
	f.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("expected 410 after limit reached, got %d", rec.Code)
	}
}

func TestFeatures_TokensAndPassword(t *testing.T) {
	f := newFeatureFixture(t)

	// Create token (secret shown once).
	rec := f.do(t, "POST", "/api/tokens", map[string]string{"name": "ci"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("token create failed: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	secret := created["token"]
	if len(secret) < 40 {
		t.Fatalf("weak issued token: %q", secret)
	}

	// Bearer token authenticates API calls.
	req := httptest.NewRequest("GET", "/api/files", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec2 := httptest.NewRecorder()
	f.server.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("bearer token must authenticate, got %d", rec2.Code)
	}

	// Revoke kills it.
	if rec := f.do(t, "DELETE", "/api/tokens/"+created["id"], nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke failed: %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "/api/files", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec2 = httptest.NewRecorder()
	f.server.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token must 401, got %d", rec2.Code)
	}

	// Password change: wrong current rejected, then rotation works.
	if rec := f.do(t, "POST", "/api/settings/password", map[string]string{"current": "nope", "new": "newpass123"}); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong current password must 403, got %d", rec.Code)
	}
	if rec := f.do(t, "POST", "/api/settings/password", map[string]string{"current": "adminpass123", "new": "newpass123"}); rec.Code != http.StatusOK {
		t.Fatalf("password change failed: %d %s", rec.Code, rec.Body.String())
	}
	hash, _ := f.db.GetSetting("admin_password_hash")
	if hash == "newpass123" || hash == "" {
		t.Fatalf("password must be stored hashed")
	}
	if !f.server.adminPasswordOK("newpass123") || f.server.adminPasswordOK("adminpass123") {
		t.Fatalf("rotation did not take effect")
	}
}

func TestFeatures_SessionsStatsTickets(t *testing.T) {
	f := newFeatureFixture(t)
	if _, err := f.db.CreateFile(nil, "s.mp4", 500, "video/mp4", 3, "3", "3", ""); err != nil {
		t.Fatal(err)
	}

	if rec := f.do(t, "GET", "/api/sessions", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"current":true`) {
		t.Fatalf("sessions must list current session: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, "GET", "/api/stats", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"files":1`) {
		t.Fatalf("stats broken: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, "GET", "/api/health", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "goroutines") {
		t.Fatalf("health broken: %d %s", rec.Code, rec.Body.String())
	}

	// Ticket mint + offline verify (no session needed to use it).
	files, _ := f.db.ListAllFiles("", "", 10)
	rec := f.do(t, "POST", "/api/files/"+files[0].ID+"/ticket", map[string]any{"expires_in": 60})
	if rec.Code != http.StatusOK {
		t.Fatalf("ticket mint failed: %d %s", rec.Code, rec.Body.String())
	}
	var ticket map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &ticket)
	u := ticket["url"]
	ticketParam := u[strings.Index(u, "ticket=")+len("ticket="):]
	if !f.server.verifyTicket(ticketParam, files[0].ID) {
		t.Fatalf("fresh ticket must verify")
	}
	if f.server.verifyTicket(ticketParam+"tampered", files[0].ID) {
		t.Fatalf("tampered ticket must not verify")
	}
	if f.server.verifyTicket(ticketParam, "other-id") {
		t.Fatalf("ticket must be bound to its file")
	}
}

func TestFeatures_CopyAndPreviewOnly(t *testing.T) {
	f := newFeatureFixture(t)
	src, err := f.db.CreateFile(nil, "orig.txt", 11, "text/plain", 5, "5", "5", "")
	if err != nil {
		t.Fatal(err)
	}

	// Copy duplicates the record, not the bytes.
	rec := f.do(t, "POST", "/api/files/"+src.ID+"/copy", map[string]any{"name": "orig-copy.txt"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("copy failed: %d %s", rec.Code, rec.Body.String())
	}
	var cp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &cp)
	if cp["name"] != "orig-copy.txt" {
		t.Fatalf("copy name wrong: %v", cp)
	}

	// Preview-only share blocks downloads but serves the landing page.
	share, err := f.db.CreateShareLink(src.ID, nil, nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/s/"+share.Token+"/download", nil)
	rec2 := httptest.NewRecorder()
	f.server.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("preview-only download must 403, got %d", rec2.Code)
	}
	req = httptest.NewRequest("GET", "/s/"+share.Token, nil)
	rec2 = httptest.NewRecorder()
	f.server.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK || !strings.Contains(rec2.Body.String(), "Preview only") {
		t.Fatalf("landing must show preview-only badge, got %d", rec2.Code)
	}

	// Storage verification without Telegram fails cleanly (503, not panic).
	if rec := f.do(t, "POST", "/api/storage/verify", map[string]any{"limit": 5}); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("verify without tg must 503, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestLoginRateLimitRecovery locks in the two behaviours that made a mistyped
// password look like a broken login: a bare 429 with no explanation, and a
// failure window that survived a later successful sign-in.
func TestLoginRateLimitRecovery(t *testing.T) {
	f := newFeatureFixture(t)
	form := func(pw string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/login", strings.NewReader("password="+pw))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "203.0.113.9:1234"
		rec := httptest.NewRecorder()
		f.server.ServeHTTP(rec, req)
		return rec
	}

	// Wrong password renders the page (200) with an actionable message.
	rec := form("nope")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Invalid admin password") {
		t.Fatalf("wrong password must render login page, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "tdocs passwd") {
		t.Fatalf("recovery hint missing from login error page")
	}

	// Burn the window, then confirm the limiter answers with the real login
	// page (not a bare 429) so the operator knows what happened.
	for i := 0; i < 14; i++ {
		form("nope")
	}
	rec = form("nope")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exhausting the window, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatalf("429 must advertise Retry-After")
	}
	if !strings.Contains(rec.Body.String(), "Too many failed attempts") {
		t.Fatalf("429 must render the login page with a reason, got: %s", rec.Body.String())
	}

	// A correct password must clear the window: no lingering lockout.
	rec = form("adminpass123")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("correct password during/after limit must still fail? got %d", rec.Code)
	}
	rec = form("nope")
	if rec.Code != http.StatusOK {
		t.Fatalf("window was not reset after success, got %d", rec.Code)
	}
}
