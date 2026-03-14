package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/models"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"golang.org/x/oauth2"
)

type DB struct {
	*sql.DB
}

type CaptureListFilters struct {
	Status       string
	CapturedFrom *time.Time
	CapturedTo   *time.Time
	Page         int
	PageSize     int
}

type CaptureListPage struct {
	Captures   []*models.Capture
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

const migrationUsersTokenColumnsID = "20260308_add_user_token_columns"
const migrationPhotoCapturesTableID = "20260308_create_photo_captures"
const migrationPostsTableID = "20260312_create_posts_table"
const migrationPostCommentsTableID = "20260314_create_post_comments_table"
const migrationPostLikesTableID = "20260314_create_post_likes_table"
const migrationAccrualTransactionsTableID = "20260314_create_accrual_transactions_table"

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
		{
			id: migrationPostsTableID,
			apply: func(tx *sql.Tx) error {
				queries := []string{
					`CREATE TABLE IF NOT EXISTS posts (
						id TEXT PRIMARY KEY,
						user_id INTEGER NOT NULL,
						content TEXT NOT NULL,
						status TEXT NOT NULL DEFAULT 'published',
						created_at TEXT NOT NULL,
						updated_at TEXT NOT NULL,
						FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
					);`,
					`CREATE INDEX IF NOT EXISTS idx_posts_user_created ON posts(user_id, created_at DESC);`,
					`CREATE TABLE IF NOT EXISTS post_captures (
						post_id TEXT NOT NULL,
						capture_id TEXT NOT NULL,
						display_order INTEGER NOT NULL DEFAULT 0,
						PRIMARY KEY(post_id, capture_id),
						FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE,
						FOREIGN KEY(capture_id) REFERENCES photo_captures(id) ON DELETE CASCADE
					);`,
					`CREATE INDEX IF NOT EXISTS idx_post_captures_post_id ON post_captures(post_id);`,
				}
				for _, query := range queries {
					if _, err := tx.Exec(query); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			id: migrationPostCommentsTableID,
			apply: func(tx *sql.Tx) error {
				queries := []string{
					`CREATE TABLE IF NOT EXISTS post_comments (
						id TEXT PRIMARY KEY,
						post_id TEXT NOT NULL,
						user_id INTEGER NOT NULL,
						content TEXT NOT NULL,
						created_at TEXT NOT NULL,
						updated_at TEXT NOT NULL,
						FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE,
						FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
					);`,
					`CREATE INDEX IF NOT EXISTS idx_post_comments_post_created ON post_comments(post_id, created_at ASC);`,
					`CREATE INDEX IF NOT EXISTS idx_post_comments_user_created ON post_comments(user_id, created_at DESC);`,
				}
				for _, query := range queries {
					if _, err := tx.Exec(query); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			id: migrationPostLikesTableID,
			apply: func(tx *sql.Tx) error {
				queries := []string{
					`CREATE TABLE IF NOT EXISTS post_likes (
						post_id TEXT NOT NULL,
						user_id INTEGER NOT NULL,
						created_at TEXT NOT NULL DEFAULT (datetime('now')),
						PRIMARY KEY(post_id, user_id),
						FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE,
						FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
					);`,
					`CREATE INDEX IF NOT EXISTS idx_post_likes_post_id ON post_likes(post_id);`,
					`CREATE INDEX IF NOT EXISTS idx_post_likes_user_id ON post_likes(user_id);`,
				}
				for _, query := range queries {
					if _, err := tx.Exec(query); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			id: migrationAccrualTransactionsTableID,
			apply: func(tx *sql.Tx) error {
				queries := []string{
					`CREATE TABLE IF NOT EXISTS accrual_transactions (
						id TEXT PRIMARY KEY,
						user_id INTEGER NOT NULL,
						amount INTEGER NOT NULL,
						reason TEXT NOT NULL,
						idempotency_key TEXT,
						created_at TEXT NOT NULL DEFAULT (datetime('now')),
						FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
					);`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_accrual_transactions_idempotency_key ON accrual_transactions(idempotency_key) WHERE idempotency_key IS NOT NULL;`,
					`CREATE INDEX IF NOT EXISTS idx_accrual_transactions_user_created ON accrual_transactions(user_id, created_at DESC);`,
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

func (db *DB) AddAuthBonuses(userID int64, isNew bool, emailVerified bool, phoneVerified bool, emailBonusKey string, phoneBonusKey string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if isNew {
		if err := insertAccrualTx(tx, userID, 1, "signup", ""); err != nil {
			return err
		}
	}

	if emailVerified {
		exists, err := accrualTxExists(tx, emailBonusKey)
		if err != nil {
			return err
		}
		if !exists {
			if err := insertAccrualTx(tx, userID, 3, "email_verified_bonus", emailBonusKey); err != nil {
				return err
			}
		}
	}

	if phoneVerified {
		exists, err := accrualTxExists(tx, phoneBonusKey)
		if err != nil {
			return err
		}
		if !exists {
			if err := insertAccrualTx(tx, userID, 5, "phone_verified_bonus", phoneBonusKey); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func accrualTxExists(tx *sql.Tx, idempotencyKey string) (bool, error) {
	var exists bool
	err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM accrual_transactions WHERE idempotency_key = ?)", idempotencyKey).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func insertAccrualTx(tx *sql.Tx, userID int64, amount int, reason string, idempotencyKey string) error {
	_, err := tx.Exec(`
		INSERT INTO accrual_transactions (id, user_id, amount, reason, idempotency_key, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, uuid.New().String(), userID, amount, reason, nullIfEmpty(idempotencyKey), time.Now().UTC().Format(time.RFC3339))
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
	result, err := db.ListCapturesPage(userID, CaptureListFilters{
		Page:     1,
		PageSize: 1000,
	})
	if err != nil {
		return nil, err
	}
	return result.Captures, nil
}

func (db *DB) ListCapturesPage(userID int64, filters CaptureListFilters) (*CaptureListPage, error) {
	filters = normalizeCaptureListFilters(filters)

	whereClauses := []string{"user_id = ?"}
	args := []interface{}{userID}

	if filters.Status != "" {
		whereClauses = append(whereClauses, "status = ?")
		args = append(args, filters.Status)
	}
	if filters.CapturedFrom != nil {
		whereClauses = append(whereClauses, "captured_at >= ?")
		args = append(args, filters.CapturedFrom.UTC().Format(time.RFC3339))
	}
	if filters.CapturedTo != nil {
		whereClauses = append(whereClauses, "captured_at < ?")
		args = append(args, filters.CapturedTo.UTC().Format(time.RFC3339))
	}

	whereSQL := " WHERE " + strings.Join(whereClauses, " AND ")

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM photo_captures`+whereSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	offset := (filters.Page - 1) * filters.PageSize
	queryArgs := append(append([]interface{}{}, args...), filters.PageSize, offset)
	rows, err := db.Query(
		captureSelectColumns+whereSQL+` ORDER BY captured_at DESC LIMIT ? OFFSET ?`,
		queryArgs...,
	)
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

	totalPages := 0
	if total > 0 {
		totalPages = (total + filters.PageSize - 1) / filters.PageSize
	}

	return &CaptureListPage{
		Captures:   captures,
		Total:      total,
		Page:       filters.Page,
		PageSize:   filters.PageSize,
		TotalPages: totalPages,
	}, nil
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
		capture          models.Capture
		clientLocalID    sql.NullString
		capturedAtRaw    string
		uploadedAtRaw    string
		latitude         sql.NullFloat64
		longitude        sql.NullFloat64
		accuracyMeters   sql.NullFloat64
		publicStorageKey sql.NullString
		publishedAtRaw   sql.NullString
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

func normalizeCaptureListFilters(filters CaptureListFilters) CaptureListFilters {
	if filters.Page <= 0 {
		filters.Page = 1
	}
	if filters.PageSize <= 0 {
		filters.PageSize = 12
	}
	if filters.PageSize > 100 {
		filters.PageSize = 100
	}
	if filters.Status != "private" && filters.Status != "published" {
		filters.Status = ""
	}
	return filters
}

func (db *DB) CreatePost(post *models.Post) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO posts (id, user_id, content, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, post.ID, post.UserID, post.Content, post.Status, post.CreatedAt.Format(time.RFC3339), post.UpdatedAt.Format(time.RFC3339))
	if err != nil {
		return err
	}

	for i, capture := range post.Captures {
		_, err = tx.Exec(`
			INSERT INTO post_captures (post_id, capture_id, display_order)
			VALUES (?, ?, ?)
		`, post.ID, capture.ID, i)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *DB) GetPost(postID string, userID int64) (*models.Post, error) {
	var p models.Post
	var createdAt, updatedAt string
	err := db.QueryRow(`
		SELECT id, user_id, content, status, created_at, updated_at,
               (SELECT COUNT(*) FROM post_likes WHERE post_id = posts.id) as likes_count,
               (SELECT EXISTS(SELECT 1 FROM post_likes WHERE post_id = posts.id AND user_id = ?)) as is_liked_by_me
		FROM posts
		WHERE id = ? AND user_id = ?
	`, userID, postID, userID).Scan(&p.ID, &p.UserID, &p.Content, &p.Status, &createdAt, &updatedAt, &p.LikesCount, &p.IsLikedByMe)
	if err != nil {
		return nil, err
	}

	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	captures, err := db.getCapturesForPost(p.ID)
	if err != nil {
		return nil, err
	}
	p.Captures = captures

	comments, err := db.getCommentsForPost(p.ID)
	if err != nil {
		return nil, err
	}
	p.Comments = comments

	return &p, nil
}

func (db *DB) UpdatePost(post *models.Post) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE posts SET content = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`, post.Content, time.Now().UTC().Format(time.RFC3339), post.ID, post.UserID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`DELETE FROM post_captures WHERE post_id = ?`, post.ID)
	if err != nil {
		return err
	}

	for i, capture := range post.Captures {
		_, err = tx.Exec(`
			INSERT INTO post_captures (post_id, capture_id, display_order)
			VALUES (?, ?, ?)
		`, post.ID, capture.ID, i)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *DB) DeletePost(postID string, userID int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM post_comments WHERE post_id = ?`, postID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM post_likes WHERE post_id = ?`, postID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM post_captures WHERE post_id = ?`, postID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM posts WHERE id = ? AND user_id = ?`, postID, userID); err != nil {
		return err
	}

	return tx.Commit()
}

func (db *DB) PublicPostExists(postID string) (bool, error) {
	var exists int
	err := db.QueryRow(`
		SELECT 1
		FROM posts
		WHERE id = ? AND status = 'published'
		LIMIT 1
	`, postID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (db *DB) CreatePostComment(comment *models.Comment) error {
	_, err := db.Exec(`
		INSERT INTO post_comments (id, post_id, user_id, content, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		comment.ID,
		comment.PostID,
		comment.UserID,
		comment.Content,
		comment.CreatedAt.UTC().Format(time.RFC3339),
		comment.UpdatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (db *DB) ListPosts(userID int64, limit, offset int) ([]*models.Post, error) {
	rows, err := db.Query(`
		SELECT id, user_id, content, status, created_at, updated_at,
               (SELECT COUNT(*) FROM post_likes WHERE post_id = posts.id) as likes_count,
               (SELECT EXISTS(SELECT 1 FROM post_likes WHERE post_id = posts.id AND user_id = ?)) as is_liked_by_me
		FROM posts
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, userID, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []*models.Post
	for rows.Next() {
		var p models.Post
		var createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.UserID, &p.Content, &p.Status, &createdAt, &updatedAt, &p.LikesCount, &p.IsLikedByMe); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		posts = append(posts, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, p := range posts {
		captures, err := db.getCapturesForPost(p.ID)
		if err != nil {
			return nil, err
		}
		p.Captures = captures

		comments, err := db.getCommentsForPost(p.ID)
		if err != nil {
			return nil, err
		}
		p.Comments = comments
	}

	return posts, nil
}

func (db *DB) ListPublicPosts(limit, offset int, currentUserID int64) ([]*models.Post, error) {
	rows, err := db.Query(`
		SELECT p.id, p.user_id, u.preferred_username, COALESCE(u.picture, ''), p.content, p.status, p.created_at, p.updated_at,
               (SELECT COUNT(*) FROM post_likes WHERE post_id = p.id) as likes_count,
               (SELECT EXISTS(SELECT 1 FROM post_likes WHERE post_id = p.id AND user_id = ?)) as is_liked_by_me
		FROM posts p
		JOIN users u ON p.user_id = u.id
		WHERE p.status = 'published'
		ORDER BY p.created_at DESC
		LIMIT ? OFFSET ?
	`, currentUserID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []*models.Post
	for rows.Next() {
		var p models.Post
		var createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.UserID, &p.AuthorName, &p.AuthorAvatar, &p.Content, &p.Status, &createdAt, &updatedAt, &p.LikesCount, &p.IsLikedByMe); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		posts = append(posts, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, p := range posts {
		captures, err := db.getCapturesForPost(p.ID)
		if err != nil {
			return nil, err
		}
		p.Captures = captures

		comments, err := db.getCommentsForPost(p.ID)
		if err != nil {
			return nil, err
		}
		p.Comments = comments
	}

	return posts, nil
}

func (db *DB) ListPublicCaptures(limit, offset int) ([]*models.Capture, error) {
	rows, err := db.Query(`
		SELECT c.id, c.user_id, u.preferred_username, COALESCE(u.picture, ''), COALESCE(c.client_local_id, ''), c.original_file_name, c.content_type, c.size_bytes, c.width, c.height,
			c.captured_at, c.uploaded_at, c.latitude, c.longitude, c.accuracy_meters, c.status, c.private_storage_key,
			COALESCE(c.public_storage_key, ''), COALESCE(c.published_at, '')
		FROM photo_captures c
		JOIN users u ON c.user_id = u.id
		WHERE c.status = 'published' AND c.public_storage_key IS NOT NULL AND c.public_storage_key != ''
		ORDER BY c.published_at DESC, c.captured_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var captures []*models.Capture
	for rows.Next() {
		var c models.Capture
		var capturedAt, uploadedAt, publishedAt string
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.AuthorName, &c.AuthorAvatar, &c.ClientLocalID, &c.OriginalFileName, &c.ContentType, &c.SizeBytes, &c.Width, &c.Height,
			&capturedAt, &uploadedAt, &c.Latitude, &c.Longitude, &c.AccuracyMeters, &c.Status, &c.PrivateStorageKey,
			&c.PublicStorageKey, &publishedAt,
		); err != nil {
			return nil, err
		}
		c.CapturedAt, _ = time.Parse(time.RFC3339, capturedAt)
		c.UploadedAt, _ = time.Parse(time.RFC3339, uploadedAt)
		if publishedAt != "" {
			c.PublishedAt, _ = time.Parse(time.RFC3339, publishedAt)
		}
		captures = append(captures, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return captures, nil
}

func (db *DB) getCapturesForPost(postID string) ([]*models.Capture, error) {
	cRows, err := db.Query(`
		SELECT c.id, c.user_id, COALESCE(c.client_local_id, ''), c.original_file_name, c.content_type, c.size_bytes, c.width, c.height,
			c.captured_at, c.uploaded_at, c.latitude, c.longitude, c.accuracy_meters, c.status, c.private_storage_key,
			COALESCE(c.public_storage_key, ''), COALESCE(c.published_at, '')
		FROM photo_captures c
		JOIN post_captures pc ON c.id = pc.capture_id
		WHERE pc.post_id = ?
		ORDER BY pc.display_order ASC
	`, postID)
	if err != nil {
		return nil, err
	}
	defer cRows.Close()

	var captures []*models.Capture
	for cRows.Next() {
		var c models.Capture
		var capturedAt, uploadedAt, publishedAt string
		if err := cRows.Scan(
			&c.ID, &c.UserID, &c.ClientLocalID, &c.OriginalFileName, &c.ContentType, &c.SizeBytes, &c.Width, &c.Height,
			&capturedAt, &uploadedAt, &c.Latitude, &c.Longitude, &c.AccuracyMeters, &c.Status, &c.PrivateStorageKey,
			&c.PublicStorageKey, &publishedAt,
		); err != nil {
			return nil, err
		}
		c.CapturedAt, _ = time.Parse(time.RFC3339, capturedAt)
		c.UploadedAt, _ = time.Parse(time.RFC3339, uploadedAt)
		if publishedAt != "" {
			c.PublishedAt, _ = time.Parse(time.RFC3339, publishedAt)
		}
		captures = append(captures, &c)
	}
	return captures, nil
}

func (db *DB) getCommentsForPost(postID string) ([]*models.Comment, error) {
	rows, err := db.Query(`
		SELECT pc.id, pc.post_id, pc.user_id, COALESCE(u.preferred_username, ''), COALESCE(u.picture, ''), pc.content, pc.created_at, pc.updated_at
		FROM post_comments pc
		JOIN users u ON pc.user_id = u.id
		WHERE pc.post_id = ?
		ORDER BY pc.created_at ASC, pc.id ASC
	`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []*models.Comment
	for rows.Next() {
		var comment models.Comment
		var createdAt string
		var updatedAt string
		if err := rows.Scan(
			&comment.ID,
			&comment.PostID,
			&comment.UserID,
			&comment.AuthorName,
			&comment.AuthorAvatar,
			&comment.Content,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		comment.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		comment.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		comments = append(comments, &comment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return comments, nil
}

func (db *DB) TogglePostLike(postID string, userID int64) (newLikesCount int, isLiked bool, err error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()

	var publishedPostID string
	err = tx.QueryRow(`SELECT id FROM posts WHERE id = ? AND status = 'published' LIMIT 1`, postID).Scan(&publishedPostID)
	if err != nil {
		return 0, false, err
	}

	var exists int
	err = tx.QueryRow(`SELECT 1 FROM post_likes WHERE post_id = ? AND user_id = ?`, postID, userID).Scan(&exists)
	if err == sql.ErrNoRows {
		// Like it
		if _, err := tx.Exec(`INSERT INTO post_likes (post_id, user_id) VALUES (?, ?)`, postID, userID); err != nil {
			return 0, false, err
		}
		isLiked = true
	} else if err != nil {
		return 0, false, err
	} else {
		// Unlike it
		if _, err := tx.Exec(`DELETE FROM post_likes WHERE post_id = ? AND user_id = ?`, postID, userID); err != nil {
			return 0, false, err
		}
		isLiked = false
	}

	err = tx.QueryRow(`SELECT COUNT(*) FROM post_likes WHERE post_id = ?`, postID).Scan(&newLikesCount)
	if err != nil {
		return 0, false, err
	}

	if err := tx.Commit(); err != nil {
		return 0, false, err
	}

	return newLikesCount, isLiked, nil
}
