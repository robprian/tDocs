package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tdocs/internal/db"
	"tdocs/internal/telegram"
)

const ClientChunkSize = 5 * 1024 * 1024 // 5 MB per client HTTP chunk

// MaxClientChunk caps a single HTTP chunk (5 MB + multipart overhead + slack).
const MaxClientChunk = 12 << 20 // 12 MB

// newUploadSession validates the target folder and creates a session pinned
// to the current storage backend for new uploads.
func (s *Server) newUploadSession(folderID *string, name string, size int64, mimeType string) (*db.UploadSession, error) {
	if folderID != nil {
		if _, err := s.db.GetFolder(*folderID); err != nil {
			return nil, fmt.Errorf("target folder not found: refresh and pick another folder")
		}
	}
	totalParts := int((size + telegram.PartSize - 1) / telegram.PartSize)
	if totalParts == 0 {
		totalParts = 1
	}
	session, err := s.db.CreateUploadSession(folderID, name, size, mimeType, totalParts, telegram.GenerateRandomID())
	if err != nil {
		return nil, err
	}
	kind := s.storageBackendKind()
	if kind == "disk" {
		if err := s.db.SetUploadSessionStorageKind(session.ID, "disk"); err != nil {
			return nil, err
		}
		session.StorageKind = "disk"
	} else {
		session.StorageKind = "telegram"
	}
	return session, nil
}

func (s *Server) handleUploadInit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string  `json:"name"`
		Size     int64   `json:"size"`
		MimeType string  `json:"mime_type"`
		FolderID *string `json:"folder_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" || body.Size <= 0 {
		http.Error(w, "invalid upload init payload: need {name, size>0, mime_type?, folder_id?}", http.StatusBadRequest)
		return
	}
	if body.Size > 4<<30 {
		http.Error(w, "file too large: max 4 GB per file (Telegram Premium limit)", http.StatusBadRequest)
		return
	}
	if body.MimeType == "" {
		body.MimeType = "application/octet-stream"
	}
	// Normalize empty-string folder to root (nil) so "" from clients != missing folder.
	if body.FolderID != nil && strings.TrimSpace(*body.FolderID) == "" {
		body.FolderID = nil
	}
	name, err := validName(body.Name, 255)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Fail fast with an actionable message instead of a cryptic MTProto error
	// halfway through the chunks. Disk uploads need no Telegram preflight.
	if s.storageBackendKind() != "disk" {
		if err := s.telegramPreflight(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	session, err := s.newUploadSession(body.FolderID, strings.TrimSpace(name), body.Size, body.MimeType)
	if err != nil {
		if strings.Contains(err.Error(), "target folder not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":           session.ID,
		"session_id":   session.ID,
		"chunk_size":   ClientChunkSize,
		"storage_kind": session.StorageKind,
		"total_parts":  session.TotalParts,
		"total_chunks": int((body.Size + ClientChunkSize - 1) / ClientChunkSize),
	})
}

// writeUploadChunk stores one client chunk for a session. It is shared by
// the authenticated upload API and the public file-request upload API.
// Telegram chunks go straight to MTProto parts; disk chunks append to a
// staging file (in-order chunks enforced, re-sends of finished chunks are
// no-ops so a lost response cannot duplicate bytes).
func (s *Server) writeUploadChunk(ctx context.Context, session *db.UploadSession, chunkIndex int, chunkData []byte) error {
	// Accumulate a running SHA-256 for duplicate detection and integrity.
	// Single-process server: an in-memory ledger keyed by session is enough.
	s.uploadHashMu.Lock()
	he, ok := s.uploadHash[session.ID]
	if !ok {
		he = &hashEntry{h: sha256.New(), at: time.Now()}
		s.uploadHash[session.ID] = he
		// Opportunistic sweep of abandoned sessions (>2h).
		for sid, old := range s.uploadHash {
			if time.Since(old.at) > 2*time.Hour {
				delete(s.uploadHash, sid)
			}
		}
	}
	_, _ = he.h.Write(chunkData)
	s.uploadHashMu.Unlock()

	if session.StorageKind == "disk" {
		return s.writeDiskChunk(session, chunkIndex, chunkData)
	}

	// Slices 5MB chunk into 512KB MTProto parts.
	partsPerChunk := ClientChunkSize / telegram.PartSize // 10
	basePartIndex := chunkIndex * partsPerChunk
	if basePartIndex >= session.TotalParts {
		return fmt.Errorf("chunk_index out of range for this file size")
	}

	// gotd MTProto calls MUST run inside ClientManager.Run. The dashboard keeps
	// a background Run for channel discovery, but request-scoped calls need
	// their own Run (same pattern as snapshot handlers).
	return s.tg.Do(ctx, func(runCtx context.Context) error {
		for offset := 0; offset < len(chunkData); offset += telegram.PartSize {
			end := offset + telegram.PartSize
			if end > len(chunkData) {
				end = len(chunkData)
			}
			partSlice := chunkData[offset:end]
			currentPartIndex := basePartIndex + (offset / telegram.PartSize)

			if currentPartIndex >= session.TotalParts {
				break
			}

			if err := s.tg.UploadPart(runCtx, session.TelegramFileID, currentPartIndex, session.TotalParts, partSlice, s.limiter); err != nil {
				return fmt.Errorf("upload MTProto part %d: %w", currentPartIndex, err)
			}

			if _, dbErr := s.db.IncrementUploadPart(session.ID); dbErr != nil {
				return fmt.Errorf("track uploaded part: %w", dbErr)
			}
			if err := s.limiter.Pace(runCtx); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Server) writeDiskChunk(session *db.UploadSession, chunkIndex int, chunkData []byte) error {
	staging := s.diskStagingPath(session.ID)
	if err := os.MkdirAll(filepath.Dir(staging), 0o755); err != nil {
		return err
	}
	var stagedSize int64
	if fi, err := os.Stat(staging); err == nil {
		stagedSize = fi.Size()
	}
	totalChunks := int((session.Size + ClientChunkSize - 1) / ClientChunkSize)
	// A chunk re-sent after its bytes already landed (lost response) is a
	// no-op instead of a duplicate append.
	if stagedSize >= session.Size && chunkIndex < totalChunks {
		return nil
	}
	if int64(chunkIndex) != stagedSize/ClientChunkSize {
		return fmt.Errorf("out-of-order chunk: expected chunk %d", stagedSize/ClientChunkSize)
	}
	f, err := os.OpenFile(staging, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(chunkData); err != nil {
		return err
	}
	return nil
}

func (s *Server) handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	// Guard: reject oversized bodies before parsing (12 MB cap).
	r.Body = http.MaxBytesReader(w, r.Body, MaxClientChunk+(1<<20))
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "parse multipart form error: chunk must be <= 5 MB (field `chunk`)", http.StatusBadRequest)
		return
	}

	sessionID := strings.TrimSpace(r.FormValue("session_id"))
	chunkIndex, err := strconv.Atoi(strings.TrimSpace(r.FormValue("chunk_index")))
	if err != nil || sessionID == "" || chunkIndex < 0 {
		http.Error(w, "invalid session_id/chunk_index", http.StatusBadRequest)
		return
	}

	session, err := s.db.GetUploadSession(sessionID)
	if err != nil {
		http.Error(w, "upload session not found or expired: re-init upload", http.StatusNotFound)
		return
	}
	// Disk sessions must not require Telegram; the session's pinned kind
	// is authoritative, not the current setting.
	if session.StorageKind != "disk" {
		if err := s.telegramReady(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
	}

	file, _, err := r.FormFile("chunk")
	if err != nil {
		http.Error(w, "chunk payload missing: multipart field `chunk` required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	chunkData, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "read chunk data failed", http.StatusInternalServerError)
		return
	}
	if len(chunkData) == 0 {
		http.Error(w, "empty chunk: client sent 0 bytes", http.StatusBadRequest)
		return
	}
	if len(chunkData) > MaxClientChunk {
		http.Error(w, "chunk too large: max 5 MB per chunk", http.StatusBadRequest)
		return
	}

	if err := s.writeUploadChunk(r.Context(), session, chunkIndex, chunkData); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "chunk_index": chunkIndex})
}

// finishUpload commits an upload session to a catalog file record. It is
// shared by the authenticated and file-request complete endpoints.
func (s *Server) finishUpload(ctx context.Context, session *db.UploadSession) (*db.File, string, error) {
	// Finalize the content hash accumulated across chunks.
	s.uploadHashMu.Lock()
	he := s.uploadHash[session.ID]
	delete(s.uploadHash, session.ID)
	s.uploadHashMu.Unlock()
	shaHex := ""
	if he != nil {
		shaHex = hex.EncodeToString(he.h.Sum(nil))
	}

	if session.StorageKind == "disk" {
		return s.finishDiskUpload(session, shaHex)
	}

	var msgID int
	var docID, accessHash int64
	if err := s.tg.Do(ctx, func(runCtx context.Context) error {
		var runErr error
		msgID, docID, accessHash, runErr = s.tg.CompleteUpload(runCtx, session.TelegramFileID, session.TotalParts, session.Name, session.MimeType)
		return runErr
	}); err != nil {
		return nil, "", fmt.Errorf("finalize upload on Telegram: %w", err)
	}

	// Same-name versioning: archive the previous record instead of forking
	// the listing with two identical names.
	if existing, verr := s.db.FindByNameInFolder(session.FolderID, session.Name); verr == nil && existing != nil {
		if _, aerr := s.db.ArchiveVersion(existing); aerr == nil {
			_ = s.db.DeleteFile(existing.ID)
		}
	}

	file, err := s.db.CreateFile(
		session.FolderID,
		session.Name,
		session.Size,
		session.MimeType,
		msgID,
		strconv.FormatInt(docID, 10),
		strconv.FormatInt(accessHash, 10),
		shaHex,
	)
	if err != nil {
		return nil, "", fmt.Errorf("save file metadata: %w", err)
	}

	_ = s.db.DeleteUploadSession(session.ID)
	return file, shaHex, nil
}

func (s *Server) finishDiskUpload(session *db.UploadSession, shaHex string) (*db.File, string, error) {
	staging := s.diskStagingPath(session.ID)
	fi, err := os.Stat(staging)
	if err != nil {
		return nil, "", fmt.Errorf("staging file missing: re-upload the file")
	}
	if fi.Size() != session.Size {
		_ = os.Remove(staging)
		return nil, "", fmt.Errorf("incomplete upload: got %d of %d bytes", fi.Size(), session.Size)
	}
	if err := os.MkdirAll(s.diskObjectsDir(), 0o755); err != nil {
		return nil, "", err
	}

	// Same-name handling: disk files have no Telegram versions to archive,
	// so the previous record (and its bytes) is replaced outright.
	if existing, verr := s.db.FindByNameInFolder(session.FolderID, session.Name); verr == nil && existing != nil {
		s.deleteDiskObject(existing)
		_ = s.db.DeleteFile(existing.ID)
	}

	// Create the record pointing at the staging name first, then move the
	// bytes to their final object path keyed by the file ID.
	file, err := s.db.CreateFileWithStorage(session.FolderID, session.Name, session.Size, session.MimeType, "disk", filepath.Join("objects", session.ID), shaHex)
	if err != nil {
		return nil, "", fmt.Errorf("save file metadata: %w", err)
	}
	finalRel := filepath.Join("objects", file.ID)
	finalAbs, ok := s.diskObjectAbs(finalRel)
	if !ok {
		_ = s.db.DeleteFile(file.ID)
		return nil, "", fmt.Errorf("invalid object path")
	}
	if err := os.Rename(staging, finalAbs); err != nil {
		_ = s.db.DeleteFile(file.ID)
		return nil, "", fmt.Errorf("move staged file: %w", err)
	}
	if err := s.db.SetFileStorage(file.ID, "disk", finalRel); err != nil {
		_ = os.Remove(finalAbs)
		_ = s.db.DeleteFile(file.ID)
		return nil, "", err
	}
	file.StorageKind = "disk"
	file.LocalPath = finalRel

	_ = s.db.DeleteUploadSession(session.ID)
	return file, shaHex, nil
}

// afterUploadComplete runs the shared post-upload tail: audit, background
// mirror for Telegram files, and the duplicate hint header.
func (s *Server) afterUploadComplete(w http.ResponseWriter, r *http.Request, file *db.File, shaHex string) {
	s.audit(r, "upload_complete", fmt.Sprintf("%s (%d bytes)", file.Name, file.Size))

	// Multi-account Mirror: when enabled, copy the new file to every
	// ready backup account in the background. Fire-and-forget on
	// purpose — a mirror failure must never fail the upload itself,
	// and the job records its own errors for the Sync Center to show.
	// Disk files are skipped inside maybeMirrorNewFile.
	go s.maybeMirrorNewFile(*file)

	// Duplicate hint without breaking the file-record contract: clients that
	// care read X-Duplicate-Of; GET /api/duplicates lists the group.
	if shaHex != "" {
		if same, _ := s.db.ListFilesBySHA(shaHex); len(same) > 1 {
			ids := make([]string, 0, len(same)-1)
			for _, f := range same {
				if f.ID != file.ID {
					ids = append(ids, f.ID)
				}
			}
			if len(ids) > 0 {
				w.Header().Set("X-Duplicate-Of", strings.Join(ids, ","))
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(file)
}

func (s *Server) handleUploadComplete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.SessionID) == "" {
		http.Error(w, "invalid payload: need {session_id}", http.StatusBadRequest)
		return
	}

	session, err := s.db.GetUploadSession(strings.TrimSpace(body.SessionID))
	if err != nil {
		http.Error(w, "upload session not found", http.StatusNotFound)
		return
	}
	if session.StorageKind != "disk" {
		if err := s.telegramReady(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
	}

	file, shaHex, err := s.finishUpload(r.Context(), session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.afterUploadComplete(w, r, file, shaHex)
}
