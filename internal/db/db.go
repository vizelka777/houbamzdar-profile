package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/models"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

type DB struct {
	*sql.DB
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
	}

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) UpsertUser(claims *models.OIDCClaims) (*models.User, bool, error) {
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

	if !exists {
		res, err := db.Exec(`
			INSERT INTO users (idp_issuer, idp_sub, preferred_username, email, email_verified, phone_number, phone_number_verified, picture, last_idp_sync_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		`, claims.Iss, claims.Sub, claims.PreferredUsername, claims.Email, ev, claims.PhoneNumber, pv, claims.Picture)
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
				last_idp_sync_at = datetime('now'),
				updated_at = datetime('now')
			WHERE id = ?
		`, claims.PreferredUsername, claims.Email, ev, claims.PhoneNumber, pv, claims.Picture, user.ID)
		if err != nil {
			return nil, false, err
		}
	}

	// Fetch updated user
	err = db.QueryRow("SELECT id, idp_issuer, idp_sub, COALESCE(preferred_username, ''), COALESCE(email, ''), email_verified, COALESCE(phone_number, ''), phone_number_verified, COALESCE(picture, ''), COALESCE(about_me, '') FROM users WHERE id = ?", user.ID).Scan(
		&user.ID, &user.IDPIssuer, &user.IDPSub, &user.PreferredUsername, &user.Email, &user.EmailVerified, &user.PhoneNumber, &user.PhoneNumberVerified, &user.Picture, &user.AboutMe,
	)
	if err != nil {
		return nil, false, err
	}

	return &user, !exists, nil
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
	err := db.QueryRow("SELECT id, idp_issuer, idp_sub, COALESCE(preferred_username, ''), COALESCE(email, ''), email_verified, COALESCE(phone_number, ''), phone_number_verified, COALESCE(picture, ''), COALESCE(about_me, '') FROM users WHERE id = ?", id).Scan(
		&user.ID, &user.IDPIssuer, &user.IDPSub, &user.PreferredUsername, &user.Email, &user.EmailVerified, &user.PhoneNumber, &user.PhoneNumberVerified, &user.Picture, &user.AboutMe,
	)
	return &user, err
}

func (db *DB) UpdateAboutMe(id int64, aboutMe string) error {
	_, err := db.Exec("UPDATE users SET about_me = ?, updated_at = datetime('now') WHERE id = ?", aboutMe, id)
	return err
}
