package web

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// doPublic issues a request without the fixture's admin cookie, proving the
// public file-request surface works for a signed-out guest.
func (f *featureFixture) doPublic(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
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
	rec := httptest.NewRecorder()
	f.server.ServeHTTP(rec, req)
	return rec
}

// doMultipart posts one file chunk the way the dashboard and the public
// request page do: session_id + chunk_index fields, file field "chunk".
func (f *featureFixture) doMultipart(t *testing.T, path, sessionID string, chunkIndex int, data []byte, authed bool) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("session_id", sessionID)
	_ = w.WriteField("chunk_index", itoa(chunkIndex))
	part, err := w.CreateFormFile("chunk", "chunk.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if authed {
		req.Header.Set("X-CSRF-Token", f.csrf)
		req.AddCookie(f.cookie)
	}
	rec := httptest.NewRecorder()
	f.server.ServeHTTP(rec, req)
	return rec
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	body, _ := io.ReadAll(rec.Result().Body)
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

func enableDiskBackend(t *testing.T, f *featureFixture) string {
	t.Helper()
	dir := t.TempDir()
	if err := f.db.SetSetting("storage_backend", "disk"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.SetSetting("disk_storage_path", dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// uploadViaInitChunkComplete runs the shared upload core end to end and
// returns the created file id.
func uploadViaInitChunkComplete(t *testing.T, f *featureFixture, initPath, chunkPath, completePath string, initBody map[string]any, data []byte, authed bool) string {
	t.Helper()
	var initRec *httptest.ResponseRecorder
	if authed {
		initRec = f.do(t, "POST", initPath, initBody)
	} else {
		initRec = f.doPublic(t, "POST", initPath, initBody)
	}
	if initRec.Code != http.StatusOK {
		t.Fatalf("init %s: got %d, body %s", initPath, initRec.Code, initRec.Body.String())
	}
	var initResp struct {
		SessionID   string `json:"session_id"`
		StorageKind string `json:"storage_kind"`
	}
	decodeJSON(t, initRec, &initResp)
	if initResp.StorageKind != "disk" {
		t.Fatalf("init storage_kind = %q, want disk", initResp.StorageKind)
	}
	if rec := f.doMultipart(t, chunkPath, initResp.SessionID, 0, data, authed); rec.Code != http.StatusOK {
		t.Fatalf("chunk: got %d, body %s", rec.Code, rec.Body.String())
	}
	var completeRec *httptest.ResponseRecorder
	completeBody := map[string]any{"session_id": initResp.SessionID}
	if authed {
		completeRec = f.do(t, "POST", completePath, completeBody)
	} else {
		completeRec = f.doPublic(t, "POST", completePath, completeBody)
	}
	if completeRec.Code != http.StatusCreated {
		t.Fatalf("complete: got %d, body %s", completeRec.Code, completeRec.Body.String())
	}
	var file struct {
		ID          string `json:"id"`
		StorageKind string `json:"storage_kind"`
	}
	decodeJSON(t, completeRec, &file)
	if file.StorageKind != "disk" {
		t.Fatalf("created file storage_kind = %q, want disk", file.StorageKind)
	}
	return file.ID
}

func TestV32_DiskUploadFlow(t *testing.T) {
	f := newFeatureFixture(t)
	enableDiskBackend(t, f)
	data := []byte("hello world")
	id := uploadViaInitChunkComplete(t, f,
		"/api/upload/init", "/api/upload/chunk", "/api/upload/complete",
		map[string]any{"name": "hello.txt", "size": len(data), "mime_type": "text/plain"},
		data, true)
	rec := f.do(t, "GET", "/api/files/"+id+"/stream", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("stream: got %d, body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != string(data) {
		t.Fatalf("stream body = %q, want %q", got, data)
	}
	got, err := f.db.GetFile(id)
	if err != nil || got.StorageKind != "disk" || got.LocalPath == "" {
		t.Fatalf("db record = %+v, err %v", got, err)
	}
}

func TestV32_PublicFileRequestUpload(t *testing.T) {
	f := newFeatureFixture(t)
	enableDiskBackend(t, f)
	fr, err := f.db.CreateFileRequest(nil, "Homework", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Guest flow: no admin cookie anywhere.
	id := uploadViaInitChunkComplete(t, f,
		"/api/requests/"+fr.Token+"/upload/init",
		"/api/requests/"+fr.Token+"/upload/chunk",
		"/api/requests/"+fr.Token+"/upload/complete",
		map[string]any{"name": "hw.txt", "size": 3, "mime_type": "text/plain"},
		[]byte("abc"), false)
	got, err := f.db.GetFile(id)
	if err != nil || got.Name != "hw.txt" {
		t.Fatalf("uploaded file = %+v, err %v", got, err)
	}
	// Wrong-token session: a session minted for request B must not upload
	// through request A's token.
	frB, err := f.db.CreateFileRequest(nil, "Other", nil)
	if err != nil {
		t.Fatal(err)
	}
	initRec := f.doPublic(t, "POST", "/api/requests/"+frB.Token+"/upload/init",
		map[string]any{"name": "x.txt", "size": 1, "mime_type": "text/plain"})
	if initRec.Code != http.StatusOK {
		t.Fatalf("init B: got %d", initRec.Code)
	}
	var initResp struct {
		SessionID string `json:"session_id"`
	}
	decodeJSON(t, initRec, &initResp)
	if rec := f.doMultipart(t, "/api/requests/"+fr.Token+"/upload/chunk", initResp.SessionID, 0, []byte("x"), false); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-token chunk: got %d, want 403", rec.Code)
	}
	// Expired request: 410 on both the info endpoint and upload init.
	past := time.Now().Add(-time.Hour)
	expired, err := f.db.CreateFileRequest(nil, "Old", &past)
	if err != nil {
		t.Fatal(err)
	}
	if rec := f.doPublic(t, "GET", "/api/requests/"+expired.Token, nil); rec.Code != http.StatusGone {
		t.Fatalf("expired info: got %d, want 410", rec.Code)
	}
	if rec := f.doPublic(t, "POST", "/api/requests/"+expired.Token+"/upload/init",
		map[string]any{"name": "y.txt", "size": 1}); rec.Code != http.StatusGone {
		t.Fatalf("expired init: got %d, want 410", rec.Code)
	}
	// Unknown token: 404.
	if rec := f.doPublic(t, "GET", "/api/requests/does-not-exist", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown token: got %d, want 404", rec.Code)
	}
	// Public page renders for a live token and 410s for an expired one.
	if rec := f.doPublic(t, "GET", "/r/"+fr.Token, nil); rec.Code != http.StatusOK {
		t.Fatalf("request page: got %d, want 200", rec.Code)
	} else if !strings.Contains(rec.Body.String(), "Homework") {
		t.Fatalf("request page should name the request")
	}
	if rec := f.doPublic(t, "GET", "/r/"+expired.Token, nil); rec.Code != http.StatusGone {
		t.Fatalf("expired page: got %d, want 410", rec.Code)
	}
}

func TestV32_TagsAndFilter(t *testing.T) {
	f := newFeatureFixture(t)
	file, err := f.db.CreateFile(nil, "report.pdf", 100, "application/pdf", 1, "f1", "h1", "sha")
	if err != nil {
		t.Fatal(err)
	}
	if rec := f.do(t, "POST", "/api/files/"+file.ID+"/tags", map[string]any{"tag": "Work Docs"}); rec.Code != http.StatusCreated {
		t.Fatalf("add tag: got %d, body %s", rec.Code, rec.Body.String())
	}
	rec := f.do(t, "GET", "/api/files/"+file.ID+"/tags", nil)
	var tags []string
	decodeJSON(t, rec, &tags)
	if len(tags) != 1 || tags[0] != "work-docs" {
		t.Fatalf("tags = %v", tags)
	}
	rec = f.do(t, "GET", "/api/files?tag=work-docs", nil)
	var listed []map[string]any
	decodeJSON(t, rec, &listed)
	found := false
	for _, item := range listed {
		if item["id"] == file.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("tag filter should list the file, got %v", listed)
	}
	rec = f.do(t, "GET", "/api/files?tag=other", nil)
	decodeJSON(t, rec, &listed)
	for _, item := range listed {
		if item["id"] == file.ID {
			t.Fatal("tag filter must not list the file for another tag")
		}
	}
	if rec := f.do(t, "DELETE", "/api/files/"+file.ID+"/tags/work-docs", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete tag: got %d", rec.Code)
	}
	rec = f.do(t, "GET", "/api/files/"+file.ID+"/tags", nil)
	decodeJSON(t, rec, &tags)
	if len(tags) != 0 {
		t.Fatalf("tags after delete = %v", tags)
	}
}

func TestV32_UsersAndViewerReadOnly(t *testing.T) {
	f := newFeatureFixture(t)
	// Admin creates a viewer user.
	rec := f.do(t, "POST", "/api/users", map[string]any{"username": "ana", "password": "password123", "role": "viewer"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user: got %d, body %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	decodeJSON(t, rec, &created)
	if rec := f.do(t, "POST", "/api/users", map[string]any{"username": "ana", "password": "password123", "role": "viewer"}); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate user: got %d, want 409", rec.Code)
	}
	// Viewer logs in with username + password.
	form := url.Values{"username": {"ana"}, "password": {"password123"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	f.server.ServeHTTP(loginRec, req)
	if loginRec.Code != http.StatusSeeOther && loginRec.Code != http.StatusFound {
		t.Fatalf("viewer login: got %d, body %s", loginRec.Code, loginRec.Body.String())
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "tdocs_session" {
			sessionCookie = c
		}
		if c.Name == "tdocs_csrf" {
			csrfCookie = c
		}
	}
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatal("login must set session + csrf cookies")
	}
	doAsViewer := func(method, path string, body any) *httptest.ResponseRecorder {
		var reader *strings.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = strings.NewReader(string(raw))
		} else {
			reader = strings.NewReader("")
		}
		r := httptest.NewRequest(method, path, reader)
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		r.Header.Set("X-CSRF-Token", csrfCookie.Value)
		r.AddCookie(sessionCookie)
		rec := httptest.NewRecorder()
		f.server.ServeHTTP(rec, r)
		return rec
	}
	// Viewer can read…
	if rec := doAsViewer("GET", "/api/files", nil); rec.Code != http.StatusOK {
		t.Fatalf("viewer read: got %d, want 200", rec.Code)
	}
	// …but cannot mutate, and cannot administer users.
	file, err := f.db.CreateFile(nil, "x.txt", 1, "text/plain", 1, "f1", "h1", "sha")
	if err != nil {
		t.Fatal(err)
	}
	if rec := doAsViewer("POST", "/api/files/"+file.ID+"/tags", map[string]any{"tag": "nope"}); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer mutation: got %d, want 403", rec.Code)
	}
	if rec := doAsViewer("POST", "/api/users", map[string]any{"username": "bob", "password": "password123", "role": "viewer"}); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer user create: got %d, want 403", rec.Code)
	}
	// Admin role change + delete still work.
	if rec := f.do(t, "PUT", "/api/users/"+created.ID, map[string]any{"role": "admin"}); rec.Code != http.StatusOK {
		t.Fatalf("role update: got %d, body %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, "DELETE", "/api/users/"+created.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete user: got %d", rec.Code)
	}
}

func TestV32_MirrorScheduleAndStorageSettings(t *testing.T) {
	f := newFeatureFixture(t)
	rec := f.do(t, "GET", "/api/telegram/mirror/schedule", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("schedule get: got %d", rec.Code)
	}
	var sched struct {
		Enabled       bool   `json:"enabled"`
		IntervalHours int    `json:"interval_hours"`
		LastRun       string `json:"last_run"`
	}
	decodeJSON(t, rec, &sched)
	if sched.Enabled || sched.IntervalHours != 24 {
		t.Fatalf("default schedule = %+v", sched)
	}
	if rec := f.do(t, "POST", "/api/telegram/mirror/schedule", map[string]any{"enabled": true, "interval_hours": 0}); rec.Code != http.StatusBadRequest {
		t.Fatalf("interval 0: got %d, want 400", rec.Code)
	}
	if rec := f.do(t, "POST", "/api/telegram/mirror/schedule", map[string]any{"enabled": true, "interval_hours": 200}); rec.Code != http.StatusBadRequest {
		t.Fatalf("interval 200: got %d, want 400", rec.Code)
	}
	if rec := f.do(t, "POST", "/api/telegram/mirror/schedule", map[string]any{"enabled": true, "interval_hours": 6}); rec.Code != http.StatusOK {
		t.Fatalf("schedule set: got %d, body %s", rec.Code, rec.Body.String())
	}
	rec = f.do(t, "GET", "/api/telegram/mirror/schedule", nil)
	decodeJSON(t, rec, &sched)
	if !sched.Enabled || sched.IntervalHours != 6 {
		t.Fatalf("saved schedule = %+v", sched)
	}
	rec = f.do(t, "GET", "/api/settings/storage", nil)
	var storage struct {
		Backend  string `json:"backend"`
		DiskPath string `json:"disk_path"`
	}
	decodeJSON(t, rec, &storage)
	if storage.Backend != "telegram" {
		t.Fatalf("default storage backend = %q", storage.Backend)
	}
	if rec := f.do(t, "POST", "/api/settings/storage", map[string]any{"backend": "s3"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("backend s3: got %d, want 400", rec.Code)
	}
	if rec := f.do(t, "POST", "/api/settings/storage", map[string]any{"backend": "disk", "disk_path": t.TempDir()}); rec.Code != http.StatusOK {
		t.Fatalf("storage set: got %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestV32_FolderZip(t *testing.T) {
	f := newFeatureFixture(t)
	enableDiskBackend(t, f)
	folder, err := f.db.CreateFolder("Pack", nil)
	if err != nil {
		t.Fatal(err)
	}
	uploadViaInitChunkComplete(t, f,
		"/api/upload/init", "/api/upload/chunk", "/api/upload/complete",
		map[string]any{"name": "a.txt", "size": 5, "mime_type": "text/plain", "folder_id": folder.ID},
		[]byte("12345"), true)
	rec := f.do(t, "GET", "/api/folders/"+folder.ID+"/zip", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("zip: got %d, body %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("zip content-type = %q", ct)
	}
	body := rec.Body.Bytes()
	if len(body) < 2 || body[0] != 'P' || body[1] != 'K' {
		t.Fatal("zip body must start with PK magic")
	}
	if rec := f.do(t, "GET", "/api/folders/does-not-exist/zip", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing folder zip: got %d, want 404", rec.Code)
	}
	// No cookie at all: the zip endpoint requires login.
	req := httptest.NewRequest("GET", "/api/folders/"+folder.ID+"/zip", nil)
	unauth := httptest.NewRecorder()
	f.server.ServeHTTP(unauth, req)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth zip: got %d, want 401", unauth.Code)
	}
}
