package db

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"time"
)

// Cloudy-era feature domains: favorites, trash, file versions, API tokens,
// audit logs, sync history, duplicates and analytics.

// ---------------------------------------------------------------- favorites ---

func (d *DB) AddFavorite(fileID string) error {
	_, err := d.Exec("INSERT OR IGNORE INTO favorites (file_id) VALUES (?)", fileID)
	return err
}

func (d *DB) RemoveFavorite(fileID string) error {
	_, err := d.Exec("DELETE FROM favorites WHERE file_id = ?", fileID)
	return err
}

func (d *DB) IsFavorite(fileID string) (bool, error) {
	var n int
	err := d.QueryRow("SELECT COUNT(*) FROM favorites WHERE file_id = ?", fileID).Scan(&n)
	return n > 0, err
}

// ListFavoriteFiles returns live favorited files.
func (d *DB) ListFavoriteFiles() ([]File, error) {
	rows, err := d.Query(`
		SELECT f.id, f.folder_id, f.name, f.size, f.mime_type, f.telegram_message_id,
		       f.telegram_file_id, f.telegram_access_hash, f.sha256, f.created_at, f.updated_at
		FROM favorites fav JOIN files f ON f.id = fav.file_id
		WHERE f.deleted_at IS NULL ORDER BY fav.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.FolderID, &f.Name, &f.Size, &f.MimeType, &f.TelegramMessageID, &f.TelegramFileID, &f.TelegramAccessHash, &f.SHA256, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// -------------------------------------------------------------------- trash ---

// TrashFile soft-deletes a file (recoverable from Trash).
func (d *DB) TrashFile(id string) error {
	_, err := d.Exec("UPDATE files SET deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL", id)
	return err
}

// RestoreFile reverses TrashFile.
func (d *DB) RestoreFile(id string) error {
	_, err := d.Exec("UPDATE files SET deleted_at = NULL WHERE id = ?", id)
	return err
}

// TrashedFile is a soft-deleted file with its folder name.
type TrashedFile struct {
	File
	FolderName string `json:"folder_name"`
}

func (d *DB) ListTrashedFiles() ([]TrashedFile, error) {
	rows, err := d.Query(`
		SELECT f.id, f.folder_id, f.name, f.size, f.mime_type, f.telegram_message_id,
		       f.telegram_file_id, f.telegram_access_hash, f.sha256, f.created_at, f.updated_at,
		       COALESCE(fo.name, '')
		FROM files f LEFT JOIN folders fo ON fo.id = f.folder_id
		WHERE f.deleted_at IS NOT NULL ORDER BY f.deleted_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrashedFile
	for rows.Next() {
		var t TrashedFile
		if err := rows.Scan(&t.ID, &t.FolderID, &t.Name, &t.Size, &t.MimeType, &t.TelegramMessageID, &t.TelegramFileID, &t.TelegramAccessHash, &t.SHA256, &t.CreatedAt, &t.UpdatedAt, &t.FolderName); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) CountTrashedFiles() (int, error) {
	var n int
	err := d.QueryRow("SELECT COUNT(*) FROM files WHERE deleted_at IS NOT NULL").Scan(&n)
	return n, err
}

// EmptyTrash permanently removes every trashed file and folder record.
// Telegram message deletion happens in the service layer (best-effort).
func (d *DB) EmptyTrash() (files, folders int64, err error) {
	res, err := d.Exec("DELETE FROM files WHERE deleted_at IS NOT NULL")
	if err != nil {
		return 0, 0, err
	}
	files, _ = res.RowsAffected()
	res, err = d.Exec("DELETE FROM folders WHERE deleted_at IS NOT NULL")
	if err != nil {
		return files, 0, err
	}
	folders, _ = res.RowsAffected()
	return files, folders, nil
}

// ----------------------------------------------------------------- versions ---

type FileVersion struct {
	ID                 string    `json:"id"`
	FileID             string    `json:"file_id"`
	Name               string    `json:"name"`
	Size               int64     `json:"size"`
	MimeType           string    `json:"mime_type"`
	TelegramMessageID  int       `json:"telegram_message_id"`
	TelegramFileID     string    `json:"telegram_file_id"`
	TelegramAccessHash string    `json:"telegram_access_hash"`
	SHA256             string    `json:"sha256"`
	CreatedAt          time.Time `json:"created_at"`
}

// ArchiveVersion snapshots the current file record as a restorable version.
func (d *DB) ArchiveVersion(f *File) (*FileVersion, error) {
	id := generateID()
	_, err := d.Exec(`
		INSERT INTO file_versions (id, file_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, f.ID, f.Name, f.Size, f.MimeType, f.TelegramMessageID, f.TelegramFileID, f.TelegramAccessHash, f.SHA256)
	if err != nil {
		return nil, err
	}
	return d.GetVersion(id)
}

func (d *DB) GetVersion(id string) (*FileVersion, error) {
	var v FileVersion
	err := d.QueryRow(`
		SELECT id, file_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, created_at
		FROM file_versions WHERE id = ?
	`, id).Scan(&v.ID, &v.FileID, &v.Name, &v.Size, &v.MimeType, &v.TelegramMessageID, &v.TelegramFileID, &v.TelegramAccessHash, &v.SHA256, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (d *DB) ListVersions(fileID string) ([]FileVersion, error) {
	rows, err := d.Query(`
		SELECT id, file_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, created_at
		FROM file_versions WHERE file_id = ? ORDER BY created_at DESC
	`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileVersion
	for rows.Next() {
		var v FileVersion
		if err := rows.Scan(&v.ID, &v.FileID, &v.Name, &v.Size, &v.MimeType, &v.TelegramMessageID, &v.TelegramFileID, &v.TelegramAccessHash, &v.SHA256, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (d *DB) DeleteVersion(id string) error {
	_, err := d.Exec("DELETE FROM file_versions WHERE id = ?", id)
	return err
}

func (d *DB) CountVersions() (int, error) {
	var n int
	err := d.QueryRow("SELECT COUNT(*) FROM file_versions").Scan(&n)
	return n, err
}

// UpdateFileCoords swaps a file's Telegram object coordinates (version restore).
func (d *DB) UpdateFileCoords(id, name string, size int64, mimeType string, msgID int, fileID, accessHash, sha string) error {
	_, err := d.Exec(`
		UPDATE files SET name = ?, size = ?, mime_type = ?, telegram_message_id = ?,
		                 telegram_file_id = ?, telegram_access_hash = ?, sha256 = ?,
		                 updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, name, size, mimeType, msgID, fileID, accessHash, sha, id)
	return err
}

// SetSHA256 records the content hash computed during upload.
func (d *DB) SetSHA256(id, hexStr string) error {
	_, err := d.Exec("UPDATE files SET sha256 = ? WHERE id = ?", hexStr, id)
	return err
}

// FindByNameInFolder locates a live file for same-name versioning decisions.
func (d *DB) FindByNameInFolder(folderID *string, name string) (*File, error) {
	var id string
	if folderID == nil {
		if err := d.QueryRow(`SELECT id FROM files WHERE folder_id IS NULL AND name = ? AND deleted_at IS NULL`, name).Scan(&id); err != nil {
			return nil, err
		}
		return d.GetFile(id)
	}
	if err := d.QueryRow(`SELECT id FROM files WHERE folder_id = ? AND name = ? AND deleted_at IS NULL`, *folderID, name).Scan(&id); err != nil {
		return nil, err
	}
	return d.GetFile(id)
}

// ------------------------------------------------------------------ tokens ---

type APIToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// NewAPIToken mints a random token. The secret is returned once and never
// stored; only its SHA-256 is persisted.
func (d *DB) NewAPIToken(name string) (id, prefix, secret string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", "", err
	}
	secret = "tdocs_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(secret))
	id = generateID()
	prefix = secret[:12]
	_, err = d.Exec(`INSERT INTO api_tokens (id, name, prefix, token_hash) VALUES (?, ?, ?, ?)`,
		id, name, prefix, hex.EncodeToString(sum[:]))
	if err != nil {
		return "", "", "", err
	}
	return id, prefix, secret, nil
}

func (d *DB) ListAPITokens() ([]APIToken, error) {
	rows, err := d.Query(`SELECT id, name, prefix, created_at, last_used_at, revoked_at FROM api_tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// VerifyAPIToken checks a presented secret against non-revoked tokens.
func (d *DB) VerifyAPIToken(secret string) (bool, error) {
	if len(secret) < 12 {
		return false, nil
	}
	sum := sha256.Sum256([]byte(secret))
	var id, stored string
	err := d.QueryRow(`SELECT id, token_hash FROM api_tokens WHERE prefix = ? AND revoked_at IS NULL`,
		secret[:12]).Scan(&id, &stored)
	if err != nil {
		return false, nil
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(hex.EncodeToString(sum[:]))) == 1 {
		_, _ = d.Exec(`UPDATE api_tokens SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
		return true, nil
	}
	return false, nil
}

func (d *DB) RevokeAPIToken(id string) error {
	_, err := d.Exec(`UPDATE api_tokens SET revoked_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

func (d *DB) DeleteAPIToken(id string) error {
	_, err := d.Exec(`DELETE FROM api_tokens WHERE id = ?`, id)
	return err
}

// ------------------------------------------------------------------- audit ---

func (d *DB) Audit(actor, action, detail, ip string) error {
	_, err := d.Exec(`INSERT INTO audit_logs (actor, action, detail, ip) VALUES (?, ?, ?, ?)`, actor, action, detail, ip)
	return err
}

type AuditEntry struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	IP        string    `json:"ip"`
}

func (d *DB) ListAudit(action string, limit, offset int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	cond := ""
	args := []any{}
	if action != "" {
		cond = "WHERE action = ?"
		args = append(args, action)
	}
	args = append(args, limit, offset)
	rows, err := d.Query(`SELECT id, created_at, actor, action, detail, ip FROM audit_logs `+cond+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.Actor, &e.Action, &e.Detail, &e.IP); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --------------------------------------------------------------- sync runs ---

type SyncRun struct {
	ID         int64      `json:"id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Trigger    string     `json:"trigger"`
	Scanned    int        `json:"scanned"`
	Inserted   int        `json:"inserted"`
	Updated    int        `json:"updated"`
	Error      string     `json:"error"`
}

func (d *DB) StartSyncRun(trigger string) (int64, error) {
	res, err := d.Exec(`INSERT INTO sync_runs (trigger) VALUES (?)`, trigger)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) FinishSyncRun(id int64, scanned, inserted, updated int, errStr string) error {
	_, err := d.Exec(`UPDATE sync_runs SET finished_at = CURRENT_TIMESTAMP, scanned = ?, inserted = ?, updated = ?, error = ? WHERE id = ?`,
		scanned, inserted, updated, errStr, id)
	return err
}

func (d *DB) ListSyncRuns(limit int) ([]SyncRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := d.Query(`SELECT id, started_at, finished_at, trigger, scanned, inserted, updated, error FROM sync_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncRun
	for rows.Next() {
		var s SyncRun
		if err := rows.Scan(&s.ID, &s.StartedAt, &s.FinishedAt, &s.Trigger, &s.Scanned, &s.Inserted, &s.Updated, &s.Error); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// -------------------------------------------------------------- duplicates ---

type DuplicateGroup struct {
	SHA256 string `json:"sha256"`
	Count  int    `json:"count"`
	Bytes  int64  `json:"bytes"`
}

func (d *DB) ListDuplicateGroups() ([]DuplicateGroup, error) {
	rows, err := d.Query(`
		SELECT sha256, COUNT(*), SUM(size) FROM files
		WHERE deleted_at IS NULL AND sha256 IS NOT NULL AND sha256 != ''
		GROUP BY sha256 HAVING COUNT(*) > 1 ORDER BY SUM(size) DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DuplicateGroup
	for rows.Next() {
		var g DuplicateGroup
		if err := rows.Scan(&g.SHA256, &g.Count, &g.Bytes); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (d *DB) ListFilesBySHA(sha string) ([]File, error) {
	rows, err := d.Query(`
		SELECT id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, created_at, updated_at
		FROM files WHERE sha256 = ? AND deleted_at IS NULL ORDER BY created_at DESC
	`, sha)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.FolderID, &f.Name, &f.Size, &f.MimeType, &f.TelegramMessageID, &f.TelegramFileID, &f.TelegramAccessHash, &f.SHA256, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- analytics ---

type MimeStat struct {
	Category string `json:"category"`
	Files    int    `json:"files"`
	Bytes    int64  `json:"bytes"`
}

func (d *DB) TotalBytes() (int64, error) {
	var n *int64
	err := d.QueryRow("SELECT SUM(size) FROM files WHERE deleted_at IS NULL").Scan(&n)
	if err != nil || n == nil {
		return 0, err
	}
	return *n, nil
}

// MimeBreakdown groups live files into Cloudy-style categories.
func (d *DB) MimeBreakdown() ([]MimeStat, error) {
	rows, err := d.Query(`
		SELECT
			CASE
				WHEN mime_type LIKE 'image/%' THEN 'image'
				WHEN mime_type LIKE 'video/%' THEN 'video'
				WHEN mime_type LIKE 'audio/%' THEN 'audio'
				WHEN mime_type = 'application/pdf' OR name LIKE '%.pdf' THEN 'document'
				WHEN mime_type LIKE 'text/%' THEN 'document'
				ELSE 'other'
			END AS cat,
			COUNT(*), COALESCE(SUM(size), 0)
		FROM files WHERE deleted_at IS NULL GROUP BY cat
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MimeStat
	for rows.Next() {
		var m MimeStat
		if err := rows.Scan(&m.Category, &m.Files, &m.Bytes); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) LargestFiles(limit int) ([]File, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := d.Query(`
		SELECT id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, created_at, updated_at
		FROM files WHERE deleted_at IS NULL ORDER BY size DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.FolderID, &f.Name, &f.Size, &f.MimeType, &f.TelegramMessageID, &f.TelegramFileID, &f.TelegramAccessHash, &f.SHA256, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FolderStat holds recursive file count + bytes for one folder.
type FolderStat struct {
	ID    string `json:"id"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// FolderStats aggregates live files over each folder's whole subtree.
func (d *DB) FolderStats() ([]FolderStat, error) {
	type folder struct {
		id, parent string
		hasParent  bool
	}
	rows, err := d.Query(`SELECT id, parent_id FROM folders WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	var folders []folder
	for rows.Next() {
		var f folder
		var pid *string
		if err := rows.Scan(&f.id, &pid); err != nil {
			rows.Close()
			return nil, err
		}
		if pid != nil {
			f.parent, f.hasParent = *pid, true
		}
		folders = append(folders, f)
	}
	rows.Close()

	type frow struct {
		folder string
		size   int64
	}
	frows, err := d.Query(`SELECT folder_id, size FROM files WHERE deleted_at IS NULL AND folder_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	direct := map[string]*FolderStat{}
	for frows.Next() {
		var fr frow
		if err := frows.Scan(&fr.folder, &fr.size); err != nil {
			frows.Close()
			return nil, err
		}
		st := direct[fr.folder]
		if st == nil {
			st = &FolderStat{ID: fr.folder}
			direct[fr.folder] = st
		}
		st.Files++
		st.Bytes += fr.size
	}
	frows.Close()

	children := map[string][]string{}
	for _, f := range folders {
		if f.hasParent {
			children[f.parent] = append(children[f.parent], f.id)
		}
	}
	memo := map[string]FolderStat{}
	var acc func(id string) FolderStat
	acc = func(id string) FolderStat {
		if s, ok := memo[id]; ok {
			return s
		}
		total := FolderStat{ID: id}
		if st, ok := direct[id]; ok {
			total.Files += st.Files
			total.Bytes += st.Bytes
		}
		for _, c := range children[id] {
			sub := acc(c)
			total.Files += sub.Files
			total.Bytes += sub.Bytes
		}
		memo[id] = total
		return total
	}
	out := make([]FolderStat, 0, len(folders))
	for _, f := range folders {
		out = append(out, acc(f.id))
	}
	return out, nil
}

func (d *DB) RecentFiles(limit int) ([]File, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := d.Query(`
		SELECT id, folder_id, name, size, mime_type, telegram_message_id, telegram_file_id, telegram_access_hash, sha256, created_at, updated_at
		FROM files WHERE deleted_at IS NULL ORDER BY updated_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.FolderID, &f.Name, &f.Size, &f.MimeType, &f.TelegramMessageID, &f.TelegramFileID, &f.TelegramAccessHash, &f.SHA256, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
