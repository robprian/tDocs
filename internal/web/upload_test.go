package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"tdocs/internal/db"
	"tdocs/internal/storage"
	"tdocs/internal/telegram"
)

type uploadBackend struct {
	storage.Backend
	parts      map[int][]byte
	name, mime string
}

func (b *uploadBackend) IsAuthorized() bool                                           { return true }
func (b *uploadBackend) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
func (b *uploadBackend) UploadPart(_ context.Context, _ int64, index, _ int, data []byte, _ *telegram.SafeLimiter) error {
	b.parts[index] = append([]byte(nil), data...)
	return nil
}
func (b *uploadBackend) CompleteUpload(_ context.Context, _ int64, _ int, name, mime string) (int, int64, int64, error) {
	b.name, b.mime = name, mime
	return 1, 2, 3, nil
}

func TestUploadAcceptsFileTypesAndMultipleChunks(t *testing.T) {
	for _, tc := range []struct {
		name, mime string
		size       int
	}{
		{"photo.jpg", "image/jpeg", 64},
		{"image.webp", "image/webp", 128},
		{"config.json", "application/json", 256},
		{"book.pdf", "application/pdf", 1200 * 1024},
		{"movie.mp4", "video/mp4", 1200 * 1024},
		{"song.mp3", "audio/mpeg", 1200 * 1024},
		{"archive.zip", "application/zip", ClientChunkSize + 17},
		{"unknown.bin", "", 1200 * 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFeatureFixture(t)
			backend := &uploadBackend{parts: map[int][]byte{}}
			f.server.tg = backend
			if err := f.db.SetSetting("storage_channel_id", "1"); err != nil {
				t.Fatal(err)
			}
			init := f.do(t, "POST", "/api/upload/init", map[string]any{"name": tc.name, "mime_type": tc.mime, "size": tc.size})
			if init.Code != http.StatusOK {
				t.Fatalf("init: %d %s", init.Code, init.Body.String())
			}
			var session struct {
				ID        string `json:"id"`
				ChunkSize int    `json:"chunk_size"`
			}
			if err := json.Unmarshal(init.Body.Bytes(), &session); err != nil {
				t.Fatal(err)
			}
			data := bytes.Repeat([]byte("x"), tc.size)
			for offset, index := 0, 0; offset < len(data); offset, index = offset+session.ChunkSize, index+1 {
				end := min(offset+session.ChunkSize, len(data))
				var body bytes.Buffer
				form := multipart.NewWriter(&body)
				if err := form.WriteField("session_id", session.ID); err != nil {
					t.Fatal(err)
				}
				if err := form.WriteField("chunk_index", fmt.Sprint(index)); err != nil {
					t.Fatal(err)
				}
				part, err := form.CreateFormFile("chunk", tc.name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := part.Write(data[offset:end]); err != nil {
					t.Fatal(err)
				}
				if err := form.Close(); err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest("POST", "/api/upload/chunk", &body)
				req.Header.Set("Content-Type", form.FormDataContentType())
				req.Header.Set("X-CSRF-Token", f.csrf)
				req.AddCookie(f.cookie)
				rec := httptest.NewRecorder()
				f.server.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("chunk %d: %d %s", index, rec.Code, rec.Body.String())
				}
			}
			complete := f.do(t, "POST", "/api/upload/complete", map[string]string{"session_id": session.ID})
			if complete.Code != http.StatusCreated {
				t.Fatalf("complete: %d %s", complete.Code, complete.Body.String())
			}
			var stored db.File
			if err := json.Unmarshal(complete.Body.Bytes(), &stored); err != nil {
				t.Fatal(err)
			}
			mime := tc.mime
			if mime == "" {
				mime = "application/octet-stream"
			}
			if stored.Name != tc.name || stored.Size != int64(tc.size) || stored.MimeType != mime || backend.name != tc.name || backend.mime != mime {
				t.Fatalf("incorrect file metadata: %+v", stored)
			}
			var uploaded bytes.Buffer
			for index := 0; index < len(backend.parts); index++ {
				uploaded.Write(backend.parts[index])
			}
			if !bytes.Equal(uploaded.Bytes(), data) {
				t.Fatal("file bytes or part ordering changed")
			}
			digest := sha256.Sum256(data)
			if stored.SHA256 != hex.EncodeToString(digest[:]) {
				t.Fatal("uploaded content hash mismatch")
			}
		})
	}
}
