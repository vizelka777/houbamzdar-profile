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
}

func (f *fakeStorage) CanReadPrivate() bool { return true }

func (f *fakeStorage) StorePrivateObject(_ context.Context, key string, payload []byte, contentType string) error {
	f.lastKey = key
	f.lastContentType = contentType
	f.lastPayload = append([]byte(nil), payload...)
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
