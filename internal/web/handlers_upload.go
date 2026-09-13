package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tdocs/internal/telegram"
)

const ClientChunkSize = 5 * 1024 * 1024 // 5 MB per client HTTP chunk

// MaxClientChunk caps a single HTTP chunk (5 MB + multipart overhead + slack).
const MaxClientChunk = 12 << 20 // 12 MB

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
	body.Name = name
	// Fail fast with an actionable message instead of a cryptic MTProto error
	// halfway through the chunks.
	if err := s.telegramPreflight(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	// Validate the target folder now, not at complete (after Telegram upload).
	if body.FolderID != nil {
		if _, err := s.db.GetFolder(*body.FolderID); err != nil {
			http.Error(w, "target folder not found: refresh and pick another folder", http.StatusNotFound)
			return
		}
	}

	totalParts := int((body.Size + telegram.PartSize - 1) / telegram.PartSize)
	if totalParts == 0 {
		totalParts = 1
	}

	tgFileID := telegram.GenerateRandomID()
	session, err := s.db.CreateUploadSession(body.FolderID, strings.TrimSpace(body.Name), body.Size, body.MimeType, totalParts, tgFileID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":           session.ID,
		"session_id":   session.ID,
		"chunk_size":   ClientChunkSize,
		"total_parts":  totalParts,
		"total_chunks": int((body.Size + ClientChunkSize - 1) / ClientChunkSize),
	})
}

func (s *Server) handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	if err := s.telegramReady(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
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

	// Slices 5MB chunk into 512KB MTProto parts.
	partsPerChunk := ClientChunkSize / telegram.PartSize // 10
	basePartIndex := chunkIndex * partsPerChunk
	if basePartIndex >= session.TotalParts {
		http.Error(w, "chunk_index out of range for this file size", http.StatusBadRequest)
		return
	}

	// gotd MTProto calls MUST run inside ClientManager.Run. The dashboard keeps
	// a background Run for channel discovery, but request-scoped calls need
	// their own Run (same pattern as snapshot handlers).
	runErr := s.tg.Do(r.Context(), func(runCtx context.Context) error {
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
	if runErr != nil {
		http.Error(w, runErr.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "chunk_index": chunkIndex})
}

func (s *Server) handleUploadComplete(w http.ResponseWriter, r *http.Request) {
	if err := s.telegramReady(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
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

	// Finalize the content hash accumulated across chunks.
	s.uploadHashMu.Lock()
	he := s.uploadHash[session.ID]
	delete(s.uploadHash, session.ID)
	s.uploadHashMu.Unlock()
	shaHex := ""
	if he != nil {
		shaHex = hex.EncodeToString(he.h.Sum(nil))
	}

	var msgID int
	var docID, accessHash int64
	if err := s.tg.Do(r.Context(), func(runCtx context.Context) error {
		var runErr error
		msgID, docID, accessHash, runErr = s.tg.CompleteUpload(runCtx, session.TelegramFileID, session.TotalParts, session.Name, session.MimeType)
		return runErr
	}); err != nil {
		http.Error(w, fmt.Sprintf("finalize upload on Telegram: %v", err), http.StatusInternalServerError)
		return
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
		http.Error(w, fmt.Sprintf("save file metadata: %v", err), http.StatusInternalServerError)
		return
	}

	_ = s.db.DeleteUploadSession(session.ID)
	s.audit(r, "upload_complete", fmt.Sprintf("%s (%d bytes)", file.Name, file.Size))

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
