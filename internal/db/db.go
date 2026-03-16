package db

import (
	"database/sql"
	"encoding/json"
	"errors"
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
const migrationCaptureCoordinatesFreeID = "20260315_add_capture_coordinates_free"
const migrationHoubickaLedgerTableID = "20260315_create_houbicka_ledger_tables"
const migrationCaptureCoordinateUnlocksTableID = "20260315_create_capture_coordinate_unlocks_table"
const migrationCapturePublicationReviewColumnsID = "20260316_add_capture_publication_review_columns"
const migrationCaptureMushroomAnalysisTablesID = "20260316_create_capture_mushroom_analysis_tables"
const migrationCaptureGeoIndexTableID = "20260316_create_capture_geo_index_table"
const migrationUsersModeratorColumnID = "20260316_add_users_is_moderator"
const migrationCaptureMushroomAnalysisReviewColumnsID = "20260316_add_capture_mushroom_analysis_review_columns"
const migrationUsersModerationColumnsID = "20260316_add_users_moderation_columns"
const migrationDisableAutoAdminBootstrapID = "20260316_disable_auto_admin_bootstrap"
const migrationContentModerationColumnsID = "20260316_add_content_moderation_columns"
const migrationModerationActionsTableID = "20260316_create_moderation_actions_table"

var ErrInsufficientHoubickaBalance = errors.New("insufficient houbicka balance")
var ErrCaptureHasNoCoordinates = errors.New("capture has no coordinates")

func isModeratorUsername(username string) bool {
	return strings.EqualFold(strings.TrimSpace(username), "houbamzdar")
}

func isAdminUsername(username string) bool {
	return false
}

func moderationNowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func publicUserNotBannedClause(alias string) string {
	return fmt.Sprintf("(COALESCE(%s.banned_until, '') = '' OR %s.banned_until <= ?)", alias, alias)
}

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
			is_moderator INTEGER NOT NULL DEFAULT 0,
			is_admin INTEGER NOT NULL DEFAULT 0,
			email TEXT,
			email_verified INTEGER NOT NULL DEFAULT 0,
			phone_number TEXT,
			phone_number_verified INTEGER NOT NULL DEFAULT 0,
			picture TEXT,
			about_me TEXT,
			banned_until TEXT,
			comments_muted_until TEXT,
			publishing_suspended_until TEXT,
			moderation_note TEXT,
			moderated_by_user_id INTEGER,
			moderated_at TEXT,
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
			id: migrationUsersModeratorColumnID,
			apply: func(tx *sql.Tx) error {
				if err := ensureColumnExists(tx, "users", "is_moderator", "INTEGER NOT NULL DEFAULT 0"); err != nil {
					return err
				}
				_, err := tx.Exec(`
					UPDATE users
					SET is_moderator = CASE
						WHEN lower(COALESCE(preferred_username, '')) = 'houbamzdar' THEN 1
						ELSE 0
					END
				`)
				return err
			},
		},
		{
			id: migrationUsersModerationColumnsID,
			apply: func(tx *sql.Tx) error {
				if err := ensureColumnExists(tx, "users", "is_admin", "INTEGER NOT NULL DEFAULT 0"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "users", "banned_until", "TEXT"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "users", "comments_muted_until", "TEXT"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "users", "publishing_suspended_until", "TEXT"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "users", "moderation_note", "TEXT"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "users", "moderated_by_user_id", "INTEGER"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "users", "moderated_at", "TEXT"); err != nil {
					return err
				}
				_, err := tx.Exec(`
					UPDATE users
					SET is_admin = COALESCE(is_admin, 0)
				`)
				return err
			},
		},
		{
			id: migrationDisableAutoAdminBootstrapID,
			apply: func(tx *sql.Tx) error {
				// Role management is intentionally disabled until a dedicated admin flow exists.
				// Remove the temporary bootstrap admin flag from the hardcoded houbamzdar account.
				_, err := tx.Exec(`
					UPDATE users
					SET is_admin = 0
					WHERE lower(COALESCE(preferred_username, '')) = 'houbamzdar'
				`)
				return err
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
						moderator_hidden INTEGER NOT NULL DEFAULT 0,
						moderation_reason_code TEXT,
						moderated_by_user_id INTEGER,
						moderated_at TEXT,
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
						moderator_hidden INTEGER NOT NULL DEFAULT 0,
						moderation_reason_code TEXT,
						moderated_by_user_id INTEGER,
						moderated_at TEXT,
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
						moderator_hidden INTEGER NOT NULL DEFAULT 0,
						moderation_reason_code TEXT,
						moderated_by_user_id INTEGER,
						moderated_at TEXT,
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
			id: migrationCaptureCoordinatesFreeID,
			apply: func(tx *sql.Tx) error {
				return ensureColumnExists(tx, "photo_captures", "coordinates_free", "INTEGER NOT NULL DEFAULT 0")
			},
		},
		{
			id: migrationCapturePublicationReviewColumnsID,
			apply: func(tx *sql.Tx) error {
				if err := ensureColumnExists(tx, "photo_captures", "publication_review_status", "TEXT NOT NULL DEFAULT 'none'"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "photo_captures", "publication_review_reason_code", "TEXT"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "photo_captures", "publication_review_last_error", "TEXT"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "photo_captures", "publication_review_checked_at", "TEXT"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "photo_captures", "publication_requested_at", "TEXT"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "photo_captures", "publication_review_attempts", "INTEGER NOT NULL DEFAULT 0"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "photo_captures", "publication_review_next_attempt_at", "TEXT"); err != nil {
					return err
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
			id: migrationCaptureMushroomAnalysisTablesID,
			apply: func(tx *sql.Tx) error {
				queries := []string{
					`CREATE TABLE IF NOT EXISTS capture_mushroom_analysis (
						capture_id TEXT PRIMARY KEY,
						has_mushrooms INTEGER NOT NULL DEFAULT 0,
						primary_latin_name TEXT,
						primary_latin_name_norm TEXT,
						primary_czech_name TEXT,
						primary_czech_name_norm TEXT,
						primary_probability REAL NOT NULL DEFAULT 0,
						model_code TEXT,
						review_source TEXT NOT NULL DEFAULT 'automatic_publish_validation',
						reviewed_by_user_id INTEGER,
						reviewed_at TEXT,
						raw_json TEXT,
						analyzed_at TEXT NOT NULL,
						FOREIGN KEY(capture_id) REFERENCES photo_captures(id) ON DELETE CASCADE,
						FOREIGN KEY(reviewed_by_user_id) REFERENCES users(id) ON DELETE SET NULL
					);`,
					`CREATE INDEX IF NOT EXISTS idx_capture_mushroom_analysis_has_mushrooms ON capture_mushroom_analysis(has_mushrooms);`,
					`CREATE INDEX IF NOT EXISTS idx_capture_mushroom_analysis_probability ON capture_mushroom_analysis(primary_probability DESC);`,
					`CREATE TABLE IF NOT EXISTS capture_mushroom_species (
						id INTEGER PRIMARY KEY AUTOINCREMENT,
						capture_id TEXT NOT NULL,
						latin_name TEXT NOT NULL,
						latin_name_norm TEXT NOT NULL,
						czech_official_name TEXT,
						czech_official_name_norm TEXT,
						probability REAL NOT NULL DEFAULT 0,
						created_at TEXT NOT NULL DEFAULT (datetime('now')),
						FOREIGN KEY(capture_id) REFERENCES photo_captures(id) ON DELETE CASCADE
					);`,
					`CREATE INDEX IF NOT EXISTS idx_capture_mushroom_species_capture_id ON capture_mushroom_species(capture_id);`,
					`CREATE INDEX IF NOT EXISTS idx_capture_mushroom_species_latin_norm ON capture_mushroom_species(latin_name_norm);`,
					`CREATE INDEX IF NOT EXISTS idx_capture_mushroom_species_czech_norm ON capture_mushroom_species(czech_official_name_norm);`,
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
			id: migrationCaptureMushroomAnalysisReviewColumnsID,
			apply: func(tx *sql.Tx) error {
				if err := ensureColumnExists(tx, "capture_mushroom_analysis", "review_source", "TEXT NOT NULL DEFAULT 'automatic_publish_validation'"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "capture_mushroom_analysis", "reviewed_by_user_id", "INTEGER"); err != nil {
					return err
				}
				if err := ensureColumnExists(tx, "capture_mushroom_analysis", "reviewed_at", "TEXT"); err != nil {
					return err
				}
				_, err := tx.Exec(`
					UPDATE capture_mushroom_analysis
					SET review_source = 'automatic_publish_validation'
					WHERE COALESCE(review_source, '') = ''
				`)
				return err
			},
		},
		{
			id: migrationCaptureGeoIndexTableID,
			apply: func(tx *sql.Tx) error {
				queries := []string{
					`CREATE TABLE IF NOT EXISTS capture_geo_index (
						capture_id TEXT PRIMARY KEY,
						country_code TEXT,
						kraj_name TEXT,
						kraj_name_norm TEXT,
						okres_name TEXT,
						okres_name_norm TEXT,
						obec_name TEXT,
						obec_name_norm TEXT,
						raw_json TEXT,
						resolved_at TEXT NOT NULL,
						FOREIGN KEY(capture_id) REFERENCES photo_captures(id) ON DELETE CASCADE
					);`,
					`CREATE INDEX IF NOT EXISTS idx_capture_geo_index_kraj_norm ON capture_geo_index(kraj_name_norm);`,
					`CREATE INDEX IF NOT EXISTS idx_capture_geo_index_okres_norm ON capture_geo_index(okres_name_norm);`,
					`CREATE INDEX IF NOT EXISTS idx_capture_geo_index_obec_norm ON capture_geo_index(obec_name_norm);`,
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
			id: migrationHoubickaLedgerTableID,
			apply: func(tx *sql.Tx) error {
				queries := []string{
					`CREATE TABLE IF NOT EXISTS houbicka_wallets (
						user_id INTEGER PRIMARY KEY,
						balance INTEGER NOT NULL DEFAULT 0,
						updated_at TEXT NOT NULL DEFAULT (datetime('now')),
						FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
					);`,
					`CREATE TABLE IF NOT EXISTS houbicka_operations (
						id TEXT PRIMARY KEY,
						kind TEXT NOT NULL,
						reason_code TEXT NOT NULL,
						idempotency_key TEXT UNIQUE,
						actor_user_id INTEGER,
						target_user_id INTEGER,
						capture_id TEXT,
						meta_json TEXT,
						created_at TEXT NOT NULL,
						FOREIGN KEY(actor_user_id) REFERENCES users(id) ON DELETE SET NULL,
						FOREIGN KEY(target_user_id) REFERENCES users(id) ON DELETE SET NULL
					);`,
					`CREATE INDEX IF NOT EXISTS idx_houbicka_operations_kind_created ON houbicka_operations(kind, created_at DESC);`,
					`CREATE INDEX IF NOT EXISTS idx_houbicka_operations_capture_created ON houbicka_operations(capture_id, created_at DESC);`,
					`CREATE TABLE IF NOT EXISTS houbicka_entries (
						id INTEGER PRIMARY KEY AUTOINCREMENT,
						operation_id TEXT NOT NULL,
						user_id INTEGER NOT NULL,
						amount_signed INTEGER NOT NULL,
						created_at TEXT NOT NULL,
						FOREIGN KEY(operation_id) REFERENCES houbicka_operations(id) ON DELETE CASCADE,
						FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
					);`,
					`CREATE INDEX IF NOT EXISTS idx_houbicka_entries_user_created ON houbicka_entries(user_id, created_at DESC);`,
					`CREATE INDEX IF NOT EXISTS idx_houbicka_entries_operation ON houbicka_entries(operation_id);`,
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
			id: migrationCaptureCoordinateUnlocksTableID,
			apply: func(tx *sql.Tx) error {
				queries := []string{
					`CREATE TABLE IF NOT EXISTS capture_coordinate_unlocks (
						viewer_user_id INTEGER NOT NULL,
						capture_id TEXT NOT NULL,
						operation_id TEXT NOT NULL,
						unlocked_at TEXT NOT NULL,
						PRIMARY KEY(viewer_user_id, capture_id),
						FOREIGN KEY(viewer_user_id) REFERENCES users(id) ON DELETE CASCADE,
						FOREIGN KEY(capture_id) REFERENCES photo_captures(id) ON DELETE CASCADE,
						FOREIGN KEY(operation_id) REFERENCES houbicka_operations(id) ON DELETE CASCADE
					);`,
					`CREATE INDEX IF NOT EXISTS idx_capture_coordinate_unlocks_capture ON capture_coordinate_unlocks(capture_id, unlocked_at DESC);`,
					`CREATE INDEX IF NOT EXISTS idx_capture_coordinate_unlocks_viewer ON capture_coordinate_unlocks(viewer_user_id, unlocked_at DESC);`,
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
			id: migrationContentModerationColumnsID,
			apply: func(tx *sql.Tx) error {
				for _, tableName := range []string{"photo_captures", "posts", "post_comments"} {
					if err := ensureColumnExists(tx, tableName, "moderator_hidden", "INTEGER NOT NULL DEFAULT 0"); err != nil {
						return err
					}
					if err := ensureColumnExists(tx, tableName, "moderation_reason_code", "TEXT"); err != nil {
						return err
					}
					if err := ensureColumnExists(tx, tableName, "moderated_by_user_id", "INTEGER"); err != nil {
						return err
					}
					if err := ensureColumnExists(tx, tableName, "moderated_at", "TEXT"); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			id: migrationModerationActionsTableID,
			apply: func(tx *sql.Tx) error {
				queries := []string{
					`CREATE TABLE IF NOT EXISTS moderation_actions (
						id TEXT PRIMARY KEY,
						actor_user_id INTEGER NOT NULL,
						target_user_id INTEGER,
						target_capture_id TEXT,
						target_post_id TEXT,
						target_comment_id TEXT,
						action_kind TEXT NOT NULL,
						reason_code TEXT,
						note TEXT,
						meta_json TEXT,
						created_at TEXT NOT NULL DEFAULT (datetime('now')),
						FOREIGN KEY(actor_user_id) REFERENCES users(id) ON DELETE CASCADE,
						FOREIGN KEY(target_user_id) REFERENCES users(id) ON DELETE SET NULL,
						FOREIGN KEY(target_capture_id) REFERENCES photo_captures(id) ON DELETE SET NULL,
						FOREIGN KEY(target_post_id) REFERENCES posts(id) ON DELETE SET NULL,
						FOREIGN KEY(target_comment_id) REFERENCES post_comments(id) ON DELETE SET NULL
					);`,
					`CREATE INDEX IF NOT EXISTS idx_moderation_actions_actor_created ON moderation_actions(actor_user_id, created_at DESC);`,
					`CREATE INDEX IF NOT EXISTS idx_moderation_actions_target_user_created ON moderation_actions(target_user_id, created_at DESC);`,
					`CREATE INDEX IF NOT EXISTS idx_moderation_actions_target_capture_created ON moderation_actions(target_capture_id, created_at DESC);`,
					`CREATE INDEX IF NOT EXISTS idx_moderation_actions_target_post_created ON moderation_actions(target_post_id, created_at DESC);`,
					`CREATE INDEX IF NOT EXISTS idx_moderation_actions_target_comment_created ON moderation_actions(target_comment_id, created_at DESC);`,
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
	isModerator := 0
	if isModeratorUsername(claims.PreferredUsername) {
		isModerator = 1
	}
	isAdmin := 0

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
			INSERT INTO users (idp_issuer, idp_sub, preferred_username, is_moderator, is_admin, email, email_verified, phone_number, phone_number_verified, picture, access_token, refresh_token, token_expires_at, last_idp_sync_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		`, claims.Iss, claims.Sub, claims.PreferredUsername, isModerator, isAdmin, claims.Email, ev, claims.PhoneNumber, pv, claims.Picture, token.AccessToken, token.RefreshToken, expiry)
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
				updated_at = datetime('now'),
				is_moderator = CASE
					WHEN is_moderator = 1 OR ? = 1 THEN 1
					ELSE 0
				END,
				is_admin = CASE
					WHEN is_admin = 1 THEN 1
					ELSE 0
				END
			WHERE id = ?
		`, claims.PreferredUsername, claims.Email, ev, claims.PhoneNumber, pv, claims.Picture, token.AccessToken, token.RefreshToken, expiry, isModerator, user.ID)
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
	var bannedUntil sql.NullString
	var commentsMutedUntil sql.NullString
	var publishingSuspendedUntil sql.NullString
	var moderatedAt sql.NullString
	err := db.QueryRow(`
		SELECT
			id,
			idp_issuer,
			idp_sub,
			COALESCE(preferred_username, ''),
			COALESCE(is_moderator, 0),
			COALESCE(is_admin, 0),
			COALESCE(email, ''),
			email_verified,
			COALESCE(phone_number, ''),
			phone_number_verified,
			COALESCE(picture, ''),
			COALESCE(about_me, ''),
			banned_until,
			comments_muted_until,
			publishing_suspended_until,
			COALESCE(moderation_note, ''),
			COALESCE(moderated_by_user_id, 0),
			moderated_at,
			COALESCE(access_token, ''),
			COALESCE(refresh_token, ''),
			token_expires_at,
			COALESCE((SELECT balance FROM houbicka_wallets WHERE user_id = users.id), 0)
		FROM users
		WHERE id = ?
	`, id).Scan(
		&user.ID, &user.IDPIssuer, &user.IDPSub, &user.PreferredUsername, &user.IsModerator, &user.IsAdmin, &user.Email, &user.EmailVerified, &user.PhoneNumber, &user.PhoneNumberVerified, &user.Picture, &user.AboutMe, &bannedUntil, &commentsMutedUntil, &publishingSuspendedUntil, &user.ModerationNote, &user.ModeratedByUserID, &moderatedAt, &user.AccessToken, &user.RefreshToken, &expiresAt, &user.HoubickaBalance,
	)
	if err == nil && expiresAt.Valid && expiresAt.String != "" {
		user.TokenExpiresAt, _ = time.Parse(time.RFC3339, expiresAt.String)
	}
	if err == nil && bannedUntil.Valid && bannedUntil.String != "" {
		user.BannedUntil, _ = time.Parse(time.RFC3339, bannedUntil.String)
	}
	if err == nil && commentsMutedUntil.Valid && commentsMutedUntil.String != "" {
		user.CommentsMutedUntil, _ = time.Parse(time.RFC3339, commentsMutedUntil.String)
	}
	if err == nil && publishingSuspendedUntil.Valid && publishingSuspendedUntil.String != "" {
		user.PublishingSuspendedUntil, _ = time.Parse(time.RFC3339, publishingSuspendedUntil.String)
	}
	if err == nil && moderatedAt.Valid && moderatedAt.String != "" {
		user.ModeratedAt, _ = time.Parse(time.RFC3339, moderatedAt.String)
	}
	return &user, err
}

func (db *DB) GetUserByPreferredUsername(username string) (*models.User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, sql.ErrNoRows
	}

	rows, err := db.Query(`
		SELECT id
		FROM users
		WHERE lower(COALESCE(preferred_username, '')) = lower(?)
		ORDER BY id
	`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, sql.ErrNoRows
	}
	if len(ids) > 1 {
		return nil, fmt.Errorf("multiple users found for preferred_username %q", username)
	}

	return db.GetUser(ids[0])
}

func (db *DB) GetPublicUserProfile(id int64) (*models.PublicUserProfile, error) {
	var (
		profile                models.PublicUserProfile
		createdAtRaw           string
		emailVerifiedInt       int
		phoneNumberVerifiedInt int
	)
	now := moderationNowRFC3339()

	err := db.QueryRow(fmt.Sprintf(`
		SELECT
			id,
			COALESCE(preferred_username, ''),
			COALESCE(picture, ''),
			COALESCE(about_me, ''),
			email_verified,
			phone_number_verified,
			created_at,
			(SELECT COUNT(*) FROM posts WHERE user_id = users.id AND status = 'published' AND COALESCE(moderator_hidden, 0) = 0),
			(SELECT COUNT(*) FROM photo_captures WHERE user_id = users.id AND status = 'published' AND COALESCE(moderator_hidden, 0) = 0 AND public_storage_key IS NOT NULL AND public_storage_key != '')
		FROM users
		WHERE id = ? AND %s
	`, publicUserNotBannedClause("users")), id, now).Scan(
		&profile.ID,
		&profile.PreferredUsername,
		&profile.Picture,
		&profile.AboutMe,
		&emailVerifiedInt,
		&phoneNumberVerifiedInt,
		&createdAtRaw,
		&profile.PublicPostsCount,
		&profile.PublicCapturesCount,
	)
	if err != nil {
		return nil, err
	}

	profile.EmailVerified = emailVerifiedInt == 1
	profile.PhoneVerified = phoneNumberVerifiedInt == 1
	profile.JoinedAt, _ = time.Parse(time.RFC3339, createdAtRaw)
	return &profile, nil
}

func (db *DB) UpdateAboutMe(id int64, aboutMe string) error {
	_, err := db.Exec("UPDATE users SET about_me = ?, updated_at = datetime('now') WHERE id = ?", aboutMe, id)
	return err
}

const captureSelectColumns = `
	SELECT id, user_id, COALESCE(client_local_id, ''), original_file_name, content_type, size_bytes, width, height,
		captured_at, uploaded_at, latitude, longitude, accuracy_meters, status, private_storage_key,
		COALESCE(public_storage_key, ''), COALESCE(published_at, ''), COALESCE(coordinates_free, 0),
		COALESCE(moderator_hidden, 0), COALESCE(moderation_reason_code, ''), COALESCE(moderated_by_user_id, 0), COALESCE(moderated_at, ''),
		COALESCE(publication_review_status, 'none'), COALESCE(publication_review_reason_code, ''),
		COALESCE(publication_review_last_error, ''), COALESCE(publication_review_checked_at, ''),
		COALESCE(publication_requested_at, ''), COALESCE(publication_review_attempts, 0),
		COALESCE(publication_review_next_attempt_at, '')
	FROM photo_captures
`

func (db *DB) CreateCapture(capture *models.Capture) error {
	_, err := db.Exec(`
		INSERT INTO photo_captures (
			id, user_id, client_local_id, original_file_name, content_type, size_bytes, width, height,
			captured_at, uploaded_at, latitude, longitude, accuracy_meters, status, private_storage_key,
			public_storage_key, published_at, coordinates_free
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		capture.CoordinatesFree,
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

func (db *DB) SetCaptureCoordinatesFree(id string, userID int64, coordinatesFree bool) error {
	_, err := db.Exec(`
		UPDATE photo_captures
		SET coordinates_free = ?
		WHERE id = ? AND user_id = ?
	`, coordinatesFree, id, userID)
	return err
}

func (db *DB) DeleteCapture(id string, userID int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM capture_coordinate_unlocks WHERE capture_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM photo_captures WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return err
	}
	return tx.Commit()
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanCaptureRow(row scanner) (*models.Capture, error) {
	return scanCapture(row)
}

func scanCapture(row scanner) (*models.Capture, error) {
	var (
		capture                        models.Capture
		clientLocalID                  sql.NullString
		capturedAtRaw                  string
		uploadedAtRaw                  string
		latitude                       sql.NullFloat64
		longitude                      sql.NullFloat64
		accuracyMeters                 sql.NullFloat64
		publicStorageKey               sql.NullString
		publishedAtRaw                 sql.NullString
		coordinatesFree                int
		moderatorHidden                int
		moderationReasonCode           sql.NullString
		moderatedByUserID              int64
		moderatedAt                    sql.NullString
		publicationReviewStatus        string
		publicationReviewReason        sql.NullString
		publicationReviewLastError     sql.NullString
		publicationReviewCheckedAt     sql.NullString
		publicationRequestedAt         sql.NullString
		publicationReviewAttempts      int
		publicationReviewNextAttemptAt sql.NullString
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
		&coordinatesFree,
		&moderatorHidden,
		&moderationReasonCode,
		&moderatedByUserID,
		&moderatedAt,
		&publicationReviewStatus,
		&publicationReviewReason,
		&publicationReviewLastError,
		&publicationReviewCheckedAt,
		&publicationRequestedAt,
		&publicationReviewAttempts,
		&publicationReviewNextAttemptAt,
	); err != nil {
		return nil, err
	}

	capture.ClientLocalID = clientLocalID.String
	capture.AuthorUserID = capture.UserID
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
	capture.CoordinatesFree = coordinatesFree == 1
	capture.ModeratorHidden = moderatorHidden == 1
	capture.ModerationReasonCode = moderationReasonCode.String
	capture.ModeratedByUserID = moderatedByUserID
	if moderatedAt.Valid && moderatedAt.String != "" {
		capture.ModeratedAt, _ = time.Parse(time.RFC3339, moderatedAt.String)
	}
	capture.PublicationReviewStatus = publicationReviewStatus
	capture.PublicationReviewReasonCode = publicationReviewReason.String
	capture.PublicationReviewLastError = publicationReviewLastError.String
	capture.PublicationReviewAttempts = publicationReviewAttempts
	if publicationReviewCheckedAt.Valid && publicationReviewCheckedAt.String != "" {
		capture.PublicationReviewCheckedAt, _ = time.Parse(time.RFC3339, publicationReviewCheckedAt.String)
	}
	if publicationRequestedAt.Valid && publicationRequestedAt.String != "" {
		capture.PublicationRequestedAt, _ = time.Parse(time.RFC3339, publicationRequestedAt.String)
	}
	if publicationReviewNextAttemptAt.Valid && publicationReviewNextAttemptAt.String != "" {
		capture.PublicationReviewNextAttemptAt, _ = time.Parse(time.RFC3339, publicationReviewNextAttemptAt.String)
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

func nullIfZeroInt64(value int64) interface{} {
	if value == 0 {
		return nil
	}
	return value
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
	p.AuthorUserID = p.UserID

	captures, err := db.getCapturesForPost(p.ID, userID, true)
	if err != nil {
		return nil, err
	}
	p.Captures = captures

	comments, err := db.getCommentsForPost(p.ID, true)
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
	now := moderationNowRFC3339()
	err := db.QueryRow(fmt.Sprintf(`
		SELECT 1
		FROM posts p
		JOIN users u ON p.user_id = u.id
		WHERE p.id = ? AND p.status = 'published' AND COALESCE(p.moderator_hidden, 0) = 0 AND %s
		LIMIT 1
	`, publicUserNotBannedClause("u")), postID, now).Scan(&exists)
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

func (db *DB) UpdatePostComment(postID, commentID string, userID int64, content string, updatedAt time.Time) (*models.Comment, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		UPDATE post_comments
		SET content = ?, updated_at = ?
		WHERE id = ? AND post_id = ? AND user_id = ?
	`, content, updatedAt.UTC().Format(time.RFC3339), commentID, postID, userID)
	if err != nil {
		return nil, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rowsAffected == 0 {
		return nil, sql.ErrNoRows
	}

	comment, err := scanCommentRow(tx.QueryRow(`
		SELECT pc.id, pc.post_id, pc.user_id, COALESCE(u.preferred_username, ''), COALESCE(u.picture, ''), pc.content, pc.created_at, pc.updated_at,
			COALESCE(pc.moderator_hidden, 0), COALESCE(pc.moderation_reason_code, ''), COALESCE(pc.moderated_by_user_id, 0), COALESCE(pc.moderated_at, '')
		FROM post_comments pc
		JOIN users u ON pc.user_id = u.id
		WHERE pc.id = ? AND pc.post_id = ?
		LIMIT 1
	`, commentID, postID))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return comment, nil
}

func (db *DB) DeletePostComment(postID, commentID string, userID int64) error {
	result, err := db.Exec(`
		DELETE FROM post_comments
		WHERE id = ? AND post_id = ? AND user_id = ?
	`, commentID, postID, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
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
		p.AuthorUserID = p.UserID
		posts = append(posts, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, p := range posts {
		captures, err := db.getCapturesForPost(p.ID, userID, true)
		if err != nil {
			return nil, err
		}
		p.Captures = captures

		comments, err := db.getCommentsForPost(p.ID, true)
		if err != nil {
			return nil, err
		}
		p.Comments = comments
	}

	return posts, nil
}

func (db *DB) ListPublicPosts(limit, offset int, currentUserID int64) ([]*models.Post, error) {
	now := moderationNowRFC3339()
	rows, err := db.Query(fmt.Sprintf(`
		SELECT p.id, p.user_id, u.preferred_username, COALESCE(u.picture, ''), p.content, p.status, p.created_at, p.updated_at,
               (SELECT COUNT(*) FROM post_likes WHERE post_id = p.id) as likes_count,
               (SELECT EXISTS(SELECT 1 FROM post_likes WHERE post_id = p.id AND user_id = ?)) as is_liked_by_me
		FROM posts p
		JOIN users u ON p.user_id = u.id
		WHERE p.status = 'published'
			AND COALESCE(p.moderator_hidden, 0) = 0
			AND %s
		ORDER BY p.created_at DESC
		LIMIT ? OFFSET ?
	`, publicUserNotBannedClause("u")), currentUserID, now, limit, offset)
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
		p.AuthorUserID = p.UserID
		posts = append(posts, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, p := range posts {
		captures, err := db.getCapturesForPost(p.ID, currentUserID, false)
		if err != nil {
			return nil, err
		}
		p.Captures = captures

		comments, err := db.getCommentsForPost(p.ID, false)
		if err != nil {
			return nil, err
		}
		p.Comments = comments
	}

	return posts, nil
}

func (db *DB) CountPublicPostsByUser(userID int64) (int, error) {
	var total int
	now := moderationNowRFC3339()
	err := db.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*)
		FROM posts p
		JOIN users u ON p.user_id = u.id
		WHERE p.user_id = ? AND p.status = 'published' AND COALESCE(p.moderator_hidden, 0) = 0 AND %s
	`, publicUserNotBannedClause("u")), userID, now).Scan(&total)
	return total, err
}

func (db *DB) ListPublicPostsByUser(userID int64, limit, offset int, currentUserID int64) ([]*models.Post, error) {
	now := moderationNowRFC3339()
	rows, err := db.Query(fmt.Sprintf(`
		SELECT p.id, p.user_id, COALESCE(u.preferred_username, ''), COALESCE(u.picture, ''), p.content, p.status, p.created_at, p.updated_at,
		       (SELECT COUNT(*) FROM post_likes WHERE post_id = p.id) as likes_count,
		       (SELECT EXISTS(SELECT 1 FROM post_likes WHERE post_id = p.id AND user_id = ?)) as is_liked_by_me
		FROM posts p
		JOIN users u ON p.user_id = u.id
		WHERE p.status = 'published' AND p.user_id = ? AND COALESCE(p.moderator_hidden, 0) = 0 AND %s
		ORDER BY p.created_at DESC
		LIMIT ? OFFSET ?
	`, publicUserNotBannedClause("u")), currentUserID, userID, now, limit, offset)
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
		p.AuthorUserID = p.UserID
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		posts = append(posts, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, p := range posts {
		captures, err := db.getCapturesForPost(p.ID, currentUserID, false)
		if err != nil {
			return nil, err
		}
		p.Captures = captures

		comments, err := db.getCommentsForPost(p.ID, false)
		if err != nil {
			return nil, err
		}
		p.Comments = comments
	}

	return posts, nil
}

func (db *DB) ListPublicCaptures(limit, offset int, viewerUserID int64) ([]*models.Capture, error) {
	now := moderationNowRFC3339()
	rows, err := db.Query(fmt.Sprintf(`
		SELECT c.id, c.user_id, u.preferred_username, COALESCE(u.picture, ''), COALESCE(c.client_local_id, ''), c.original_file_name, c.content_type, c.size_bytes, c.width, c.height,
			c.captured_at, c.uploaded_at, c.latitude, c.longitude, c.accuracy_meters, c.status, c.private_storage_key,
			COALESCE(c.public_storage_key, ''), COALESCE(c.published_at, ''), COALESCE(c.coordinates_free, 0)
		FROM photo_captures c
		JOIN users u ON c.user_id = u.id
		WHERE c.status = 'published' AND COALESCE(c.moderator_hidden, 0) = 0 AND c.public_storage_key IS NOT NULL AND c.public_storage_key != '' AND %s
		ORDER BY c.published_at DESC, c.captured_at DESC
		LIMIT ? OFFSET ?
	`, publicUserNotBannedClause("u")), now, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var captures []*models.Capture
	for rows.Next() {
		var c models.Capture
		var capturedAt, uploadedAt, publishedAt string
		var coordinatesFree int
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.AuthorName, &c.AuthorAvatar, &c.ClientLocalID, &c.OriginalFileName, &c.ContentType, &c.SizeBytes, &c.Width, &c.Height,
			&capturedAt, &uploadedAt, &c.Latitude, &c.Longitude, &c.AccuracyMeters, &c.Status, &c.PrivateStorageKey,
			&c.PublicStorageKey, &publishedAt, &coordinatesFree,
		); err != nil {
			return nil, err
		}
		c.CapturedAt, _ = time.Parse(time.RFC3339, capturedAt)
		c.UploadedAt, _ = time.Parse(time.RFC3339, uploadedAt)
		c.AuthorUserID = c.UserID
		if publishedAt != "" {
			c.PublishedAt, _ = time.Parse(time.RFC3339, publishedAt)
		}
		c.CoordinatesFree = coordinatesFree == 1
		captures = append(captures, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := db.maskCaptureCoordinatesForViewer(viewerUserID, captures); err != nil {
		return nil, err
	}

	return captures, nil
}

func (db *DB) CountPublicCapturesByUser(userID int64) (int, error) {
	var total int
	now := moderationNowRFC3339()
	err := db.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*)
		FROM photo_captures c
		JOIN users u ON c.user_id = u.id
		WHERE c.user_id = ? AND c.status = 'published' AND COALESCE(c.moderator_hidden, 0) = 0 AND c.public_storage_key IS NOT NULL AND c.public_storage_key != '' AND %s
	`, publicUserNotBannedClause("u")), userID, now).Scan(&total)
	return total, err
}

func (db *DB) ListPublicCapturesByUser(userID int64, limit, offset int, viewerUserID int64) ([]*models.Capture, error) {
	now := moderationNowRFC3339()
	rows, err := db.Query(fmt.Sprintf(`
		SELECT c.id, c.user_id, COALESCE(u.preferred_username, ''), COALESCE(u.picture, ''), COALESCE(c.client_local_id, ''), c.original_file_name, c.content_type, c.size_bytes, c.width, c.height,
			c.captured_at, c.uploaded_at, c.latitude, c.longitude, c.accuracy_meters, c.status, c.private_storage_key,
			COALESCE(c.public_storage_key, ''), COALESCE(c.published_at, ''), COALESCE(c.coordinates_free, 0)
		FROM photo_captures c
		JOIN users u ON c.user_id = u.id
		WHERE c.user_id = ? AND c.status = 'published' AND COALESCE(c.moderator_hidden, 0) = 0 AND c.public_storage_key IS NOT NULL AND c.public_storage_key != '' AND %s
		ORDER BY c.published_at DESC, c.captured_at DESC
		LIMIT ? OFFSET ?
	`, publicUserNotBannedClause("u")), userID, now, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var captures []*models.Capture
	for rows.Next() {
		var c models.Capture
		var capturedAt, uploadedAt, publishedAt string
		var coordinatesFree int
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.AuthorName, &c.AuthorAvatar, &c.ClientLocalID, &c.OriginalFileName, &c.ContentType, &c.SizeBytes, &c.Width, &c.Height,
			&capturedAt, &uploadedAt, &c.Latitude, &c.Longitude, &c.AccuracyMeters, &c.Status, &c.PrivateStorageKey,
			&c.PublicStorageKey, &publishedAt, &coordinatesFree,
		); err != nil {
			return nil, err
		}
		c.AuthorUserID = c.UserID
		c.CapturedAt, _ = time.Parse(time.RFC3339, capturedAt)
		if uploadedAt != "" {
			c.UploadedAt, _ = time.Parse(time.RFC3339, uploadedAt)
		}
		if publishedAt != "" {
			c.PublishedAt, _ = time.Parse(time.RFC3339, publishedAt)
		}
		c.CoordinatesFree = coordinatesFree == 1
		captures = append(captures, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := db.maskCaptureCoordinatesForViewer(viewerUserID, captures); err != nil {
		return nil, err
	}

	return captures, nil
}

func (db *DB) getCapturesForPost(postID string, viewerUserID int64, includeModerated bool) ([]*models.Capture, error) {
	var (
		query string
		args  []interface{}
	)
	if includeModerated {
		query = `
		SELECT c.id, c.user_id, COALESCE(c.client_local_id, ''), c.original_file_name, c.content_type, c.size_bytes, c.width, c.height,
			c.captured_at, c.uploaded_at, c.latitude, c.longitude, c.accuracy_meters, c.status, c.private_storage_key,
			COALESCE(c.public_storage_key, ''), COALESCE(c.published_at, ''), COALESCE(c.coordinates_free, 0)
		FROM photo_captures c
		JOIN post_captures pc ON c.id = pc.capture_id
		WHERE pc.post_id = ?
		ORDER BY pc.display_order ASC
	`
		args = []interface{}{postID}
	} else {
		query = fmt.Sprintf(`
		SELECT c.id, c.user_id, COALESCE(c.client_local_id, ''), c.original_file_name, c.content_type, c.size_bytes, c.width, c.height,
			c.captured_at, c.uploaded_at, c.latitude, c.longitude, c.accuracy_meters, c.status, c.private_storage_key,
			COALESCE(c.public_storage_key, ''), COALESCE(c.published_at, ''), COALESCE(c.coordinates_free, 0)
		FROM photo_captures c
		JOIN post_captures pc ON c.id = pc.capture_id
		JOIN users u ON c.user_id = u.id
		WHERE pc.post_id = ?
			AND c.status = 'published'
			AND COALESCE(c.moderator_hidden, 0) = 0
			AND c.public_storage_key IS NOT NULL
			AND c.public_storage_key != ''
			AND %s
		ORDER BY pc.display_order ASC
	`, publicUserNotBannedClause("u"))
		args = []interface{}{postID, moderationNowRFC3339()}
	}
	cRows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer cRows.Close()

	var captures []*models.Capture
	for cRows.Next() {
		var c models.Capture
		var capturedAt, uploadedAt, publishedAt string
		var coordinatesFree int
		if err := cRows.Scan(
			&c.ID, &c.UserID, &c.ClientLocalID, &c.OriginalFileName, &c.ContentType, &c.SizeBytes, &c.Width, &c.Height,
			&capturedAt, &uploadedAt, &c.Latitude, &c.Longitude, &c.AccuracyMeters, &c.Status, &c.PrivateStorageKey,
			&c.PublicStorageKey, &publishedAt, &coordinatesFree,
		); err != nil {
			return nil, err
		}
		c.AuthorUserID = c.UserID
		c.CapturedAt, _ = time.Parse(time.RFC3339, capturedAt)
		c.UploadedAt, _ = time.Parse(time.RFC3339, uploadedAt)
		if publishedAt != "" {
			c.PublishedAt, _ = time.Parse(time.RFC3339, publishedAt)
		}
		c.CoordinatesFree = coordinatesFree == 1
		captures = append(captures, &c)
	}
	if err := cRows.Err(); err != nil {
		return nil, err
	}
	if err := db.maskCaptureCoordinatesForViewer(viewerUserID, captures); err != nil {
		return nil, err
	}
	return captures, nil
}

func (db *DB) GetPublicCaptureByID(captureID string) (*models.Capture, error) {
	row := db.QueryRow(fmt.Sprintf(`
		SELECT c.id, c.user_id, COALESCE(c.client_local_id, ''), c.original_file_name, c.content_type, c.size_bytes, c.width, c.height,
			c.captured_at, c.uploaded_at, c.latitude, c.longitude, c.accuracy_meters, c.status, c.private_storage_key,
			COALESCE(c.public_storage_key, ''), COALESCE(c.published_at, ''), COALESCE(c.coordinates_free, 0),
			COALESCE(c.moderator_hidden, 0), COALESCE(c.moderation_reason_code, ''), COALESCE(c.moderated_by_user_id, 0), COALESCE(c.moderated_at, ''),
			COALESCE(c.publication_review_status, 'none'), COALESCE(c.publication_review_reason_code, ''),
			COALESCE(c.publication_review_last_error, ''), COALESCE(c.publication_review_checked_at, ''),
			COALESCE(c.publication_requested_at, ''), COALESCE(c.publication_review_attempts, 0),
			COALESCE(c.publication_review_next_attempt_at, '')
		FROM photo_captures c
		JOIN users u ON c.user_id = u.id
		WHERE c.id = ? AND c.status = 'published' AND COALESCE(c.moderator_hidden, 0) = 0 AND %s
		LIMIT 1
	`, publicUserNotBannedClause("u")), captureID, moderationNowRFC3339())
	return scanCaptureRow(row)
}

func (db *DB) HasCaptureUnlock(viewerUserID int64, captureID string) (bool, error) {
	if viewerUserID == 0 || captureID == "" {
		return false, nil
	}
	var exists int
	err := db.QueryRow(`
		SELECT 1
		FROM capture_coordinate_unlocks
		WHERE viewer_user_id = ? AND capture_id = ?
		LIMIT 1
	`, viewerUserID, captureID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (db *DB) getUnlockedCaptureSet(viewerUserID int64, captureIDs []string) (map[string]struct{}, error) {
	unlocked := make(map[string]struct{})
	if viewerUserID == 0 || len(captureIDs) == 0 {
		return unlocked, nil
	}

	placeholders := make([]string, 0, len(captureIDs))
	args := make([]interface{}, 0, len(captureIDs)+1)
	args = append(args, viewerUserID)
	for _, captureID := range captureIDs {
		placeholders = append(placeholders, "?")
		args = append(args, captureID)
	}

	query := `
		SELECT capture_id
		FROM capture_coordinate_unlocks
		WHERE viewer_user_id = ? AND capture_id IN (` + strings.Join(placeholders, ", ") + `)
	`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var captureID string
		if err := rows.Scan(&captureID); err != nil {
			return nil, err
		}
		unlocked[captureID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return unlocked, nil
}

func (db *DB) maskCaptureCoordinatesForViewer(viewerUserID int64, captures []*models.Capture) error {
	captureIDs := make([]string, 0, len(captures))
	for _, capture := range captures {
		if capture == nil {
			continue
		}
		if capture.UserID == viewerUserID || capture.CoordinatesFree {
			continue
		}
		if capture.Latitude == nil && capture.Longitude == nil {
			continue
		}
		captureIDs = append(captureIDs, capture.ID)
	}

	unlocked, err := db.getUnlockedCaptureSet(viewerUserID, captureIDs)
	if err != nil {
		return err
	}

	for _, capture := range captures {
		if capture == nil {
			continue
		}
		hasCoordinates := capture.Latitude != nil && capture.Longitude != nil
		if !hasCoordinates {
			capture.CoordinatesLocked = false
			continue
		}
		if capture.UserID == viewerUserID || capture.CoordinatesFree {
			capture.CoordinatesLocked = false
			continue
		}
		if _, ok := unlocked[capture.ID]; ok {
			capture.CoordinatesLocked = false
			continue
		}
		capture.Latitude = nil
		capture.Longitude = nil
		capture.AccuracyMeters = nil
		capture.CoordinatesLocked = true
	}
	return nil
}

func (db *DB) ensureWalletTx(tx *sql.Tx, userID int64) error {
	_, err := tx.Exec(`
		INSERT OR IGNORE INTO houbicka_wallets (user_id, balance, updated_at)
		VALUES (?, 0, ?)
	`, userID, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (db *DB) getWalletBalanceTx(tx *sql.Tx, userID int64) (int64, error) {
	if err := db.ensureWalletTx(tx, userID); err != nil {
		return 0, err
	}
	var balance int64
	err := tx.QueryRow(`SELECT balance FROM houbicka_wallets WHERE user_id = ?`, userID).Scan(&balance)
	return balance, err
}

func (db *DB) GetHoubickaBalance(userID int64) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	balance, err := db.getWalletBalanceTx(tx, userID)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return balance, nil
}

func (db *DB) applyHoubickaOperationTx(tx *sql.Tx, operation *models.HoubickaOperation, entries []models.HoubickaEntry) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("transaction is required")
	}
	if operation == nil {
		return false, fmt.Errorf("operation is required")
	}
	if len(entries) == 0 {
		return false, fmt.Errorf("entries are required")
	}

	if operation.IdempotencyKey != "" {
		var existing string
		err := tx.QueryRow(`SELECT id FROM houbicka_operations WHERE idempotency_key = ?`, operation.IdempotencyKey).Scan(&existing)
		if err == nil {
			return false, nil
		}
		if err != nil && err != sql.ErrNoRows {
			return false, err
		}
	}

	deltas := make(map[int64]int64)
	for _, entry := range entries {
		deltas[entry.UserID] += entry.AmountSigned
	}

	for userID := range deltas {
		if err := db.ensureWalletTx(tx, userID); err != nil {
			return false, err
		}
	}

	for userID, delta := range deltas {
		if delta >= 0 {
			continue
		}
		balance, err := db.getWalletBalanceTx(tx, userID)
		if err != nil {
			return false, err
		}
		if balance+delta < 0 {
			return false, ErrInsufficientHoubickaBalance
		}
	}

	if operation.ID == "" {
		operation.ID = uuid.New().String()
	}
	now := time.Now().UTC()

	if _, err := tx.Exec(`
		INSERT INTO houbicka_operations (
			id, kind, reason_code, idempotency_key, actor_user_id, target_user_id, capture_id, meta_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		operation.ID,
		operation.Kind,
		operation.ReasonCode,
		nullIfEmpty(operation.IdempotencyKey),
		nullIfZeroInt64(operation.ActorUserID),
		nullIfZeroInt64(operation.TargetUserID),
		nullIfEmpty(operation.CaptureID),
		nullIfEmpty(operation.MetaJSON),
		now.Format(time.RFC3339),
	); err != nil {
		return false, err
	}

	for _, entry := range entries {
		if _, err := tx.Exec(`
			INSERT INTO houbicka_entries (operation_id, user_id, amount_signed, created_at)
			VALUES (?, ?, ?, ?)
		`, operation.ID, entry.UserID, entry.AmountSigned, now.Format(time.RFC3339)); err != nil {
			return false, err
		}
	}

	for userID, delta := range deltas {
		if _, err := tx.Exec(`
			UPDATE houbicka_wallets
			SET balance = balance + ?, updated_at = ?
			WHERE user_id = ?
		`, delta, now.Format(time.RFC3339), userID); err != nil {
			return false, err
		}
	}

	operation.CreatedAt = now
	return true, nil
}

func (db *DB) GrantAuthBonuses(userID int64, isNew, emailVerified, phoneVerified bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if isNew {
		_, err = db.applyHoubickaOperationTx(tx, &models.HoubickaOperation{
			Kind:           "reward",
			ReasonCode:     "signup_bonus",
			IdempotencyKey: fmt.Sprintf("reward:signup:%d", userID),
			TargetUserID:   userID,
		}, []models.HoubickaEntry{{UserID: userID, AmountSigned: 1}})
		if err != nil {
			return err
		}
	}

	if emailVerified {
		_, err = db.applyHoubickaOperationTx(tx, &models.HoubickaOperation{
			Kind:           "reward",
			ReasonCode:     "email_verified_bonus",
			IdempotencyKey: fmt.Sprintf("reward:email_verified:%d", userID),
			TargetUserID:   userID,
		}, []models.HoubickaEntry{{UserID: userID, AmountSigned: 3}})
		if err != nil {
			return err
		}
	}

	if phoneVerified {
		_, err = db.applyHoubickaOperationTx(tx, &models.HoubickaOperation{
			Kind:           "reward",
			ReasonCode:     "phone_verified_bonus",
			IdempotencyKey: fmt.Sprintf("reward:phone_verified:%d", userID),
			TargetUserID:   userID,
		}, []models.HoubickaEntry{{UserID: userID, AmountSigned: 5}})
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *DB) UnlockCaptureCoordinates(viewerUserID int64, captureID string) (int64, bool, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()

	capture, err := scanCaptureRow(tx.QueryRow(fmt.Sprintf(`
		SELECT c.id, c.user_id, COALESCE(c.client_local_id, ''), c.original_file_name, c.content_type, c.size_bytes, c.width, c.height,
			c.captured_at, c.uploaded_at, c.latitude, c.longitude, c.accuracy_meters, c.status, c.private_storage_key,
			COALESCE(c.public_storage_key, ''), COALESCE(c.published_at, ''), COALESCE(c.coordinates_free, 0),
			COALESCE(c.moderator_hidden, 0), COALESCE(c.moderation_reason_code, ''), COALESCE(c.moderated_by_user_id, 0), COALESCE(c.moderated_at, ''),
			COALESCE(c.publication_review_status, 'none'), COALESCE(c.publication_review_reason_code, ''),
			COALESCE(c.publication_review_last_error, ''), COALESCE(c.publication_review_checked_at, ''),
			COALESCE(c.publication_requested_at, ''), COALESCE(c.publication_review_attempts, 0),
			COALESCE(c.publication_review_next_attempt_at, '')
		FROM photo_captures c
		JOIN users u ON c.user_id = u.id
		WHERE c.id = ? AND c.status = 'published' AND COALESCE(c.moderator_hidden, 0) = 0 AND %s
		LIMIT 1
	`, publicUserNotBannedClause("u")), captureID, moderationNowRFC3339()))
	if err != nil {
		return 0, false, err
	}
	if capture.Latitude == nil || capture.Longitude == nil {
		return 0, false, ErrCaptureHasNoCoordinates
	}
	if capture.UserID == viewerUserID || capture.CoordinatesFree {
		balance, err := db.getWalletBalanceTx(tx, viewerUserID)
		if err != nil {
			return 0, false, err
		}
		if err := tx.Commit(); err != nil {
			return 0, false, err
		}
		return balance, true, nil
	}

	var existingOperationID string
	err = tx.QueryRow(`
		SELECT operation_id
		FROM capture_coordinate_unlocks
		WHERE viewer_user_id = ? AND capture_id = ?
		LIMIT 1
	`, viewerUserID, captureID).Scan(&existingOperationID)
	if err == nil {
		balance, err := db.getWalletBalanceTx(tx, viewerUserID)
		if err != nil {
			return 0, false, err
		}
		if err := tx.Commit(); err != nil {
			return 0, false, err
		}
		return balance, true, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return 0, false, err
	}

	metaBytes, _ := json.Marshal(map[string]int64{"price": 1})
	operation := &models.HoubickaOperation{
		Kind:           "capture_unlock",
		ReasonCode:     "capture_coordinates_unlock",
		IdempotencyKey: fmt.Sprintf("unlock:capture:%d:%s", viewerUserID, captureID),
		ActorUserID:    viewerUserID,
		TargetUserID:   capture.UserID,
		CaptureID:      captureID,
		MetaJSON:       string(metaBytes),
	}

	applied, err := db.applyHoubickaOperationTx(tx, operation, []models.HoubickaEntry{
		{UserID: viewerUserID, AmountSigned: -1},
		{UserID: capture.UserID, AmountSigned: 1},
	})
	if err != nil {
		return 0, false, err
	}

	if applied {
		if _, err := tx.Exec(`
			INSERT INTO capture_coordinate_unlocks (viewer_user_id, capture_id, operation_id, unlocked_at)
			VALUES (?, ?, ?, ?)
		`, viewerUserID, captureID, operation.ID, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return 0, false, err
		}
	}

	balance, err := db.getWalletBalanceTx(tx, viewerUserID)
	if err != nil {
		return 0, false, err
	}

	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return balance, !applied, nil
}

func (db *DB) ListViewedCapturesByUser(viewerUserID int64, limit, offset int) ([]*models.Capture, error) {
	rows, err := db.Query(`
		SELECT
			c.id,
			c.user_id,
			COALESCE(u.preferred_username, ''),
			COALESCE(u.picture, ''),
			COALESCE(c.client_local_id, ''),
			c.original_file_name,
			c.content_type,
			c.size_bytes,
			c.width,
			c.height,
			c.captured_at,
			c.uploaded_at,
			c.latitude,
			c.longitude,
			c.accuracy_meters,
			c.status,
			c.private_storage_key,
			COALESCE(c.public_storage_key, ''),
			COALESCE(c.published_at, ''),
			COALESCE(c.coordinates_free, 0),
			cu.unlocked_at
		FROM capture_coordinate_unlocks cu
		JOIN photo_captures c ON c.id = cu.capture_id
		JOIN users u ON u.id = c.user_id
		WHERE cu.viewer_user_id = ?
		ORDER BY cu.unlocked_at DESC
		LIMIT ? OFFSET ?
	`, viewerUserID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var captures []*models.Capture
	for rows.Next() {
		var capture models.Capture
		var capturedAtRaw, uploadedAtRaw, unlockedAtRaw string
		var latitude, longitude, accuracyMeters sql.NullFloat64
		var publicStorageKey, publishedAtRaw sql.NullString
		var coordinatesFree int

		if err := rows.Scan(
			&capture.ID,
			&capture.UserID,
			&capture.AuthorName,
			&capture.AuthorAvatar,
			&capture.ClientLocalID,
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
			&coordinatesFree,
			&unlockedAtRaw,
		); err != nil {
			return nil, err
		}

		capture.PublicStorageKey = publicStorageKey.String
		capture.AuthorUserID = capture.UserID
		capture.CapturedAt, _ = time.Parse(time.RFC3339, capturedAtRaw)
		capture.UploadedAt, _ = time.Parse(time.RFC3339, uploadedAtRaw)
		capture.UnlockedAt, _ = time.Parse(time.RFC3339, unlockedAtRaw)
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
		capture.CoordinatesFree = coordinatesFree == 1
		captures = append(captures, &capture)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return captures, nil
}

func (db *DB) CountViewedCapturesByUser(viewerUserID int64) (int, error) {
	var total int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM capture_coordinate_unlocks
		WHERE viewer_user_id = ?
	`, viewerUserID).Scan(&total)
	return total, err
}

func (db *DB) getCommentsForPost(postID string, includeModerated bool) ([]*models.Comment, error) {
	var (
		query string
		args  []interface{}
	)
	if includeModerated {
		query = `
		SELECT pc.id, pc.post_id, pc.user_id, COALESCE(u.preferred_username, ''), COALESCE(u.picture, ''), pc.content, pc.created_at, pc.updated_at,
			COALESCE(pc.moderator_hidden, 0), COALESCE(pc.moderation_reason_code, ''), COALESCE(pc.moderated_by_user_id, 0), COALESCE(pc.moderated_at, '')
		FROM post_comments pc
		JOIN users u ON pc.user_id = u.id
		WHERE pc.post_id = ?
		ORDER BY pc.created_at ASC, pc.id ASC
	`
		args = []interface{}{postID}
	} else {
		query = fmt.Sprintf(`
		SELECT pc.id, pc.post_id, pc.user_id, COALESCE(u.preferred_username, ''), COALESCE(u.picture, ''), pc.content, pc.created_at, pc.updated_at,
			COALESCE(pc.moderator_hidden, 0), COALESCE(pc.moderation_reason_code, ''), COALESCE(pc.moderated_by_user_id, 0), COALESCE(pc.moderated_at, '')
		FROM post_comments pc
		JOIN users u ON pc.user_id = u.id
		WHERE pc.post_id = ? AND COALESCE(pc.moderator_hidden, 0) = 0 AND %s
		ORDER BY pc.created_at ASC, pc.id ASC
	`, publicUserNotBannedClause("u"))
		args = []interface{}{postID, moderationNowRFC3339()}
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []*models.Comment
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return comments, nil
}

func scanCommentRow(row scanner) (*models.Comment, error) {
	return scanComment(row)
}

func scanComment(row scanner) (*models.Comment, error) {
	var comment models.Comment
	var createdAt string
	var updatedAt string
	var moderatorHidden int
	var moderationReasonCode string
	var moderatedByUserID int64
	var moderatedAt string
	if err := row.Scan(
		&comment.ID,
		&comment.PostID,
		&comment.UserID,
		&comment.AuthorName,
		&comment.AuthorAvatar,
		&comment.Content,
		&createdAt,
		&updatedAt,
		&moderatorHidden,
		&moderationReasonCode,
		&moderatedByUserID,
		&moderatedAt,
	); err != nil {
		return nil, err
	}
	comment.AuthorUserID = comment.UserID
	comment.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	comment.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	comment.ModeratorHidden = moderatorHidden == 1
	comment.ModerationReasonCode = moderationReasonCode
	comment.ModeratedByUserID = moderatedByUserID
	if moderatedAt != "" {
		comment.ModeratedAt, _ = time.Parse(time.RFC3339, moderatedAt)
	}
	return &comment, nil
}

func (db *DB) TogglePostLike(postID string, userID int64) (newLikesCount int, isLiked bool, err error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()

	var publishedPostID string
	err = tx.QueryRow(fmt.Sprintf(`
		SELECT p.id
		FROM posts p
		JOIN users u ON p.user_id = u.id
		WHERE p.id = ? AND p.status = 'published' AND COALESCE(p.moderator_hidden, 0) = 0 AND %s
		LIMIT 1
	`, publicUserNotBannedClause("u")), postID, moderationNowRFC3339()).Scan(&publishedPostID)
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
