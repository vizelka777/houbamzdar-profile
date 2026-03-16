package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/models"
	"golang.org/x/oauth2"
	_ "modernc.org/sqlite"
)

type fakeStorage struct {
	lastKey         string
	lastContentType string
	lastPayload     []byte
	deletedKeys     []string
}

func (f *fakeStorage) CanReadPrivate() bool { return true }

func (f *fakeStorage) StorePrivateObject(_ context.Context, key string, payload []byte, contentType string) error {
	f.lastKey = key
	f.lastContentType = contentType
	f.lastPayload = append([]byte(nil), payload...)
	return nil
}

func (f *fakeStorage) DeletePrivateObject(_ context.Context, key string) error {
	f.deletedKeys = append(f.deletedKeys, key)
	return nil
}

func TestCreateBackupStoresGzippedSnapshotAndHistory(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:             "file:" + filepath.Join(t.TempDir(), "backup.db"),
		FrontOrigin:       "https://houbamzdar.cz",
		AppBaseURL:        "https://api.houbamzdar.cz",
		SessionCookieName: "hzd_session",
	}

	database, err := db.New(cfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}

	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "backup-user",
		PreferredUsername: "backup-user",
		Email:             "backup@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	fake := &fakeStorage{}
	service := NewWithStorage(cfg, database, fake)

	backupRecord, err := service.CreateBackup(context.Background(), user.ID, "manual")
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}

	if backupRecord.Status != "completed" {
		t.Fatalf("expected completed backup, got %q", backupRecord.Status)
	}
	if backupRecord.StorageKey == "" || fake.lastKey == "" {
		t.Fatalf("expected storage key to be written")
	}
	if fake.lastContentType != "application/gzip" {
		t.Fatalf("expected application/gzip, got %q", fake.lastContentType)
	}
	if len(fake.lastPayload) == 0 {
		t.Fatalf("expected compressed backup payload")
	}

	reader, err := gzip.NewReader(bytes.NewReader(fake.lastPayload))
	if err != nil {
		t.Fatalf("open gzip payload: %v", err)
	}
	defer reader.Close()

	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read gzip payload: %v", err)
	}

	var payload struct {
		BackupID    string `json:"backup_id"`
		TriggerKind string `json:"trigger_kind"`
		AppBaseURL  string `json:"app_base_url"`
		Tables      []struct {
			Name string `json:"name"`
		} `json:"tables"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}

	if payload.BackupID != backupRecord.ID {
		t.Fatalf("expected backup id %q, got %q", backupRecord.ID, payload.BackupID)
	}
	if payload.TriggerKind != "manual" {
		t.Fatalf("expected trigger kind manual, got %q", payload.TriggerKind)
	}
	if payload.AppBaseURL != cfg.AppBaseURL {
		t.Fatalf("expected app base url %q, got %q", cfg.AppBaseURL, payload.AppBaseURL)
	}

	foundUsers := false
	foundAdminBackups := false
	for _, table := range payload.Tables {
		if table.Name == "users" {
			foundUsers = true
		}
		if table.Name == "admin_backups" {
			foundAdminBackups = true
		}
	}
	if !foundUsers || !foundAdminBackups {
		t.Fatalf("expected users and admin_backups tables in snapshot, got %+v", payload.Tables)
	}

	history, err := database.ListAdminBackups(10, 0)
	if err != nil {
		t.Fatalf("list admin backups: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected one backup history row, got %d", len(history))
	}
	if history[0].ChecksumSHA256 == "" || history[0].SizeBytes <= 0 {
		t.Fatalf("expected checksum and size to be stored, got %+v", history[0])
	}
}

func TestPruneExpiredBackupsRemovesOldCompletedBackups(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:                    "file:" + filepath.Join(t.TempDir(), "backup-retention.db"),
		FrontOrigin:              "https://houbamzdar.cz",
		AppBaseURL:               "https://api.houbamzdar.cz",
		SessionCookieName:        "hzd_session",
		AdminBackupRetentionDays: 30,
		AdminBackupMaxCompleted:  2,
	}

	database, err := db.New(cfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	now := time.Now().UTC()
	records := []struct {
		id         string
		startedAt  time.Time
		finishedAt time.Time
		storageKey string
	}{
		{id: "backup-newest", startedAt: now.Add(-2 * time.Hour), finishedAt: now.Add(-90 * time.Minute), storageKey: "backups/database/2026/03/newest.json.gz"},
		{id: "backup-keep", startedAt: now.Add(-5 * 24 * time.Hour), finishedAt: now.Add(-5*24*time.Hour + time.Hour), storageKey: "backups/database/2026/03/keep.json.gz"},
		{id: "backup-old", startedAt: now.Add(-45 * 24 * time.Hour), finishedAt: now.Add(-45*24*time.Hour + time.Hour), storageKey: "backups/database/2026/01/old.json.gz"},
	}
	for _, record := range records {
		if _, err := database.Exec(`
			INSERT INTO admin_backups (
				id, status, trigger_kind, storage_key, checksum_sha256, size_bytes, started_at, finished_at
			) VALUES (?, 'completed', 'scheduled', ?, 'checksum', 1234, ?, ?)
		`, record.id, record.storageKey, record.startedAt.Format(time.RFC3339), record.finishedAt.Format(time.RFC3339)); err != nil {
			t.Fatalf("seed backup %s: %v", record.id, err)
		}
	}

	fake := &fakeStorage{}
	service := NewWithStorage(cfg, database, fake)

	deletedCount, err := service.PruneExpiredBackups(context.Background())
	if err != nil {
		t.Fatalf("prune backups: %v", err)
	}
	if deletedCount != 1 {
		t.Fatalf("expected one deleted backup, got %d", deletedCount)
	}
	if len(fake.deletedKeys) != 1 || fake.deletedKeys[0] != "backups/database/2026/01/old.json.gz" {
		t.Fatalf("unexpected deleted keys: %+v", fake.deletedKeys)
	}

	backups, err := database.ListCompletedAdminBackupsForRetention()
	if err != nil {
		t.Fatalf("list retained backups: %v", err)
	}
	if len(backups) != 2 {
		t.Fatalf("expected two retained backups, got %d", len(backups))
	}
	for _, backupRecord := range backups {
		if backupRecord.ID == "backup-old" {
			t.Fatalf("old backup should have been pruned")
		}
	}
}

func TestRunScheduledBackupIfDueCreatesNewBackupAfterInterval(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:                    "file:" + filepath.Join(t.TempDir(), "backup-scheduler.db"),
		FrontOrigin:              "https://houbamzdar.cz",
		AppBaseURL:               "https://api.houbamzdar.cz",
		SessionCookieName:        "hzd_session",
		AdminBackupIntervalHours: 6,
	}

	database, err := db.New(cfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	oldStartedAt := time.Now().UTC().Add(-8 * time.Hour)
	oldFinishedAt := oldStartedAt.Add(10 * time.Minute)
	if _, err := database.Exec(`
		INSERT INTO admin_backups (
			id, status, trigger_kind, storage_key, checksum_sha256, size_bytes, started_at, finished_at
		) VALUES (?, 'completed', 'manual', ?, 'checksum', 1234, ?, ?)
	`, "existing-backup", "backups/database/2026/03/existing.json.gz", oldStartedAt.Format(time.RFC3339), oldFinishedAt.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed existing backup: %v", err)
	}

	fake := &fakeStorage{}
	service := NewWithStorage(cfg, database, fake)

	createdBackup, err := service.runScheduledBackupIfDue(context.Background())
	if err != nil {
		t.Fatalf("run scheduled backup: %v", err)
	}
	if createdBackup == nil {
		t.Fatalf("expected a new scheduled backup to be created")
	}
	if createdBackup.ID == "existing-backup" {
		t.Fatalf("expected a new backup record, got the old one")
	}
	if createdBackup.TriggerKind != "scheduled" {
		t.Fatalf("expected scheduled backup, got %q", createdBackup.TriggerKind)
	}

	total, err := database.CountAdminBackups()
	if err != nil {
		t.Fatalf("count backups: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected two backup rows after scheduler run, got %d", total)
	}
}
