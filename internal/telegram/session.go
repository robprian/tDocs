package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/gotd/td/session"
	"tdocs/internal/crypto"
	"tdocs/internal/db"
)

// EncryptedSessionStorage implements session.Storage using SQLite and AES-256-GCM.
type EncryptedSessionStorage struct {
	db        *db.DB
	secretKey []byte

	// mu guards loaded, the ciphertext this process last read or wrote.
	// `tdocs login` runs as a separate process against the same database, so
	// a write from here may be stale: gotd persists its own (revoked) auth key
	// and would overwrite the fresh session the CLI just stored, which is what
	// made the dashboard demand pairing again after a successful CLI login.
	mu       sync.Mutex
	loaded   string
	observed bool
}

func NewEncryptedSessionStorage(database *db.DB, secretKey string) *EncryptedSessionStorage {
	return &EncryptedSessionStorage{
		db:        database,
		secretKey: crypto.DeriveKey(secretKey),
	}
}

// legacyDefaultKey decrypts sessions written before TELEDRIVE_SECRET_KEY /
// .teledrive.key management existed (hardcoded default in LoadConfig).
func legacyDefaultKey() []byte {
	return crypto.DeriveKey("teledrive-default-local-secret-32b")
}

func (s *EncryptedSessionStorage) LoadSession(ctx context.Context) ([]byte, error) {
	val, err := s.db.GetSetting("telegram_session")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.remember("")
			return nil, session.ErrNotFound
		}
		return nil, fmt.Errorf("load session from db: %w", err)
	}

	if decrypted, err := crypto.Decrypt(val, s.secretKey); err == nil {
		s.remember(val)
		return decrypted, nil
	}

	// Backward compatibility: session may have been encrypted with the legacy
	// hardcoded default key before secret rotation / keyfile support.
	if decrypted, err := crypto.Decrypt(val, legacyDefaultKey()); err == nil {
		s.remember(val)
		return decrypted, nil
	}

	return nil, fmt.Errorf("cannot decrypt Telegram session: use the original TDOCS_SECRET_KEY and data directory; the stored session was preserved (use `tdocs login --reset` only to deliberately replace it)")
}

func (s *EncryptedSessionStorage) StoreSession(ctx context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replacedLocked() {
		// Another process stored a newer session while this client was running.
		// Keeping our own bytes would throw that session away.
		return nil
	}

	encrypted, err := crypto.Encrypt(data, s.secretKey)
	if err != nil {
		return fmt.Errorf("encrypt session: %w", err)
	}
	if s.observed {
		var result sql.Result
		if s.loaded == "" {
			result, err = s.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('telegram_session', ?)", encrypted)
		} else {
			result, err = s.db.Exec("UPDATE settings SET value = ?, updated_at = CURRENT_TIMESTAMP WHERE key = 'telegram_session' AND value = ?", encrypted, s.loaded)
		}
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 0 {
			return nil
		}
	} else if err := s.db.SetSetting("telegram_session", encrypted); err != nil {
		return err
	}
	s.loaded = encrypted
	s.observed = true
	return nil
}

// replaced reports whether the stored session changed under this process, i.e.
// `tdocs login` wrote a new one after we loaded ours.
func (s *EncryptedSessionStorage) replaced() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replacedLocked()
}

func (s *EncryptedSessionStorage) SessionReplaced() bool {
	return s.replaced()
}

func (s *EncryptedSessionStorage) replacedLocked() bool {
	if !s.observed {
		return false
	}
	current, err := s.db.GetSetting("telegram_session")
	if errors.Is(err, sql.ErrNoRows) {
		current = ""
	} else if err != nil {
		return false
	}
	return current != s.loaded
}

// forget drops the loaded-session marker so the next client run loads whatever
// is in the database now.
func (s *EncryptedSessionStorage) forget() {
	s.mu.Lock()
	s.loaded = ""
	s.observed = false
	s.mu.Unlock()
}

func (s *EncryptedSessionStorage) remember(ciphertext string) {
	s.mu.Lock()
	s.loaded = ciphertext
	s.observed = true
	s.mu.Unlock()
}
