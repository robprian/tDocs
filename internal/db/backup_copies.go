package db

// Backup-copy bookkeeping for multi-Telegram accounts. One row means the
// file's bytes already exist as a message in that account's "tDocs Vault"
// channel. Rows are keyed by (file, account) so a re-run only copies what
// is missing, and deleting a backup copy row re-queues that file without
// touching the primary catalog.
func (d *DB) HasBackupCopy(fileID, accountID string) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM backup_copies WHERE file_id = ? AND account_id = ?`, fileID, accountID).Scan(&n)
	return n > 0, err
}

func (d *DB) RecordBackupCopy(fileID, accountID string, backupMessageID int) error {
	_, err := d.Exec(`INSERT OR REPLACE INTO backup_copies (file_id, account_id, backup_message_id) VALUES (?, ?, ?)`,
		fileID, accountID, backupMessageID)
	return err
}

func (d *DB) CountBackupCopies(accountID string) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM backup_copies WHERE account_id = ?`, accountID).Scan(&n)
	return n, err
}
