package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

const schema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS folders (
    id TEXT PRIMARY KEY,
    parent_id TEXT NULL REFERENCES folders(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_folders_parent ON folders(parent_id);

CREATE TABLE IF NOT EXISTS files (
    id TEXT PRIMARY KEY,
    folder_id TEXT NULL REFERENCES folders(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    size INTEGER NOT NULL,
    mime_type TEXT NOT NULL,
    telegram_message_id INTEGER NOT NULL,
    telegram_file_id TEXT NOT NULL,
    telegram_access_hash TEXT NOT NULL,
    sha256 TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_files_folder ON files(folder_id);
CREATE INDEX IF NOT EXISTS idx_files_name ON files(name);

CREATE TABLE IF NOT EXISTS upload_sessions (
    id TEXT PRIMARY KEY,
    folder_id TEXT REFERENCES folders(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    size INTEGER NOT NULL,
    mime_type TEXT NOT NULL,
    total_parts INTEGER NOT NULL,
    uploaded_parts INTEGER DEFAULT 0,
    telegram_file_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS share_links (
    id TEXT PRIMARY KEY,
    token TEXT UNIQUE NOT NULL,
    file_id TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    password_hash TEXT NULL,
    expires_at DATETIME NULL,
    download_count INTEGER DEFAULT 0,
    max_downloads INTEGER NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_share_token ON share_links(token);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS favorites (
    file_id TEXT PRIMARY KEY REFERENCES files(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS file_versions (
    id TEXT PRIMARY KEY,
    file_id TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    size INTEGER NOT NULL,
    mime_type TEXT NOT NULL,
    telegram_message_id INTEGER NOT NULL,
    telegram_file_id TEXT NOT NULL,
    telegram_access_hash TEXT NOT NULL,
    sha256 TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_versions_file ON file_versions(file_id);

CREATE TABLE IF NOT EXISTS api_tokens (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    prefix TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_used_at DATETIME NULL,
    revoked_at DATETIME NULL
);
CREATE INDEX IF NOT EXISTS idx_tokens_prefix ON api_tokens(prefix);

CREATE TABLE IF NOT EXISTS audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    actor TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '',
    ip TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_logs(action);

CREATE TABLE IF NOT EXISTS sync_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    finished_at DATETIME NULL,
    trigger TEXT NOT NULL DEFAULT '',
    scanned INTEGER NOT NULL DEFAULT 0,
    inserted INTEGER NOT NULL DEFAULT 0,
    updated INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT ''
);
`

// Open initializes the SQLite database with WAL journal mode and executes the schema.
func Open(path string) (*DB, error) {
	// ponytail: SQLite with WAL mode and 5s busy timeout handles concurrent readers
	// and serialized writes cleanly without external database servers.
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	// Limit connection pool to ensure thread safety
	sdb.SetMaxOpenConns(10)
	sdb.SetMaxIdleConns(5)
	sdb.SetConnMaxLifetime(time.Hour)

	if _, err := sdb.Exec(schema); err != nil {
		_ = sdb.Close()
		return nil, fmt.Errorf("initialize schema: %w", err)
	}

	// Lightweight migrations for pre-existing databases (Trash support).
	// Missing columns are added; "duplicate column" errors are expected on
	// re-open and therefore ignored.
	for _, stmt := range []string{
		`ALTER TABLE files ADD COLUMN deleted_at DATETIME NULL`,
		`ALTER TABLE folders ADD COLUMN deleted_at DATETIME NULL`,
		`ALTER TABLE share_links ADD COLUMN preview_only INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := sdb.Exec(stmt); err != nil &&
			!strings.Contains(err.Error(), "duplicate column name") {
			_ = sdb.Close()
			return nil, fmt.Errorf("migrate schema: %w", err)
		}
	}

	return &DB{DB: sdb}, nil
}

// GetSetting retrieves a setting value by key.
func (d *DB) GetSetting(key string) (string, error) {
	var val string
	err := d.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err != nil {
		return "", err
	}
	return val, nil
}

// SetSetting stores or updates a setting key-value pair.
func (d *DB) SetSetting(key, val string) error {
	_, err := d.Exec(`
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, key, val)
	return err
}

// DeleteSetting removes a setting key-value pair. Missing keys are not an error.
func (d *DB) DeleteSetting(key string) error {
	_, err := d.Exec("DELETE FROM settings WHERE key = ?", key)
	return err
}
