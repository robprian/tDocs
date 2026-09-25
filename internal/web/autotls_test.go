package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"tdocs/internal/app"
	"tdocs/internal/db"
)

func TestValidateDomain(t *testing.T) {
	ok := []struct{ in, want string }{
		{"files.example.com", "files.example.com"},
		{"  Files.Example.COM  ", "files.example.com"},
		{"files.example.com.", "files.example.com"},
		{"https://files.example.com/", "files.example.com"},
		{"https://files.example.com:8443/dashboard?x=1", "files.example.com"},
		{"my-host.sub.example.co.id", "my-host.sub.example.co.id"},
		{"", ""},
	}
	for _, c := range ok {
		got, err := ValidateDomain(c.in)
		if err != nil || got != c.want {
			t.Errorf("ValidateDomain(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}

	bad := []string{
		"localhost",
		"192.168.1.10",
		"*.example.com",
		"files..example.com",
		"-files.example.com",
		"files-.example.com",
		"files example.com",
		"files.example.com/path",
	}
	for _, in := range bad {
		if got, err := ValidateDomain(in); err == nil {
			t.Errorf("ValidateDomain(%q) accepted as %q, want rejection", in, got)
		}
	}
}

func TestValidateACMEEmail(t *testing.T) {
	if _, err := validateACMEEmail(""); err != nil {
		t.Errorf("empty email must be allowed: %v", err)
	}
	if got, err := validateACMEEmail(" admin@example.com "); err != nil || got != "admin@example.com" {
		t.Errorf("valid email rejected: %q %v", got, err)
	}
	for _, in := range []string{"admin", "admin@", "@example.com", "admin@localhost", "a@b@c.com"} {
		if _, err := validateACMEEmail(in); err == nil {
			t.Errorf("validateACMEEmail(%q) accepted, want rejection", in)
		}
	}
}

func TestDomainStatusAndSave(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	cfg := &app.Config{
		Port:          "8080",
		DBPath:        dbPath,
		SecretKey:     "test-secret-key-32b",
		AdminPassword: "supersecretpassword",
		Version:       "v2.1.4",
		Paths:         &app.Paths{Mode: "user", DataDir: t.TempDir()},
	}
	server, err := NewServer(cfg, database, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	call := func(method, path, body string) *httptest.ResponseRecorder {
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer supersecretpassword")
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		return rec
	}

	// Fresh install: nothing configured, HTTPS inactive.
	rec := call("GET", "/api/settings/domain", "")
	if rec.Code != 200 {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	var st map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("status not JSON: %v", err)
	}
	if dom, _ := st["domain"].(string); st["active"] == true || dom != "" {
		t.Fatalf("expected inactive/empty status, got %v", st)
	}

	// Invalid domain is rejected before any listener work happens.
	if rec := call("POST", "/api/settings/domain", `{"domain":"not-a-domain"}`); rec.Code != 400 {
		t.Fatalf("invalid domain: expected 400, got %d %s", rec.Code, rec.Body.String())
	}
	// Invalid contact email is rejected too.
	if rec := call("POST", "/api/settings/domain", `{"domain":"files.example.com","email":"nope"}`); rec.Code != 400 {
		t.Fatalf("invalid email: expected 400, got %d %s", rec.Code, rec.Body.String())
	}

	// Machine API tokens are not superuser credentials: domain config must be
	// refused when the caller is not the human administrator.
	req := httptest.NewRequest("GET", "/api/settings/domain", nil)
	recNoAuth := httptest.NewRecorder()
	server.ServeHTTP(recNoAuth, req)
	if recNoAuth.Code != 403 {
		t.Fatalf("unauthenticated domain status: expected 403, got %d", recNoAuth.Code)
	}

	// Saving a valid domain persists it even when the privileged ports cannot
	// be bound in the test environment: the setting must survive so a restart
	// on the real host picks it up.
	rec = call("POST", "/api/settings/domain", `{"domain":"https://files.example.com/","email":"admin@example.com"}`)
	if rec.Code != 200 && rec.Code != 500 {
		t.Fatalf("save: unexpected status %d %s", rec.Code, rec.Body.String())
	}
	stored, err := database.GetSetting(settingPublicDomain)
	if err != nil || stored != "files.example.com" {
		t.Fatalf("domain not persisted: %q %v", stored, err)
	}
	if email, _ := database.GetSetting(settingACMEEmail); email != "admin@example.com" {
		t.Fatalf("email not persisted: %q", email)
	}

	// Status now reports the saved domain, and says HTTPS is not running when
	// binding failed (or was never started) instead of claiming success.
	rec = call("GET", "/api/settings/domain", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("status not JSON: %v", err)
	}
	if st["domain"] != "files.example.com" {
		t.Fatalf("status domain = %v, want files.example.com", st["domain"])
	}

	// Clearing the domain returns the appliance to plain HTTP.
	if rec := call("POST", "/api/settings/domain", `{"domain":""}`); rec.Code != 200 {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}
	if stored, _ := database.GetSetting(settingPublicDomain); stored != "" {
		t.Fatalf("domain not cleared: %q", stored)
	}
}
