package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// v3.2.0 additions: tags, file requests, dashboard users, and per-file
// storage backend (telegram vs local disk). All tables are created by the
// schema in db.go; this file holds their accessors.

// ------------------------------------------------------------------ tags --

// NormalizeTag lowercases a tag, turns spaces into dashes, and drops
// characters outside [a-z0-9-_]. It returns "" for an unusable tag.
func NormalizeTag(raw string) string {
	t := strings.ToLower(strings.TrimSpace(raw))
	t = strings.ReplaceAll(t, " ", "-")
	var b strings.Builder
	for _, r := range t {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	t = b.String()
	if len(t) > 32 {
		t = t[:32]
	}
	return strings.Trim(t, "-")
}

func (d *DB) AddTag(fileID, tag string) error {
	tag = NormalizeTag(tag)
	if tag == "" {
		return fmt.Errorf("invalid tag")
	}
	if _, err := d.GetFileAny(fileID); err != nil {
		return err
	}
	_, err := d.Exec(`INSERT OR IGNORE INTO file_tags (file_id, tag) VALUES (?, ?)`, fileID, tag)
	return err
}

func (d *DB) RemoveTag(fileID, tag string) error {
	_, err := d.Exec(`DELETE FROM file_tags WHERE file_id = ? AND tag = ?`, fileID, NormalizeTag(tag))
	return err
}

func (d *DB) ListFileTags(fileID string) ([]string, error) {
	rows, err := d.Query(`SELECT tag FROM file_tags WHERE file_id = ? ORDER BY tag ASC`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

func (d *DB) ListAllTags() ([]TagCount, error) {
	rows, err := d.Query(`
		SELECT ft.tag, COUNT(*) FROM file_tags ft
		JOIN files f ON f.id = ft.file_id
		WHERE f.deleted_at IS NULL
		GROUP BY ft.tag ORDER BY ft.tag ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TagCount{}
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

// ListFilesByTag returns live files carrying tag, optionally within folder.
func (d *DB) ListFilesByTag(tag string, folderID *string) ([]File, error) {
	tag = NormalizeTag(tag)
	return d.listFilesByTag(tag, folderID)
}

func (d *DB) listFilesByTag(tag string, folderID *string) ([]File, error) {
	var rows *sql.Rows
	var err error
	base := `
		SELECT f.id, f.folder_id, f.name, f.size, f.mime_type, f.telegram_message_id, f.telegram_file_id, f.telegram_access_hash, f.sha256, f.storage_kind, f.local_path, f.created_at, f.updated_at
		FROM files f JOIN file_tags ft ON ft.file_id = f.id
		WHERE ft.tag = ? AND f.deleted_at IS NULL`
	if folderID == nil {
		rows, err = d.Query(base+` AND f.folder_id IS NULL ORDER BY f.name ASC`, tag)
	} else {
		rows, err = d.Query(base+` AND f.folder_id = ? ORDER BY f.name ASC`, tag, *folderID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := []File{}
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.FolderID, &f.Name, &f.Size, &f.MimeType, &f.TelegramMessageID, &f.TelegramFileID, &f.TelegramAccessHash, &f.SHA256, &f.StorageKind, &f.LocalPath, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// ---------------------------------------------------------- file requests --

type FileRequest struct {
	ID        string     `json:"id"`
	Token     string     `json:"token"`
	FolderID  *string    `json:"folder_id"`
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

func (d *DB) CreateFileRequest(folderID *string, name string, expiresAt *time.Time) (*FileRequest, error) {
	if folderID != nil {
		if _, err := d.GetFolder(*folderID); err != nil {
			return nil, fmt.Errorf("target folder not found: %w", err)
		}
	}
	id := generateID()
	token := generateID() + generateID()
	_, err := d.Exec(`INSERT INTO file_requests (id, token, folder_id, name, expires_at) VALUES (?, ?, ?, ?, ?)`,
		id, token, folderID, name, expiresAt)
	if err != nil {
		return nil, err
	}
	return d.GetFileRequest(id)
}

func (d *DB) GetFileRequest(id string) (*FileRequest, error) {
	var fr FileRequest
	err := d.QueryRow(`SELECT id, token, folder_id, name, expires_at, created_at FROM file_requests WHERE id = ?`, id).
		Scan(&fr.ID, &fr.Token, &fr.FolderID, &fr.Name, &fr.ExpiresAt, &fr.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &fr, nil
}

func (d *DB) GetFileRequestByToken(token string) (*FileRequest, error) {
	var fr FileRequest
	err := d.QueryRow(`SELECT id, token, folder_id, name, expires_at, created_at FROM file_requests WHERE token = ?`, token).
		Scan(&fr.ID, &fr.Token, &fr.FolderID, &fr.Name, &fr.ExpiresAt, &fr.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &fr, nil
}

// Expired reports whether the request can no longer accept uploads.
func (fr *FileRequest) Expired() bool {
	return fr.ExpiresAt != nil && time.Now().After(*fr.ExpiresAt)
}

func (d *DB) ListFileRequests() ([]FileRequest, error) {
	rows, err := d.Query(`SELECT id, token, folder_id, name, expires_at, created_at FROM file_requests ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FileRequest{}
	for rows.Next() {
		var fr FileRequest
		if err := rows.Scan(&fr.ID, &fr.Token, &fr.FolderID, &fr.Name, &fr.ExpiresAt, &fr.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, fr)
	}
	return out, rows.Err()
}

func (d *DB) DeleteFileRequest(id string) error {
	_, err := d.Exec(`DELETE FROM file_requests WHERE id = ?`, id)
	return err
}

func (d *DB) LinkRequestSession(sessionID, token string) error {
	_, err := d.Exec(`INSERT OR REPLACE INTO request_sessions (session_id, token) VALUES (?, ?)`, sessionID, token)
	return err
}

func (d *DB) RequestTokenForSession(sessionID string) (string, error) {
	var token string
	err := d.QueryRow(`SELECT token FROM request_sessions WHERE session_id = ?`, sessionID).Scan(&token)
	return token, err
}

func (d *DB) DeleteRequestSession(sessionID string) error {
	_, err := d.Exec(`DELETE FROM request_sessions WHERE session_id = ?`, sessionID)
	return err
}

// ------------------------------------------------------------------ users --

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

func validRole(role string) bool { return role == "admin" || role == "viewer" }

func (d *DB) CreateUser(username, passwordHash, role string) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 64 {
		return nil, fmt.Errorf("invalid username")
	}
	if !validRole(role) {
		return nil, fmt.Errorf("invalid role")
	}
	id := generateID()
	if _, err := d.Exec(`INSERT INTO users (id, username, password_hash, role) VALUES (?, ?, ?, ?)`,
		id, username, passwordHash, role); err != nil {
		return nil, err
	}
	return d.GetUser(id)
}

func (d *DB) GetUser(id string) (*User, error) {
	var u User
	err := d.QueryRow(`SELECT id, username, password_hash, role, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (d *DB) GetUserByUsername(username string) (*User, error) {
	var u User
	err := d.QueryRow(`SELECT id, username, password_hash, role, created_at FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (d *DB) ListUsers() ([]User, error) {
	rows, err := d.Query(`SELECT id, username, password_hash, role, created_at FROM users ORDER BY username ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (d *DB) DeleteUser(id string) error {
	_, err := d.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

func (d *DB) UpdateUserRole(id, role string) error {
	if !validRole(role) {
		return fmt.Errorf("invalid role")
	}
	_, err := d.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id)
	return err
}

func (d *DB) UpdateUserPassword(id, passwordHash string) error {
	_, err := d.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	return err
}

// ------------------------------------------------------- storage variants --

// CreateFileWithStorage is CreateFile for a non-Telegram backend. For disk
// files the Telegram coordinates are zero/empty and LocalPath is relative
// to the disk storage root.
func (d *DB) CreateFileWithStorage(folderID *string, name string, size int64, mimeType, kind, localPath, sha256 string) (*File, error) {
	if kind != "disk" {
		return nil, fmt.Errorf("CreateFileWithStorage only supports disk files, got %q", kind)
	}
	id := generateID()
	_, err := d.Exec(`
		INSERT INTO files (id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, storage_kind, local_path)
		VALUES (?, ?, ?, ?, ?, 0, '', '', ?, ?, ?)
		`, id, folderID, name, size, mimeType, sha256, kind, localPath)
	if err != nil {
		return nil, fmt.Errorf("create file record: %w", err)
	}
	return d.GetFile(id)
}

// SetFileStorage updates a file's backend coordinates (used to point a just
// created disk record at its final object path).
func (d *DB) SetFileStorage(id, kind, localPath string) error {
	_, err := d.Exec(`UPDATE files SET storage_kind = ?, local_path = ? WHERE id = ?`, kind, localPath, id)
	return err
}
