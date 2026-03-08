package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/models"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
	"golang.org/x/oauth2"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("libsql", "file:"+path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := db.Ping(); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	return db
}

func userColumnSet(t *testing.T, db *sql.DB) map[string]struct{} {
	t.Helper()

	rows, err := db.Query("PRAGMA table_info(users)")
	if err != nil {
		t.Fatalf("pragma table_info(users): %v", err)
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			t.Fatalf("scan pragma row: %v", err)
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pragma rows: %v", err)
	}
	return columns
}

func tableColumnSet(t *testing.T, db *sql.DB, tableName string) map[string]struct{} {
	t.Helper()

	rows, err := db.Query("PRAGMA table_info(" + tableName + ")")
	if err != nil {
		t.Fatalf("pragma table_info(%s): %v", tableName, err)
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			t.Fatalf("scan pragma row for %s: %v", tableName, err)
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pragma rows for %s: %v", tableName, err)
	}
	return columns
}

func TestMigrateUpgradesLegacyUsersTable(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	legacySchema := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			idp_issuer TEXT NOT NULL,
			idp_sub TEXT NOT NULL,
			preferred_username TEXT,
			email TEXT,
			email_verified INTEGER NOT NULL DEFAULT 0,
			phone_number TEXT,
			phone_number_verified INTEGER NOT NULL DEFAULT 0,
			picture TEXT,
			about_me TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			last_idp_sync_at TEXT,
			UNIQUE(idp_issuer, idp_sub)
		);`,
		`CREATE TABLE sessions (
			session_id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			expires_at TEXT NOT NULL
		);`,
		`CREATE TABLE oidc_login_state (
			state TEXT PRIMARY KEY,
			nonce TEXT NOT NULL,
			pkce_verifier TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			expires_at TEXT NOT NULL,
			post_login_redirect TEXT
		);`,
	}
	for _, query := range legacySchema {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("apply legacy schema: %v", err)
		}
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}

	columns := userColumnSet(t, db)
	for _, name := range []string{"access_token", "refresh_token", "token_expires_at"} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("expected users.%s to exist after migration", name)
		}
	}

	var applied int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE id = ?`,
		migrationUsersTokenColumnsID,
	).Scan(&applied); err != nil {
		t.Fatalf("query schema migration row: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected migration row to be recorded once, got %d", applied)
	}

	wrapped := &DB{db}
	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}
	user, isNew, err := wrapped.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "user-123",
		PreferredUsername: "houbar",
		Email:             "houbar@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("upsert user on migrated schema: %v", err)
	}
	if !isNew {
		t.Fatalf("expected inserted user to be marked as new")
	}

	got, err := wrapped.GetUser(user.ID)
	if err != nil {
		t.Fatalf("get migrated user: %v", err)
	}
	if got.AccessToken != token.AccessToken {
		t.Fatalf("expected access token to persist, got %q", got.AccessToken)
	}
	if got.RefreshToken != token.RefreshToken {
		t.Fatalf("expected refresh token to persist, got %q", got.RefreshToken)
	}
	if got.TokenExpiresAt.IsZero() {
		t.Fatalf("expected token expiry to persist")
	}
}

func TestMigrateIsIdempotentForFreshSchema(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	if err := migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	columns := userColumnSet(t, db)
	for _, name := range []string{"access_token", "refresh_token", "token_expires_at"} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("expected users.%s to exist after fresh migrate", name)
		}
	}

	var applied int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE id = ?`,
		migrationUsersTokenColumnsID,
	).Scan(&applied); err != nil {
		t.Fatalf("query schema migration row: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected one recorded migration row after repeated migrate, got %d", applied)
	}
}

func TestMigrateCreatesPhotoCapturesTableAndSupportsCRUD(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	columns := tableColumnSet(t, rawDB, "photo_captures")
	for _, name := range []string{
		"id",
		"user_id",
		"client_local_id",
		"original_file_name",
		"content_type",
		"size_bytes",
		"width",
		"height",
		"captured_at",
		"uploaded_at",
		"latitude",
		"longitude",
		"accuracy_meters",
		"status",
		"private_storage_key",
		"public_storage_key",
		"published_at",
	} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("expected photo_captures.%s to exist", name)
		}
	}

	var applied int
	if err := rawDB.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE id = ?`,
		migrationPhotoCapturesTableID,
	).Scan(&applied); err != nil {
		t.Fatalf("query photo capture migration row: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected photo capture migration row once, got %d", applied)
	}

	database := &DB{rawDB}
	token := &oauth2.Token{
		AccessToken:  "capture-access",
		RefreshToken: "capture-refresh",
		Expiry:       time.Now().Add(2 * time.Hour).UTC(),
	}
	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "capture-user",
		PreferredUsername: "mykolog",
		Email:             "mykolog@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create user for capture test: %v", err)
	}

	lat := 49.12345
	lng := 17.45678
	acc := 6.5
	capturedAt := time.Date(2026, time.March, 8, 10, 11, 12, 0, time.UTC)
	uploadedAt := capturedAt.Add(3 * time.Minute)

	capture := &models.Capture{
		ID:                "capture-001",
		UserID:            user.ID,
		ClientLocalID:     "local-001",
		OriginalFileName:  "lesni-nalez.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         2048,
		Width:             1600,
		Height:            1200,
		CapturedAt:        capturedAt,
		UploadedAt:        uploadedAt,
		Latitude:          &lat,
		Longitude:         &lng,
		AccuracyMeters:    &acc,
		Status:            "private",
		PrivateStorageKey: "captures/1/2026/03/capture-001/original.jpg",
	}

	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create capture: %v", err)
	}

	captures, err := database.ListCapturesByUser(user.ID)
	if err != nil {
		t.Fatalf("list captures: %v", err)
	}
	if len(captures) != 1 {
		t.Fatalf("expected one capture, got %d", len(captures))
	}
	if captures[0].PrivateStorageKey != capture.PrivateStorageKey {
		t.Fatalf("unexpected private storage key: %q", captures[0].PrivateStorageKey)
	}

	if err := database.PublishCapture(capture.ID, user.ID, "captures/published/1/2026/03/capture-001.jpg"); err != nil {
		t.Fatalf("publish capture: %v", err)
	}

	published, err := database.GetCaptureForUser(capture.ID, user.ID)
	if err != nil {
		t.Fatalf("get published capture: %v", err)
	}
	if published.Status != "published" {
		t.Fatalf("expected published status, got %q", published.Status)
	}
	if published.PublicStorageKey == "" {
		t.Fatalf("expected public storage key after publish")
	}
	if published.PublishedAt.IsZero() {
		t.Fatalf("expected published_at to be set")
	}

	if err := database.UnpublishCapture(capture.ID, user.ID); err != nil {
		t.Fatalf("unpublish capture: %v", err)
	}

	unpublished, err := database.GetCaptureForUser(capture.ID, user.ID)
	if err != nil {
		t.Fatalf("get unpublished capture: %v", err)
	}
	if unpublished.Status != "private" {
		t.Fatalf("expected private status after unpublish, got %q", unpublished.Status)
	}
	if unpublished.PublicStorageKey != "" {
		t.Fatalf("expected empty public storage key after unpublish, got %q", unpublished.PublicStorageKey)
	}

	if err := database.DeleteCapture(capture.ID, user.ID); err != nil {
		t.Fatalf("delete capture: %v", err)
	}

	if _, err := database.GetCaptureForUser(capture.ID, user.ID); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows after delete, got %v", err)
	}
}
