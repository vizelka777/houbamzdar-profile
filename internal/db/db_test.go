package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/models"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"golang.org/x/oauth2"
	_ "modernc.org/sqlite"
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
	for _, name := range []string{"access_token", "refresh_token", "token_expires_at", "is_moderator"} {
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
	if got.AccessToken != "" {
		t.Fatalf("expected access token to stay empty in storage, got %q", got.AccessToken)
	}
	if got.RefreshToken != "" {
		t.Fatalf("expected refresh token to stay empty in storage, got %q", got.RefreshToken)
	}
	if !got.TokenExpiresAt.IsZero() {
		t.Fatalf("expected token expiry to stay empty in storage, got %v", got.TokenExpiresAt)
	}
	if got.IsModerator {
		t.Fatalf("expected ordinary migrated user to stay non-moderator")
	}

	staleExpiry := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
		UPDATE users
		SET access_token = ?, refresh_token = ?, token_expires_at = ?
		WHERE id = ?
	`, "stale-access-token", "stale-refresh-token", staleExpiry, user.ID); err != nil {
		t.Fatalf("seed stale tokens: %v", err)
	}

	if _, isNew, err := wrapped.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "user-123",
		PreferredUsername: "houbar",
		Email:             "houbar@example.test",
		EmailVerified:     true,
	}, token); err != nil {
		t.Fatalf("upsert existing user with stale tokens: %v", err)
	} else if isNew {
		t.Fatalf("expected second upsert to update existing user")
	}

	got, err = wrapped.GetUser(user.ID)
	if err != nil {
		t.Fatalf("get updated migrated user: %v", err)
	}
	if got.AccessToken != "" {
		t.Fatalf("expected stale access token to be cleared, got %q", got.AccessToken)
	}
	if got.RefreshToken != "" {
		t.Fatalf("expected stale refresh token to be cleared, got %q", got.RefreshToken)
	}
	if !got.TokenExpiresAt.IsZero() {
		t.Fatalf("expected stale token expiry to be cleared, got %v", got.TokenExpiresAt)
	}

	var accessTokenIsNull bool
	var refreshTokenIsNull bool
	var tokenExpiryIsNull bool
	if err := db.QueryRow(`
		SELECT access_token IS NULL, refresh_token IS NULL, token_expires_at IS NULL
		FROM users
		WHERE id = ?
	`, user.ID).Scan(&accessTokenIsNull, &refreshTokenIsNull, &tokenExpiryIsNull); err != nil {
		t.Fatalf("query raw token columns: %v", err)
	}
	if !accessTokenIsNull || !refreshTokenIsNull || !tokenExpiryIsNull {
		t.Fatalf("expected stored token columns to be NULL, got access=%v refresh=%v expiry=%v", accessTokenIsNull, refreshTokenIsNull, tokenExpiryIsNull)
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
	for _, name := range []string{"access_token", "refresh_token", "token_expires_at", "is_moderator"} {
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

func TestUpsertUserMarksHoubamzdarAsModeratorAndAdmin(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	wrapped := &DB{rawDB}
	user, _, err := wrapped.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "moderator-123",
		PreferredUsername: "houbamzdar",
		Email:             "mod@example.test",
	}, &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("upsert moderator user: %v", err)
	}
	if !user.IsModerator {
		t.Fatalf("expected houbamzdar user to be marked as moderator")
	}
	if !user.IsAdmin {
		t.Fatalf("expected houbamzdar user to be marked as admin")
	}
}

func TestGetUserByPreferredUsername(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	wrapped := &DB{rawDB}
	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}

	standa, _, err := wrapped.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "standa-123",
		PreferredUsername: "Standa",
		Email:             "standa@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("upsert standa: %v", err)
	}

	if _, _, err := wrapped.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "other-123",
		PreferredUsername: "Jana",
		Email:             "jana@example.test",
		EmailVerified:     true,
	}, token); err != nil {
		t.Fatalf("upsert jana: %v", err)
	}

	got, err := wrapped.GetUserByPreferredUsername("standa")
	if err != nil {
		t.Fatalf("lookup standa: %v", err)
	}
	if got.ID != standa.ID {
		t.Fatalf("expected user id %d, got %d", standa.ID, got.ID)
	}

	if _, err := wrapped.GetUserByPreferredUsername("missing"); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows for missing user, got %v", err)
	}
}

func TestUpsertUserDoesNotPersistContactValues(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	wrapped := &DB{rawDB}
	user, _, err := wrapped.UpsertUser(&models.OIDCClaims{
		Iss:                 "https://ahoj420.eu",
		Sub:                 "privacy-123",
		PreferredUsername:   "atlas-sberac_2026",
		Email:               "atlas@example.test",
		EmailVerified:       true,
		PhoneNumber:         "+420777000111",
		PhoneNumberVerified: true,
	}, &oauth2.Token{
		AccessToken:  "privacy-access",
		RefreshToken: "privacy-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	if _, err := ValidatePreferredUsername(user.PreferredUsername); err != nil {
		t.Fatalf("expected generated nickname to be valid, got %q: %v", user.PreferredUsername, err)
	}

	var (
		email                 string
		phoneNumber           string
		emailVerified         bool
		phoneNumberVerified   bool
		preferredUsernameNorm string
	)
	if err := rawDB.QueryRow(`
		SELECT
			COALESCE(email, ''),
			COALESCE(phone_number, ''),
			COALESCE(email_verified, 0),
			COALESCE(phone_number_verified, 0),
			COALESCE(preferred_username_norm, '')
		FROM users
		WHERE id = ?
	`, user.ID).Scan(&email, &phoneNumber, &emailVerified, &phoneNumberVerified, &preferredUsernameNorm); err != nil {
		t.Fatalf("load stored user row: %v", err)
	}

	if email != "" {
		t.Fatalf("expected email to stay empty in storage, got %q", email)
	}
	if phoneNumber != "" {
		t.Fatalf("expected phone_number to stay empty in storage, got %q", phoneNumber)
	}
	if !emailVerified || !phoneNumberVerified {
		t.Fatalf("expected verification flags to persist, got email=%v phone=%v", emailVerified, phoneNumberVerified)
	}
	if preferredUsernameNorm == "" {
		t.Fatalf("expected preferred_username_norm to be stored")
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

func TestMigrateBackfillsPublishedCapturePrivateStorageKey(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("initial migrate: %v", err)
	}

	database := &DB{rawDB}
	token := &oauth2.Token{
		AccessToken:  "capture-access",
		RefreshToken: "capture-refresh",
		Expiry:       time.Now().Add(2 * time.Hour).UTC(),
	}
	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "capture-migration-user",
		PreferredUsername: "migrace",
		Email:             "migrace@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create user for migration test: %v", err)
	}

	capturedAt := time.Date(2026, time.March, 21, 10, 11, 12, 0, time.UTC)
	uploadedAt := capturedAt.Add(3 * time.Minute)
	capture := &models.Capture{
		ID:                "capture-published-legacy",
		UserID:            user.ID,
		OriginalFileName:  "legacy-published.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         2048,
		Width:             1600,
		Height:            1200,
		CapturedAt:        capturedAt,
		UploadedAt:        uploadedAt,
		Status:            "private",
		PrivateStorageKey: "captures/1/2026/03/capture-published-legacy.jpg",
	}
	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create legacy capture: %v", err)
	}
	if err := database.PublishCapture(capture.ID, user.ID, "captures/published/1/2026/03/capture-published-legacy.jpg"); err != nil {
		t.Fatalf("publish legacy capture: %v", err)
	}

	before, err := database.GetCaptureForUser(capture.ID, user.ID)
	if err != nil {
		t.Fatalf("load capture before backfill: %v", err)
	}
	if before.PrivateStorageKey == before.PublicStorageKey {
		t.Fatalf("expected legacy keys to differ before backfill")
	}

	if err := migrate(rawDB); err != nil {
		t.Fatalf("rerun migrate for backfill: %v", err)
	}

	after, err := database.GetCaptureForUser(capture.ID, user.ID)
	if err != nil {
		t.Fatalf("load capture after backfill: %v", err)
	}
	if after.PrivateStorageKey != after.PublicStorageKey {
		t.Fatalf("expected private key %q to match public key %q after backfill", after.PrivateStorageKey, after.PublicStorageKey)
	}

	var applied int
	if err := rawDB.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE id = ?`,
		migrationBackfillPublishedCapturePrivateKeysID,
	).Scan(&applied); err != nil {
		t.Fatalf("query backfill migration row: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected backfill migration row once, got %d", applied)
	}
}

func TestListCapturesPageSupportsStatusDateAndPagination(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	database := &DB{rawDB}
	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "paged-user",
		PreferredUsername: "paged",
		Email:             "paged@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "paged-access",
		RefreshToken: "paged-refresh",
		Expiry:       time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	firstDay := time.Date(2026, time.March, 8, 9, 0, 0, 0, time.UTC)
	secondDay := time.Date(2026, time.March, 9, 11, 30, 0, 0, time.UTC)

	captures := []*models.Capture{
		{
			ID:                "cap-1",
			UserID:            user.ID,
			OriginalFileName:  "one.jpg",
			ContentType:       "image/jpeg",
			SizeBytes:         100,
			Width:             1000,
			Height:            800,
			CapturedAt:        firstDay,
			UploadedAt:        firstDay,
			Status:            "private",
			PrivateStorageKey: "captures/1/2026/03/cap-1.jpg",
		},
		{
			ID:                "cap-2",
			UserID:            user.ID,
			OriginalFileName:  "two.jpg",
			ContentType:       "image/jpeg",
			SizeBytes:         200,
			Width:             1200,
			Height:            900,
			CapturedAt:        secondDay,
			UploadedAt:        secondDay,
			Status:            "published",
			PrivateStorageKey: "captures/1/2026/03/cap-2.jpg",
			PublicStorageKey:  "captures/published/1/2026/03/cap-2.jpg",
			PublishedAt:       secondDay.Add(10 * time.Minute),
		},
		{
			ID:                "cap-3",
			UserID:            user.ID,
			OriginalFileName:  "three.jpg",
			ContentType:       "image/jpeg",
			SizeBytes:         300,
			Width:             1400,
			Height:            1000,
			CapturedAt:        secondDay.Add(5 * time.Minute),
			UploadedAt:        secondDay.Add(5 * time.Minute),
			Status:            "private",
			PrivateStorageKey: "captures/1/2026/03/cap-3.jpg",
		},
	}

	for _, capture := range captures {
		if err := database.CreateCapture(capture); err != nil {
			t.Fatalf("create capture %s: %v", capture.ID, err)
		}
	}
	if err := database.PublishCapture("cap-2", user.ID, "captures/published/1/2026/03/cap-2.jpg"); err != nil {
		t.Fatalf("publish cap-2: %v", err)
	}

	privatePage, err := database.ListCapturesPage(user.ID, CaptureListFilters{
		Status:   "private",
		Page:     1,
		PageSize: 1,
	})
	if err != nil {
		t.Fatalf("list private page: %v", err)
	}
	if privatePage.Total != 2 || privatePage.TotalPages != 2 {
		t.Fatalf("unexpected private pagination totals: %+v", privatePage)
	}
	if len(privatePage.Captures) != 1 || privatePage.Captures[0].ID != "cap-3" {
		t.Fatalf("unexpected first private page: %+v", privatePage.Captures)
	}

	capturedFrom := time.Date(2026, time.March, 9, 0, 0, 0, 0, time.UTC)
	capturedTo := capturedFrom.Add(24 * time.Hour)
	dayPage, err := database.ListCapturesPage(user.ID, CaptureListFilters{
		CapturedFrom: &capturedFrom,
		CapturedTo:   &capturedTo,
		Page:         1,
		PageSize:     10,
	})
	if err != nil {
		t.Fatalf("list day page: %v", err)
	}
	if dayPage.Total != 2 {
		t.Fatalf("expected 2 captures on second day, got %d", dayPage.Total)
	}

	publishedPage, err := database.ListCapturesPage(user.ID, CaptureListFilters{
		Status:   "published",
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("list published page: %v", err)
	}
	if publishedPage.Total != 1 || len(publishedPage.Captures) != 1 || publishedPage.Captures[0].ID != "cap-2" {
		t.Fatalf("unexpected published page: %+v", publishedPage.Captures)
	}
}

func TestDeleteUserByAdminRemovesUserSessionsPostsAndCaptureRefs(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	database := &DB{rawDB}
	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "delete-user-admin-target",
		PreferredUsername: "delete-target",
		Email:             "delete-target@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "delete-target-access",
		RefreshToken: "delete-target-refresh",
		Expiry:       time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := database.CreateSession(&models.Session{
		SessionID: "delete-target-session",
		UserID:    user.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	privateCapture := &models.Capture{
		ID:                "delete-target-private",
		UserID:            user.ID,
		OriginalFileName:  "private.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         1024,
		Width:             800,
		Height:            600,
		CapturedAt:        time.Now().UTC(),
		UploadedAt:        time.Now().UTC(),
		Status:            "private",
		PrivateStorageKey: "captures/private/delete-target-private.jpg",
	}
	if err := database.CreateCapture(privateCapture); err != nil {
		t.Fatalf("create private capture: %v", err)
	}

	publishedCapture := &models.Capture{
		ID:                "delete-target-published",
		UserID:            user.ID,
		OriginalFileName:  "published.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         2048,
		Width:             1200,
		Height:            900,
		CapturedAt:        time.Now().UTC(),
		UploadedAt:        time.Now().UTC(),
		Status:            "private",
		PrivateStorageKey: "captures/private/delete-target-published.jpg",
	}
	if err := database.CreateCapture(publishedCapture); err != nil {
		t.Fatalf("create published capture: %v", err)
	}
	if err := database.PublishCapture(publishedCapture.ID, user.ID, "captures/published/delete-target-published.jpg"); err != nil {
		t.Fatalf("publish capture: %v", err)
	}

	post := &models.Post{
		ID:        "delete-target-post",
		UserID:    user.ID,
		Content:   "to be deleted",
		Status:    "published",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Captures:  []*models.Capture{{ID: publishedCapture.ID}},
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	refs, err := database.ListUserCaptureStorageRefs(user.ID)
	if err != nil {
		t.Fatalf("list capture refs: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 capture refs, got %d", len(refs))
	}

	if err := database.DeleteUserByAdmin(user.ID); err != nil {
		t.Fatalf("delete user by admin: %v", err)
	}

	if _, err := database.GetUser(user.ID); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows for user, got %v", err)
	}
	if _, err := database.GetSession("delete-target-session"); err != sql.ErrNoRows {
		t.Fatalf("expected session to be deleted, got %v", err)
	}
	if _, err := database.GetCaptureForUser(privateCapture.ID, user.ID); err != sql.ErrNoRows {
		t.Fatalf("expected private capture to be deleted, got %v", err)
	}
	if _, err := database.GetCaptureForUser(publishedCapture.ID, user.ID); err != sql.ErrNoRows {
		t.Fatalf("expected published capture to be deleted, got %v", err)
	}

	var postCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM posts WHERE id = ?`, post.ID).Scan(&postCount); err != nil {
		t.Fatalf("count posts: %v", err)
	}
	if postCount != 0 {
		t.Fatalf("expected posts to cascade delete, got %d", postCount)
	}
}

func TestPostsSupportComments(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	columns := tableColumnSet(t, rawDB, "post_comments")
	for _, name := range []string{"id", "post_id", "user_id", "content", "created_at", "updated_at"} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("expected post_comments.%s to exist", name)
		}
	}

	var applied int
	if err := rawDB.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE id = ?`,
		migrationPostCommentsTableID,
	).Scan(&applied); err != nil {
		t.Fatalf("query post comments migration row: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected post comments migration row once, got %d", applied)
	}

	database := &DB{rawDB}
	author, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "author-user",
		PreferredUsername: "autor",
		Email:             "author@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "author-access",
		RefreshToken: "author-refresh",
		Expiry:       time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	commenter, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "commenter-user",
		PreferredUsername: "komentator",
		Email:             "commenter@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "commenter-access",
		RefreshToken: "commenter-refresh",
		Expiry:       time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create commenter: %v", err)
	}

	postCreatedAt := time.Date(2026, time.March, 14, 10, 0, 0, 0, time.UTC)
	post := &models.Post{
		ID:        "post-comments-001",
		UserID:    author.ID,
		Content:   "Prispevek s komentari",
		Status:    "published",
		CreatedAt: postCreatedAt,
		UpdatedAt: postCreatedAt,
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	firstComment := &models.Comment{
		ID:        "comment-001",
		PostID:    post.ID,
		UserID:    commenter.ID,
		Content:   "Tohle je skvely nalez.",
		CreatedAt: postCreatedAt.Add(5 * time.Minute),
		UpdatedAt: postCreatedAt.Add(5 * time.Minute),
	}
	if err := database.CreatePostComment(firstComment); err != nil {
		t.Fatalf("create first comment: %v", err)
	}

	secondComment := &models.Comment{
		ID:        "comment-002",
		PostID:    post.ID,
		UserID:    author.ID,
		Content:   "Diky, zitra jdu znovu.",
		CreatedAt: postCreatedAt.Add(8 * time.Minute),
		UpdatedAt: postCreatedAt.Add(8 * time.Minute),
	}
	if err := database.CreatePostComment(secondComment); err != nil {
		t.Fatalf("create second comment: %v", err)
	}

	publicPosts, err := database.ListPublicPosts(10, 0, 0)
	if err != nil {
		t.Fatalf("list public posts: %v", err)
	}
	if len(publicPosts) != 1 {
		t.Fatalf("expected 1 public post, got %d", len(publicPosts))
	}
	if len(publicPosts[0].Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(publicPosts[0].Comments))
	}
	if publicPosts[0].Comments[0].Content != firstComment.Content {
		t.Fatalf("unexpected first comment content: %q", publicPosts[0].Comments[0].Content)
	}
	if publicPosts[0].Comments[0].AuthorName != commenter.PreferredUsername {
		t.Fatalf("unexpected first comment author: %q", publicPosts[0].Comments[0].AuthorName)
	}
	if publicPosts[0].Comments[1].AuthorName != author.PreferredUsername {
		t.Fatalf("unexpected second comment author: %q", publicPosts[0].Comments[1].AuthorName)
	}

	ownerPost, err := database.GetPost(post.ID, author.ID)
	if err != nil {
		t.Fatalf("get owner post: %v", err)
	}
	if len(ownerPost.Comments) != 2 {
		t.Fatalf("expected comments on owner post, got %d", len(ownerPost.Comments))
	}

	if err := database.DeletePost(post.ID, author.ID); err != nil {
		t.Fatalf("delete post: %v", err)
	}

	var remainingComments int
	if err := rawDB.QueryRow(`SELECT COUNT(*) FROM post_comments WHERE post_id = ?`, post.ID).Scan(&remainingComments); err != nil {
		t.Fatalf("count remaining comments: %v", err)
	}
	if remainingComments != 0 {
		t.Fatalf("expected 0 remaining comments after delete, got %d", remainingComments)
	}

	var remainingPosts int
	if err := rawDB.QueryRow(`SELECT COUNT(*) FROM posts WHERE id = ?`, post.ID).Scan(&remainingPosts); err != nil {
		t.Fatalf("count remaining posts: %v", err)
	}
	if remainingPosts != 0 {
		t.Fatalf("expected post to be deleted, got %d rows", remainingPosts)
	}
}

func TestPostLikesSupportFeedStateAndDeleteCleanup(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	columns := tableColumnSet(t, rawDB, "post_likes")
	for _, name := range []string{"post_id", "user_id", "created_at"} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("expected post_likes.%s to exist after migration", name)
		}
	}

	var applied int
	if err := rawDB.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE id = ?`,
		migrationPostLikesTableID,
	).Scan(&applied); err != nil {
		t.Fatalf("query likes migration row: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected likes migration row to be recorded once, got %d", applied)
	}

	database := &DB{rawDB}

	author, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "likes-author",
		PreferredUsername: "autor",
		Email:             "likes-author@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "author-access",
		RefreshToken: "author-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	liker, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "likes-liker",
		PreferredUsername: "fanousek",
		Email:             "likes-liker@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "liker-access",
		RefreshToken: "liker-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create liker: %v", err)
	}

	postCreatedAt := time.Date(2026, time.March, 14, 14, 0, 0, 0, time.UTC)
	post := &models.Post{
		ID:        "post-likes-001",
		UserID:    author.ID,
		Content:   "Prispevek s lajky",
		Status:    "published",
		CreatedAt: postCreatedAt,
		UpdatedAt: postCreatedAt,
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	likesCount, isLiked, err := database.TogglePostLike(post.ID, liker.ID)
	if err != nil {
		t.Fatalf("toggle like: %v", err)
	}
	if likesCount != 1 || !isLiked {
		t.Fatalf("unexpected toggle like result: count=%d isLiked=%v", likesCount, isLiked)
	}

	likedPosts, err := database.ListPublicPosts(10, 0, liker.ID)
	if err != nil {
		t.Fatalf("list public posts for liker: %v", err)
	}
	if len(likedPosts) != 1 {
		t.Fatalf("expected 1 public post for liker, got %d", len(likedPosts))
	}
	if likedPosts[0].LikesCount != 1 || !likedPosts[0].IsLikedByMe {
		t.Fatalf("unexpected liker feed state: likes=%d likedByMe=%v", likedPosts[0].LikesCount, likedPosts[0].IsLikedByMe)
	}

	guestPosts, err := database.ListPublicPosts(10, 0, 0)
	if err != nil {
		t.Fatalf("list public posts for guest: %v", err)
	}
	if len(guestPosts) != 1 {
		t.Fatalf("expected 1 public post for guest, got %d", len(guestPosts))
	}
	if guestPosts[0].LikesCount != 1 || guestPosts[0].IsLikedByMe {
		t.Fatalf("unexpected guest feed state: likes=%d likedByMe=%v", guestPosts[0].LikesCount, guestPosts[0].IsLikedByMe)
	}

	if err := database.DeletePost(post.ID, author.ID); err != nil {
		t.Fatalf("delete post: %v", err)
	}

	var remainingLikes int
	if err := rawDB.QueryRow(`SELECT COUNT(*) FROM post_likes WHERE post_id = ?`, post.ID).Scan(&remainingLikes); err != nil {
		t.Fatalf("count remaining likes: %v", err)
	}
	if remainingLikes != 0 {
		t.Fatalf("expected 0 remaining likes after delete, got %d", remainingLikes)
	}
}

func TestGrantAuthBonusesAreIdempotent(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	database := &DB{rawDB}
	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "houbicka-bonus-user",
		PreferredUsername: "bonusnik",
		Email:             "bonusnik@example.test",
		PhoneNumber:       "+420777000111",
	}, &oauth2.Token{
		AccessToken:  "bonus-access",
		RefreshToken: "bonus-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := database.GrantAuthBonuses(user.ID, true, false, false); err != nil {
		t.Fatalf("grant signup bonus: %v", err)
	}
	if err := database.GrantAuthBonuses(user.ID, false, true, true); err != nil {
		t.Fatalf("grant verification bonuses: %v", err)
	}
	if err := database.GrantAuthBonuses(user.ID, false, true, true); err != nil {
		t.Fatalf("regrant verification bonuses: %v", err)
	}

	balance, err := database.GetHoubickaBalance(user.ID)
	if err != nil {
		t.Fatalf("get houbicka balance: %v", err)
	}
	if balance != 9 {
		t.Fatalf("expected final balance 9, got %d", balance)
	}

	var operationCount int
	if err := rawDB.QueryRow(`SELECT COUNT(*) FROM houbicka_operations WHERE target_user_id = ?`, user.ID).Scan(&operationCount); err != nil {
		t.Fatalf("count houbicka operations: %v", err)
	}
	if operationCount != 3 {
		t.Fatalf("expected 3 recorded operations, got %d", operationCount)
	}
}

func TestUnlockCaptureCoordinatesTransfersBalanceAndKeepsViewedList(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	database := &DB{rawDB}
	author, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "unlock-author",
		PreferredUsername: "autor",
		Email:             "unlock-author@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "author-access",
		RefreshToken: "author-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	viewer, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "unlock-viewer",
		PreferredUsername: "divak",
		Email:             "unlock-viewer@example.test",
	}, &oauth2.Token{
		AccessToken:  "viewer-access",
		RefreshToken: "viewer-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}

	if err := database.GrantAuthBonuses(viewer.ID, true, false, false); err != nil {
		t.Fatalf("grant initial viewer balance: %v", err)
	}

	lat := 49.2468
	lon := 17.1357
	capturedAt := time.Date(2026, time.March, 15, 9, 30, 0, 0, time.UTC)
	capture := &models.Capture{
		ID:                "unlock-capture-001",
		UserID:            author.ID,
		OriginalFileName:  "unlock.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         3000,
		Width:             1200,
		Height:            800,
		CapturedAt:        capturedAt,
		UploadedAt:        capturedAt,
		Latitude:          &lat,
		Longitude:         &lon,
		Status:            "published",
		PrivateStorageKey: "captures/author/unlock.jpg",
		PublicStorageKey:  "captures/published/author/unlock.jpg",
		PublishedAt:       capturedAt.Add(time.Minute),
	}
	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create capture: %v", err)
	}

	balance, alreadyUnlocked, err := database.UnlockCaptureCoordinates(viewer.ID, capture.ID)
	if err != nil {
		t.Fatalf("unlock capture coordinates: %v", err)
	}
	if alreadyUnlocked {
		t.Fatalf("expected first unlock to be charged")
	}
	if balance != 0 {
		t.Fatalf("expected viewer balance 0 after unlock, got %d", balance)
	}

	authorBalance, err := database.GetHoubickaBalance(author.ID)
	if err != nil {
		t.Fatalf("get author balance: %v", err)
	}
	if authorBalance != 1 {
		t.Fatalf("expected author balance 1 after unlock, got %d", authorBalance)
	}

	viewedCaptures, err := database.ListViewedCapturesByUser(viewer.ID, 10, 0)
	if err != nil {
		t.Fatalf("list viewed captures: %v", err)
	}
	if len(viewedCaptures) != 1 {
		t.Fatalf("expected 1 viewed capture, got %d", len(viewedCaptures))
	}
	if viewedCaptures[0].ID != capture.ID {
		t.Fatalf("unexpected viewed capture id: %q", viewedCaptures[0].ID)
	}
	if viewedCaptures[0].UnlockedAt.IsZero() {
		t.Fatalf("expected unlock timestamp to be stored")
	}
	if viewedCaptures[0].Latitude == nil || viewedCaptures[0].Longitude == nil {
		t.Fatalf("expected viewed capture coordinates to remain visible")
	}

	balance, alreadyUnlocked, err = database.UnlockCaptureCoordinates(viewer.ID, capture.ID)
	if err != nil {
		t.Fatalf("unlock capture coordinates second time: %v", err)
	}
	if !alreadyUnlocked {
		t.Fatalf("expected repeated unlock to be treated as already unlocked")
	}
	if balance != 0 {
		t.Fatalf("expected repeated unlock to keep viewer balance 0, got %d", balance)
	}

	authorBalance, err = database.GetHoubickaBalance(author.ID)
	if err != nil {
		t.Fatalf("get author balance after repeated unlock: %v", err)
	}
	if authorBalance != 1 {
		t.Fatalf("expected author balance to stay 1 after repeated unlock, got %d", authorBalance)
	}
}

func TestListPublicCapturesMasksPaidCoordinatesAndLeavesFreeVisible(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	database := &DB{rawDB}
	author, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "mask-author",
		PreferredUsername: "maskautor",
		Email:             "mask-author@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "mask-author-access",
		RefreshToken: "mask-author-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	viewer, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "mask-viewer",
		PreferredUsername: "maskviewer",
		Email:             "mask-viewer@example.test",
	}, &oauth2.Token{
		AccessToken:  "mask-viewer-access",
		RefreshToken: "mask-viewer-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}

	if err := database.GrantAuthBonuses(viewer.ID, true, false, false); err != nil {
		t.Fatalf("grant viewer signup bonus: %v", err)
	}

	makeCapture := func(id string, free bool, lat, lon float64, publishedAt time.Time) *models.Capture {
		return &models.Capture{
			ID:                id,
			UserID:            author.ID,
			OriginalFileName:  id + ".jpg",
			ContentType:       "image/jpeg",
			SizeBytes:         2048,
			Width:             1400,
			Height:            900,
			CapturedAt:        publishedAt,
			UploadedAt:        publishedAt,
			Latitude:          &lat,
			Longitude:         &lon,
			CoordinatesFree:   free,
			Status:            "published",
			PrivateStorageKey: "captures/private/" + id + ".jpg",
			PublicStorageKey:  "captures/public/" + id + ".jpg",
			PublishedAt:       publishedAt,
		}
	}

	paidCapture := makeCapture("mask-paid", false, 49.001, 17.001, time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC))
	freeCapture := makeCapture("mask-free", true, 49.002, 17.002, time.Date(2026, time.March, 15, 10, 5, 0, 0, time.UTC))

	if err := database.CreateCapture(paidCapture); err != nil {
		t.Fatalf("create paid capture: %v", err)
	}
	if err := database.CreateCapture(freeCapture); err != nil {
		t.Fatalf("create free capture: %v", err)
	}

	guestCaptures, err := database.ListPublicCaptures(10, 0, 0)
	if err != nil {
		t.Fatalf("list guest public captures: %v", err)
	}

	captureByID := make(map[string]*models.Capture, len(guestCaptures))
	for _, capture := range guestCaptures {
		captureByID[capture.ID] = capture
	}

	if captureByID[paidCapture.ID] == nil || !captureByID[paidCapture.ID].CoordinatesLocked {
		t.Fatalf("expected paid capture coordinates to be locked for guests")
	}
	if captureByID[paidCapture.ID].Latitude != nil || captureByID[paidCapture.ID].Longitude != nil {
		t.Fatalf("expected paid capture coordinates to be hidden for guests")
	}

	if captureByID[freeCapture.ID] == nil {
		t.Fatalf("expected free capture to appear in guest list")
	}
	if captureByID[freeCapture.ID].CoordinatesLocked {
		t.Fatalf("expected free capture coordinates to stay visible")
	}
	if captureByID[freeCapture.ID].Latitude == nil || captureByID[freeCapture.ID].Longitude == nil {
		t.Fatalf("expected free capture coordinates to be visible for guests")
	}

	if _, _, err := database.UnlockCaptureCoordinates(viewer.ID, paidCapture.ID); err != nil {
		t.Fatalf("unlock paid capture for viewer: %v", err)
	}

	viewerCaptures, err := database.ListPublicCaptures(10, 0, viewer.ID)
	if err != nil {
		t.Fatalf("list viewer public captures: %v", err)
	}

	captureByID = make(map[string]*models.Capture, len(viewerCaptures))
	for _, capture := range viewerCaptures {
		captureByID[capture.ID] = capture
	}

	if captureByID[paidCapture.ID] == nil {
		t.Fatalf("expected paid capture to appear for viewer after unlock")
	}
	if captureByID[paidCapture.ID].CoordinatesLocked {
		t.Fatalf("expected paid capture coordinates to be visible after unlock")
	}
	if captureByID[paidCapture.ID].Latitude == nil || captureByID[paidCapture.ID].Longitude == nil {
		t.Fatalf("expected paid capture coordinates after unlock")
	}
}

func TestMigrateCreatesCaptureEnrichmentTablesAndColumns(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	photoColumns := tableColumnSet(t, rawDB, "photo_captures")
	for _, name := range []string{
		"coordinates_free",
		"publication_review_status",
		"publication_review_reason_code",
		"publication_review_last_error",
		"publication_review_checked_at",
		"publication_requested_at",
		"publication_review_attempts",
		"publication_review_next_attempt_at",
	} {
		if _, ok := photoColumns[name]; !ok {
			t.Fatalf("expected photo_captures.%s to exist", name)
		}
	}

	mushroomAnalysisColumns := tableColumnSet(t, rawDB, "capture_mushroom_analysis")
	for _, name := range []string{
		"capture_id",
		"has_mushrooms",
		"primary_latin_name",
		"primary_latin_name_norm",
		"primary_czech_name",
		"primary_czech_name_norm",
		"primary_probability",
		"model_code",
		"review_source",
		"reviewed_by_user_id",
		"reviewed_at",
		"raw_json",
		"analyzed_at",
	} {
		if _, ok := mushroomAnalysisColumns[name]; !ok {
			t.Fatalf("expected capture_mushroom_analysis.%s to exist", name)
		}
	}

	mushroomSpeciesColumns := tableColumnSet(t, rawDB, "capture_mushroom_species")
	for _, name := range []string{
		"id",
		"capture_id",
		"latin_name",
		"latin_name_norm",
		"czech_official_name",
		"czech_official_name_norm",
		"probability",
	} {
		if _, ok := mushroomSpeciesColumns[name]; !ok {
			t.Fatalf("expected capture_mushroom_species.%s to exist", name)
		}
	}

	geoColumns := tableColumnSet(t, rawDB, "capture_geo_index")
	for _, name := range []string{
		"capture_id",
		"country_code",
		"kraj_name",
		"kraj_name_norm",
		"okres_name",
		"okres_name_norm",
		"obec_name",
		"obec_name_norm",
		"raw_json",
		"resolved_at",
	} {
		if _, ok := geoColumns[name]; !ok {
			t.Fatalf("expected capture_geo_index.%s to exist", name)
		}
	}
}

func TestListPublicCapturesWithFiltersRespectsGeoPrivacyAndSpeciesSearch(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	database := &DB{rawDB}
	author, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "filter-author",
		PreferredUsername: "filtrautor",
		Email:             "filter-author@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "filter-author-access",
		RefreshToken: "filter-author-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	makeCapture := func(id string, lat, lon float64, capturedAt time.Time) *models.Capture {
		return &models.Capture{
			ID:                id,
			UserID:            author.ID,
			OriginalFileName:  id + ".jpg",
			ContentType:       "image/jpeg",
			SizeBytes:         2048,
			Width:             1400,
			Height:            900,
			CapturedAt:        capturedAt,
			UploadedAt:        capturedAt,
			Latitude:          &lat,
			Longitude:         &lon,
			Status:            "private",
			PrivateStorageKey: "captures/private/" + id + ".jpg",
		}
	}

	insertPublished := func(capture *models.Capture, coordinatesFree bool, latinName, czechName string, probability float64, krajName, okresName, obecName string) {
		t.Helper()

		if err := database.CreateCapture(capture); err != nil {
			t.Fatalf("create capture %s: %v", capture.ID, err)
		}

		analysis := &models.CaptureMushroomAnalysis{
			CaptureID:                capture.ID,
			HasMushrooms:             true,
			PrimaryLatinName:         latinName,
			PrimaryCzechOfficialName: czechName,
			PrimaryProbability:       probability,
			ModelCode:                "gemini-2.5-flash",
			RawJSON:                  "{}",
			AnalyzedAt:               capture.CapturedAt.Add(2 * time.Minute),
		}
		species := []*models.CaptureMushroomSpecies{
			{
				CaptureID:         capture.ID,
				LatinName:         latinName,
				CzechOfficialName: czechName,
				Probability:       probability,
			},
		}
		geo := &models.CaptureGeoIndex{
			CaptureID:   capture.ID,
			CountryCode: "CZ",
			KrajName:    krajName,
			OkresName:   okresName,
			ObecName:    obecName,
			RawJSON:     "{}",
			ResolvedAt:  capture.CapturedAt.Add(time.Minute),
		}

		if err := database.FinalizeCapturePublicationApproved(
			capture.ID,
			author.ID,
			"captures/public/"+capture.ID+".jpg",
			analysis,
			species,
			geo,
		); err != nil {
			t.Fatalf("finalize capture %s: %v", capture.ID, err)
		}

		if err := database.SetCaptureCoordinatesFree(capture.ID, author.ID, coordinatesFree); err != nil {
			t.Fatalf("set coordinates_free for %s: %v", capture.ID, err)
		}
	}

	paidMoravia := makeCapture("capture-paid-moravia", 49.1001, 16.7001, time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC))
	freeMoravia := makeCapture("capture-free-moravia", 49.2002, 16.8002, time.Date(2026, time.March, 15, 9, 0, 0, 0, time.UTC))
	freePraha := makeCapture("capture-free-praha", 50.0875, 14.4213, time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC))

	insertPublished(paidMoravia, false, "Boletus edulis", "hřib smrkový", 0.96, "Jihomoravský kraj", "Brno-venkov", "Rosice")
	insertPublished(freeMoravia, true, "Boletus reticulatus", "hřib dubový", 0.88, "Jihomoravský kraj", "Brno-venkov", "Říčany")
	insertPublished(freePraha, true, "Amanita muscaria", "muchomůrka červená", 0.72, "Hlavní město Praha", "Praha", "Praha")

	speciesMatches, err := database.ListPublicCapturesWithFilters(PublicCaptureFilters{
		Limit:        10,
		SpeciesQuery: "hrib",
	}, 0)
	if err != nil {
		t.Fatalf("list captures by species: %v", err)
	}
	if len(speciesMatches) != 2 {
		t.Fatalf("expected 2 captures for species search, got %d", len(speciesMatches))
	}
	speciesTotal, err := database.CountPublicCapturesWithFilters(PublicCaptureFilters{
		Limit:        1,
		SpeciesQuery: "hrib",
	}, 0)
	if err != nil {
		t.Fatalf("count captures by species: %v", err)
	}
	if speciesTotal != 2 {
		t.Fatalf("expected species search total 2, got %d", speciesTotal)
	}

	byID := make(map[string]*models.Capture, len(speciesMatches))
	for _, capture := range speciesMatches {
		byID[capture.ID] = capture
	}
	if byID[paidMoravia.ID] == nil || byID[freeMoravia.ID] == nil {
		t.Fatalf("expected both boletus captures in species results, got %+v", byID)
	}
	if byID[paidMoravia.ID].KrajName != "Jihomoravský kraj" {
		t.Fatalf("expected paid capture to keep kraj visible, got %+v", byID[paidMoravia.ID])
	}
	if byID[paidMoravia.ID].OkresName != "" || byID[paidMoravia.ID].ObecName != "" {
		t.Fatalf("expected paid capture to hide lower geo levels, got %+v", byID[paidMoravia.ID])
	}
	if byID[paidMoravia.ID].Latitude != nil || byID[paidMoravia.ID].Longitude != nil || !byID[paidMoravia.ID].CoordinatesLocked {
		t.Fatalf("expected paid capture coordinates to stay hidden for guests, got %+v", byID[paidMoravia.ID])
	}
	if byID[freeMoravia.ID].OkresName != "Brno-venkov" || byID[freeMoravia.ID].ObecName != "Říčany" {
		t.Fatalf("expected free capture to expose lower geo levels, got %+v", byID[freeMoravia.ID])
	}

	krajMatches, err := database.ListPublicCapturesWithFilters(PublicCaptureFilters{
		Limit:     10,
		KrajQuery: "jihomoravsky",
	}, 0)
	if err != nil {
		t.Fatalf("list captures by kraj: %v", err)
	}
	if len(krajMatches) != 2 {
		t.Fatalf("expected 2 captures for kraj search, got %d", len(krajMatches))
	}

	okresMatches, err := database.ListPublicCapturesWithFilters(PublicCaptureFilters{
		Limit:      10,
		OkresQuery: "brno",
	}, 0)
	if err != nil {
		t.Fatalf("list captures by okres: %v", err)
	}
	if len(okresMatches) != 1 || okresMatches[0].ID != freeMoravia.ID {
		t.Fatalf("expected only free moravia capture for okres search, got %+v", okresMatches)
	}

	obecMatches, err := database.ListPublicCapturesWithFilters(PublicCaptureFilters{
		Limit:     10,
		ObecQuery: "ricany",
	}, 0)
	if err != nil {
		t.Fatalf("list captures by obec: %v", err)
	}
	if len(obecMatches) != 1 || obecMatches[0].ID != freeMoravia.ID {
		t.Fatalf("expected only free moravia capture for obec search, got %+v", obecMatches)
	}

	sortedByProbability, err := database.ListPublicCapturesWithFilters(PublicCaptureFilters{
		Limit: 10,
		Sort:  "probability_desc",
	}, 0)
	if err != nil {
		t.Fatalf("list captures by probability: %v", err)
	}
	if len(sortedByProbability) != 3 {
		t.Fatalf("expected 3 captures for sorted search, got %d", len(sortedByProbability))
	}
	if sortedByProbability[0].ID != paidMoravia.ID || sortedByProbability[1].ID != freeMoravia.ID || sortedByProbability[2].ID != freePraha.ID {
		t.Fatalf("unexpected probability ordering: %+v", []string{sortedByProbability[0].ID, sortedByProbability[1].ID, sortedByProbability[2].ID})
	}

	mapMatches, err := database.ListPublicMapCapturesWithFilters(PublicCaptureFilters{
		Limit: 10,
	}, 0)
	if err != nil {
		t.Fatalf("list public map captures: %v", err)
	}
	if len(mapMatches) != 2 {
		t.Fatalf("expected 2 visible map captures for guest, got %d", len(mapMatches))
	}
	mapTotal, err := database.CountPublicMapCapturesWithFilters(PublicCaptureFilters{
		Limit: 1,
	}, 0)
	if err != nil {
		t.Fatalf("count public map captures: %v", err)
	}
	if mapTotal != 2 {
		t.Fatalf("expected visible map total 2 for guest, got %d", mapTotal)
	}
	if mapMatches[0].ID != freePraha.ID || mapMatches[1].ID != freeMoravia.ID {
		t.Fatalf("unexpected guest map captures ordering: %+v", []string{mapMatches[0].ID, mapMatches[1].ID})
	}
	for _, capture := range mapMatches {
		if capture.Latitude == nil || capture.Longitude == nil || capture.CoordinatesLocked {
			t.Fatalf("expected visible coordinates in map captures, got %+v", capture)
		}
	}

	mapSpeciesMatches, err := database.ListPublicMapCapturesWithFilters(PublicCaptureFilters{
		Limit:        10,
		SpeciesQuery: "hrib",
	}, 0)
	if err != nil {
		t.Fatalf("list public map captures by species: %v", err)
	}
	if len(mapSpeciesMatches) != 1 || mapSpeciesMatches[0].ID != freeMoravia.ID {
		t.Fatalf("expected only free moravia capture on guest map species search, got %+v", mapSpeciesMatches)
	}
}

func TestModerationHidesPublicContentAndBannedProfiles(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	database := &DB{rawDB}
	moderator, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "moderation-root",
		PreferredUsername: "houbamzdar",
		Email:             "houbamzdar@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "moderation-root-access",
		RefreshToken: "moderation-root-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create moderator: %v", err)
	}

	author, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "moderation-author",
		PreferredUsername: "lesni_autor",
		Email:             "lesni-autor@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "moderation-author-access",
		RefreshToken: "moderation-author-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	commenter, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "moderation-commenter",
		PreferredUsername: "komentator",
		Email:             "komentator@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "moderation-commenter-access",
		RefreshToken: "moderation-commenter-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create commenter: %v", err)
	}

	capturedAt := time.Date(2026, time.March, 16, 10, 0, 0, 0, time.UTC)
	lat := 49.1234
	lon := 16.5432
	capture := &models.Capture{
		ID:                "moderation-capture-001",
		UserID:            author.ID,
		OriginalFileName:  "moderation-capture.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         4096,
		Width:             1600,
		Height:            1200,
		CapturedAt:        capturedAt,
		UploadedAt:        capturedAt,
		Latitude:          &lat,
		Longitude:         &lon,
		Status:            "published",
		PrivateStorageKey: "captures/private/moderation-capture-001.jpg",
		PublicStorageKey:  "captures/public/moderation-capture-001.jpg",
		PublishedAt:       capturedAt.Add(2 * time.Minute),
	}
	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create capture: %v", err)
	}

	post := &models.Post{
		ID:        "moderation-post-001",
		UserID:    author.ID,
		Content:   "Veřejný post před moderací",
		Status:    "published",
		CreatedAt: capturedAt.Add(3 * time.Minute),
		UpdatedAt: capturedAt.Add(3 * time.Minute),
		Captures:  []*models.Capture{capture},
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	comment := &models.Comment{
		ID:        "moderation-comment-001",
		PostID:    post.ID,
		UserID:    commenter.ID,
		Content:   "Tohle je potřeba zkontrolovat.",
		CreatedAt: capturedAt.Add(4 * time.Minute),
		UpdatedAt: capturedAt.Add(4 * time.Minute),
	}
	if err := database.CreatePostComment(comment); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	publicPosts, err := database.ListPublicPosts(10, 0, 0)
	if err != nil {
		t.Fatalf("list public posts before moderation: %v", err)
	}
	if len(publicPosts) != 1 || len(publicPosts[0].Comments) != 1 || len(publicPosts[0].Captures) != 1 {
		t.Fatalf("unexpected public post before moderation: %+v", publicPosts)
	}

	if err := database.SetCommentModeratorHidden(comment.ID, moderator.ID, true, "manual_review", "hide comment"); err != nil {
		t.Fatalf("hide comment: %v", err)
	}

	publicPosts, err = database.ListPublicPosts(10, 0, 0)
	if err != nil {
		t.Fatalf("list public posts after comment moderation: %v", err)
	}
	if len(publicPosts) != 1 || len(publicPosts[0].Comments) != 0 {
		t.Fatalf("expected moderated comment to disappear from public feed, got %+v", publicPosts)
	}

	ownerPost, err := database.GetPost(post.ID, author.ID)
	if err != nil {
		t.Fatalf("get owner post after comment moderation: %v", err)
	}
	if len(ownerPost.Comments) != 1 || !ownerPost.Comments[0].ModeratorHidden {
		t.Fatalf("expected owner view to keep moderated comment metadata, got %+v", ownerPost.Comments)
	}

	if err := database.SetCaptureModeratorHidden(capture.ID, moderator.ID, true, "manual_review", "hide capture"); err != nil {
		t.Fatalf("hide capture: %v", err)
	}

	publicCaptures, err := database.ListPublicCapturesWithFilters(PublicCaptureFilters{Limit: 10}, 0)
	if err != nil {
		t.Fatalf("list public captures after moderation: %v", err)
	}
	if len(publicCaptures) != 0 {
		t.Fatalf("expected moderated capture to disappear from public captures, got %+v", publicCaptures)
	}
	if totalCaptures, err := database.CountPublicCapturesByUser(author.ID); err != nil {
		t.Fatalf("count public captures after moderation: %v", err)
	} else if totalCaptures != 0 {
		t.Fatalf("expected 0 public captures after moderation, got %d", totalCaptures)
	}

	publicPosts, err = database.ListPublicPosts(10, 0, 0)
	if err != nil {
		t.Fatalf("list public posts after capture moderation: %v", err)
	}
	if len(publicPosts) != 1 || len(publicPosts[0].Captures) != 0 {
		t.Fatalf("expected moderated capture to disappear from public post, got %+v", publicPosts)
	}

	if err := database.SetPostModeratorHidden(post.ID, moderator.ID, true, "manual_review", "hide post"); err != nil {
		t.Fatalf("hide post: %v", err)
	}

	publicPosts, err = database.ListPublicPosts(10, 0, 0)
	if err != nil {
		t.Fatalf("list public posts after post moderation: %v", err)
	}
	if len(publicPosts) != 0 {
		t.Fatalf("expected moderated post to disappear from public feed, got %+v", publicPosts)
	}
	exists, err := database.PublicPostExists(post.ID)
	if err != nil {
		t.Fatalf("public post exists after moderation: %v", err)
	}
	if exists {
		t.Fatalf("expected moderated post to be treated as non-public")
	}

	profile, err := database.GetPublicUserProfile(author.ID)
	if err != nil {
		t.Fatalf("get public profile before ban: %v", err)
	}
	if profile.PublicPostsCount != 0 || profile.PublicCapturesCount != 0 {
		t.Fatalf("expected moderated content to be removed from public counts, got %+v", profile)
	}

	if err := database.SetUserRestrictions(author.ID, moderator.ID, time.Now().UTC().Add(2*time.Hour), time.Time{}, time.Time{}, "ban", "temporary ban"); err != nil {
		t.Fatalf("ban author: %v", err)
	}

	if _, err := database.GetPublicUserProfile(author.ID); err != sql.ErrNoRows {
		t.Fatalf("expected banned user profile to disappear, got %v", err)
	}
}
