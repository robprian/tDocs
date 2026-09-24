package web

import (
	"fmt"
	"net/http"
)

// openAPISpecTmpl is rendered per-request so the reported version always
// matches the running binary (a const would go stale on every release).
const openAPISpecTmpl = `{
  "openapi": "3.0.3",
  "info": {
    "title": "tDocs API",
    "version": "%s",
    "description": "Self-hosted cloud storage on Telegram MTProto. Dashboard cookie auth or Authorization Bearer header with TDOCS_ADMIN_PASSWORD for machine clients. CDN stream URLs are public by default (TDOCS_CDN_PUBLIC) so video/audio/img tags can embed them directly."
  },
  "servers": [{ "url": "/" }],
  "security": [{ "cookieAuth": [] }, { "bearerAuth": [] }],
  "components": {
    "securitySchemes": {
      "cookieAuth": { "type": "apiKey", "in": "cookie", "name": "tdocs_session" },
      "bearerAuth": { "type": "http", "scheme": "bearer", "bearerFormat": "admin-password" }
    },
    "schemas": {
      "Folder": { "type": "object", "properties": { "id": { "type": "string" }, "parent_id": { "type": ["string", "null"] }, "name": { "type": "string" } } },
      "File": { "type": "object", "properties": { "id": { "type": "string" }, "folder_id": { "type": ["string", "null"] }, "name": { "type": "string" }, "size": { "type": "integer" }, "mime_type": { "type": "string" } } },
      "CDNFile": { "type": "object", "properties": { "id": { "type": "string" }, "name": { "type": "string" }, "size": { "type": "integer" }, "mime_type": { "type": "string" }, "folder_id": { "type": ["string", "null"] }, "stream_url": { "type": "string", "format": "uri" }, "download_url": { "type": "string", "format": "uri" } } }
    }
  },
  "paths": {
    "/api/folders": {
      "get": { "summary": "List virtual folders", "parameters": [{ "name": "folder_id", "in": "query", "schema": { "type": "string" } }], "responses": { "200": { "description": "folders" } } },
      "post": { "summary": "Create virtual folder", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "properties": { "name": { "type": "string" }, "parent_id": { "type": ["string", "null"] } } } } } }, "responses": { "201": { "description": "created" } } }
    },
    "/api/folders/{id}": {
      "put": { "summary": "Rename/move folder", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "ok" } } },
      "delete": { "summary": "Delete folder (cascades)", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "deleted" } } }
    },
    "/api/files": {
      "get": { "summary": "List files", "parameters": [{ "name": "folder_id", "in": "query", "schema": { "type": "string" } }, { "name": "search", "in": "query", "schema": { "type": "string" } }], "responses": { "200": { "description": "files" } } }
    },
    "/api/files/{id}": {
      "get": { "summary": "Get one file record", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "file" }, "404": { "description": "not found" } } },
      "put": { "summary": "Rename/move file", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "ok" } } },
      "delete": { "summary": "Delete file", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "deleted" } } }
    },
    "/api/files/{id}/stream": {
      "get": { "summary": "Stream file (auth, Range seeking)", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }, { "name": "Range", "in": "header", "schema": { "type": "string" } }], "responses": { "200": { "description": "bytes" }, "206": { "description": "partial bytes" } } }
    },
    "/api/files/{id}/download": {
      "get": { "summary": "Download file (auth, attachment)", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "bytes" } } }
    },
    "/api/upload/init": {
      "post": { "summary": "Open resumable upload session", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "required": ["name", "size"], "properties": { "name": { "type": "string" }, "size": { "type": "integer" }, "mime_type": { "type": "string" }, "folder_id": { "type": ["string", "null"] } } } } } }, "responses": { "200": { "description": "{id, chunk_size, total_parts}" } } }
    },
    "/api/upload/chunk": {
      "post": { "summary": "Upload 5MB chunk (multipart: session_id, chunk_index, chunk)", "requestBody": { "content": { "multipart/form-data": { "schema": { "type": "object" } } } }, "responses": { "200": { "description": "part stored" } } }
    },
    "/api/upload/complete": {
      "post": { "summary": "Assemble parts into Storage Channel document", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "properties": { "session_id": { "type": "string" } } } } } }, "responses": { "201": { "description": "file record" } } }
    },
    "/api/share": {
      "post": { "summary": "Create share link", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "required": ["file_id"], "properties": { "file_id": { "type": "string" }, "password": { "type": "string" }, "expiry_days": { "type": "integer" } } } } } }, "responses": { "201": { "description": "{token, page_url, stream_url, download_url}" } } }
    },
    "/api/shares": {
      "get": { "summary": "List share links", "responses": { "200": { "description": "shares" } } }
    },
    "/api/shares/{id}": {
      "delete": { "summary": "Delete share link", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "deleted" } } }
    },
    "/s/{token}": {
      "get": { "summary": "Share landing page", "security": [], "parameters": [{ "name": "token", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "html" } } }
    },
    "/s/{token}/stream": {
      "get": { "summary": "Guest stream via share token (Range seeking)", "security": [], "parameters": [{ "name": "token", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "bytes" }, "206": { "description": "partial bytes" } } }
    },
    "/s/{token}/download": {
      "get": { "summary": "Guest download via share token", "security": [], "parameters": [{ "name": "token", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "bytes" } } }
    },
    "/api/cdn/files": {
      "get": { "summary": "CDN catalog for external websites (film site)", "description": "Machine-readable catalog with embeddable stream_url/download_url per file. Filter mime=video/ for films.", "parameters": [{ "name": "search", "in": "query", "schema": { "type": "string" } }, { "name": "mime", "in": "query", "description": "exact mime or prefix like video/", "schema": { "type": "string" } }, { "name": "limit", "in": "query", "schema": { "type": "integer", "default": 100 } }], "responses": { "200": { "description": "{files: CDNFile[], total}" } } }
    },
    "/api/cdn/files/{id}": {
      "get": { "summary": "One CDN catalog entry with embed URLs", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "CDNFile" }, "404": { "description": "not found" } } }
    },
    "/cdn/{id}/stream": {
      "get": { "summary": "Public embed URL for <video>/<audio>/<img> (Range + CORS + ETag)", "security": [], "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "bytes" }, "206": { "description": "partial bytes" }, "304": { "description": "not modified" } } }
    },
    "/cdn/{id}/download": {
      "get": { "summary": "Public save-as download URL (CORS)", "security": [], "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "bytes" } } }
    },
    "/api/snapshots": {
      "get": { "summary": "List Database Snapshots", "responses": { "200": { "description": "snapshots" } } },
      "post": { "summary": "Create + upload Database Snapshot", "responses": { "201": { "description": "created" } } }
    },
    "/api/snapshots/{id}/restore": {
      "post": { "summary": "Point-in-Time Restore from snapshot", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "restored" } } }
    },
    "/api/snapshots/{id}/download": {
      "get": { "summary": "Download snapshot file", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "gzip bytes" } } }
    },
    "/api/snapshots/{id}": {
      "delete": { "summary": "Delete snapshot", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "deleted" } } }
    },
    "/api": {
      "get": { "summary": "API endpoint index (this catalog as JSON)", "responses": { "200": { "description": "{name, version, base_url, auth, cdn, endpoints[]}" } } }
    },
    "/api/status": {
      "get": { "summary": "Server health: session, storage channel, file/folder counts, db path", "responses": { "200": { "description": "{version, telegram_session, storage_channel, files, folders, db_path}" } } }
    },
    "/api/stats": {
      "get": { "summary": "Storage analytics: totals, mime breakdown, recent + largest files", "responses": { "200": { "description": "{files, folders, bytes, breakdown[], recent[], largest[]}" } } }
    },
    "/api/favorites": {
      "get": { "summary": "List starred files", "responses": { "200": { "description": "files" } } }
    },
    "/api/files/{id}/favorite": {
      "post": { "summary": "Star a file", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "starred" } } },
      "delete": { "summary": "Unstar a file", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "unstarred" } } }
    },
    "/api/files/{id}/copy": {
      "post": { "summary": "Duplicate catalog record (no re-upload)", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "requestBody": { "content": { "application/json": { "schema": { "type": "object", "properties": { "folder_id": { "type": ["string", "null"] }, "name": { "type": "string" } } } } } }, "responses": { "201": { "description": "file record" } } }
    },
    "/api/storage/verify": {
      "post": { "summary": "Verify recent files are retrievable from the channel", "responses": { "200": { "description": "{checked, ok, failed[]}" } } }
    },
    "/api/files/{id}/ticket": {
      "post": { "summary": "Mint signed expiring download/stream URLs", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "requestBody": { "content": { "application/json": { "schema": { "type": "object", "properties": { "expires_in": { "type": "integer" } } } } } }, "responses": { "200": { "description": "{url, stream_url, expires_at}" } } }
    },
    "/api/files/{id}/versions": {
      "get": { "summary": "List file versions", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "versions" } } }
    },
    "/api/files/bulk": {
      "post": { "summary": "Bulk trash/restore/purge/move/favorite", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "required": ["action", "ids"], "properties": { "action": { "type": "string" }, "ids": { "type": "array", "items": { "type": "string" } }, "folder_id": { "type": ["string", "null"] } } } } } }, "responses": { "200": { "description": "{done, failed}" } } }
    },
    "/api/duplicates": {
      "get": { "summary": "SHA-256 duplicate groups (or ?sha= for members)", "responses": { "200": { "description": "groups or files" } } }
    },
    "/api/trash": {
      "get": { "summary": "List trashed files and folders", "responses": { "200": { "description": "{files, folders}" } } }
    },
    "/api/trash/restore": {
      "post": { "summary": "Restore a trashed item", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "required": ["kind", "id"], "properties": { "kind": { "type": "string" }, "id": { "type": "string" } } } } } }, "responses": { "200": { "description": "restored" } } }
    },
    "/api/trash/empty": {
      "post": { "summary": "Purge all trash permanently", "responses": { "204": { "description": "emptied" } } }
    },
    "/api/audit": {
      "get": { "summary": "Audit log", "parameters": [{ "name": "action", "in": "query", "schema": { "type": "string" } }, { "name": "limit", "in": "query", "schema": { "type": "integer" } }], "responses": { "200": { "description": "entries" } } }
    },
    "/api/tokens": {
      "get": { "summary": "List API tokens (hashes never exposed)", "responses": { "200": { "description": "tokens" } } },
      "post": { "summary": "Create API token (secret shown once)", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "required": ["name"], "properties": { "name": { "type": "string" } } } } } }, "responses": { "201": { "description": "{id, name, prefix, token}" } } }
    },
    "/api/health": {
      "get": { "summary": "Runtime health", "responses": { "200": { "description": "{version, uptime_seconds, goroutines, memory_alloc_bytes, db_bytes}" } } }
    },
    "/api/sync/runs": {
      "get": { "summary": "Sync history", "responses": { "200": { "description": "runs" } } }
    },
    "/api/sync": {
      "post": { "summary": "Rebuild the file catalog from documents stored in the Telegram Storage Channel", "description": "Idempotent reconciliation matched by Telegram message ID. Recovers files after a database wipe, fresh volume, or server restart. Never creates a new channel.", "responses": { "200": { "description": "{scanned, inserted, updated, total}" }, "503": { "description": "telegram not ready or storage channel not bound" } } }
    },
    "/api/telegram/wizard/start": {
      "post": { "summary": "Start browser-based Telegram auth wizard (superuser only)", "description": "Sends the login code via Telegram when the stored session is dead. Returns {id, phase, success}; poll status until waiting_code. A still-valid session returns done without any SMS.", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "required": ["phone"], "properties": { "phone": { "type": "string", "example": "+628123456789" } } } } } }, "responses": { "200": { "description": "{id, phase, success, error}" } } }
    },
    "/api/telegram/wizard/status": {
      "get": { "summary": "Poll wizard phase (superuser only)", "parameters": [{ "name": "id", "in": "query", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "{phase: sending_code|waiting_code|waiting_password|error|done, success, error}" } } }
    },
    "/api/telegram/wizard/submit": {
      "post": { "summary": "Submit login code or 2FA password (superuser only)", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "required": ["id"], "properties": { "id": { "type": "string" }, "code": { "type": "string" }, "password": { "type": "string" } } } } } }, "responses": { "200": { "description": "{ok}" } } }
    },
    "/api/telegram/wizard/discard": {
      "post": { "summary": "Clean up a finished wizard (superuser only)", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "required": ["id"], "properties": { "id": { "type": "string" } } } } } }, "responses": { "200": { "description": "{ok}" } } }
    },
    "/api/telegram/wizard/cancel": {
      "post": { "summary": "Abandon an in-flight wizard so a fresh code can be requested (superuser only)", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "required": ["id"], "properties": { "id": { "type": "string" } } } } } }, "responses": { "200": { "description": "{ok}" } } }
    },
    "/api/telegram/wizard/needed": {
      "get": { "summary": "Whether browser pairing is needed (superuser only)", "responses": { "200": { "description": "{configured, client_available, storage_channel, telegram_authorized, superuser}" } } }
    },
    "/api/telegram/onboard": {
      "post": { "summary": "Alias of wizard start (superuser only)", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "required": ["phone"], "properties": { "phone": { "type": "string" } } } } } }, "responses": { "200": { "description": "{id, phase, success, error}" } } }
    },
    "/api/telegram/state": {
      "get": { "summary": "Verified Telegram backend state (superuser only)", "description": "Honest state: connected only after a live probe; otherwise authentication_required / channel_unavailable / not_configured.", "responses": { "200": { "description": "{state, message, checks, verified_at}" } } }
    },
    "/api/telegram/test": {
      "post": { "summary": "Live connection test, bypasses cache (superuser only)", "responses": { "200": { "description": "{state, message}" } } }
    },
    "/api/folders/stats": {
      "get": { "summary": "Folder tree with file and byte counts", "responses": { "200": { "description": "folder stats" } } }
    },
    "/api/files/{id}/meta": {
      "get": { "summary": "File metadata with CDN embed URLs", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "file meta" }, "404": { "description": "not found" } } }
    },
    "/api/files/{id}/versions/{vid}/restore": {
      "post": { "summary": "Restore a file version", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }, { "name": "vid", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "restored" } } }
    },
    "/api/files/{id}/versions/{vid}": {
      "delete": { "summary": "Delete a file version", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "deleted" } } }
    },
    "/api/sessions": {
      "get": { "summary": "List active web sessions", "responses": { "200": { "description": "sessions" } } }
    },
    "/api/sessions/{id}": {
      "delete": { "summary": "Revoke a web session", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "revoked" } } }
    },
    "/api/tokens/{id}/purge": {
      "delete": { "summary": "Delete an API token row", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "deleted" } } }
    },
    "/api/settings/password": {
      "post": { "summary": "Change the dashboard password", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "required": ["current_password", "new_password"], "properties": { "current_password": { "type": "string" }, "new_password": { "type": "string" } } } } } }, "responses": { "200": { "description": "changed" } } }
    },
    "/api/notifications": {
      "get": { "summary": "Recent activity for the notification center", "responses": { "200": { "description": "notifications" } } }
    },
    "/s/{token}/unlock": {
      "post": { "summary": "Unlock a password-protected share", "parameters": [{ "name": "token", "in": "path", "required": true, "schema": { "type": "string" } }], "requestBody": { "content": { "application/json": { "schema": { "type": "object", "required": ["password"], "properties": { "password": { "type": "string" } } } } } }, "responses": { "200": { "description": "unlocked" }, "401": { "description": "wrong password" } } }
    },
    "/s/{token}/stream": {
      "get": { "summary": "Stream a shared file (Range 206)", "security": [], "parameters": [{ "name": "token", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "bytes" }, "206": { "description": "partial bytes" } } }
    },
    "/s/{token}/download": {
      "get": { "summary": "Download a shared file", "security": [], "parameters": [{ "name": "token", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "attachment" } } }
    },
    "/api/snapshots/upload-restore": {
      "post": { "summary": "Upload a snapshot file and restore it", "requestBody": { "content": { "multipart/form-data": { "schema": { "type": "object" } } } }, "responses": { "200": { "description": "restored" } } }
    },
    "/api/update/status": {
      "get": { "summary": "Update check: current vs latest release + upgrade command (superuser only)", "parameters": [{ "name": "refresh", "in": "query", "schema": { "type": "string" } }], "responses": { "200": { "description": "{current, latest, available, url, hint, install_kind, checked_at, checking}" }, "403": { "description": "superuser required" } } }
    }
  }
}`

func (s *Server) handleOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(s.renderOpenAPISpec()))
}

// renderOpenAPISpec injects the running binary version into the contract.
func (s *Server) renderOpenAPISpec() string {
	return fmt.Sprintf(openAPISpecTmpl, s.currentVersion())
}

const docsPageTmpl = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>tDocs API · Docs</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
<style>
:root{--neu-bg:#e0e5ec;--neu-dark:#a3b1c6;--neu-light:#ffffff;--ink:#3d4a5c;--accent:#4d7cfe}
*{box-sizing:border-box}body{margin:0;background:var(--neu-bg);color:var(--ink);font-family:-apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif}
.neu-bar{background:var(--neu-bg);padding:22px 28px;box-shadow:6px 6px 12px var(--neu-dark),-6px -6px 12px var(--neu-light);border-radius:0 0 24px 24px;display:flex;align-items:center;gap:16px;flex-wrap:wrap}
.neu-logo{width:46px;height:46px;border-radius:14px;background:var(--neu-bg);box-shadow:5px 5px 10px var(--neu-dark),-5px -5px 10px var(--neu-light);display:flex;align-items:center;justify-content:center;font-size:22px}
.neu-title{font-size:20px;font-weight:800;letter-spacing:-.02em}
.neu-sub{font-size:13px;opacity:.75}
.neu-links{margin-left:auto;display:flex;gap:10px;flex-wrap:wrap}
.neu-btn{background:var(--neu-bg);border:none;color:var(--ink);font-weight:700;font-size:13px;padding:10px 16px;border-radius:12px;box-shadow:4px 4px 8px var(--neu-dark),-4px -4px 8px var(--neu-light);cursor:pointer;text-decoration:none}
.neu-btn:active{box-shadow:inset 4px 4px 8px var(--neu-dark),inset -4px -4px 8px var(--neu-light)}
.neu-btn.primary{color:var(--accent)}
#ui{padding:18px}.swagger-ui .wrapper{background:transparent}
.curl-hint{margin:0 28px 10px;font-size:12.5px;background:var(--neu-bg);padding:12px 16px;border-radius:12px;box-shadow:inset 4px 4px 8px var(--neu-dark),inset -4px -4px 8px var(--neu-light);overflow-x:auto}
.curl-hint code{font-family:ui-monospace,Menlo,Consolas,monospace}
</style>
</head><body>
<div class="neu-bar"><div class="neu-logo">◈</div><div><div class="neu-title">tDocs API</div><div class="neu-sub">%s · cookie <b>tdocs_session</b> atau <b>Authorization: Bearer &lt;admin password&gt;</b></div></div>
<div class="neu-links"><a class="neu-btn primary" href="/api">GET /api (index JSON)</a><a class="neu-btn" href="/api/openapi.json">openapi.json</a><a class="neu-btn" href="/">← Dashboard</a></div></div>
<p class="curl-hint"><code>curl -H "Authorization: Bearer ADMIN_PASS" "http://localhost:8080/api/cdn/files?mime=video/&amp;limit=100" &nbsp;·&nbsp; embed: &lt;video src="http://localhost:8080/cdn/{id}/stream"&gt;</code></p>
<div id="ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
SwaggerUIBundle({ url: "/api/openapi.json", dom_id: "#ui", deepLinking: true });
</script>
</body></html>`

func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(s.renderDocsPage()))
}

// renderDocsPage injects the running binary version into the docs banner.
func (s *Server) renderDocsPage() string {
	return fmt.Sprintf(docsPageTmpl, s.currentVersion())
}
