package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/media"
	"github.com/houbamzdar/bff/internal/models"
)

type Storage interface {
	CanReadPrivate() bool
	StorePrivateObject(ctx context.Context, key string, payload []byte, contentType string) error
}

type Service struct {
	cfg     *config.Config
	db      *db.DB
	storage Storage

	mu      sync.Mutex
	running bool
}

type snapshot struct {
	BackupID    string          `json:"backup_id"`
	TriggerKind string          `json:"trigger_kind"`
	GeneratedAt string          `json:"generated_at"`
	AppBaseURL  string          `json:"app_base_url,omitempty"`
	Tables      []snapshotTable `json:"tables"`
}

type tableMeta struct {
	Name      string
	CreateSQL string
}

type snapshotTable struct {
	Name      string          `json:"name"`
	CreateSQL string          `json:"create_sql"`
	Columns   []string        `json:"columns"`
	Rows      [][]interface{} `json:"rows"`
}

func New(cfg *config.Config, database *db.DB, bunnyStorage *media.BunnyStorage) *Service {
	return NewWithStorage(cfg, database, bunnyStorage)
}

func NewWithStorage(cfg *config.Config, database *db.DB, backupStorage Storage) *Service {
	return &Service{
		cfg:     cfg,
		db:      database,
		storage: backupStorage,
	}
}

func (s *Service) Enabled() bool {
	return s != nil && s.db != nil && s.storage != nil && s.storage.CanReadPrivate()
}

func (s *Service) SchedulerEnabled() bool {
	return s.Enabled() && s.cfg != nil && s.cfg.AdminBackupIntervalHours > 0
}

func (s *Service) RunScheduler(ctx context.Context) {
	if !s.SchedulerEnabled() {
		return
	}

	interval := time.Duration(s.cfg.AdminBackupIntervalHours) * time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = s.CreateBackup(ctx, 0, "scheduled")
		}
	}
}

func (s *Service) CreateBackup(ctx context.Context, initiatedByUserID int64, triggerKind string) (*models.AdminBackup, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("backup service is not configured")
	}
	triggerKind = strings.TrimSpace(triggerKind)
	if triggerKind == "" {
		triggerKind = "manual"
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil, fmt.Errorf("another backup is already running")
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	record, err := s.db.CreateAdminBackup(triggerKind, initiatedByUserID)
	if err != nil {
		return nil, err
	}

	compressedBytes, err := s.buildCompressedSnapshot(ctx, record.ID, triggerKind)
	if err != nil {
		_ = s.db.FailAdminBackup(record.ID, err.Error())
		return nil, err
	}

	storageKey := backupStorageKey(record.ID, record.StartedAt)
	if err := s.storage.StorePrivateObject(ctx, storageKey, compressedBytes, "application/gzip"); err != nil {
		_ = s.db.FailAdminBackup(record.ID, err.Error())
		return nil, err
	}

	checksum := sha256.Sum256(compressedBytes)
	if err := s.db.CompleteAdminBackup(record.ID, storageKey, hex.EncodeToString(checksum[:]), int64(len(compressedBytes))); err != nil {
		return nil, err
	}

	return s.db.GetAdminBackup(record.ID)
}

func backupStorageKey(backupID string, startedAt time.Time) string {
	return fmt.Sprintf(
		"backups/database/%s/%s/%s-%s.json.gz",
		startedAt.UTC().Format("2006"),
		startedAt.UTC().Format("01"),
		startedAt.UTC().Format("20060102T150405Z"),
		backupID,
	)
}

func (s *Service) buildCompressedSnapshot(ctx context.Context, backupID string, triggerKind string) ([]byte, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin backup transaction: %w", err)
	}
	defer tx.Rollback()

	tableRows, err := tx.QueryContext(ctx, `
		SELECT name, sql
		FROM sqlite_master
		WHERE type = 'table'
			AND name NOT LIKE 'sqlite_%'
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list sqlite tables: %w", err)
	}
	defer tableRows.Close()

	tableMetas := make([]tableMeta, 0, 16)
	for tableRows.Next() {
		var tableName string
		var createSQL string
		if err := tableRows.Scan(&tableName, &createSQL); err != nil {
			return nil, fmt.Errorf("scan sqlite table: %w", err)
		}
		tableMetas = append(tableMetas, tableMeta{Name: tableName, CreateSQL: createSQL})
	}
	if err := tableRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite tables: %w", err)
	}
	if err := tableRows.Close(); err != nil {
		return nil, fmt.Errorf("close sqlite table cursor: %w", err)
	}

	snapshotTables := make([]snapshotTable, 0, len(tableMetas))
	for _, meta := range tableMetas {
		tableDump, err := dumpTable(ctx, tx, meta.Name, meta.CreateSQL)
		if err != nil {
			return nil, err
		}
		snapshotTables = append(snapshotTables, tableDump)
	}

	payload := snapshot{
		BackupID:    backupID,
		TriggerKind: triggerKind,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		AppBaseURL:  strings.TrimSpace(s.cfg.AppBaseURL),
		Tables:      snapshotTables,
	}

	var raw bytes.Buffer
	encoder := json.NewEncoder(&raw)
	if err := encoder.Encode(payload); err != nil {
		return nil, fmt.Errorf("encode backup snapshot: %w", err)
	}

	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	if _, err := gzipWriter.Write(raw.Bytes()); err != nil {
		return nil, fmt.Errorf("gzip backup snapshot: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, fmt.Errorf("finalize gzip backup snapshot: %w", err)
	}

	return compressed.Bytes(), nil
}

func dumpTable(ctx context.Context, tx *sql.Tx, tableName string, createSQL string) (snapshotTable, error) {
	quotedTableName := quoteIdentifier(tableName)
	rows, err := tx.QueryContext(ctx, "SELECT * FROM "+quotedTableName)
	if err != nil {
		return snapshotTable{}, fmt.Errorf("dump table %s: %w", tableName, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return snapshotTable{}, fmt.Errorf("columns for table %s: %w", tableName, err)
	}

	table := snapshotTable{
		Name:      tableName,
		CreateSQL: createSQL,
		Columns:   columns,
		Rows:      make([][]interface{}, 0),
	}

	for rows.Next() {
		values := make([]interface{}, len(columns))
		destinations := make([]interface{}, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return snapshotTable{}, fmt.Errorf("scan row for table %s: %w", tableName, err)
		}

		row := make([]interface{}, len(values))
		for index, value := range values {
			row[index] = normalizeSnapshotValue(value)
		}
		table.Rows = append(table.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return snapshotTable{}, fmt.Errorf("iterate rows for table %s: %w", tableName, err)
	}

	return table, nil
}

func normalizeSnapshotValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case nil:
		return nil
	case []byte:
		return string(typed)
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	default:
		return typed
	}
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
