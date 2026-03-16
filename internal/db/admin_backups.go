package db

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/houbamzdar/bff/internal/models"
)

func scanAdminBackup(rows scanner) (*models.AdminBackup, error) {
	var (
		backup        models.AdminBackup
		finishedAtRaw string
		startedAtRaw  string
	)

	if err := rows.Scan(
		&backup.ID,
		&backup.Status,
		&backup.TriggerKind,
		&backup.StorageKey,
		&backup.ChecksumSHA256,
		&backup.SizeBytes,
		&startedAtRaw,
		&finishedAtRaw,
		&backup.InitiatedByUserID,
		&backup.InitiatedByName,
		&backup.ErrorMessage,
	); err != nil {
		return nil, err
	}

	backup.StartedAt = parseRFC3339Value(startedAtRaw)
	backup.FinishedAt = parseRFC3339Value(finishedAtRaw)
	return &backup, nil
}

func (db *DB) CreateAdminBackup(triggerKind string, initiatedByUserID int64) (*models.AdminBackup, error) {
	backup := &models.AdminBackup{
		ID:                uuid.New().String(),
		Status:            "running",
		TriggerKind:       triggerKind,
		StartedAt:         time.Now().UTC(),
		InitiatedByUserID: initiatedByUserID,
	}

	_, err := db.Exec(`
		INSERT INTO admin_backups (
			id, status, trigger_kind, started_at, initiated_by_user_id
		) VALUES (?, ?, ?, ?, ?)
	`,
		backup.ID,
		backup.Status,
		backup.TriggerKind,
		backup.StartedAt.Format(time.RFC3339),
		nullIfZeroInt64(initiatedByUserID),
	)
	if err != nil {
		return nil, err
	}
	return backup, nil
}

func (db *DB) CompleteAdminBackup(backupID string, storageKey string, checksumSHA256 string, sizeBytes int64) error {
	_, err := db.Exec(`
		UPDATE admin_backups
		SET status = 'completed',
			storage_key = ?,
			checksum_sha256 = ?,
			size_bytes = ?,
			finished_at = ?
		WHERE id = ?
	`,
		nullIfEmpty(storageKey),
		nullIfEmpty(checksumSHA256),
		sizeBytes,
		time.Now().UTC().Format(time.RFC3339),
		backupID,
	)
	return err
}

func (db *DB) FailAdminBackup(backupID string, errorMessage string) error {
	_, err := db.Exec(`
		UPDATE admin_backups
		SET status = 'failed',
			error_message = ?,
			finished_at = ?
		WHERE id = ?
	`,
		nullIfEmpty(errorMessage),
		time.Now().UTC().Format(time.RFC3339),
		backupID,
	)
	return err
}

func (db *DB) GetAdminBackup(backupID string) (*models.AdminBackup, error) {
	row := db.QueryRow(`
		SELECT
			b.id,
			b.status,
			b.trigger_kind,
			COALESCE(b.storage_key, ''),
			COALESCE(b.checksum_sha256, ''),
			COALESCE(b.size_bytes, 0),
			b.started_at,
			COALESCE(b.finished_at, ''),
			COALESCE(b.initiated_by_user_id, 0),
			COALESCE(u.preferred_username, ''),
			COALESCE(b.error_message, '')
		FROM admin_backups b
		LEFT JOIN users u ON u.id = b.initiated_by_user_id
		WHERE b.id = ?
	`, backupID)
	return scanAdminBackup(row)
}

func (db *DB) ListAdminBackups(limit int, offset int) ([]*models.AdminBackup, error) {
	rows, err := db.Query(`
		SELECT
			b.id,
			b.status,
			b.trigger_kind,
			COALESCE(b.storage_key, ''),
			COALESCE(b.checksum_sha256, ''),
			COALESCE(b.size_bytes, 0),
			b.started_at,
			COALESCE(b.finished_at, ''),
			COALESCE(b.initiated_by_user_id, 0),
			COALESCE(u.preferred_username, ''),
			COALESCE(b.error_message, '')
		FROM admin_backups b
		LEFT JOIN users u ON u.id = b.initiated_by_user_id
		ORDER BY b.started_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	backups := make([]*models.AdminBackup, 0, limit)
	for rows.Next() {
		backup, err := scanAdminBackup(rows)
		if err != nil {
			return nil, err
		}
		backups = append(backups, backup)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return backups, nil
}

func (db *DB) CountAdminBackups() (int, error) {
	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM admin_backups`).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (db *DB) GetLatestCompletedAdminBackup() (*models.AdminBackup, error) {
	row := db.QueryRow(`
		SELECT
			b.id,
			b.status,
			b.trigger_kind,
			COALESCE(b.storage_key, ''),
			COALESCE(b.checksum_sha256, ''),
			COALESCE(b.size_bytes, 0),
			b.started_at,
			COALESCE(b.finished_at, ''),
			COALESCE(b.initiated_by_user_id, 0),
			COALESCE(u.preferred_username, ''),
			COALESCE(b.error_message, '')
		FROM admin_backups b
		LEFT JOIN users u ON u.id = b.initiated_by_user_id
		WHERE b.status = 'completed'
		ORDER BY COALESCE(b.finished_at, b.started_at) DESC, b.started_at DESC
		LIMIT 1
	`)
	backup, err := scanAdminBackup(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return backup, nil
}

func (db *DB) ListCompletedAdminBackupsForRetention() ([]*models.AdminBackup, error) {
	rows, err := db.Query(`
		SELECT
			b.id,
			b.status,
			b.trigger_kind,
			COALESCE(b.storage_key, ''),
			COALESCE(b.checksum_sha256, ''),
			COALESCE(b.size_bytes, 0),
			b.started_at,
			COALESCE(b.finished_at, ''),
			COALESCE(b.initiated_by_user_id, 0),
			COALESCE(u.preferred_username, ''),
			COALESCE(b.error_message, '')
		FROM admin_backups b
		LEFT JOIN users u ON u.id = b.initiated_by_user_id
		WHERE b.status = 'completed'
		ORDER BY COALESCE(b.finished_at, b.started_at) DESC, b.started_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	backups := make([]*models.AdminBackup, 0, 32)
	for rows.Next() {
		backup, err := scanAdminBackup(rows)
		if err != nil {
			return nil, err
		}
		backups = append(backups, backup)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return backups, nil
}

func (db *DB) DeleteAdminBackup(backupID string) error {
	_, err := db.Exec(`DELETE FROM admin_backups WHERE id = ?`, backupID)
	return err
}
