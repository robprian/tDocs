package telegram

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"tdocs/internal/db"
)

// TelegramAccount is one additional Telegram account (a second phone
// number) registered as a backup storage target. The primary account is
// NOT stored here: it keeps the legacy single-account behaviour
// (telegram_session + storage_channel_id settings) untouched, so existing
// installs keep working with zero migration.
//
// An additional account owns:
//   - its own encrypted MTProto session (settings key telegram_session_<id>),
//   - its own authorization flag and storage channel keys (suffixed the
//     same way), so login/channel discovery for one account can never
//     overwrite another account's — including the primary's.
//   - its own "tDocs Vault" channel, used only as a backup copy target.
//
// Files are NEVER merged across accounts: the local catalog keeps the
// primary account's message IDs, and backup copies are recorded
// separately in backup_copies (file_id, account_id, backup_message_id).
// That avoids message-ID collisions between channels, which share one
// integer namespace.
type TelegramAccount struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	ChannelID int64  `json:"channel_id"`
	CreatedAt string `json:"created_at"`
}

func (a TelegramAccount) sessionKey() string   { return "telegram_session_" + a.ID }
func (a TelegramAccount) channelIDKey() string { return "storage_channel_id_" + a.ID }
func (a TelegramAccount) channelHashKey() string {
	return "storage_channel_hash_" + a.ID
}
func (a TelegramAccount) authorizedKey() string { return "telegram_authorized_" + a.ID }

// LoadTelegramAccounts returns the registered additional accounts (never
// the primary account). An absent setting means none are registered.
func LoadTelegramAccounts(database *db.DB) ([]TelegramAccount, error) {
	raw, err := database.GetSetting("telegram_accounts")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load telegram accounts: %w", err)
	}
	var accounts []TelegramAccount
	if err := json.Unmarshal([]byte(raw), &accounts); err != nil {
		return nil, fmt.Errorf("parse telegram accounts: %w", err)
	}
	return accounts, nil
}

// SaveTelegramAccounts persists the additional-account list.
func SaveTelegramAccounts(database *db.DB, accounts []TelegramAccount) error {
	raw, err := json.Marshal(accounts)
	if err != nil {
		return err
	}
	return database.SetSetting("telegram_accounts", string(raw))
}

func newAccountID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "tg-" + hex.EncodeToString(b[:])
}

// RegisterTelegramAccount adds a new additional account in the
// "pending_login" state: no session yet, no channel yet. The caller then
// runs the browser wizard against this account's manager (see
// AccountRegistry.ManagerFor) to pair the phone number.
func RegisterTelegramAccount(database *db.DB, name, phone string) (TelegramAccount, error) {
	accounts, err := LoadTelegramAccounts(database)
	if err != nil {
		return TelegramAccount{}, err
	}
	acct := TelegramAccount{
		ID:        newAccountID(),
		Name:      name,
		Phone:     phone,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	accounts = append(accounts, acct)
	if err := SaveTelegramAccounts(database, accounts); err != nil {
		return TelegramAccount{}, err
	}
	return acct, nil
}

// UpdateTelegramAccountChannel records the account's storage channel after
// EnsureStorageChannel (run through the account's own manager, hence its
// own suffixed keys) has bound one.
func UpdateTelegramAccountChannel(database *db.DB, id string, channelID int64) error {
	accounts, err := LoadTelegramAccounts(database)
	if err != nil {
		return err
	}
	for i := range accounts {
		if accounts[i].ID == id {
			accounts[i].ChannelID = channelID
			return SaveTelegramAccounts(database, accounts)
		}
	}
	return fmt.Errorf("telegram account %s not found", id)
}

// DeleteTelegramAccount removes the account record and its session,
// authorization flag and channel keys. It does NOT delete the account's
// Telegram channel itself: backup copies already stored there stay
// recoverable from Telegram until the owner deletes the channel.
func DeleteTelegramAccount(database *db.DB, id string) error {
	accounts, err := LoadTelegramAccounts(database)
	if err != nil {
		return err
	}
	kept := accounts[:0]
	found := false
	for _, a := range accounts {
		if a.ID == id {
			found = true
			continue
		}
		kept = append(kept, a)
	}
	if !found {
		return fmt.Errorf("telegram account %s not found", id)
	}
	if err := SaveTelegramAccounts(database, kept); err != nil {
		return err
	}
	acct := TelegramAccount{ID: id}
	_ = database.DeleteSetting(acct.sessionKey())
	_ = database.DeleteSetting(acct.authorizedKey())
	_ = database.DeleteSetting(acct.channelIDKey())
	_ = database.DeleteSetting(acct.channelHashKey())
	return nil
}

// AccountRegistry owns one ClientManager per additional account and the
// long-lived Run loop for each. Managers are created lazily: an account
// with no session yet gets a manager only when its wizard starts.
type AccountRegistry struct {
	db      *db.DB
	appID   int
	appHash string
	secret  string

	mu       sync.Mutex
	managers map[string]*ClientManager
	running  map[string]bool
	cancels  map[string]context.CancelFunc
}

func NewAccountRegistry(database *db.DB, appID int, appHash, secret string) *AccountRegistry {
	return &AccountRegistry{
		db:       database,
		appID:    appID,
		appHash:  appHash,
		secret:   secret,
		managers: map[string]*ClientManager{},
		running:  map[string]bool{},
		cancels:  map[string]context.CancelFunc{},
	}
}

// ManagerFor returns (creating if needed) the account's ClientManager.
// The manager is NOT running until Start is called; the wizard's Do()
// calls will fail until then, which is why Start is called before any
// wizard or backup operation for the account.
func (r *AccountRegistry) ManagerFor(acct TelegramAccount) *ClientManager {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.managers[acct.ID]; ok {
		return m
	}
	m := NewClientManagerForAccount(r.db, r.appID, r.appHash, r.secret,
		acct.sessionKey(), acct.channelIDKey(), acct.channelHashKey(), acct.authorizedKey())
	r.managers[acct.ID] = m
	return m
}

// Start launches the account's long-lived Run loop (idempotent) on a
// server-lifetime context owned by the registry. Like the primary runner
// it checks authorization and binds the storage channel, then waits —
// but it deliberately does NOT sync the channel catalog into the local
// database: backup accounts must never merge their message IDs into the
// primary catalog.
func (r *AccountRegistry) Start(acct TelegramAccount) {
	r.mu.Lock()
	if r.running[acct.ID] {
		r.mu.Unlock()
		return
	}
	r.running[acct.ID] = true
	ctx, cancel := context.WithCancel(context.Background())
	r.cancels[acct.ID] = cancel
	r.mu.Unlock()

	mgr := r.ManagerFor(acct)
	go func() {
		defer func() {
			r.mu.Lock()
			r.running[acct.ID] = false
			delete(r.cancels, acct.ID)
			r.mu.Unlock()
		}()
		_ = mgr.Run(ctx, func(runCtx context.Context) error {
			if mgr.CheckAuthorized(runCtx) {
				_ = mgr.EnsureStorageChannel(runCtx)
			}
			<-runCtx.Done()
			return nil
		})
	}()
}

// Stop cancels an account's Run loop. The manager stays in the map so a
// later Start reuses it; a deleted account is dropped from the map by
// Remove.
func (r *AccountRegistry) Stop(id string) {
	r.mu.Lock()
	cancel, ok := r.cancels[id]
	r.mu.Unlock()
	if ok {
		cancel()
	}
}

// Remove drops a manager (used after DeleteTelegramAccount). A running
// loop keeps running until its context ends, but no handler can reach
// the manager anymore, so no new work is dispatched to it.
func (r *AccountRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.managers, id)
}

// StartAll launches Run loops for every registered account that already
// has a session stored. Accounts still pending login are left stopped
// until their wizard starts them.
func (r *AccountRegistry) StartAll() error {
	accounts, err := LoadTelegramAccounts(r.db)
	if err != nil {
		return err
	}
	for _, acct := range accounts {
		if _, err := r.db.GetSetting(acct.sessionKey()); err != nil {
			continue
		}
		r.Start(acct)
	}
	return nil
}

// HasSession reports whether the account has a stored MTProto session.
func (r *AccountRegistry) HasSession(acct TelegramAccount) bool {
	_, err := r.db.GetSetting(acct.sessionKey())
	return err == nil
}

// ChannelID returns the account's bound storage channel ID, or 0 when the
// account has not bound one yet (login pending or wizard not finished).
func (r *AccountRegistry) ChannelID(acct TelegramAccount) int64 {
	raw, err := r.db.GetSetting(acct.channelIDKey())
	if err != nil {
		return 0
	}
	var id int64
	_, _ = fmt.Sscanf(raw, "%d", &id)
	return id
}
