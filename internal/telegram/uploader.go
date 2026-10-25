package telegram

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"

	"github.com/gotd/td/tg"
)

const (
	// PartSize is 512 KB, the optimal and maximum part size for MTProto upload.saveBigFilePart.
	PartSize = 512 * 1024
)

// GenerateRandomID generates a 63-bit random int64 for Telegram file IDs.
func GenerateRandomID() int64 {
	n, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	return n.Int64()
}

// UploadPart uploads a single 512 KB part to Telegram with FloodWait backoff.
func (m *ClientManager) UploadPart(ctx context.Context, fileID int64, partIndex int, totalParts int, data []byte, limiter *SafeLimiter) error {
	return limiter.ExecuteWithFloodWait(ctx, func() error {
		_, err := m.api.UploadSaveBigFilePart(ctx, &tg.UploadSaveBigFilePartRequest{
			FileID:         fileID,
			FilePart:       partIndex,
			FileTotalParts: totalParts,
			Bytes:          data,
		})
		return err
	})
}

// CompleteUpload commits uploaded parts as a document message in the Storage Channel.
func (m *ClientManager) CompleteUpload(ctx context.Context, fileID int64, totalParts int, fileName, mimeType string) (int, int64, int64, error) {
	channelID, accessHash, err := m.StorageChannel()
	if err != nil {
		return 0, 0, 0, err
	}

	peer := &tg.InputPeerChannel{
		ChannelID:  channelID,
		AccessHash: accessHash,
	}

	var inputFile tg.InputFileClass = &tg.InputFileBig{
		ID:    fileID,
		Parts: totalParts,
		Name:  fileName,
	}

	media := &tg.InputMediaUploadedDocument{
		File:     inputFile,
		MimeType: mimeType,
		Attributes: []tg.DocumentAttributeClass{
			&tg.DocumentAttributeFilename{
				FileName: fileName,
			},
		},
	}

	randomID := GenerateRandomID()
	updates, err := m.api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    media,
		Message:  "",
		RandomID: randomID,
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("send media to storage channel: %w", err)
	}

	return extractDocumentInfo(updates)
}

// UploadFromReader streams an entire io.Reader to Telegram in sequential 512KB parts.
func (m *ClientManager) UploadFromReader(ctx context.Context, r io.Reader, size int64, fileName, mimeType string, limiter *SafeLimiter, onProgress func(uploaded, total int64)) (int, int64, int64, string, error) {
	unlock := limiter.AcquireUpload()
	defer unlock()

	totalParts := int((size + PartSize - 1) / PartSize)
	if totalParts == 0 {
		totalParts = 1
	}

	fileID := GenerateRandomID()
	hasher := sha256.New()
	buf := make([]byte, PartSize)

	var uploadedBytes int64
	for partIndex := 0; partIndex < totalParts; partIndex++ {
		// Check context cancellation
		if err := ctx.Err(); err != nil {
			return 0, 0, 0, "", err
		}

		n, err := io.ReadFull(r, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return 0, 0, 0, "", fmt.Errorf("read chunk: %w", err)
		}

		if n > 0 {
			chunk := buf[:n]
			hasher.Write(chunk)

			if err := m.UploadPart(ctx, fileID, partIndex, totalParts, chunk, limiter); err != nil {
				return 0, 0, 0, "", fmt.Errorf("upload part %d/%d: %w", partIndex+1, totalParts, err)
			}

			uploadedBytes += int64(n)
			if onProgress != nil {
				onProgress(uploadedBytes, size)
			}

			// Pacing between parts per Safe Mode
			if err := limiter.Pace(ctx); err != nil {
				return 0, 0, 0, "", err
			}
		}

		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
	}

	msgID, docID, docHash, err := m.CompleteUpload(ctx, fileID, totalParts, fileName, mimeType)
	if err != nil {
		return 0, 0, 0, "", err
	}

	shaHex := hex.EncodeToString(hasher.Sum(nil))
	return msgID, docID, docHash, shaHex, nil
}

func extractDocumentInfo(updates tg.UpdatesClass) (int, int64, int64, error) {
	switch u := updates.(type) {
	case *tg.Updates:
		for _, up := range u.Updates {
			switch msgUp := up.(type) {
			case *tg.UpdateNewChannelMessage:
				if msg, ok := msgUp.Message.(*tg.Message); ok {
					return parseMessageDocument(msg)
				}
			case *tg.UpdateNewMessage:
				if msg, ok := msgUp.Message.(*tg.Message); ok {
					return parseMessageDocument(msg)
				}
			}
		}
	}
	return 0, 0, 0, fmt.Errorf("could not find document media in Telegram updates response")
}

func parseMessageDocument(msg *tg.Message) (int, int64, int64, error) {
	if media, ok := msg.Media.(*tg.MessageMediaDocument); ok {
		if doc, ok := media.Document.(*tg.Document); ok {
			return msg.ID, doc.ID, doc.AccessHash, nil
		}
	}
	return 0, 0, 0, fmt.Errorf("message did not contain a document media object")
}
