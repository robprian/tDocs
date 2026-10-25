package telegram

import (
	"path/filepath"
	"testing"

	"tdocs/internal/db"
)

// Additional accounts must never share settings keys with the primary
// account or with each other: a session, channel or auth flag written for
// one account must be invisible to every other account.
func TestAccountKeyIsolation(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	primary := NewClientManager(database, 1, "hash", "secret")
	a, err := RegisterTelegramAccount(database, "Backup HP 2", "+628111111111")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	b, err := RegisterTelegramAccount(database, "Backup HP 3", "+628222222222")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("account IDs collided: %s", a.ID)
	}

	// Primary defaults must be exactly the legacy keys.
	cID, cHash, auth := primary.ChannelKeys()
	if cID != "storage_channel_id" || cHash != "storage_channel_hash" || auth != "telegram_authorized" {
		t.Fatalf("primary keys changed: %s %s %s", cID, cHash, auth)
	}

	reg := NewAccountRegistry(database, 1, "hash", "secret")
	ma := reg.ManagerFor(a)
	mb := reg.ManagerFor(b)
	aID, aHash, aAuth := ma.ChannelKeys()
	bID, bHash, bAuth := mb.ChannelKeys()
	if aID == bID || aHash == bHash || aAuth == bAuth {
		t.Fatalf("account channel keys collide: %s/%s %s/%s %s/%s", aID, bID, aHash, bHash, aAuth, bAuth)
	}
	if aID == "storage_channel_id" || aAuth == "telegram_authorized" {
		t.Fatalf("additional account reused primary keys: %s %s", aID, aAuth)
	}

	// Writing a session for A must not create one for B or primary.
	if err := database.SetSetting(a.sessionKey(), "session-a"); err != nil {
		t.Fatalf("set session a: %v", err)
	}
	if !reg.HasSession(a) {
		t.Fatalf("account A session not found")
	}
	if reg.HasSession(b) {
		t.Fatalf("account B sees account A's session")
	}
	if _, err := database.GetSetting("telegram_session"); err == nil {
		t.Fatalf("primary session key was written by account A")
	}

	// Channel binding for A must not leak to B or primary.
	if err := UpdateTelegramAccountChannel(database, a.ID, 12345); err != nil {
		t.Fatalf("update channel: %v", err)
	}
	if err := database.SetSetting(a.channelIDKey(), "12345"); err != nil {
		t.Fatalf("set channel a: %v", err)
	}
	if reg.ChannelID(a) != 12345 {
		t.Fatalf("account A channel = %d, want 12345", reg.ChannelID(a))
	}
	if reg.ChannelID(b) != 0 {
		t.Fatalf("account B channel leaked: %d", reg.ChannelID(b))
	}

	// Delete removes A's keys and record, leaves B untouched.
	if err := DeleteTelegramAccount(database, a.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := database.GetSetting(a.sessionKey()); err == nil {
		t.Fatalf("account A session survived delete")
	}
	accounts, err := LoadTelegramAccounts(database)
	if err != nil {
		t.Fatalf("load after delete: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != b.ID {
		t.Fatalf("accounts after delete = %v, want only %s", accounts, b.ID)
	}
}

func TestBackupCopiesRoundTrip(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	f, err := database.CreateFile(nil, "doc.pdf", 100, "application/pdf", 77, "1", "2", "")
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	has, err := database.HasBackupCopy(f.ID, "tg-abc")
	if err != nil || has {
		t.Fatalf("has before record = %v, %v", has, err)
	}
	if err := database.RecordBackupCopy(f.ID, "tg-abc", 999); err != nil {
		t.Fatalf("record: %v", err)
	}
	has, err = database.HasBackupCopy(f.ID, "tg-abc")
	if err != nil || !has {
		t.Fatalf("has after record = %v, %v", has, err)
	}
	// A different account must still see the file as not copied.
	has, err = database.HasBackupCopy(f.ID, "tg-other")
	if err != nil || has {
		t.Fatalf("other account has = %v, %v", has, err)
	}
	n, err := database.CountBackupCopies("tg-abc")
	if err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
}
