package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type Folder struct {
	ID        string    `json:"id"`
	ParentID  *string   `json:"parent_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type File struct {
	ID                 string    `json:"id"`
	FolderID           *string   `json:"folder_id"`
	Name               string    `json:"name"`
	Size               int64     `json:"size"`
	MimeType           string    `json:"mime_type"`
	TelegramMessageID  int       `json:"telegram_message_id"`
	TelegramFileID     string    `json:"telegram_file_id"`
	TelegramAccessHash string    `json:"telegram_access_hash"`
	SHA256             string    `json:"sha256"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type UploadSession struct {
	ID             string    `json:"id"`
	FolderID       *string   `json:"folder_id"`
	Name           string    `json:"name"`
	Size           int64     `json:"size"`
	MimeType       string    `json:"mime_type"`
	TotalParts     int       `json:"total_parts"`
	UploadedParts  int       `json:"uploaded_parts"`
	TelegramFileID int64     `json:"telegram_file_id"`
	CreatedAt      time.Time `json:"created_at"`
}

type ShareLink struct {
	ID            string     `json:"id"`
	Token         string     `json:"token"`
	FileID        string     `json:"file_id"`
	PasswordHash  *string    `json:"password_hash"`
	ExpiresAt     *time.Time `json:"expires_at"`
	DownloadCount int        `json:"download_count"`
	MaxDownloads  *int       `json:"max_downloads"`
	PreviewOnly   bool       `json:"preview_only"`
	CreatedAt     time.Time  `json:"created_at"`
}

// generateID produces a random 16-hex string ID.
func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// CreateFolder adds a new virtual folder.
func (d *DB) CreateFolder(name string, parentID *string) (*Folder, error) {
	id := generateID()
	_, err := d.Exec("INSERT INTO folders (id, parent_id, name) VALUES (?, ?, ?)", id, parentID, name)
	if err != nil {
		return nil, fmt.Errorf("create folder: %w", err)
	}
	return d.GetFolder(id)
}

// GetFolder retrieves a single live folder by ID (trashed folders are hidden).
func (d *DB) GetFolder(id string) (*Folder, error) {
	row := d.QueryRow("SELECT id, parent_id, name, created_at, updated_at FROM folders WHERE id = ? AND deleted_at IS NULL", id)
	var f Folder
	if err := row.Scan(&f.ID, &f.ParentID, &f.Name, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, err
	}
	return &f, nil
}

// GetFolderAny retrieves a folder regardless of trash state (restore flows).
func (d *DB) GetFolderAny(id string) (*Folder, error) {
	row := d.QueryRow("SELECT id, parent_id, name, created_at, updated_at FROM folders WHERE id = ?", id)
	var f Folder
	if err := row.Scan(&f.ID, &f.ParentID, &f.Name, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, err
	}
	return &f, nil
}

// ListFolders lists live subfolders inside parentID (or root if parentID is nil).
func (d *DB) ListFolders(parentID *string) ([]Folder, error) {
	var rows *sql.Rows
	var err error
	if parentID == nil {
		rows, err = d.Query("SELECT id, parent_id, name, created_at, updated_at FROM folders WHERE parent_id IS NULL AND deleted_at IS NULL ORDER BY name ASC")
	} else {
		rows, err = d.Query("SELECT id, parent_id, name, created_at, updated_at FROM folders WHERE parent_id = ? AND deleted_at IS NULL ORDER BY name ASC", *parentID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.ParentID, &f.Name, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// RenameFolder updates the name of a folder.
func (d *DB) RenameFolder(id, newName string) error {
	_, err := d.Exec("UPDATE folders SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", newName, id)
	return err
}

// MoveFolder moves a folder under a new parent (with cycle prevention check).
func (d *DB) MoveFolder(id string, newParentID *string) error {
	if newParentID != nil && *newParentID == id {
		return fmt.Errorf("cannot move folder inside itself")
	}
	// Check for ancestor cycle
	curr := newParentID
	for curr != nil {
		var p *string
		err := d.QueryRow("SELECT parent_id FROM folders WHERE id = ?", *curr).Scan(&p)
		if err != nil {
			break
		}
		if p != nil && *p == id {
			return fmt.Errorf("cannot move folder into its own descendant")
		}
		curr = p
	}

	_, err := d.Exec("UPDATE folders SET parent_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", newParentID, id)
	return err
}

// DeleteFolder permanently deletes a folder and cascades to subfolders;
// files inside are detached (SET NULL), never deleted.
func (d *DB) DeleteFolder(id string) error {
	_, err := d.Exec("DELETE FROM folders WHERE id = ?", id)
	return err
}

// DescendantFolderIDs returns id plus every folder id nested under it.
func (d *DB) DescendantFolderIDs(id string) ([]string, error) {
	ids := []string{id}
	rows, err := d.Query(`
		WITH RECURSIVE sub(id) AS (
			SELECT id FROM folders WHERE parent_id = ?
			UNION ALL
			SELECT f.id FROM folders f JOIN sub s ON f.parent_id = s.id
		) SELECT id FROM sub
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var fid string
		if err := rows.Scan(&fid); err != nil {
			return nil, err
		}
		ids = append(ids, fid)
	}
	return ids, rows.Err()
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := "?"
	for range n - 1 {
		out += ",?"
	}
	return out
}

func anySlice(ids []string) []any {
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, id)
	}
	return out
}

// TrashFolder soft-deletes a folder, its subfolders and every file inside.
func (d *DB) TrashFolder(id string) error {
	ids, err := d.DescendantFolderIDs(id)
	if err != nil {
		return err
	}
	args := anySlice(ids)
	if _, err := d.Exec("UPDATE folders SET deleted_at = CURRENT_TIMESTAMP WHERE id IN ("+placeholders(len(ids))+")", args...); err != nil {
		return err
	}
	// Files directly inside any of these folders.
	ph := placeholders(len(ids))
	if _, err := d.Exec("UPDATE files SET deleted_at = CURRENT_TIMESTAMP WHERE folder_id IN ("+ph+")", args...); err != nil {
		return err
	}
	return nil
}

// RestoreFolder reverses TrashFolder for the same subtree.
func (d *DB) RestoreFolder(id string) error {
	ids, err := d.DescendantFolderIDs(id)
	if err != nil {
		return err
	}
	args := anySlice(ids)
	if _, err := d.Exec("UPDATE folders SET deleted_at = NULL WHERE id IN ("+placeholders(len(ids))+")", args...); err != nil {
		return err
	}
	if _, err := d.Exec("UPDATE files SET deleted_at = NULL WHERE folder_id IN ("+placeholders(len(ids))+")", args...); err != nil {
		return err
	}
	return nil
}

// ListTrashedFolders returns soft-deleted folders (top-level first).
func (d *DB) ListTrashedFolders() ([]Folder, error) {
	rows, err := d.Query("SELECT id, parent_id, name, created_at, updated_at FROM folders WHERE deleted_at IS NOT NULL ORDER BY name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var folders []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.ParentID, &f.Name, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// CreateFile inserts a new file record linked to Telegram object metadata.
func (d *DB) CreateFile(folderID *string, name string, size int64, mimeType string, msgID int, fileID, accessHash, sha256 string) (*File, error) {
	id := generateID()
	_, err := d.Exec(`
		INSERT INTO files (id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, folderID, name, size, mimeType, msgID, fileID, accessHash, sha256)
	if err != nil {
		return nil, fmt.Errorf("create file record: %w", err)
	}
	return d.GetFile(id)
}

// GetFile retrieves a live file by ID (trashed files are hidden).
func (d *DB) GetFile(id string) (*File, error) {
	row := d.QueryRow(`
		SELECT id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, created_at, updated_at
		FROM files WHERE id = ? AND deleted_at IS NULL
	`, id)
	var f File
	if err := row.Scan(&f.ID, &f.FolderID, &f.Name, &f.Size, &f.MimeType, &f.TelegramMessageID, &f.TelegramFileID, &f.TelegramAccessHash, &f.SHA256, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, err
	}
	return &f, nil
}

// GetFileAny retrieves a file regardless of trash state (restore flows).
func (d *DB) GetFileAny(id string) (*File, error) {
	row := d.QueryRow(`
		SELECT id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, created_at, updated_at
		FROM files WHERE id = ?
	`, id)
	var f File
	if err := row.Scan(&f.ID, &f.FolderID, &f.Name, &f.Size, &f.MimeType, &f.TelegramMessageID, &f.TelegramFileID, &f.TelegramAccessHash, &f.SHA256, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, err
	}
	return &f, nil
}

// ListFiles returns all live files within a specific folder (or root if folderID is nil).
func (d *DB) ListFiles(folderID *string) ([]File, error) {
	var rows *sql.Rows
	var err error
	if folderID == nil {
		rows, err = d.Query(`
			SELECT id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, created_at, updated_at
			FROM files WHERE folder_id IS NULL AND deleted_at IS NULL ORDER BY name ASC
		`)
	} else {
		rows, err = d.Query(`
			SELECT id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, created_at, updated_at
			FROM files WHERE folder_id = ? AND deleted_at IS NULL ORDER BY name ASC
		`, *folderID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.FolderID, &f.Name, &f.Size, &f.MimeType, &f.TelegramMessageID, &f.TelegramFileID, &f.TelegramAccessHash, &f.SHA256, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// ListAllFiles returns the full catalog across all Virtual Folders for CDN/API
// consumption. Empty mime matches everything; mime ending in "/" or "/*"
// acts as a prefix filter (e.g. "video/" matches "video/mp4").
func (d *DB) ListAllFiles(search, mime string, limit int) ([]File, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	cond := ""
	args := []any{}
	if search != "" {
		cond += " AND name LIKE ?"
		args = append(args, "%"+search+"%")
	}
	if mime != "" {
		prefix := strings.TrimSuffix(mime, "*")
		if strings.HasSuffix(prefix, "/") {
			cond += " AND mime_type LIKE ?"
			args = append(args, prefix+"%")
		} else {
			cond += " AND mime_type = ?"
			args = append(args, mime)
		}
	}
	args = append(args, limit)
	rows, err := d.Query(`
		SELECT id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, created_at, updated_at
		FROM files WHERE deleted_at IS NULL`+cond+` ORDER BY created_at DESC LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.FolderID, &f.Name, &f.Size, &f.MimeType, &f.TelegramMessageID, &f.TelegramFileID, &f.TelegramAccessHash, &f.SHA256, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// CountFiles returns the total number of virtual files (for status panels).
func (d *DB) CountFiles() (int, error) {
	var n int
	err := d.QueryRow("SELECT COUNT(*) FROM files WHERE deleted_at IS NULL").Scan(&n)
	return n, err
}

// GetFileByTelegramMessageID looks up a file record by its Telegram message ID.
// Used by channel re-sync to avoid creating duplicates for objects that are
// already tracked locally.
func (d *DB) GetFileByTelegramMessageID(msgID int) (*File, error) {
	row := d.QueryRow(`
		SELECT id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, created_at, updated_at
		FROM files WHERE telegram_message_id = ?
	`, msgID)
	var f File
	if err := row.Scan(&f.ID, &f.FolderID, &f.Name, &f.Size, &f.MimeType, &f.TelegramMessageID, &f.TelegramFileID, &f.TelegramAccessHash, &f.SHA256, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, err
	}
	return &f, nil
}

// UpsertFileFromChannel inserts a file discovered in the Telegram Storage
// Channel, or refreshes its Telegram coordinates when a record for the same
// message already exists. Returns true when a new row was created.
//
// This is the recovery path that makes files survive a lost/empty local
// database: metadata is rebuilt from the channel itself.
func (d *DB) UpsertFileFromChannel(folderID *string, name string, size int64, mimeType string, msgID int, fileID, accessHash string) (bool, error) {
	if existing, err := d.GetFileByTelegramMessageID(msgID); err == nil {
		_, uerr := d.Exec(`
			UPDATE files SET name = ?, size = ?, mime_type = ?, telegram_file_id = ?, telegram_access_hash = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, name, size, mimeType, fileID, accessHash, existing.ID)
		return false, uerr
	}

	id := generateID()
	_, err := d.Exec(`
		INSERT INTO files (id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '')
	`, id, folderID, name, size, mimeType, msgID, fileID, accessHash)
	if err != nil {
		return false, fmt.Errorf("upsert file from channel: %w", err)
	}
	return true, nil
}

// CountFolders returns the total number of virtual folders.
func (d *DB) CountFolders() (int, error) {
	var n int
	err := d.QueryRow("SELECT COUNT(*) FROM folders WHERE deleted_at IS NULL").Scan(&n)
	return n, err
}

// RenameFile updates a file's name.
func (d *DB) RenameFile(id, newName string) error {
	_, err := d.Exec("UPDATE files SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", newName, id)
	return err
}

// MoveFile changes the parent folder of a file.
func (d *DB) MoveFile(id string, newFolderID *string) error {
	_, err := d.Exec("UPDATE files SET folder_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", newFolderID, id)
	return err
}

// DeleteFile removes a file record.
func (d *DB) DeleteFile(id string) error {
	_, err := d.Exec("DELETE FROM files WHERE id = ?", id)
	return err
}

// SearchFiles finds live files matching a name query.
func (d *DB) SearchFiles(query string) ([]File, error) {
	return d.SearchFilesFiltered(query, "", 0, 0, "")
}

// SearchFilesFiltered finds live files with optional mime prefix, size bounds
// (bytes, 0 = unbounded) and "updated since" cutoff ("" = any time).
func (d *DB) SearchFilesFiltered(query, mime string, minSize, maxSize int64, since string) ([]File, error) {
	cond := " AND deleted_at IS NULL AND name LIKE ?"
	args := []any{"%" + query + "%"}
	if mime != "" {
		prefix := strings.TrimSuffix(mime, "*")
		if strings.HasSuffix(prefix, "/") {
			cond += " AND mime_type LIKE ?"
			args = append(args, prefix+"%")
		} else {
			cond += " AND mime_type = ?"
			args = append(args, mime)
		}
	}
	if minSize > 0 {
		cond += " AND size >= ?"
		args = append(args, minSize)
	}
	if maxSize > 0 {
		cond += " AND size <= ?"
		args = append(args, maxSize)
	}
	if since != "" {
		cond += " AND updated_at >= ?"
		args = append(args, since)
	}
	rows, err := d.Query(`
		SELECT id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, created_at, updated_at
		FROM files WHERE 1 = 1`+cond+` ORDER BY updated_at DESC LIMIT 100
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.FolderID, &f.Name, &f.Size, &f.MimeType, &f.TelegramMessageID, &f.TelegramFileID, &f.TelegramAccessHash, &f.SHA256, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// CreateUploadSession initializes an in-flight upload session.
func (d *DB) CreateUploadSession(folderID *string, name string, size int64, mimeType string, totalParts int, tgFileID int64) (*UploadSession, error) {
	id := generateID()
	_, err := d.Exec(`
		INSERT INTO upload_sessions (id, folder_id, name, size, mime_type, total_parts, uploaded_parts, telegram_file_id)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?)
	`, id, folderID, name, size, mimeType, totalParts, tgFileID)
	if err != nil {
		return nil, err
	}
	return &UploadSession{
		ID:             id,
		FolderID:       folderID,
		Name:           name,
		Size:           size,
		MimeType:       mimeType,
		TotalParts:     totalParts,
		UploadedParts:  0,
		TelegramFileID: tgFileID,
		CreatedAt:      time.Now(),
	}, nil
}

// IncrementUploadPart marks an additional part as received.
func (d *DB) IncrementUploadPart(id string) (int, error) {
	var count int
	err := d.QueryRow(`
		UPDATE upload_sessions SET uploaded_parts = uploaded_parts + 1 WHERE id = ? RETURNING uploaded_parts
	`, id).Scan(&count)
	return count, err
}

// GetUploadSession retrieves an active upload session.
func (d *DB) GetUploadSession(id string) (*UploadSession, error) {
	var s UploadSession
	err := d.QueryRow(`
		SELECT id, folder_id, name, size, mime_type, total_parts, uploaded_parts, telegram_file_id, created_at
		FROM upload_sessions WHERE id = ?
	`, id).Scan(&s.ID, &s.FolderID, &s.Name, &s.Size, &s.MimeType, &s.TotalParts, &s.UploadedParts, &s.TelegramFileID, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// DeleteUploadSession cleans up an upload session.
func (d *DB) DeleteUploadSession(id string) error {
	_, err := d.Exec("DELETE FROM upload_sessions WHERE id = ?", id)
	return err
}

// CreateShareLink generates a public token for a file.
func (d *DB) CreateShareLink(fileID string, passwordHash *string, expiresAt *time.Time, maxDownloads *int, previewOnly bool) (*ShareLink, error) {
	id := generateID()
	token := generateID() + generateID() // 32 hex chars
	po := 0
	if previewOnly {
		po = 1
	}
	_, err := d.Exec(`
		INSERT INTO share_links (id, token, file_id, password_hash, expires_at, max_downloads, preview_only)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, token, fileID, passwordHash, expiresAt, maxDownloads, po)
	if err != nil {
		return nil, err
	}
	return d.GetShareLink(token)
}

// GetShareLink finds a share link by its public token.
func (d *DB) GetShareLink(token string) (*ShareLink, error) {
	var sl ShareLink
	var po int
	err := d.QueryRow(`
		SELECT id, token, file_id, password_hash, expires_at, download_count, max_downloads, preview_only, created_at
		FROM share_links WHERE token = ?
	`, token).Scan(&sl.ID, &sl.Token, &sl.FileID, &sl.PasswordHash, &sl.ExpiresAt, &sl.DownloadCount, &sl.MaxDownloads, &po, &sl.CreatedAt)
	sl.PreviewOnly = po != 0
	if err != nil {
		return nil, err
	}
	return &sl, nil
}

// IncrementShareDownload tracks download counts on a share link.
func (d *DB) IncrementShareDownload(token string) error {
	_, err := d.Exec("UPDATE share_links SET download_count = download_count + 1 WHERE token = ?", token)
	return err
}

type ShareLinkInfo struct {
	ID            string     `json:"id"`
	Token         string     `json:"token"`
	FileID        string     `json:"file_id"`
	FileName      string     `json:"file_name"`
	FileSize      int64      `json:"file_size"`
	HasPassword   bool       `json:"has_password"`
	ExpiresAt     *time.Time `json:"expires_at"`
	DownloadCount int        `json:"download_count"`
	MaxDownloads  *int       `json:"max_downloads"`
	PreviewOnly   bool       `json:"preview_only"`
	CreatedAt     time.Time  `json:"created_at"`
}

// ListShareLinks returns all active public share links joined with file metadata.
func (d *DB) ListShareLinks() ([]ShareLinkInfo, error) {
	rows, err := d.Query(`
		SELECT s.id, s.token, s.file_id, COALESCE(f.name, 'Deleted File'), COALESCE(f.size, 0),
		       (s.password_hash IS NOT NULL AND s.password_hash != ''),
		       s.expires_at, s.download_count, s.max_downloads, s.preview_only, s.created_at
		FROM share_links s
		LEFT JOIN files f ON s.file_id = f.id
		ORDER BY s.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shares []ShareLinkInfo
	for rows.Next() {
		var s ShareLinkInfo
		var po int
		if err := rows.Scan(&s.ID, &s.Token, &s.FileID, &s.FileName, &s.FileSize, &s.HasPassword, &s.ExpiresAt, &s.DownloadCount, &s.MaxDownloads, &po, &s.CreatedAt); err != nil {
			return nil, err
		}
		s.PreviewOnly = po != 0
		shares = append(shares, s)
	}
	return shares, rows.Err()
}

// DeleteShareLink removes a public share link by its ID or token.
func (d *DB) DeleteShareLink(id string) error {
	_, err := d.Exec("DELETE FROM share_links WHERE id = ? OR token = ?", id, id)
	return err
}
