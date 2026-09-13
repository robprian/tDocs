package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

// ParseRange parses an HTTP Range header (e.g. "bytes=0-1023", "bytes=100-", "bytes=-500").
// Returns start, end (inclusive), hasRange boolean, or an error if invalid.
func ParseRange(rangeHeader string, fileSize int64) (int64, int64, bool, error) {
	if rangeHeader == "" {
		return 0, fileSize - 1, false, nil
	}

	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, false, errors.New("invalid range unit")
	}

	raw := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.Split(raw, "-")
	if len(parts) != 2 {
		return 0, 0, false, errors.New("invalid range format")
	}

	var start, end int64

	if parts[0] == "" {
		// Suffix range: bytes=-500
		suffixLen, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffixLen <= 0 {
			return 0, 0, false, errors.New("invalid suffix range length")
		}
		if suffixLen > fileSize {
			suffixLen = fileSize
		}
		start = fileSize - suffixLen
		end = fileSize - 1
		return start, end, true, nil
	}

	s, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || s < 0 || s >= fileSize {
		return 0, 0, false, errors.New("range start out of bounds")
	}
	start = s

	if parts[1] == "" {
		// Open-ended range: bytes=100-
		end = fileSize - 1
	} else {
		e, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || e < start {
			return 0, 0, false, errors.New("invalid range end")
		}
		if e >= fileSize {
			e = fileSize - 1
		}
		end = e
	}

	return start, end, true, nil
}

// locationFor builds a document file location. The file reference is attached
// whenever available: Telegram requires a fresh file_reference for
// upload.getFile, and calls without one fail once the reference rotates.
func locationFor(docID, accessHash int64, fileRef []byte) *tg.InputDocumentFileLocation {
	return &tg.InputDocumentFileLocation{
		ID:            docID,
		AccessHash:    accessHash,
		FileReference: fileRef,
	}
}

// DownloadFull streams the complete document from Telegram to an io.Writer.
func (m *ClientManager) DownloadFull(ctx context.Context, docID, accessHash int64, w io.Writer) error {
	return m.DownloadFullLoc(ctx, locationFor(docID, accessHash, nil), w)
}

// DownloadFullLoc streams the complete document at loc to an io.Writer.
func (m *ClientManager) DownloadFullLoc(ctx context.Context, loc *tg.InputDocumentFileLocation, w io.Writer) error {
	if m == nil || m.api == nil {
		return fmt.Errorf("telegram client not initialized")
	}
	if loc == nil {
		return fmt.Errorf("file location is nil")
	}
	if w == nil {
		return fmt.Errorf("response writer is nil")
	}
	d := downloader.NewDownloader()
	_, err := d.Download(m.api, loc).Stream(ctx, w)
	return err
}

// DownloadRange streams a specific byte range [start, end] from Telegram using aligned chunks.
func (m *ClientManager) DownloadRange(ctx context.Context, docID, accessHash int64, start, end int64, w io.Writer, limiter *SafeLimiter) error {
	return m.DownloadRangeLoc(ctx, locationFor(docID, accessHash, nil), start, end, w, limiter)
}

// DownloadRangeLoc streams a specific byte range [start, end] using an
// explicit file location (including its file reference).
func (m *ClientManager) DownloadRangeLoc(ctx context.Context, loc *tg.InputDocumentFileLocation, start, end int64, w io.Writer, limiter *SafeLimiter) error {
	if m == nil || m.api == nil {
		return fmt.Errorf("telegram client not initialized")
	}
	if loc == nil {
		return fmt.Errorf("file location is nil")
	}
	if w == nil {
		return fmt.Errorf("response writer is nil")
	}
	const chunkSize = 512 * 1024 // 512 KB per MTProto getFile part
	alignedStart := (start / chunkSize) * chunkSize
	currOffset := alignedStart

	for currOffset <= end {
		if err := ctx.Err(); err != nil {
			return err
		}

		var res tg.UploadFileClass
		err := limiter.ExecuteWithFloodWait(ctx, func() error {
			var rErr error
			res, rErr = m.api.UploadGetFile(ctx, &tg.UploadGetFileRequest{
				Location: loc,
				Offset:   currOffset,
				Limit:    chunkSize,
			})
			return rErr
		})
		if err != nil {
			return fmt.Errorf("upload.getFile offset %d: %w", currOffset, err)
		}

		var bytesData []byte
		switch f := res.(type) {
		case *tg.UploadFile:
			bytesData = f.Bytes
		default:
			return fmt.Errorf("unsupported upload file response type: %T", res)
		}

		if len(bytesData) == 0 {
			break
		}

		// Calculate overlap with requested [start, end]
		chunkEnd := currOffset + int64(len(bytesData)) - 1
		sliceStart := int64(0)
		sliceEnd := int64(len(bytesData))

		if start > currOffset {
			sliceStart = start - currOffset
		}
		if end < chunkEnd {
			sliceEnd = int64(len(bytesData)) - (chunkEnd - end)
		}

		if sliceStart < sliceEnd && sliceStart >= 0 && sliceEnd <= int64(len(bytesData)) {
			if _, err := w.Write(bytesData[sliceStart:sliceEnd]); err != nil {
				return err
			}
		}

		currOffset += int64(len(bytesData))
		if currOffset > end {
			break
		}

		_ = limiter.Pace(ctx)
	}

	return nil
}

// docCacheTTL bounds how long a resolved document reference is reused. Media
// playback issues many Range requests for the same object; without this cache
// every one of them paid a live channels.getMessages round-trip before
// streaming a single byte, which dominated seek and start-up latency. The TTL
// keeps stale references self-healing.
const docCacheTTL = 5 * time.Minute

type docCacheEntry struct {
	doc *ChannelDocument
	at  time.Time
}

// ResolveChannelDocumentCached returns a recently resolved document, hitting
// the network only when the cache misses or the entry has expired.
func (m *ClientManager) ResolveChannelDocumentCached(ctx context.Context, msgID int) (*ChannelDocument, error) {
	m.docCacheMu.Lock()
	if e, ok := m.docCache[msgID]; ok && time.Since(e.at) < docCacheTTL {
		doc := e.doc
		m.docCacheMu.Unlock()
		return doc, nil
	}
	m.docCacheMu.Unlock()

	doc, err := m.ResolveChannelDocument(ctx, msgID)
	if err != nil {
		return nil, err
	}
	m.docCacheMu.Lock()
	if m.docCache == nil {
		m.docCache = map[int]docCacheEntry{}
	}
	// Bound the cache: references are tiny, but a long-running server should
	// not accumulate every object it has ever touched.
	if len(m.docCache) > 512 {
		cutoff := time.Now().Add(-docCacheTTL)
		for k, v := range m.docCache {
			if v.at.Before(cutoff) {
				delete(m.docCache, k)
			}
		}
	}
	m.docCache[msgID] = docCacheEntry{doc: doc, at: time.Now()}
	m.docCacheMu.Unlock()
	return doc, nil
}

// invalidateDocCache drops a cached document so the next read re-resolves it.
func (m *ClientManager) invalidateDocCache(msgID int) {
	m.docCacheMu.Lock()
	delete(m.docCache, msgID)
	m.docCacheMu.Unlock()
}

// ResolveChannelDocument fetches a fresh copy of a Storage Channel message's
// document, including its current file reference and access hash.
//
// Stored coordinates go stale (reference rotation, old snapshots, re-created
// vaults). Resolving from the live message is what makes preview and download
// work for files recovered by channel sync.
func (m *ClientManager) ResolveChannelDocument(ctx context.Context, msgID int) (*ChannelDocument, error) {
	channelID, accessHash, err := m.StorageChannel()
	if err != nil {
		// Fall back to the persisted binding when the in-memory cache is empty.
		if berr := m.channelBoundOrRestore(); berr != nil {
			return nil, berr
		}
		channelID, accessHash, err = m.StorageChannel()
		if err != nil {
			return nil, err
		}
	}

	res, err := m.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
		Channel: &tg.InputChannel{
			ChannelID:  channelID,
			AccessHash: accessHash,
		},
		ID: []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}},
	})
	if err != nil {
		return nil, fmt.Errorf("get channel message %d: %w", msgID, err)
	}

	msgs, _ := extractHistoryMessages(res)
	for _, message := range msgs {
		msg, ok := message.(*tg.Message)
		if !ok || msg.ID != msgID {
			continue
		}
		if doc, ok := documentFromMessage(msg); ok {
			return &doc, nil
		}
		return nil, fmt.Errorf("message %d holds no downloadable document", msgID)
	}
	return nil, fmt.Errorf("message %d not found in Storage Channel", msgID)
}

// DownloadFullByMessage downloads a file using freshly resolved coordinates,
// falling back to the stored coordinates when the live message cannot be read.
func (m *ClientManager) DownloadFullByMessage(ctx context.Context, msgID int, fallbackID, fallbackHash int64, w io.Writer) error {
	if doc, err := m.ResolveChannelDocumentCached(ctx, msgID); err == nil {
		err = m.DownloadFullLoc(ctx, doc.Location(), w)
		if err == nil {
			return nil
		}
		// A cached reference may have expired mid-flight: re-resolve once.
		m.invalidateDocCache(msgID)
		if fresh, rerr := m.ResolveChannelDocument(ctx, msgID); rerr == nil {
			if derr := m.DownloadFullLoc(ctx, fresh.Location(), w); derr == nil {
				return nil
			}
		}
		return err
	} else if fallbackID != 0 {
		return m.DownloadFull(ctx, fallbackID, fallbackHash, w)
	} else {
		return fmt.Errorf("resolve document: %w", err)
	}
}

// DownloadRangeByMessage streams a byte range using freshly resolved
// coordinates, falling back to the stored coordinates when the live message
// cannot be read.
func (m *ClientManager) DownloadRangeByMessage(ctx context.Context, msgID int, fallbackID, fallbackHash int64, start, end int64, w io.Writer, limiter *SafeLimiter) error {
	if doc, err := m.ResolveChannelDocumentCached(ctx, msgID); err == nil {
		err = m.DownloadRangeLoc(ctx, doc.Location(), start, end, w, limiter)
		if err == nil {
			return nil
		}
		// Seeks and resume requests reuse the same object heavily; retry once
		// with a freshly resolved reference before surfacing a failure.
		m.invalidateDocCache(msgID)
		if fresh, rerr := m.ResolveChannelDocument(ctx, msgID); rerr == nil {
			if derr := m.DownloadRangeLoc(ctx, fresh.Location(), start, end, w, limiter); derr == nil {
				return nil
			}
		}
		return err
	} else if fallbackID != 0 {
		return m.DownloadRange(ctx, fallbackID, fallbackHash, start, end, w, limiter)
	} else {
		return fmt.Errorf("resolve document: %w", err)
	}
}
