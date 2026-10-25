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
    "description": "Self-hosted cloud storage on Telegram MTProto.\n\nAuth (pick one): dashboard cookie 'tdocs_session' (POST /login), or machine clients send 'Authorization: Bearer <TDOCS_ADMIN_PASSWORD>' — '?api_key=<password>' also works. CDN stream/download URLs are public by default (TDOCS_CDN_PUBLIC) so <video>/<audio>/<img> tags can embed them directly.\n\nQuickstart: curl -H \"Authorization: Bearer ADMIN_PASS\" \"http://localhost:8080/api/cdn/files?mime=video/&limit=100\" then embed <video src=\"http://localhost:8080/cdn/{id}/stream\">."
  },
  "servers": [{ "url": "/" }],
  "security": [{ "cookieAuth": [] }, { "bearerAuth": [] }],
  "tags": [
    { "name": "Drive", "description": "Folders and file records" },
    { "name": "Upload", "description": "Resumable chunked upload sessions" },
    { "name": "Shares", "description": "Public share links and guest access (no auth)" },
    { "name": "CDN", "description": "Machine catalog + public embed URLs for external sites" },
    { "name": "Library", "description": "Favorites, versions, duplicates, stats, bulk ops" },
    { "name": "Trash", "description": "Soft-delete, restore, purge" },
    { "name": "Snapshots", "description": "Database snapshots and point-in-time restore" },
    { "name": "Admin", "description": "Audit, tokens, sessions, settings, health" },
    { "name": "Telegram", "description": "Browser pairing wizard + backend state (superuser only)" },
    { "name": "System", "description": "Index, health, sync, updates" }
  ],
  "components": {
    "securitySchemes": {
      "cookieAuth": { "type": "apiKey", "in": "cookie", "name": "tdocs_session" },
      "bearerAuth": { "type": "http", "scheme": "bearer", "bearerFormat": "admin-password" }
    },
    "schemas": {
      "Folder": { "type": "object", "properties": { "id": { "type": "string" }, "parent_id": { "type": "string", "nullable": true }, "name": { "type": "string" } } },
      "File": { "type": "object", "properties": { "id": { "type": "string" }, "folder_id": { "type": "string", "nullable": true }, "name": { "type": "string" }, "size": { "type": "integer", "format": "int64" }, "mime_type": { "type": "string" } } },
      "CDNFile": { "type": "object", "properties": { "id": { "type": "string" }, "name": { "type": "string" }, "size": { "type": "integer", "format": "int64" }, "mime_type": { "type": "string" }, "folder_id": { "type": "string", "nullable": true }, "stream_url": { "type": "string", "format": "uri" }, "download_url": { "type": "string", "format": "uri" } } },
      "Error": { "type": "object", "properties": { "error": { "type": "string" } } }
    }
  },
  "paths": {
    "/api/folders": {
      "get": { "tags": ["Drive"], "operationId": "listFolders", "summary": "List virtual folders", "parameters": [{ "name": "folder_id", "in": "query", "description": "Parent id; omit for root", "schema": { "type": "string" } }], "responses": { "200": { "description": "folders" } } },
      "post": { "tags": ["Drive"], "operationId": "createFolder", "summary": "Create virtual folder", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["name"], "properties": { "name": { "type": "string" }, "parent_id": { "type": "string", "nullable": true } } } } } }, "responses": { "201": { "description": "created" } } }
    },
    "/api/folders/{id}": {
      "put": { "tags": ["Drive"], "operationId": "updateFolder", "summary": "Rename/move folder", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "requestBody": { "content": { "application/json": { "schema": { "type": "object", "properties": { "name": { "type": "string" }, "parent_id": { "type": "string", "nullable": true } } } } } }, "responses": { "200": { "description": "ok" }, "404": { "description": "not found" } } },
      "delete": { "tags": ["Drive"], "operationId": "deleteFolder", "summary": "Delete folder (cascades)", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "deleted" }, "404": { "description": "not found" } } }
    },
    "/api/folders/stats": {
      "get": { "tags": ["Library"], "operationId": "folderStats", "summary": "Folder tree with file and byte counts", "responses": { "200": { "description": "folder stats" } } }
    },
    "/api/files": {
      "get": { "tags": ["Drive"], "operationId": "listFiles", "summary": "List files", "parameters": [{ "name": "folder_id", "in": "query", "schema": { "type": "string" } }, { "name": "search", "in": "query", "schema": { "type": "string" } }, { "name": "mime", "in": "query", "description": "exact mime or prefix like video/", "schema": { "type": "string" } }, { "name": "min_size", "in": "query", "schema": { "type": "integer", "format": "int64" } }, { "name": "max_size", "in": "query", "schema": { "type": "integer", "format": "int64" } }, { "name": "since", "in": "query", "description": "YYYY-MM-DD prefix filter on updated_at", "schema": { "type": "string" } }], "responses": { "200": { "description": "files" } } }
    },
    "/api/files/{id}": {
      "get": { "tags": ["Drive"], "operationId": "getFile", "summary": "Get one file record", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "file" }, "404": { "description": "not found" } } },
      "put": { "tags": ["Drive"], "operationId": "updateFile", "summary": "Rename/move file", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "requestBody": { "content": { "application/json": { "schema": { "type": "object", "properties": { "name": { "type": "string" }, "folder_id": { "type": "string", "nullable": true } } } } } }, "responses": { "200": { "description": "ok" }, "404": { "description": "not found" } } },
      "delete": { "tags": ["Drive"], "operationId": "deleteFile", "summary": "Delete file", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "deleted" }, "404": { "description": "not found" } } }
    },
    "/api/files/{id}/meta": {
      "get": { "tags": ["Drive"], "operationId": "fileMeta", "summary": "File metadata with CDN embed URLs", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "file meta" }, "404": { "description": "not found" } } }
    },
    "/api/files/{id}/stream": {
      "get": { "tags": ["Drive"], "operationId": "streamFile", "summary": "Stream file (auth, Range seeking)", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }, { "name": "Range", "in": "header", "description": "e.g. bytes=0-1048575", "schema": { "type": "string" } }], "responses": { "200": { "description": "bytes" }, "206": { "description": "partial bytes" }, "404": { "description": "not found" } } }
    },
    "/api/files/{id}/download": {
      "get": { "tags": ["Drive"], "operationId": "downloadFile", "summary": "Download file (auth, attachment)", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "bytes" }, "404": { "description": "not found" } } }
    },
    "/api/files/{id}/ticket": {
      "post": { "tags": ["Drive"], "operationId": "mintTicket", "summary": "Mint signed expiring download/stream URLs", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "requestBody": { "content": { "application/json": { "schema": { "type": "object", "properties": { "expires_in": { "type": "integer", "description": "seconds, default 3600, max 86400" } } } } } }, "responses": { "200": { "description": "{url, stream_url, expires_at}" }, "404": { "description": "not found" } } }
    },
    "/api/files/{id}/copy": {
      "post": { "tags": ["Drive"], "operationId": "copyFile", "summary": "Duplicate catalog record (no re-upload)", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "requestBody": { "content": { "application/json": { "schema": { "type": "object", "properties": { "folder_id": { "type": "string", "nullable": true }, "name": { "type": "string" } } } } } }, "responses": { "201": { "description": "file record" }, "404": { "description": "not found" } } }
    },
    "/api/files/{id}/favorite": {
      "post": { "tags": ["Library"], "operationId": "starFile", "summary": "Star a file", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "starred" }, "404": { "description": "not found" } } },
      "delete": { "tags": ["Library"], "operationId": "unstarFile", "summary": "Unstar a file", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "unstarred" } } }
    },
    "/api/files/{id}/versions": {
      "get": { "tags": ["Library"], "operationId": "listVersions", "summary": "List file versions", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "versions" }, "404": { "description": "not found" } } }
    },
    "/api/files/{id}/versions/{vid}/restore": {
      "post": { "tags": ["Library"], "operationId": "restoreVersion", "summary": "Restore a file version", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }, { "name": "vid", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "restored" }, "404": { "description": "not found" } } }
    },
    "/api/files/{id}/versions/{vid}": {
      "delete": { "tags": ["Library"], "operationId": "deleteVersion", "summary": "Delete a file version", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }, { "name": "vid", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "deleted" }, "404": { "description": "not found" } } }
    },
    "/api/files/bulk": {
      "post": { "tags": ["Library"], "operationId": "bulkFiles", "summary": "Bulk trash/restore/purge/move/favorite", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["action", "ids"], "properties": { "action": { "type": "string", "enum": ["trash", "restore", "purge", "move", "favorite", "unfavorite"] }, "ids": { "type": "array", "items": { "type": "string" } }, "folder_id": { "type": "string", "nullable": true } } } } } }, "responses": { "200": { "description": "{done, failed}" } } }
    },
    "/api/upload/init": {
      "post": { "tags": ["Upload"], "operationId": "uploadInit", "summary": "Open resumable upload session", "description": "Max 4 GB per file. Returns chunk_size 5 MB; slice each client chunk into 512 KB MTProto parts server-side.", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["name", "size"], "properties": { "name": { "type": "string" }, "size": { "type": "integer", "format": "int64" }, "mime_type": { "type": "string", "default": "application/octet-stream" }, "folder_id": { "type": "string", "nullable": true } } } } } }, "responses": { "200": { "description": "{id, session_id, chunk_size, total_parts, total_chunks}" }, "400": { "description": "invalid payload" }, "503": { "description": "telegram not ready" } } }
    },
    "/api/upload/chunk": {
      "post": { "tags": ["Upload"], "operationId": "uploadChunk", "summary": "Upload 5 MB chunk (multipart: session_id, chunk_index, chunk)", "requestBody": { "required": true, "content": { "multipart/form-data": { "schema": { "type": "object", "required": ["session_id", "chunk_index", "chunk"], "properties": { "session_id": { "type": "string" }, "chunk_index": { "type": "integer" }, "chunk": { "type": "string", "format": "binary" } } } } } }, "responses": { "200": { "description": "{ok, chunk_index}" }, "503": { "description": "telegram not ready" } } }
    },
    "/api/upload/complete": {
      "post": { "tags": ["Upload"], "operationId": "uploadComplete", "summary": "Assemble parts into Storage Channel document", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["session_id"], "properties": { "session_id": { "type": "string" } } } } } }, "responses": { "201": { "description": "file record" } } }
    },
    "/api/share": {
      "post": { "tags": ["Shares"], "operationId": "createShare", "summary": "Create share link", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["file_id"], "properties": { "file_id": { "type": "string" }, "password": { "type": "string" }, "expiry_days": { "type": "integer", "maximum": 3650 }, "max_downloads": { "type": "integer" }, "preview_only": { "type": "boolean" } } } } } }, "responses": { "201": { "description": "{token, page_url, stream_url, download_url}" }, "404": { "description": "file not found" } } }
    },
    "/api/shares": {
      "get": { "tags": ["Shares"], "operationId": "listShares", "summary": "List share links", "responses": { "200": { "description": "shares" } } }
    },
    "/api/shares/{id}": {
      "delete": { "tags": ["Shares"], "operationId": "deleteShare", "summary": "Revoke share link", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "revoked" } } }
    },
    "/s/{token}": {
      "get": { "tags": ["Shares"], "operationId": "sharePage", "summary": "Share landing page", "security": [], "parameters": [{ "name": "token", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "html" }, "404": { "description": "unknown token" } } }
    },
    "/s/{token}/unlock": {
      "post": { "tags": ["Shares"], "operationId": "shareUnlock", "summary": "Unlock a password-protected share", "security": [], "parameters": [{ "name": "token", "in": "path", "required": true, "schema": { "type": "string" } }], "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["password"], "properties": { "password": { "type": "string" } } } } } }, "responses": { "200": { "description": "unlocked" }, "401": { "description": "wrong password" } } }
    },
    "/s/{token}/stream": {
      "get": { "tags": ["Shares"], "operationId": "shareStream", "summary": "Guest stream via share token (Range 206)", "description": "Public; a password cookie is required first when the link is locked.", "security": [], "parameters": [{ "name": "token", "in": "path", "required": true, "schema": { "type": "string" } }, { "name": "Range", "in": "header", "schema": { "type": "string" } }], "responses": { "200": { "description": "bytes" }, "206": { "description": "partial bytes" }, "401": { "description": "locked" }, "404": { "description": "unknown token" } } }
    },
    "/s/{token}/download": {
      "get": { "tags": ["Shares"], "operationId": "shareDownload", "summary": "Guest download via share token", "security": [], "parameters": [{ "name": "token", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "attachment" }, "401": { "description": "locked" }, "404": { "description": "unknown token" } } }
    },
    "/api/cdn/files": {
      "get": { "tags": ["CDN"], "operationId": "cdnCatalog", "summary": "CDN catalog for external websites (film site)", "description": "Machine-readable catalog with embeddable stream_url/download_url per file. Filter mime=video/ for films.", "parameters": [{ "name": "search", "in": "query", "schema": { "type": "string" } }, { "name": "mime", "in": "query", "description": "exact mime or prefix like video/", "schema": { "type": "string" } }, { "name": "limit", "in": "query", "schema": { "type": "integer", "default": 100 } }], "responses": { "200": { "description": "{files: CDNFile[], total}" } } }
    },
    "/api/cdn/files/{id}": {
      "get": { "tags": ["CDN"], "operationId": "cdnEntry", "summary": "One CDN catalog entry with embed URLs", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "CDNFile" }, "404": { "description": "not found" } } }
    },
    "/cdn/{id}/stream": {
      "get": { "tags": ["CDN"], "operationId": "cdnStream", "summary": "Public embed URL for <video>/<audio>/<img> (Range + CORS + ETag)", "description": "Open when TDOCS_CDN_PUBLIC=true, else Bearer required.", "security": [], "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "bytes" }, "206": { "description": "partial bytes" }, "304": { "description": "not modified" }, "404": { "description": "not found" } } }
    },
    "/cdn/{id}/download": {
      "get": { "tags": ["CDN"], "operationId": "cdnDownload", "summary": "Public save-as download URL (CORS)", "security": [], "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "bytes" }, "404": { "description": "not found" } } }
    },
    "/api/snapshots": {
      "get": { "tags": ["Snapshots"], "operationId": "listSnapshots", "summary": "List Database Snapshots", "responses": { "200": { "description": "snapshots" }, "503": { "description": "telegram not ready" } } },
      "post": { "tags": ["Snapshots"], "operationId": "createSnapshot", "summary": "Create + upload Database Snapshot", "responses": { "201": { "description": "created" }, "503": { "description": "telegram not ready" } } }
    },
    "/api/snapshots/{id}/restore": {
      "post": { "tags": ["Snapshots"], "operationId": "restoreSnapshot", "summary": "Point-in-Time Restore from snapshot", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "restored" } } }
    },
    "/api/snapshots/{id}/download": {
      "get": { "tags": ["Snapshots"], "operationId": "downloadSnapshot", "summary": "Download snapshot file", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "gzip bytes" }, "404": { "description": "not found" } } }
    },
    "/api/snapshots/{id}": {
      "delete": { "tags": ["Snapshots"], "operationId": "deleteSnapshot", "summary": "Delete snapshot", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "deleted" } } }
    },
    "/api/snapshots/upload-restore": {
      "post": { "tags": ["Snapshots"], "operationId": "uploadRestoreSnapshot", "summary": "Upload a snapshot file and restore it", "description": "multipart file field, max 50 MB.", "requestBody": { "required": true, "content": { "multipart/form-data": { "schema": { "type": "object" } } } }, "responses": { "200": { "description": "restored" }, "400": { "description": "invalid file" } } }
    },
    "/api": {
      "get": { "tags": ["System"], "operationId": "apiIndex", "summary": "API endpoint index (this catalog as JSON)", "responses": { "200": { "description": "{name, version, base_url, auth, cdn, endpoints[]}" } } }
    },
    "/api/status": {
      "get": { "tags": ["System"], "operationId": "status", "summary": "Server health: session, storage channel, file/folder counts, db path", "responses": { "200": { "description": "{version, telegram_session, storage_channel, files, folders, db_path}" } } }
    },
    "/api/health": {
      "get": { "tags": ["System"], "operationId": "health", "summary": "Runtime health", "responses": { "200": { "description": "{version, uptime_seconds, goroutines, memory_alloc_bytes, db_bytes}" } } }
    },
    "/api/stats": {
      "get": { "tags": ["Library"], "operationId": "stats", "summary": "Storage analytics: totals, mime breakdown, recent + largest files", "parameters": [{ "name": "limit", "in": "query", "schema": { "type": "integer" } }], "responses": { "200": { "description": "{files, folders, bytes, breakdown[], recent[], largest[]}" } } }
    },
    "/api/favorites": {
      "get": { "tags": ["Library"], "operationId": "listFavorites", "summary": "List starred files", "responses": { "200": { "description": "files" } } }
    },
    "/api/duplicates": {
      "get": { "tags": ["Library"], "operationId": "duplicates", "summary": "SHA-256 duplicate groups (or ?sha= for members)", "parameters": [{ "name": "sha", "in": "query", "schema": { "type": "string" } }], "responses": { "200": { "description": "groups or files" } } }
    },
    "/api/storage/verify": {
      "post": { "tags": ["System"], "operationId": "verifyStorage", "summary": "Verify recent files are retrievable from the channel", "responses": { "200": { "description": "{checked, ok, failed[]}" }, "503": { "description": "telegram not ready" } } }
    },
    "/api/trash": {
      "get": { "tags": ["Trash"], "operationId": "listTrash", "summary": "List trashed files and folders", "responses": { "200": { "description": "{files, folders}" } } }
    },
    "/api/trash/restore": {
      "post": { "tags": ["Trash"], "operationId": "restoreTrash", "summary": "Restore a trashed item", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["kind", "id"], "properties": { "kind": { "type": "string", "enum": ["file", "folder"] }, "id": { "type": "string" } } } } } }, "responses": { "200": { "description": "restored" } } }
    },
    "/api/trash/empty": {
      "post": { "tags": ["Trash"], "operationId": "emptyTrash", "summary": "Purge all trash permanently", "responses": { "204": { "description": "emptied" } } }
    },
    "/api/trash/{kind}/{id}": {
      "delete": { "tags": ["Trash"], "operationId": "purgeTrashItem", "summary": "Purge one trashed item forever", "parameters": [{ "name": "kind", "in": "path", "required": true, "schema": { "type": "string", "enum": ["file", "folder"] } }, { "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "purged" }, "404": { "description": "not found" } } }
    },
    "/api/audit": {
      "get": { "tags": ["Admin"], "operationId": "auditLog", "summary": "Audit log", "parameters": [{ "name": "action", "in": "query", "schema": { "type": "string" } }, { "name": "limit", "in": "query", "schema": { "type": "integer" } }, { "name": "offset", "in": "query", "schema": { "type": "integer" } }], "responses": { "200": { "description": "entries" } } }
    },
    "/api/notifications": {
      "get": { "tags": ["Admin"], "operationId": "notifications", "summary": "Recent activity for the notification center", "responses": { "200": { "description": "notifications" } } }
    },
    "/api/tokens": {
      "get": { "tags": ["Admin"], "operationId": "listTokens", "summary": "List API tokens (hashes never exposed)", "responses": { "200": { "description": "tokens" } } },
      "post": { "tags": ["Admin"], "operationId": "createToken", "summary": "Create API token (secret shown once)", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["name"], "properties": { "name": { "type": "string" } } } } } }, "responses": { "201": { "description": "{id, name, prefix, token}" } } }
    },
    "/api/tokens/{id}": {
      "delete": { "tags": ["Admin"], "operationId": "revokeToken", "summary": "Revoke an API token", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "revoked" } } }
    },
    "/api/tokens/{id}/purge": {
      "delete": { "tags": ["Admin"], "operationId": "purgeToken", "summary": "Delete an API token row", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "deleted" } } }
    },
    "/api/sessions": {
      "get": { "tags": ["Admin"], "operationId": "listSessions", "summary": "List active web sessions", "responses": { "200": { "description": "sessions" } } }
    },
    "/api/sessions/{id}": {
      "delete": { "tags": ["Admin"], "operationId": "revokeSession", "summary": "Revoke a web session", "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string" } }], "responses": { "204": { "description": "revoked" } } }
    },
    "/api/settings/password": {
      "post": { "tags": ["Admin"], "operationId": "changePassword", "summary": "Change the dashboard password", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["current", "new"], "properties": { "current": { "type": "string" }, "new": { "type": "string", "minLength": 8 } } } } } }, "responses": { "200": { "description": "changed" }, "403": { "description": "wrong current password" } } }
    },
    "/api/settings/domain": {
      "get": { "tags": ["Admin"], "operationId": "domainStatus", "summary": "Public domain + HTTPS status (superuser only)", "responses": { "200": { "description": "{domain, active, cert_expiry, last_error, hint}" }, "403": { "description": "superuser required" } } },
      "post": { "tags": ["Admin"], "operationId": "domainSave", "summary": "Set the public domain and enable automatic HTTPS (superuser only)", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "properties": { "domain": { "type": "string" }, "email": { "type": "string" } } } } } }, "responses": { "200": { "description": "saved" }, "400": { "description": "invalid domain" }, "403": { "description": "superuser required" } } }
    },
    "/api/settings/domain/retry": {
      "post": { "tags": ["Admin"], "operationId": "domainRetry", "summary": "Restart HTTPS listeners (superuser only)", "responses": { "200": { "description": "restarted" }, "403": { "description": "superuser required" } } }
    },
    "/api/sync": {
      "post": { "tags": ["System"], "operationId": "syncChannel", "summary": "Rebuild the file catalog from documents stored in the Telegram Storage Channel", "description": "Idempotent reconciliation matched by Telegram message ID. Recovers files after a database wipe, fresh volume, or server restart. Never creates a new channel.", "responses": { "200": { "description": "{scanned, inserted, updated, total}" }, "503": { "description": "telegram not ready or storage channel not bound" } } }
    },
    "/api/sync/runs": {
      "get": { "tags": ["System"], "operationId": "syncRuns", "summary": "Sync history", "parameters": [{ "name": "limit", "in": "query", "schema": { "type": "integer" } }], "responses": { "200": { "description": "runs" } } }
    },
    "/api/telegram/wizard/start": {
      "post": { "tags": ["Telegram"], "operationId": "wizardStart", "summary": "Start browser-based Telegram auth wizard (superuser only)", "description": "Sends the login code via Telegram when the stored session is dead. Returns {id, phase, success}; poll status until waiting_code. A still-valid session returns done without any SMS.", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["phone"], "properties": { "phone": { "type": "string", "example": "+628123456789" } } } } } }, "responses": { "200": { "description": "{id, phase, success, error}" }, "403": { "description": "superuser required" } } }
    },
    "/api/telegram/wizard/status": {
      "get": { "tags": ["Telegram"], "operationId": "wizardStatus", "summary": "Poll wizard phase (superuser only)", "parameters": [{ "name": "id", "in": "query", "required": true, "schema": { "type": "string" } }], "responses": { "200": { "description": "{phase: sending_code|waiting_code|waiting_password|error|done, success, error}" }, "403": { "description": "superuser required" } } }
    },
    "/api/telegram/wizard/submit": {
      "post": { "tags": ["Telegram"], "operationId": "wizardSubmit", "summary": "Submit login code or 2FA password (superuser only)", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["id"], "properties": { "id": { "type": "string" }, "code": { "type": "string" }, "password": { "type": "string" } } } } } }, "responses": { "200": { "description": "{ok}" }, "403": { "description": "superuser required" } } }
    },
    "/api/telegram/wizard/discard": {
      "post": { "tags": ["Telegram"], "operationId": "wizardDiscard", "summary": "Clean up a finished wizard (superuser only)", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["id"], "properties": { "id": { "type": "string" } } } } } }, "responses": { "200": { "description": "{ok}" }, "403": { "description": "superuser required" } } }
    },
    "/api/telegram/wizard/cancel": {
      "post": { "tags": ["Telegram"], "operationId": "wizardCancel", "summary": "Abandon an in-flight wizard so a fresh code can be requested (superuser only)", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["id"], "properties": { "id": { "type": "string" } } } } } }, "responses": { "200": { "description": "{ok}" }, "403": { "description": "superuser required" } } }
    },
    "/api/telegram/wizard/needed": {
      "get": { "tags": ["Telegram"], "operationId": "wizardNeeded", "summary": "Whether browser pairing is needed (superuser only)", "responses": { "200": { "description": "{configured, client_available, storage_channel, telegram_authorized, superuser, app_id_set, app_hash_set}" }, "403": { "description": "superuser required" } } }
    },
    "/api/telegram/onboard": {
      "post": { "tags": ["Telegram"], "operationId": "wizardOnboard", "summary": "Alias of wizard start (superuser only)", "requestBody": { "required": true, "content": { "application/json": { "schema": { "type": "object", "required": ["phone"], "properties": { "phone": { "type": "string", "example": "+628123456789" } } } } } }, "responses": { "200": { "description": "{id, phase, success, error}" }, "403": { "description": "superuser required" } } }
    },
    "/api/telegram/state": {
      "get": { "tags": ["Telegram"], "operationId": "telegramState", "summary": "Verified Telegram backend state (superuser only)", "description": "Honest state: connected only after a live probe; otherwise authentication_required / channel_unavailable / not_configured. ?live=1 forces a real end-to-end check.", "parameters": [{ "name": "live", "in": "query", "schema": { "type": "string" } }], "responses": { "200": { "description": "{state, message, checks, verified_at}" }, "403": { "description": "superuser required" } } }
    },
    "/api/telegram/test": {
      "post": { "tags": ["Telegram"], "operationId": "telegramTest", "summary": "Live connection test, bypasses cache (superuser only)", "responses": { "200": { "description": "{state, message}" }, "403": { "description": "superuser required" } } }
    },
    "/api/update/status": {
      "get": { "tags": ["System"], "operationId": "updateStatus", "summary": "Update check: current vs latest release + upgrade command (superuser only)", "parameters": [{ "name": "refresh", "in": "query", "schema": { "type": "string" } }], "responses": { "200": { "description": "{current, latest, available, url, hint, install_kind, checked_at, checking}" }, "403": { "description": "superuser required" } } }
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
.neu-sub{font-size:13px;opacity:.75;max-width:860px}
.neu-links{margin-left:auto;display:flex;gap:10px;flex-wrap:wrap}
.neu-btn{background:var(--neu-bg);border:none;color:var(--ink);font-weight:700;font-size:13px;padding:10px 16px;border-radius:12px;box-shadow:4px 4px 8px var(--neu-dark),-4px -4px 8px var(--neu-light);cursor:pointer;text-decoration:none}
.neu-btn:active{box-shadow:inset 4px 4px 8px var(--neu-dark),inset -4px -4px 8px var(--neu-light)}
.neu-btn.primary{color:var(--accent)}
#ui{padding:18px}.swagger-ui .wrapper{background:transparent}
.auth-hint{margin:0 28px 10px;font-size:12.5px;background:var(--neu-bg);padding:12px 16px;border-radius:12px;box-shadow:inset 4px 4px 8px var(--neu-dark),inset -4px -4px 8px var(--neu-light);overflow-x:auto;line-height:1.7}
.auth-hint code{font-family:ui-monospace,Menlo,Consolas,monospace}
.swagger-ui .auth-wrapper .authorize{border-color:var(--accent)}
</style>
</head><body>
<div class="neu-bar"><div class="neu-logo">◈</div><div><div class="neu-title">tDocs API</div><div class="neu-sub">%s · cookie <b>tdocs_session</b> atau <b>Authorize → bearerAuth → Bearer &lt;admin password&gt;</b> · klik <b>Try it out</b> lalu <b>Execute</b></div></div>
<div class="neu-links"><a class="neu-btn primary" href="/api">GET /api (index JSON)</a><a class="neu-btn" href="/api/openapi.json">openapi.json</a><a class="neu-btn" href="/">← Dashboard</a></div></div>
<p class="auth-hint"><code>curl -H "Authorization: Bearer ADMIN_PASS" "http://localhost:8080/api/cdn/files?mime=video/&amp;limit=100"</code><br><code>&lt;video controls preload="metadata" src="http://localhost:8080/cdn/{id}/stream"&gt;</code> · cari endpoint lewat kolom <b>Filter</b> di bawah · durasi tiap request tampil otomatis</p>
<div id="ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
<script>
window.onload = function() {
  SwaggerUIBundle({
    url: "/api/openapi.json",
    dom_id: "#ui",
    deepLinking: true,
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
    layout: "StandaloneLayout",
    filter: true,
    persistAuthorization: true,
    displayRequestDuration: true,
    docExpansion: "list",
    defaultModelsExpandDepth: 3,
    tryItOutEnabled: true,
    syntaxHighlight: { theme: "agate" }
  });
};
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
