package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tdocs/internal/app"
	"tdocs/internal/db"
)

func TestWebServer_AuthAndFolderAPI(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open db failed: %v", err)
	}
	defer database.Close()

	cfg := &app.Config{
		Port:          "8080",
		DBPath:        dbPath,
		SecretKey:     "test-secret-key-32b",
		AdminPassword: "supersecretpassword",
	}

	server, err := NewServer(cfg, database, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// 1. Unauthenticated request to / should redirect to /login
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("Expected redirect to /login, got code %d, loc %s", rec.Code, rec.Header().Get("Location"))
	}

	// 2. Submit wrong password
	form := url.Values{"password": {"wrong"}}
	req = httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "Invalid admin password") {
		t.Fatalf("Expected error message in login HTML, got body: %s", rec.Body.String())
	}

	// 3. Submit correct password
	form = url.Values{"password": {"supersecretpassword"}}
	req = httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	cookies := rec.Result().Cookies()
	var authCookie, csrfCookie *http.Cookie
	for _, c := range cookies {
		switch c.Name {
		case "tdocs_session":
			authCookie = c
		case "tdocs_csrf":
			csrfCookie = c
		}
	}
	if authCookie == nil || len(authCookie.Value) < 32 {
		t.Fatalf("Expected random session cookie to be set, got: %v", cookies)
	}
	if !authCookie.HttpOnly {
		t.Fatalf("Expected session cookie to be HttpOnly")
	}
	if csrfCookie == nil || len(csrfCookie.Value) < 32 {
		t.Fatalf("Expected CSRF cookie to be set, got: %v", cookies)
	}
	// Every test request carrying the cookie must also carry the CSRF header
	// for mutating calls; attach it via a helper below.
	csrfToken := csrfCookie.Value

	// 4. Authenticated request to / should render dashboard HTML
	req = httptest.NewRequest("GET", "/", nil)
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "tDocs") {
		t.Fatalf("Expected dashboard HTML 200 OK, got code %d", rec.Code)
	}

	// 4b. Forged static cookies ("authenticated", incl. pre-rebrand names) are
	// rejected: sessions must be server-issued random tokens.
	for _, name := range []string{"tdocs_session", "robdocs_session", "teledrive_session"} {
		req = httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: name, Value: "authenticated"})
		rec = httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("Expected forged cookie %s to be rejected, got code %d", name, rec.Code)
		}
	}

	// 4c. Cookie-authed mutation without CSRF token is rejected.
	folderPayload, _ := json.Marshal(map[string]any{"name": "Projects"})
	req = httptest.NewRequest("POST", "/api/folders", bytes.NewReader(folderPayload))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 without CSRF token, got %d", rec.Code)
	}

	// 5. Create folder via API (with CSRF token)
	folderPayload, _ = json.Marshal(map[string]any{"name": "Projects"})
	req = httptest.NewRequest("POST", "/api/folders", bytes.NewReader(folderPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for folder, got %d: %s", rec.Code, rec.Body.String())
	}

	// 6. List folders via API
	req = httptest.NewRequest("GET", "/api/folders", nil)
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Projects") {
		t.Fatalf("Expected folder list with 'Projects', got: %s", rec.Body.String())
	}

	// 7. Create a file & share link, then test GET /api/shares & DELETE /api/shares/{id}
	file, err := database.CreateFile(nil, "doc.pdf", 2048, "application/pdf", 11, "tg_1", "hash_1", "sha_1")
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	sharePayload, _ := json.Marshal(map[string]any{"file_id": file.ID})
	req = httptest.NewRequest("POST", "/api/share", bytes.NewReader(sharePayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for share, got %d", rec.Code)
	}

	// GET /api/shares
	req = httptest.NewRequest("GET", "/api/shares", nil)
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "doc.pdf") {
		t.Fatalf("Expected shares list containing 'doc.pdf', got %d: %s", rec.Code, rec.Body.String())
	}

	var shares []db.ShareLinkInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &shares)
	if len(shares) == 0 {
		t.Fatalf("Expected at least 1 share in list")
	}

	// DELETE /api/shares/{id}
	req = httptest.NewRequest("DELETE", "/api/shares/"+shares[0].ID, nil)
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected 204 No Content for delete share, got %d", rec.Code)
	}
}

func TestWebServer_SnapshotsAPI(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open db failed: %v", err)
	}
	defer database.Close()

	cfg := &app.Config{
		Port:          "8080",
		DBPath:        dbPath,
		SecretKey:     "test-secret-key-32b",
		AdminPassword: "supersecretpassword",
	}

	server, err := NewServer(cfg, database, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	sess, err := server.sessions.Create("test", "test")
	if err != nil {
		t.Fatalf("Create session failed: %v", err)
	}
	authCookie := &http.Cookie{Name: "tdocs_session", Value: sess.Token}

	// 1. GET /api/snapshots without a working Telegram backend must report an
	// honest unavailable state — never an empty list that implies success.
	req := httptest.NewRequest("GET", "/api/snapshots", nil)
	req.AddCookie(authCookie)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("Expected 503 for snapshots list without Telegram, got %d", rec.Code)
	}
	var snapErr struct {
		Error string `json:"error"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &snapErr); err != nil {
		t.Fatalf("snapshots error body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if snapErr.Error != "telegram_storage_unavailable" || snapErr.State != string(TgNotConfigured) {
		t.Fatalf("expected not-configured telegram state, got error=%q state=%q", snapErr.Error, snapErr.State)
	}

	// 2. POST /api/snapshots with nil tg -> unavailable, not a fake success
	req = httptest.NewRequest("POST", "/api/snapshots", nil)
	req.Header.Set("X-CSRF-Token", sess.CSRF)
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("Expected 503 for snapshot create without Telegram, got %d", rec.Code)
	}

	// 3. POST /api/snapshots/invalid/restore -> returns 400 Bad Request
	req = httptest.NewRequest("POST", "/api/snapshots/abc/restore", nil)
	req.Header.Set("X-CSRF-Token", sess.CSRF)
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 Bad Request for invalid snapshot ID, got %d", rec.Code)
	}

	// 3b. The Telegram configuration surface is superuser-only: an API token
	// (which is admin-equivalent for file operations) must be rejected.
	_, _, tokenRaw, err := database.NewAPIToken("ci-token")
	if err != nil {
		t.Fatalf("NewAPIToken failed: %v", err)
	}
	tgReq := httptest.NewRequest("GET", "/api/telegram/state", nil)
	tgReq.Header.Set("Authorization", "Bearer "+tokenRaw)
	tgRec := httptest.NewRecorder()
	server.ServeHTTP(tgRec, tgReq)
	if tgRec.Code != http.StatusForbidden {
		t.Fatalf("expected API token to be denied on /api/telegram/state, got %d", tgRec.Code)
	}

	// The administrator session may read it, and the state must be honest.
	tgReq = httptest.NewRequest("GET", "/api/telegram/state", nil)
	tgReq.AddCookie(authCookie)
	tgRec = httptest.NewRecorder()
	server.ServeHTTP(tgRec, tgReq)
	if tgRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin on /api/telegram/state, got %d", tgRec.Code)
	}
	var tgState struct {
		State TelegramState `json:"state"`
	}
	if err := json.Unmarshal(tgRec.Body.Bytes(), &tgState); err != nil {
		t.Fatalf("telegram state body is not JSON: %v", err)
	}
	if tgState.State != TgNotConfigured {
		t.Fatalf("expected not_configured state without credentials, got %q", tgState.State)
	}

	// 4. Create snapshot locally and test /api/snapshots/upload-restore
	gzPath, err := database.CreateSnapshot()
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}
	defer os.Remove(gzPath)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("snapshot", filepath.Base(gzPath))
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	gzFile, err := os.Open(gzPath)
	if err != nil {
		t.Fatalf("Open gzPath failed: %v", err)
	}
	_, _ = io.Copy(part, gzFile)
	gzFile.Close()
	writer.Close()

	req = httptest.NewRequest("POST", "/api/snapshots/upload-restore", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", sess.CSRF)
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for upload-restore, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "restored successfully") {
		t.Fatalf("Expected success message in response, got: %s", rec.Body.String())
	}
}

func TestWebServer_CDNAndDocs(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open db failed: %v", err)
	}
	defer database.Close()

	cfg := &app.Config{
		Port:          "8080",
		DBPath:        dbPath,
		SecretKey:     "test-secret-key-32b",
		AdminPassword: "supersecretpassword",
		CDNPublic:     true,
	}

	server, err := NewServer(cfg, database, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Seed one video file directly in the catalog.
	file, err := database.CreateFile(nil, "film.mp4", 1024, "video/mp4", 1, "123", "456", "")
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	// 1. Catalog without credentials -> 401.
	req := httptest.NewRequest("GET", "/api/cdn/files?mime=video/", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 without key, got %d", rec.Code)
	}

	// 2. Catalog with Bearer key -> 200 with embed URLs, no Telegram secrets.
	req = httptest.NewRequest("GET", "/api/cdn/files?mime=video/", nil)
	req.Header.Set("Authorization", "Bearer supersecretpassword")
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for catalog, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/cdn/"+file.ID+"/stream") {
		t.Fatalf("Expected stream_url in catalog, got: %s", body)
	}
	if strings.Contains(body, "telegram_file_id") || strings.Contains(body, "telegram_access_hash") {
		t.Fatalf("Catalog must not leak Telegram coordinates, got: %s", body)
	}

	// 3. Detail entry -> 200.
	req = httptest.NewRequest("GET", "/api/cdn/files/"+file.ID, nil)
	req.Header.Set("Authorization", "Bearer supersecretpassword")
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for detail, got %d", rec.Code)
	}

	// 4. Public stream of missing file -> 404 without touching MTProto.
	req = httptest.NewRequest("GET", "/cdn/doesnotexist/stream", nil)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("Expected 404 for missing CDN file, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("Expected CORS header on public CDN responses")
	}

	// 4b. Browser preflight on the key-protected catalog -> 204 with CORS,
	// no credentials attached.
	req = httptest.NewRequest("OPTIONS", "/api/cdn/files", nil)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected 204 for CORS preflight, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("Expected CORS header on preflight response")
	}

	_ = database.DeleteFile(file.ID)
	req = httptest.NewRequest("GET", "/api/cdn/files", nil)
	req.Header.Set("Authorization", "Bearer supersecretpassword")
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"files":[]`) {
		t.Fatalf("Expected empty files array, got: %s", rec.Body.String())
	}
	cdnSess, err := server.sessions.Create("test", "test")
	if err != nil {
		t.Fatalf("Create session failed: %v", err)
	}
	authCookie := &http.Cookie{Name: "tdocs_session", Value: cdnSess.Token}
	req = httptest.NewRequest("GET", "/api/files", nil)
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("Expected [] for empty file list, got: %s", rec.Body.String())
	}

	// 4d. Single file detail + share create URLs for API clients.
	file2, err := database.CreateFile(nil, "film2.mp4", 2048, "video/mp4", 2, "789", "012", "")
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}
	req = httptest.NewRequest("GET", "/api/files/"+file2.ID, nil)
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for file detail, got %d", rec.Code)
	}
	sharePayload, _ := json.Marshal(map[string]any{"file_id": file2.ID})
	req = httptest.NewRequest("POST", "/api/share", bytes.NewReader(sharePayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", cdnSess.CSRF)
	req.AddCookie(authCookie)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 for share create, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"page_url"`, `"stream_url"`, `"download_url"`, "/s/"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("Expected %s in share response, got: %s", want, rec.Body.String())
		}
	}
	// 5. OpenAPI + docs.
	req = httptest.NewRequest("GET", "/api/openapi.json", nil)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/cdn/{id}/stream") {
		t.Fatalf("Expected OpenAPI spec with CDN paths, got %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "/docs", nil)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "swagger") {
		t.Fatalf("Expected Swagger UI docs page, got %d", rec.Code)
	}
}

func TestWebServer_WizardStartWithoutTelegramBackend(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open db failed: %v", err)
	}
	defer database.Close()

	cfg := &app.Config{
		Port:          "8080",
		DBPath:        dbPath,
		SecretKey:     "test-secret-key-32b",
		AdminPassword: "supersecretpassword",
	}

	// nil manager: server runs degraded without Telegram credentials.
	server, err := NewServer(cfg, database, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Preflight reports the backend as unavailable (drives the UI message).
	req := httptest.NewRequest("GET", "/api/telegram/wizard/needed", nil)
	req.Header.Set("Authorization", "Bearer supersecretpassword")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for needed, got %d: %s", rec.Code, rec.Body.String())
	}
	var needed map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &needed); err != nil {
		t.Fatalf("needed is not JSON: %v", err)
	}
	if needed["client_available"] != false {
		t.Fatalf("Expected client_available=false, got %v", needed["client_available"])
	}

	// Wizard start must fail with machine-readable JSON (never plain text):
	// the dashboard renders .error/.hint instead of "Failed to start wizard".
	payload, _ := json.Marshal(map[string]any{"phone": "+628123456789"})
	req = httptest.NewRequest("POST", "/api/telegram/wizard/start", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer supersecretpassword")
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("Expected 503 for wizard start without backend, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("wizard start error is not JSON: %v (body %q)", err, rec.Body.String())
	}
	if body["code"] != "telegram_not_configured" {
		t.Fatalf("Expected code=telegram_not_configured, got %v", body)
	}
	if _, ok := body["hint"].(string); !ok {
		t.Fatalf("Expected actionable hint in response, got %v", body)
	}
}

func TestWebServer_UpdateStatusBanner(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open db failed: %v", err)
	}
	defer database.Close()

	// Stub the GitHub lookup: deterministic, no network.
	oldFetch := updateFetchFunc
	calls := 0
	updateFetchFunc = func(ctx context.Context) (*app.ReleaseInfo, error) {
		calls++
		return &app.ReleaseInfo{Tag: "v9.9.9"}, nil
	}
	defer func() { updateFetchFunc = oldFetch }()

	cfg := &app.Config{
		Port:          "8080",
		DBPath:        dbPath,
		SecretKey:     "test-secret-key-32b",
		AdminPassword: "supersecretpassword",
		Version:       "v2.1.0",
	}
	server, err := NewServer(cfg, database, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	getStatus := func() map[string]any {
		req := httptest.NewRequest("GET", "/api/update/status", nil)
		req.Header.Set("Authorization", "Bearer supersecretpassword")
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 for update status, got %d: %s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("update status is not JSON: %v", err)
		}
		return body
	}

	// Poll until the background refresh lands (max ~5s).
	var body map[string]any
	for i := 0; i < 50; i++ {
		body = getStatus()
		if avail, _ := body["available"].(bool); avail {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if avail, _ := body["available"].(bool); !avail {
		t.Fatalf("Expected available=true, got %v", body)
	}
	if body["latest"] != "v9.9.9" {
		t.Fatalf("Expected latest=v9.9.9, got %v", body)
	}
	if hint, _ := body["hint"].(string); hint == "" {
		t.Fatalf("Expected upgrade hint, got %v", body)
	}
	if body["current"] != "v2.1.0" {
		t.Fatalf("Expected current=v2.1.0, got %v", body)
	}
	if calls == 0 {
		t.Fatal("Expected at least one release lookup")
	}

	// Unauthenticated callers get no version intelligence.
	req := httptest.NewRequest("GET", "/api/update/status", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 without admin auth, got %d", rec.Code)
	}
}
