package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/gotd/td/session"
	"tdocs/internal/crypto"
	"tdocs/internal/db"
)

// EncryptedSessionStorage implements session.Storage using SQLite and AES-256-GCM.
type EncryptedSessionStorage struct {
	db        *db.DB
	secretKey []byte
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
			return nil, session.ErrNotFound
		}
		return nil, fmt.Errorf("load session from db: %w", err)
	}

	if decrypted, err := crypto.Decrypt(val, s.secretKey); err == nil {
		return decrypted, nil
	}

	// Backward compatibility: session may have been encrypted with the legacy
	// hardcoded default key before secret rotation / keyfile support.
	if decrypted, err := crypto.Decrypt(val, legacyDefaultKey()); err == nil {
		return decrypted, nil
	}

	// Wrong TELEDRIVE_SECRET_KEY, truncated value, or corrupt snapshot restore.
	// Delete the undecryptable row so gotd sees ErrNotFound and starts a fresh
	// auth flow instead of failing every login/server run with
	// "decrypt session: cipher: message authentication failed".
	_ = s.db.DeleteSetting("telegram_session")
	return nil, session.ErrNotFound
}

func (s *EncryptedSessionStorage) StoreSession(ctx context.Context, data []byte) error {
	encrypted, err := crypto.Encrypt(data, s.secretKey)
	if err != nil {
		return fmt.Errorf("encrypt session: %w", err)
	}
	return s.db.SetSetting("telegram_session", encrypted)
}
