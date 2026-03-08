package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/models"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"golang.org/x/oauth2"
)

type DB struct {
	*sql.DB
}

const migrationUsersTokenColumnsID = "20260308_add_user_token_columns"
const migrationPhotoCapturesTableID = "20260308_create_photo_captures"

func New(cfg *config.Config) (*DB, error) {
	url := cfg.DBURL
	if cfg.DBToken != "" {
		url = fmt.Sprintf("%s?authToken=%s", cfg.DBURL, cfg.DBToken)
	}

	db, err := sql.Open("libsql", url)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	if err := migrate(db); err != nil {
		return nil, err
	}

	return &DB{db}, nil
}

func migrate(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
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
			access_token TEXT,
			refresh_token TEXT,
			token_expires_at TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			last_idp_sync_at TEXT,
			UNIQUE(idp_issuer, idp_sub)
		);`,
		`CREATE TABLE IF NOT EXISTS sessions (
			session_id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			expires_at TEXT NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS oidc_login_state (
			state TEXT PRIMARY KEY,
			nonce TEXT NOT NULL,
			pkce_verifier TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			expires_at TEXT NOT NULL,
			post_login_redirect TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			id TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		);`,
	}

	for _, q := range queries {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}

	migrations := []struct {
		id    string
		apply func(*sql.Tx) error
	}{
		{
			id: migrationUsersTokenColumnsID,
			apply: func(tx *sql.Tx) error {
				if err := ensureColumnExists(tx, "users", "access_token", "TEXT"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "users", "refresh_token", "TEXT"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "users", "token_expires_at", "TEXT"); err != nil {
					return err
				}
				return nil
			},
		},
		{
			id: migrationPhotoCapturesTableID,
			apply: func(tx *sql.Tx) error {
				queries := []string{
					`CREATE TABLE IF NOT EXISTS photo_captures (
						id TEXT PRIMARY KEY,
						user_id INTEGER NOT NULL,
						client_local_id TEXT,
						original_file_name TEXT NOT NULL,
						content_type TEXT NOT NULL,
						size_bytes INTEGER NOT NULL,
						width INTEGER NOT NULL DEFAULT 0,
						height INTEGER NOT NULL DEFAULT 0,
						captured_at TEXT NOT NULL,
						uploaded_at TEXT NOT NULL DEFAULT (datetime('now')),
						latitude REAL,
						longitude REAL,
						accuracy_meters REAL,
						status TEXT NOT NULL DEFAULT 'private',
						private_storage_key TEXT NOT NULL,
						public_storage_key TEXT,
						published_at TEXT,
						FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
					);`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_photo_captures_private_storage_key ON photo_captures(private_storage_key);`,
					`CREATE INDEX IF NOT EXISTS idx_photo_captures_user_captured_at ON photo_captures(user_id, captured_at DESC);`,
					`CREATE INDEX IF NOT EXISTS idx_photo_captures_status ON photo_captures(status);`,
				}

				for _, query := range queries {
					if _, err := tx.Exec(query); err != nil {
						return err
					}
				}
				return nil
			},
		},
	}

	for _, migration := range migrations {
		if err := migration.apply(tx); err != nil {
			return fmt.Errorf("apply migration %s: %w", migration.id, err)
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO schema_migrations (id, applied_at) VALUES (?, datetime('now'))`,
			migration.id,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", migration.id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func ensureColumnExists(tx *sql.Tx, tableName, columnName, columnDef string) error {
	columns, err := tableColumns(tx, tableName)
	if err != nil {
		return err
	}
	if _, ok := columns[columnName]; ok {
		return nil
	}

	query := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, columnName, columnDef)
	if _, err := tx.Exec(query); err != nil {
		return err
	}
	return nil
}

func tableColumns(tx *sql.Tx, tableName string) (map[string]struct{}, error) {
	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return nil, err
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
			return nil, err
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func (db *DB) UpsertUser(claims *models.OIDCClaims, token *oauth2.Token) (*models.User, bool, error) {
	var user models.User
	var exists bool

	err := db.QueryRow("SELECT id, COALESCE(about_me, '') FROM users WHERE idp_issuer = ? AND idp_sub = ?", claims.Iss, claims.Sub).Scan(&user.ID, &user.AboutMe)
	if err == sql.ErrNoRows {
		exists = false
	} else if err != nil {
		return nil, false, err
	} else {
		exists = true
	}

	ev := 0
	if claims.EmailVerified {
		ev = 1
	}
	pv := 0
	if claims.PhoneNumberVerified {
		pv = 1
	}

	expiry := ""
	if !token.Expiry.IsZero() {
		expiry = token.Expiry.Format(time.RFC3339)
	}

	if !exists {
		res, err := db.Exec(`
			INSERT INTO users (idp_issuer, idp_sub, preferred_username, email, email_verified, phone_number, phone_number_verified, picture, access_token, refresh_token, token_expires_at, last_idp_sync_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		`, claims.Iss, claims.Sub, claims.PreferredUsername, claims.Email, ev, claims.PhoneNumber, pv, claims.Picture, token.AccessToken, token.RefreshToken, expiry)
		if err != nil {
			return nil, false, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, false, err
		}
		user.ID = id
	} else {
		_, err := db.Exec(`
			UPDATE users SET
				preferred_username = ?,
				email = ?,
				email_verified = ?,
				phone_number = ?,
				phone_number_verified = ?,
				picture = ?,
				access_token = ?,
				refresh_token = ?,
				token_expires_at = ?,
				last_idp_sync_at = datetime('now'),
				updated_at = datetime('now')
			WHERE id = ?
		`, claims.PreferredUsername, claims.Email, ev, claims.PhoneNumber, pv, claims.Picture, token.AccessToken, token.RefreshToken, expiry, user.ID)
		if err != nil {
			return nil, false, err
		}
	}

	// Fetch updated user
	updated, err := db.GetUser(user.ID)
	return updated, !exists, err
}

func (db *DB) SaveState(state *models.OIDCLoginState) error {
	_, err := db.Exec(`
		INSERT INTO oidc_login_state (state, nonce, pkce_verifier, expires_at, post_login_redirect)
		VALUES (?, ?, ?, ?, ?)
	`, state.State, state.Nonce, state.PKCEVerifier, state.ExpiresAt.Format(time.RFC3339), state.PostLoginRedirect)
	return err
}

func (db *DB) GetAndRemoveState(stateStr string) (*models.OIDCLoginState, error) {
	var state models.OIDCLoginState
	var expiresAt string

	err := db.QueryRow("SELECT state, nonce, pkce_verifier, expires_at, post_login_redirect FROM oidc_login_state WHERE state = ?", stateStr).Scan(
		&state.State, &state.Nonce, &state.PKCEVerifier, &expiresAt, &state.PostLoginRedirect,
	)
	if err != nil {
		return nil, err
	}

	db.Exec("DELETE FROM oidc_login_state WHERE state = ?", stateStr)

	t, _ := time.Parse(time.RFC3339, expiresAt)
	if time.Now().After(t) {
		return nil, fmt.Errorf("state expired")
	}

	return &state, nil
}

func (db *DB) CreateSession(session *models.Session) error {
	_, err := db.Exec(`
		INSERT INTO sessions (session_id, user_id, expires_at)
		VALUES (?, ?, ?)
	`, session.SessionID, session.UserID, session.ExpiresAt.Format(time.RFC3339))
	return err
}

func (db *DB) GetSession(sessionID string) (*models.Session, error) {
	var session models.Session
	var expiresAt string
	err := db.QueryRow("SELECT session_id, user_id, expires_at FROM sessions WHERE session_id = ?", sessionID).Scan(
		&session.SessionID, &session.UserID, &expiresAt,
	)
	if err != nil {
		return nil, err
	}

	t, _ := time.Parse(time.RFC3339, expiresAt)
	if time.Now().After(t) {
		db.DeleteSession(sessionID)
		return nil, sql.ErrNoRows
	}
	session.ExpiresAt = t
	return &session, nil
}

func (db *DB) DeleteSession(sessionID string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE session_id = ?", sessionID)
	return err
}

func (db *DB) GetUser(id int64) (*models.User, error) {
	var user models.User
	var expiresAt sql.NullString
	err := db.QueryRow("SELECT id, idp_issuer, idp_sub, COALESCE(preferred_username, ''), COALESCE(email, ''), email_verified, COALESCE(phone_number, ''), phone_number_verified, COALESCE(picture, ''), COALESCE(about_me, ''), COALESCE(access_token, ''), COALESCE(refresh_token, ''), token_expires_at FROM users WHERE id = ?", id).Scan(
		&user.ID, &user.IDPIssuer, &user.IDPSub, &user.PreferredUsername, &user.Email, &user.EmailVerified, &user.PhoneNumber, &user.PhoneNumberVerified, &user.Picture, &user.AboutMe, &user.AccessToken, &user.RefreshToken, &expiresAt,
	)
	if err == nil && expiresAt.Valid && expiresAt.String != "" {
		user.TokenExpiresAt, _ = time.Parse(time.RFC3339, expiresAt.String)
	}
	return &user, err
}

func (db *DB) UpdateAboutMe(id int64, aboutMe string) error {
	_, err := db.Exec("UPDATE users SET about_me = ?, updated_at = datetime('now') WHERE id = ?", aboutMe, id)
	return err
}

const captureSelectColumns = `
	SELECT id, user_id, COALESCE(client_local_id, ''), original_file_name, content_type, size_bytes, width, height,
		captured_at, uploaded_at, latitude, longitude, accuracy_meters, status, private_storage_key,
		COALESCE(public_storage_key, ''), COALESCE(published_at, '')
	FROM photo_captures
`

func (db *DB) CreateCapture(capture *models.Capture) error {
	_, err := db.Exec(`
		INSERT INTO photo_captures (
			id, user_id, client_local_id, original_file_name, content_type, size_bytes, width, height,
			captured_at, uploaded_at, latitude, longitude, accuracy_meters, status, private_storage_key,
			public_storage_key, published_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		capture.ID,
		capture.UserID,
		nullIfEmpty(capture.ClientLocalID),
		capture.OriginalFileName,
		capture.ContentType,
		capture.SizeBytes,
		capture.Width,
		capture.Height,
		capture.CapturedAt.UTC().Format(time.RFC3339),
		capture.UploadedAt.UTC().Format(time.RFC3339),
		floatPointerValue(capture.Latitude),
		floatPointerValue(capture.Longitude),
		floatPointerValue(capture.AccuracyMeters),
		capture.Status,
		capture.PrivateStorageKey,
		nullIfEmpty(capture.PublicStorageKey),
		nullIfZeroTime(capture.PublishedAt),
	)
	return err
}

func (db *DB) ListCapturesByUser(userID int64) ([]*models.Capture, error) {
	rows, err := db.Query(captureSelectColumns+` WHERE user_id = ? ORDER BY captured_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var captures []*models.Capture
	for rows.Next() {
		capture, err := scanCapture(rows)
		if err != nil {
			return nil, err
		}
		captures = append(captures, capture)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return captures, nil
}

func (db *DB) GetCaptureForUser(id string, userID int64) (*models.Capture, error) {
	row := db.QueryRow(captureSelectColumns+` WHERE id = ? AND user_id = ? LIMIT 1`, id, userID)
	return scanCaptureRow(row)
}

func (db *DB) PublishCapture(id string, userID int64, publicStorageKey string) error {
	_, err := db.Exec(`
		UPDATE photo_captures
		SET status = 'published', public_storage_key = ?, published_at = ?, uploaded_at = uploaded_at
		WHERE id = ? AND user_id = ?
	`, publicStorageKey, time.Now().UTC().Format(time.RFC3339), id, userID)
	return err
}

func (db *DB) UnpublishCapture(id string, userID int64) error {
	_, err := db.Exec(`
		UPDATE photo_captures
		SET status = 'private', public_storage_key = NULL, published_at = NULL
		WHERE id = ? AND user_id = ?
	`, id, userID)
	return err
}

func (db *DB) DeleteCapture(id string, userID int64) error {
	_, err := db.Exec(`DELETE FROM photo_captures WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanCaptureRow(row scanner) (*models.Capture, error) {
	return scanCapture(row)
}

func scanCapture(row scanner) (*models.Capture, error) {
	var (
		capture            models.Capture
		clientLocalID      sql.NullString
		capturedAtRaw      string
		uploadedAtRaw      string
		latitude           sql.NullFloat64
		longitude          sql.NullFloat64
		accuracyMeters     sql.NullFloat64
		publicStorageKey   sql.NullString
		publishedAtRaw     sql.NullString
	)

	if err := row.Scan(
		&capture.ID,
		&capture.UserID,
		&clientLocalID,
		&capture.OriginalFileName,
		&capture.ContentType,
		&capture.SizeBytes,
		&capture.Width,
		&capture.Height,
		&capturedAtRaw,
		&uploadedAtRaw,
		&latitude,
		&longitude,
		&accuracyMeters,
		&capture.Status,
		&capture.PrivateStorageKey,
		&publicStorageKey,
		&publishedAtRaw,
	); err != nil {
		return nil, err
	}

	capture.ClientLocalID = clientLocalID.String
	capture.PublicStorageKey = publicStorageKey.String
	capture.CapturedAt, _ = time.Parse(time.RFC3339, capturedAtRaw)
	capture.UploadedAt, _ = time.Parse(time.RFC3339, uploadedAtRaw)
	if latitude.Valid {
		value := latitude.Float64
		capture.Latitude = &value
	}
	if longitude.Valid {
		value := longitude.Float64
		capture.Longitude = &value
	}
	if accuracyMeters.Valid {
		value := accuracyMeters.Float64
		capture.AccuracyMeters = &value
	}
	if publishedAtRaw.Valid && publishedAtRaw.String != "" {
		capture.PublishedAt, _ = time.Parse(time.RFC3339, publishedAtRaw.String)
	}

	return &capture, nil
}

func nullIfEmpty(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func floatPointerValue(value *float64) interface{} {
	if value == nil {
		return nil
	}
	return *value
}

func nullIfZeroTime(value time.Time) interface{} {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}
