package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"

	"tdocs/internal/telegram"
)

type apiEndpoint struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Auth        string `json:"auth"`
	Description string `json:"description"`
}

// handleAPIIndex is the machine-readable endpoint catalog.
// GET /api — auth: cookie session OR Bearer <admin password> (same as drive API).
// Human docs: GET /docs (Swagger UI). Raw contract: GET /api/openapi.json.
func (s *Server) handleAPIIndex(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	base := s.cdnBaseURL(r)
	endpoints := []apiEndpoint{
		{"GET", "/api", "key", "This index: every endpoint with auth + embed URLs"},
		{"GET", "/api/status", "key", "Health: session, storage channel, counts, db path"},
		{"POST", "/api/sync", "key", "Rebuild file catalog from Telegram Storage Channel documents"},
		{"GET", "/api/openapi.json", "none", "OpenAPI 3.0 contract (machine-readable)"},
		{"GET", "/api/folders?folder_id=", "key", "List virtual folders"},
		{"POST", "/api/folders", "key", `Create folder {name, parent_id?}`},
		{"PUT", "/api/folders/{id}", "key", "Rename/move folder"},
		{"DELETE", "/api/folders/{id}", "key", "Delete folder (cascades)"},
		{"GET", "/api/files?folder_id=&search=", "key", "List files"},
		{"GET", "/api/files/{id}", "key", "Get one file record"},
		{"PUT", "/api/files/{id}", "key", "Rename/move file"},
		{"DELETE", "/api/files/{id}", "key", "Delete file"},
		{"GET", "/api/files/{id}/stream", "key", "Stream file bytes (Range seeking, ETag)"},
		{"GET", "/api/files/{id}/download", "key", "Download file (attachment)"},
		{"GET", "/api/files/{id}/meta", "key", "File + favorite flag, version/share/duplicate counts"},
		{"POST", "/api/files/{id}/copy", "key", "Duplicate record (same Telegram object, no re-upload)"},
		{"GET", "/api/folders/stats", "key", "Recursive file count + bytes per folder"},
		{"POST", "/api/storage/verify", "key", "Re-resolve recent files against the channel"},
		{"POST", "/api/files/{id}/ticket", "key", "Mint signed expiring download/stream URLs"},
		{"POST", "/api/files/{id}/favorite", "key", "Star a file"},
		{"DELETE", "/api/files/{id}/favorite", "key", "Unstar a file"},
		{"GET", "/api/files/{id}/versions", "key", "List file versions"},
		{"POST", "/api/files/{id}/versions/{vid}/restore", "key", "Restore a version"},
		{"DELETE", "/api/files/{id}/versions/{vid}", "key", "Delete a version"},
		{"POST", "/api/files/bulk", "key", "Bulk trash|restore|purge|move|favorite {ids[], folder_id?}"},
		{"GET", "/api/favorites", "key", "List starred files"},
		{"GET", "/api/duplicates", "key", "SHA-256 duplicate groups (or ?sha= for members)"},
		{"GET", "/api/stats", "key", "Analytics: totals, breakdown, recent, largest"},
		{"GET", "/api/notifications", "key", "Recent activity feed"},
		{"GET", "/api/trash", "key", "List trashed files + folders"},
		{"POST", "/api/trash/restore", "key", "Restore {kind: file|folder, id}"},
		{"DELETE", "/api/trash/{kind}/{id}", "key", "Purge one item forever"},
		{"POST", "/api/trash/empty", "key", "Purge all trash"},
		{"GET", "/api/audit", "key", "Audit log (?action=&limit=&offset=)"},
		{"GET", "/api/tokens", "key", "List API tokens"},
		{"POST", "/api/tokens", "key", "Create API token (secret shown once)"},
		{"DELETE", "/api/tokens/{id}", "key", "Revoke API token"},
		{"DELETE", "/api/tokens/{id}/purge", "key", "Delete API token"},
		{"GET", "/api/sessions", "key", "List web sessions"},
		{"DELETE", "/api/sessions/{id}", "key", "Revoke web session"},
		{"POST", "/api/settings/password", "key", "Change admin password (Argon2id)"},
		{"GET", "/api/health", "key", "Runtime health: uptime, memory, db, sessions"},
		{"GET", "/api/sync/runs", "key", "Sync history"},
		{"POST", "/api/upload/init", "key", "Open session {name,size,mime_type?,folder_id?} -> {id,chunk_size,total_parts}"},
		{"POST", "/api/upload/chunk", "key", "Upload 5MB chunk (multipart: session_id, chunk_index, chunk)"},
		{"POST", "/api/upload/complete", "key", "Assemble parts -> 201 file record"},
		{"POST", "/api/share", "key", "Create share link {file_id,password?,expiry_days?,max_downloads?,preview_only?}"},
		{"GET", "/api/shares", "key", "List share links"},
		{"DELETE", "/api/shares/{id}", "key", "Revoke share link"},
		{"GET", "/s/{token}", "none", "Public share landing page"},
		{"GET", "/s/{token}/stream", "none*", "Guest stream (*password cookie when locked)"},
		{"GET", "/s/{token}/download", "none*", "Guest download"},
		{"GET", "/api/cdn/files?search=&mime=video/&limit=", "key", "CDN catalog for film sites -> {files:[{id,name,stream_url,download_url}],total}"},
		{"GET", "/api/cdn/files/{id}", "key", "One CDN entry with embed URLs"},
		{"GET", "/cdn/{id}/stream", "public*", "Embed <video>/<audio>/<img> (Range, CORS, ETag)"},
		{"GET", "/api/snapshots", "key", "List Database Snapshots"},
		{"POST", "/api/snapshots", "key", "Create + upload snapshot"},
		{"POST", "/api/snapshots/{id}/restore", "key", "Point-in-Time Restore"},
		{"GET", "/api/snapshots/{id}/download", "key", "Download snapshot .db.gz"},
		{"DELETE", "/api/snapshots/{id}", "key", "Delete snapshot"},
		{"GET", "/api/update/status", "superuser", "Update check: current vs latest release + upgrade command"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":    "tDocs API",
		"version": s.currentVersion(),
		"auth": map[string]string{
			"dashboard": "cookie tdocs_session=authenticated (POST /login)",
			"machine":   "Authorization: Bearer <TDOCS_ADMIN_PASSWORD> or ?api_key=<password>",
			"docs":      "GET /docs (Swagger UI) · GET /api/openapi.json",
		},
		"cdn": map[string]any{
			"public":        s.cfg.CDNPublic,
			"embed_example": base + "/cdn/{id}/stream",
			"video_tag":     `<video controls preload="metadata" src="` + base + `/cdn/{id}/stream"></video>`,
		},
		"auth_note": "key = cookie OR Bearer; public* = open when TDOCS_CDN_PUBLIC=true else Bearer; none = no auth",
		"endpoints": endpoints,
	})
}

// handleStatus reports live server health for the dashboard sidebar widget.
// GET /api/status — same auth as the drive API. All checks are local
// (DB + settings); nothing pings Telegram, so it is cheap to poll.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	_, sessErr := s.db.GetSetting("telegram_session")
	_, authErr := s.db.GetSetting("telegram_authorized")
	_, chanIDErr := s.db.GetSetting("storage_channel_id")
	_, chanHashErr := s.db.GetSetting("storage_channel_hash")
	files, _ := s.db.CountFiles()
	folders, _ := s.db.CountFolders()
	dbPath, _ := filepath.Abs(s.cfg.DBPath)

	// Verified state (cached probe) — never inferred from config alone.
	state, detail := s.telegramState(r.Context(), false)
	last := s.lastTelegramHealth()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"version":              s.currentVersion(),
		"telegram_session":     sessErr == nil,
		"telegram_authorized":  authErr == nil,
		"telegram_connected":   state == TgConnected,
		"telegram_state":       string(state),
		"telegram_detail":      detail,
		"telegram_verified_at": last.At,
		"storage_channel":      chanIDErr == nil && chanHashErr == nil,
		"files":                files,
		"folders":              folders,
		"db_path":              dbPath,
	})
}

// handleSyncChannel reconciles the local catalog with the documents that live
// in the Telegram Storage Channel. This recovers files after a database wipe
// or a fresh server start, and is safe to run repeatedly.
// POST /api/sync — same auth as the drive API.
func (s *Server) handleSyncChannel(w http.ResponseWriter, r *http.Request) {
	if !s.requireTelegram(w, r) {
		return
	}

	var res *telegram.SyncResult
	err := s.tg.Do(r.Context(), func(runCtx context.Context) error {
		var runErr error
		res, runErr = s.tg.SyncAndRecord(runCtx, "manual")
		return runErr
	})
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "telegram_storage_unavailable",
			"state":   string(telegramUnavailableState(err)),
			"message": telegram.ClassifyHealth(err),
		})
		return
	}
	s.audit(r, "sync_manual", fmt.Sprintf("scanned=%d inserted=%d updated=%d", res.Scanned, res.Inserted, res.Updated))

	// The background restore path may have swapped the DB reference; re-read
	// counts from the live handle for an accurate response.
	files, _ := s.db.CountFiles()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"scanned":  res.Scanned,
		"inserted": res.Inserted,
		"updated":  res.Updated,
		"total":    files,
		"message":  "Channel catalog synchronized",
	})
}
