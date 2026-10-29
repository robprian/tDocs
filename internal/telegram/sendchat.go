package telegram

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gotd/td/tg"
)

// ResolveChatPeer turns a public upload target into a sendable peer: an
// @username (user or channel) resolved live, or a numeric user ID.
// Numeric IDs without a known access hash may be rejected by Telegram for
// users the account never contacted; usernames are the reliable form.
func (m *ClientManager) ResolveChatPeer(ctx context.Context, target string) (tg.InputPeerClass, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("empty target")
	}
	if id, err := strconv.ParseInt(strings.TrimPrefix(target, "@"), 10, 64); err == nil && !strings.Contains(target, "@") {
		// Pure digits: treat as a user ID. AccessHash 0 works only when
		// Telegram can resolve the peer without it; otherwise it errors.
		return &tg.InputPeerUser{UserID: id}, nil
	}
	username := strings.TrimPrefix(target, "@")
	resolved, err := m.api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
	if err != nil {
		return nil, fmt.Errorf("resolve username @%s: %w", username, err)
	}
	for _, u := range resolved.Users {
		if user, ok := u.(*tg.User); ok && strings.EqualFold(user.Username, username) {
			return &tg.InputPeerUser{UserID: user.ID, AccessHash: user.AccessHash}, nil
		}
	}
	for _, c := range resolved.Chats {
		if ch, ok := c.(*tg.Channel); ok && strings.EqualFold(ch.Username, username) {
			return &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, nil
		}
	}
	return nil, fmt.Errorf("username @%s not found", username)
}

// SendFileToChat uploads bytes as a new document and sends it to target.
// It re-uploads (Telegram has no server-side copy between arbitrary peers
// for user accounts), so the cost is one download + one upload.
func (m *ClientManager) SendFileToChat(ctx context.Context, target string, r io.Reader, size int64, fileName, mimeType string, limiter *SafeLimiter) (int, error) {
	peer, err := m.ResolveChatPeer(ctx, target)
	if err != nil {
		return 0, err
	}
	totalParts := int((size + PartSize - 1) / PartSize)
	if totalParts == 0 {
		totalParts = 1
	}
	fileID := GenerateRandomID()
	buf := make([]byte, PartSize)
	for partIndex := 0; partIndex < totalParts; partIndex++ {
		n, rerr := io.ReadFull(r, buf)
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			return 0, fmt.Errorf("read chunk: %w", rerr)
		}
		if n > 0 {
			if err := m.UploadPart(ctx, fileID, partIndex, totalParts, buf[:n], limiter); err != nil {
				return 0, fmt.Errorf("upload part %d/%d: %w", partIndex+1, totalParts, err)
			}
			if err := limiter.Pace(ctx); err != nil {
				return 0, err
			}
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
	}
	media := &tg.InputMediaUploadedDocument{
		File: &tg.InputFileBig{
			ID:    fileID,
			Parts: totalParts,
			Name:  fileName,
		},
		MimeType: mimeType,
		Attributes: []tg.DocumentAttributeClass{
			&tg.DocumentAttributeFilename{FileName: fileName},
		},
	}
	updates, err := m.api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    media,
		Message:  "",
		RandomID: GenerateRandomID(),
	})
	if err != nil {
		return 0, fmt.Errorf("send media to chat: %w", err)
	}
	msgID, _, _, err := extractDocumentInfo(updates)
	return msgID, err
}
