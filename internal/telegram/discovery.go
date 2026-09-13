package telegram

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/tg"
	"tdocs/internal/db"
)

// DiscoveredChannel represents an existing Telegram Storage Channel found in dialogs.
type DiscoveredChannel struct {
	ID             int64
	AccessHash     int64
	Title          string
	CreatedAt      time.Time
	SnapshotCount  int
	LatestSnapshot *SnapshotInfo
}

// storageChannelTitles lists the current channel title first, then the
// pre-rebrand titles so existing RobDocs Vault / TeleDrive Vault installs
// keep working.
var storageChannelTitles = []string{"tDocs Vault", "RobDocs Vault", "TeleDrive Vault"}

// isStorageChannelTitle reports whether a channel title belongs to tDocs.
func isStorageChannelTitle(title string) bool {
	for _, t := range storageChannelTitles {
		if title == t {
			return true
		}
	}
	return false
}

// DiscoverStorageChannels queries Telegram dialogs for existing storage vault
// broadcast channels.
func (m *ClientManager) DiscoverStorageChannels(ctx context.Context) ([]DiscoveredChannel, error) {
	// ponytail: Single-query 100 dialog limit covers recent channels without pagination overhead.
	res, err := m.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      100,
	})
	if err != nil {
		return nil, fmt.Errorf("query dialogs: %w", err)
	}

	var chats []tg.ChatClass
	switch d := res.(type) {
	case *tg.MessagesDialogs:
		chats = d.Chats
	case *tg.MessagesDialogsSlice:
		chats = d.Chats
	}

	var candidates []DiscoveredChannel
	for _, chat := range chats {
		if c, ok := chat.(*tg.Channel); ok {
			if c.Broadcast && isStorageChannelTitle(c.Title) {
				accessHash, _ := c.GetAccessHash()
				cand := DiscoveredChannel{
					ID:         c.ID,
					AccessHash: accessHash,
					Title:      c.Title,
					CreatedAt:  time.Unix(int64(c.Date), 0),
				}
				if snaps, err := m.ListSnapshotsForPeer(ctx, c.ID, accessHash); err == nil {
					cand.SnapshotCount = len(snaps)
					if len(snaps) > 0 {
						cand.LatestSnapshot = &snaps[0]
					}
				}
				candidates = append(candidates, cand)
			}
		}
	}

	return candidates, nil
}

// ResolveChannelByID searches dialogs to find the access hash for a specific channel ID.
func (m *ClientManager) ResolveChannelByID(ctx context.Context, channelID int64) (int64, error) {
	res, err := m.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      100,
	})
	if err != nil {
		return 0, fmt.Errorf("query dialogs: %w", err)
	}

	var chats []tg.ChatClass
	switch d := res.(type) {
	case *tg.MessagesDialogs:
		chats = d.Chats
	case *tg.MessagesDialogsSlice:
		chats = d.Chats
	}

	for _, chat := range chats {
		if c, ok := chat.(*tg.Channel); ok && c.ID == channelID {
			accessHash, _ := c.GetAccessHash()
			return accessHash, nil
		}
	}
	return 0, fmt.Errorf("channel with ID %d not found in Telegram dialogs", channelID)
}

// CreateStorageChannel creates a new private Telegram Storage Channel "tDocs Vault".
func (m *ClientManager) CreateStorageChannel(ctx context.Context) (int64, int64, error) {
	fmt.Println("Creating private Telegram Storage Channel 'tDocs Vault'...")
	updates, err := m.api.ChannelsCreateChannel(ctx, &tg.ChannelsCreateChannelRequest{
		Broadcast: true,
		Title:     "tDocs Vault",
		About:     "Private cloud storage object vault managed by tDocs. DO NOT delete or rename.",
	})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create storage channel: %w", err)
	}

	var channelID int64
	var accessHash int64

	switch u := updates.(type) {
	case *tg.Updates:
		for _, chat := range u.Chats {
			if c, ok := chat.(*tg.Channel); ok {
				channelID = c.ID
				accessHash = c.AccessHash
				break
			}
		}
	}

	if channelID == 0 {
		return 0, 0, fmt.Errorf("could not extract created channel ID from Telegram response")
	}

	m.SetStorageChannel(channelID, accessHash)
	_ = m.db.SetSetting("storage_channel_id", strconv.FormatInt(channelID, 10))
	_ = m.db.SetSetting("storage_channel_hash", strconv.FormatInt(accessHash, 10))

	fmt.Printf("✓ Created private Storage Channel 'tDocs Vault' (ID: %d)\n", channelID)
	return channelID, accessHash, nil
}

// OnboardStorageChannelInteractive guides user through discovering, selecting, and restoring storage channel.
func (m *ClientManager) OnboardStorageChannelInteractive(ctx context.Context, reader *bufio.Reader, destDBPath string) error {
	storedID, err := m.db.GetSetting("storage_channel_id")
	storedHash, errHash := m.db.GetSetting("storage_channel_hash")

	if err == nil && errHash == nil && storedID != "" && storedHash != "" {
		cID, _ := strconv.ParseInt(storedID, 10, 64)
		cHash, _ := strconv.ParseInt(storedHash, 10, 64)
		m.SetStorageChannel(cID, cHash)
		fmt.Printf("✓ Using existing Storage Channel (ID: %d)\n", cID)
		return nil
	}

	if m.configuredChannelID != 0 {
		hash, err := m.ResolveChannelByID(ctx, m.configuredChannelID)
		if err == nil {
			m.SetStorageChannel(m.configuredChannelID, hash)
			_ = m.db.SetSetting("storage_channel_id", strconv.FormatInt(m.configuredChannelID, 10))
			_ = m.db.SetSetting("storage_channel_hash", strconv.FormatInt(hash, 10))
			fmt.Printf("✓ Bound to configured Storage Channel (ID: %d)\n", m.configuredChannelID)
			return nil
		}
		fmt.Printf("Warning: Configured channel ID %d not found in dialogs (%v). Scanning...\n", m.configuredChannelID, err)
	}

	fmt.Println("Scanning Telegram for existing Storage Channels...")
	candidates, err := m.DiscoverStorageChannels(ctx)
	if err != nil {
		fmt.Printf("Notice: Channel discovery failed (%v). Falling back to channel creation.\n", err)
		_, _, err := m.CreateStorageChannel(ctx)
		return err
	}

	var selected *DiscoveredChannel

	if len(candidates) == 0 {
		fmt.Println("✓ No existing Storage Channel found. Creating a new one...")
		_, _, err := m.CreateStorageChannel(ctx)
		return err
	} else if len(candidates) == 1 {
		cand := candidates[0]
		snapText := "no snapshots"
		if cand.SnapshotCount > 0 {
			snapText = fmt.Sprintf("%d snapshot(s), latest: %s", cand.SnapshotCount, cand.LatestSnapshot.CreatedAt.Format("2006-01-02 15:04"))
		}
		fmt.Printf("Found existing Storage Channel '%s' (ID: %d, Created: %s, %s)\n",
			cand.Title, cand.ID, cand.CreatedAt.Format("2006-01-02"), snapText)

		if promptYesNo(reader, "Connect to this Storage Channel? [Y/n]: ", true) {
			selected = &cand
		} else {
			fmt.Println("Creating a new Storage Channel instead...")
			_, _, err := m.CreateStorageChannel(ctx)
			return err
		}
	} else {
		fmt.Printf("\nFound %d Storage Channels:\n", len(candidates))
		for i, cand := range candidates {
			snapText := "no snapshots"
			if cand.SnapshotCount > 0 {
				snapText = fmt.Sprintf("%d snapshot(s), latest: %s", cand.SnapshotCount, cand.LatestSnapshot.CreatedAt.Format("2006-01-02 15:04"))
			}
			fmt.Printf("  [%d] '%s' | Channel ID: %d | Created: %s | %s\n",
				i+1, cand.Title, cand.ID, cand.CreatedAt.Format("2006-01-02"), snapText)
		}
		fmt.Printf("  [0] Create a new empty Storage Channel\n\n")

		for {
			fmt.Print("Select channel to connect (0 to create new): ")
			choiceStr, _ := reader.ReadString('\n')
			choiceStr = strings.TrimSpace(choiceStr)
			choice, err := strconv.Atoi(choiceStr)
			if err != nil || choice < 0 || choice > len(candidates) {
				fmt.Println("Invalid selection. Please enter a valid number.")
				continue
			}
			if choice == 0 {
				_, _, err := m.CreateStorageChannel(ctx)
				return err
			}
			selected = &candidates[choice-1]
			break
		}
	}

	m.SetStorageChannel(selected.ID, selected.AccessHash)
	_ = m.db.SetSetting("storage_channel_id", strconv.FormatInt(selected.ID, 10))
	_ = m.db.SetSetting("storage_channel_hash", strconv.FormatInt(selected.AccessHash, 10))
	fmt.Printf("✓ Storage Channel linked (ID: %d)\n", selected.ID)

	if selected.SnapshotCount > 0 && selected.LatestSnapshot != nil {
		fmt.Printf("\nLatest database backup snapshot found: %s (%s, %.2f MB)\n",
			selected.LatestSnapshot.FileName,
			selected.LatestSnapshot.CreatedAt.Format("2006-01-02 15:04:05"),
			float64(selected.LatestSnapshot.Size)/(1024*1024))

		if promptYesNo(reader, "Restore database from this snapshot now? [Y/n]: ", true) {
			fmt.Println("Restoring database snapshot from Telegram...")
			_ = m.db.Close()
			if err := m.RestoreSnapshotByID(ctx, selected.LatestSnapshot.MessageID, destDBPath); err != nil {
				return fmt.Errorf("snapshot restore failed: %w", err)
			}
			reopened, err := db.Open(destDBPath)
			if err != nil {
				return fmt.Errorf("reopen restored database: %w", err)
			}
			m.SetDB(reopened)
			_ = reopened.SetSetting("storage_channel_id", strconv.FormatInt(selected.ID, 10))
			_ = reopened.SetSetting("storage_channel_hash", strconv.FormatInt(selected.AccessHash, 10))
			fmt.Println("✓ Database successfully restored from Telegram Storage Channel!")
		} else {
			fmt.Println("Skipping restore. tDocs will run with current local database.")
		}
	} else {
		fmt.Println("No backup snapshots found in this channel. tDocs will run with a fresh database.")
	}

	return nil
}

func promptYesNo(reader *bufio.Reader, prompt string, defaultYes bool) bool {
	fmt.Print(prompt)
	if reader == nil {
		return defaultYes
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return defaultYes
	}
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultYes
	}
	return line == "y" || line == "yes"
}
