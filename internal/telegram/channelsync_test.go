package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/td/tg"
	"tdocs/internal/db"
)

func TestDocumentFromMessage_ExtractsUserFile(t *testing.T) {
	msg := &tg.Message{
		ID:   42,
		Date: 1700000000,
		Media: &tg.MessageMediaDocument{
			Document: &tg.Document{
				ID:            777,
				AccessHash:    888,
				FileReference: []byte{0x01, 0x02, 0x03},
				Size:          1024,
				MimeType:      "video/mp4",
				Attributes: []tg.DocumentAttributeClass{
					&tg.DocumentAttributeFilename{FileName: "film.mp4"},
				},
			},
		},
	}

	doc, ok := documentFromMessage(msg)
	if !ok {
		t.Fatalf("expected document to be extracted")
	}
	if doc.FileName != "film.mp4" || doc.DocumentID != 777 || doc.AccessHash != 888 || doc.Size != 1024 {
		t.Fatalf("unexpected document: %+v", doc)
	}
	if doc.MimeType != "video/mp4" {
		t.Fatalf("expected mime video/mp4, got %q", doc.MimeType)
	}
	if doc.MessageID != 42 {
		t.Fatalf("expected message id 42, got %d", doc.MessageID)
	}
	if got := doc.CreatedAt.Unix(); got != 1700000000 {
		t.Fatalf("expected created unix 1700000000, got %d", got)
	}
	if string(doc.FileReference) != string([]byte{0x01, 0x02, 0x03}) {
		t.Fatalf("expected file reference to be captured, got %v", doc.FileReference)
	}
	loc := doc.Location()
	if loc.ID != 777 || loc.AccessHash != 888 || string(loc.FileReference) != string([]byte{0x01, 0x02, 0x03}) {
		t.Fatalf("Location() must carry id, hash and file reference: %+v", loc)
	}
}

func TestDocumentFromMessage_SkipsSnapshots(t *testing.T) {
	for _, name := range []string{
		"tdocs-backup-20260101.db.gz",
		"robdocs-backup-20260101.db.gz",
		"teledrive-backup-20260101.db.gz",
	} {
		msg := &tg.Message{
			ID: 7,
			Media: &tg.MessageMediaDocument{
				Document: &tg.Document{
					ID:   1,
					Size: 10,
					Attributes: []tg.DocumentAttributeClass{
						&tg.DocumentAttributeFilename{FileName: name},
					},
				},
			},
		}

		if _, ok := documentFromMessage(msg); ok {
			t.Fatalf("snapshot %q must not be treated as a user file", name)
		}
	}
}

func TestDocumentFromMessage_FallsBackWhenNoFilename(t *testing.T) {
	msg := &tg.Message{
		ID: 9,
		Media: &tg.MessageMediaDocument{
			Document: &tg.Document{
				ID:         123,
				AccessHash: 456,
				Size:       3,
				MimeType:   "",
				Attributes: nil,
			},
		},
	}

	doc, ok := documentFromMessage(msg)
	if !ok {
		t.Fatalf("expected document without filename attribute to still be addressable")
	}
	if doc.FileName == "" {
		t.Fatalf("expected generated fallback name, got empty")
	}
	if doc.MimeType != "application/octet-stream" {
		t.Fatalf("expected default mime, got %q", doc.MimeType)
	}
}

func TestDocumentFromMessage_IgnoresNonDocuments(t *testing.T) {
	msg := &tg.Message{ID: 1, Media: &tg.MessageMediaPhoto{}}
	if _, ok := documentFromMessage(msg); ok {
		t.Fatalf("non-document media must be ignored")
	}
}

func TestExtractHistoryMessages(t *testing.T) {
	msg := &tg.Message{ID: 5}

	channelMsgs, isSlice := extractHistoryMessages(&tg.MessagesChannelMessages{Messages: []tg.MessageClass{msg}})
	if !isSlice || len(channelMsgs) != 1 {
		t.Fatalf("channel messages should be treated as paginatable")
	}

	plain, isSlice := extractHistoryMessages(&tg.MessagesMessages{Messages: []tg.MessageClass{msg}})
	if isSlice || len(plain) != 1 {
		t.Fatalf("plain messages should be a single complete page")
	}

	slice, isSlice := extractHistoryMessages(&tg.MessagesMessagesSlice{Messages: []tg.MessageClass{msg}})
	if !isSlice || len(slice) != 1 {
		t.Fatalf("message slice should be paginatable")
	}

	if _, isSlice := extractHistoryMessages(&tg.MessagesMessagesNotModified{}); isSlice {
		t.Fatalf("not-modified should not be considered a slice")
	}
}

func TestClientManagerDoRejectsWhenNotRunning(t *testing.T) {
	mgr := &ClientManager{taskCh: make(chan runTask)}
	err := mgr.Do(context.Background(), func(ctx context.Context) error { return nil })
	if err == nil {
		t.Fatalf("expected Do to fail when the session is not running")
	}
}

func TestClientManagerRunIsNotReentrant(t *testing.T) {
	mgr := &ClientManager{taskCh: make(chan runTask)}
	mgr.running = true
	err := mgr.Run(context.Background(), func(ctx context.Context) error { return nil })
	if err == nil {
		t.Fatalf("expected second Run to be rejected while a session is active")
	}
}

func TestClientManagerDoDispatchesToRunningSession(t *testing.T) {
	mgr := &ClientManager{taskCh: make(chan runTask)}
	mgr.running = true

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.serveTasks(runCtx)

	var ran bool
	if err := mgr.Do(context.Background(), func(ctx context.Context) error {
		ran = true
		return nil
	}); err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	if !ran {
		t.Fatalf("expected dispatched task to run inside the session")
	}
}

func TestSyncAndRecordPersistsFailedRun(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/sync.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// No channel is bound, so the sync must fail fast WITHOUT network — and
	// the failure still has to land in the sync history.
	mgr := NewClientManager(database, 1, "hash", "test-secret")
	if _, err := mgr.SyncAndRecord(context.Background(), "unit-test"); err == nil {
		t.Fatalf("expected sync to fail without a bound channel")
	}
	runs, err := database.ListSyncRuns(5)
	if err != nil || len(runs) != 1 {
		t.Fatalf("expected 1 recorded run, got %+v (%v)", runs, err)
	}
	if runs[0].Trigger != "unit-test" || runs[0].Error == "" {
		t.Fatalf("run must carry trigger + error: %+v", runs[0])
	}
	if runs[0].FinishedAt == nil {
		t.Fatalf("run must be finished")
	}
}

func TestClientManagerDoPropagatesTaskError(t *testing.T) {
	mgr := &ClientManager{taskCh: make(chan runTask)}
	mgr.running = true

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.serveTasks(runCtx)

	want := errors.New("mtproto boom")
	err := mgr.Do(context.Background(), func(ctx context.Context) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("expected task error to propagate, got %v", err)
	}
}
