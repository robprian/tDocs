package telegram

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/gotd/td/tg"
)

// ChannelDocument describes a document object stored in the Telegram Storage
// Channel. It is the raw material used to rebuild the local file catalog when
// the SQLite metadata is lost or emptied.
type ChannelDocument struct {
	MessageID     int
	DocumentID    int64
	AccessHash    int64
	FileReference []byte
	FileName      string
	Size          int64
	MimeType      string
	CreatedAt     time.Time
}

// Location builds the MTProto file location for this document, attaching its
// file reference so upload.getFile calls succeed.
func (d ChannelDocument) Location() *tg.InputDocumentFileLocation {
	return locationFor(d.DocumentID, d.AccessHash, d.FileReference)
}

// SyncResult summarizes a channel -> database reconciliation pass.
type SyncResult struct {
	Scanned  int
	Inserted int
	Updated  int
}

// channelBoundOrRestore resolves the active storage channel, falling back to
// the persisted binding when the in-memory cache is empty. It never creates a
// channel, so a sync can never silently provision a second vault.
func (m *ClientManager) channelBoundOrRestore() error {
	if _, _, err := m.StorageChannel(); err == nil {
		return nil
	}
	storedID, idErr := m.db.GetSetting("storage_channel_id")
	storedHash, hashErr := m.db.GetSetting("storage_channel_hash")
	if idErr != nil || hashErr != nil || storedID == "" || storedHash == "" {
		return fmt.Errorf("storage channel belum terikat — jalankan `tdocs login` untuk menautkan channel")
	}
	cID, _ := strconv.ParseInt(storedID, 10, 64)
	cHash, _ := strconv.ParseInt(storedHash, 10, 64)
	if cID == 0 || cHash == 0 {
		return fmt.Errorf("storage channel tidak valid — jalankan `tdocs login` untuk menautkan ulang")
	}
	m.SetStorageChannel(cID, cHash)
	return nil
}

// DeleteChannelMessage removes one message from the Storage Channel.
// Best-effort: callers log failures instead of failing the whole operation.
func (m *ClientManager) DeleteChannelMessage(ctx context.Context, msgID int) error {
	channelID, accessHash, err := m.StorageChannel()
	if err != nil {
		return err
	}
	_, err = m.api.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{
		Channel: &tg.InputChannel{
			ChannelID:  channelID,
			AccessHash: accessHash,
		},
		ID: []int{msgID},
	})
	return err
}

// SyncAndRecord runs SyncChannelToDB and persists the outcome to the sync
// history table so the Sync Center can show past runs.
func (m *ClientManager) SyncAndRecord(ctx context.Context, trigger string) (*SyncResult, error) {
	runID, _ := m.db.StartSyncRun(trigger)
	res, err := m.SyncChannelToDB(ctx)
	errStr := ""
	scanned, inserted, updated := 0, 0, 0
	if err != nil {
		errStr = err.Error()
	} else if res != nil {
		scanned, inserted, updated = res.Scanned, res.Inserted, res.Updated
	}
	if runID != 0 {
		_ = m.db.FinishSyncRun(runID, scanned, inserted, updated, errStr)
	}
	return res, err
}

// SyncChannelToDB reconciles the local file catalog with the documents that
// actually live in the Telegram Storage Channel. It is idempotent: records are
// matched by Telegram message ID, so repeated runs never duplicate files.
//
// This is the resilience path that lets a tDocs instance recover its file
// list after a server restart, database wipe, or fresh volume. It deliberately
// does NOT create a channel: syncing must never silently provision a new vault
// when the configured one is missing.
func (m *ClientManager) SyncChannelToDB(ctx context.Context) (*SyncResult, error) {
	if err := m.channelBoundOrRestore(); err != nil {
		return nil, err
	}

	docs, err := m.ScanStorageChannelDocuments(ctx)
	if err != nil {
		return nil, err
	}

	result := &SyncResult{Scanned: len(docs)}
	for _, d := range docs {
		inserted, err := m.db.UpsertFileFromChannel(
			nil,
			d.FileName,
			d.Size,
			d.MimeType,
			d.MessageID,
			fmt.Sprintf("%d", d.DocumentID),
			fmt.Sprintf("%d", d.AccessHash),
		)
		if err != nil {
			return result, fmt.Errorf("sync document %d (%s): %w", d.MessageID, d.FileName, err)
		}
		if inserted {
			result.Inserted++
		} else {
			result.Updated++
		}
	}
	return result, nil
}

// ScanStorageChannelDocuments walks the Storage Channel history and returns
// every non-snapshot document it finds. It paginates so vaults larger than a
// single messages.getHistory page are fully covered.
//
// Snapshots (tdocs-backup-*.db.gz) are deliberately excluded: they are
// metadata backups, not user files, and are reconstructed separately by the
// snapshot history view.
func (m *ClientManager) ScanStorageChannelDocuments(ctx context.Context) ([]ChannelDocument, error) {
	channelID, accessHash, err := m.StorageChannel()
	if err != nil {
		return nil, err
	}

	peer := &tg.InputPeerChannel{
		ChannelID:  channelID,
		AccessHash: accessHash,
	}

	const pageSize = 100
	const maxMessages = 5000 // safety cap for very large vaults

	offsetID := 0
	var docs []ChannelDocument

	for len(docs) < maxMessages {
		history, err := m.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
			Peer:     peer,
			OffsetID: offsetID,
			Limit:    pageSize,
		})
		if err != nil {
			return nil, fmt.Errorf("get channel history: %w", err)
		}

		msgs, isSlice := extractHistoryMessages(history)
		if len(msgs) == 0 {
			break
		}

		pageCount := 0
		minID := 0
		for _, message := range msgs {
			msg, ok := message.(*tg.Message)
			if !ok {
				continue
			}
			pageCount++
			if minID == 0 || msg.ID < minID {
				minID = msg.ID
			}
			if doc, ok := documentFromMessage(msg); ok {
				docs = append(docs, doc)
			}
		}

		// Not a slice -> the whole history fit in one page.
		if !isSlice || pageCount == 0 || pageCount < pageSize {
			break
		}
		offsetID = minID
	}

	return docs, nil
}

// extractHistoryMessages normalizes the two possible messages.getHistory
// response shapes.
func extractHistoryMessages(history tg.MessagesMessagesClass) ([]tg.MessageClass, bool) {
	switch h := history.(type) {
	case *tg.MessagesChannelMessages:
		return h.Messages, true
	case *tg.MessagesMessages:
		return h.Messages, false
	case *tg.MessagesMessagesSlice:
		return h.Messages, true
	default:
		return nil, false
	}
}

// documentFromMessage extracts file-object metadata from a message, skipping
// database snapshots. Documents without a filename attribute (rare, e.g.
// voice notes) fall back to a generated name so they remain addressable.
func documentFromMessage(msg *tg.Message) (ChannelDocument, bool) {
	media, ok := msg.Media.(*tg.MessageMediaDocument)
	if !ok {
		return ChannelDocument{}, false
	}
	doc, ok := media.Document.(*tg.Document)
	if !ok {
		return ChannelDocument{}, false
	}

	name := ""
	for _, attr := range doc.Attributes {
		if fn, ok := attr.(*tg.DocumentAttributeFilename); ok {
			name = fn.FileName
			break
		}
	}
	if name == "" {
		name = fmt.Sprintf("document-%d", doc.ID)
	}
	if isSnapshotFile(name) {
		return ChannelDocument{}, false
	}

	mime := doc.MimeType
	if mime == "" {
		mime = "application/octet-stream"
	}

	return ChannelDocument{
		MessageID:     msg.ID,
		DocumentID:    doc.ID,
		AccessHash:    doc.AccessHash,
		FileReference: doc.FileReference,
		FileName:      name,
		Size:          doc.Size,
		MimeType:      mime,
		CreatedAt:     time.Unix(int64(msg.Date), 0),
	}, true
}
