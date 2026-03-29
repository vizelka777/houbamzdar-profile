package chatidentity

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

type User struct {
	ID                 int64     `json:"id"`
	PreferredUsername  string    `json:"preferred_username"`
	Picture            string    `json:"picture"`
	EmailVerified      bool      `json:"email_verified"`
	IsModerator        bool      `json:"is_moderator"`
	IsAdmin            bool      `json:"is_admin"`
	BannedUntil        time.Time `json:"banned_until,omitempty"`
	CommentsMutedUntil time.Time `json:"comments_muted_until,omitempty"`
}

func New(url string, token string) (*DB, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, fmt.Errorf("identity db url is required")
	}
	if strings.TrimSpace(token) != "" {
		url = fmt.Sprintf("%s?authToken=%s", url, token)
	}

	sqlDB, err := sql.Open("libsql", url)
	if err != nil {
		return nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}
	return &DB{DB: sqlDB}, nil
}

func (db *DB) GetUser(userID int64) (*User, error) {
	var (
		user             User
		bannedUntilRaw   sql.NullString
		commentsMutedRaw sql.NullString
	)
	err := db.QueryRow(`
		SELECT
			id,
			COALESCE(preferred_username, ''),
			COALESCE(picture, ''),
			COALESCE(email_verified, 0),
			COALESCE(is_moderator, 0),
			COALESCE(is_admin, 0),
			banned_until,
			comments_muted_until
		FROM users
		WHERE id = ?
		LIMIT 1
	`, userID).Scan(
		&user.ID,
		&user.PreferredUsername,
		&user.Picture,
		&user.EmailVerified,
		&user.IsModerator,
		&user.IsAdmin,
		&bannedUntilRaw,
		&commentsMutedRaw,
	)
	if err != nil {
		return nil, err
	}

	if bannedUntilRaw.Valid && bannedUntilRaw.String != "" {
		user.BannedUntil, _ = time.Parse(time.RFC3339, bannedUntilRaw.String)
	}
	if commentsMutedRaw.Valid && commentsMutedRaw.String != "" {
		user.CommentsMutedUntil, _ = time.Parse(time.RFC3339, commentsMutedRaw.String)
	}
	return &user, nil
}

func (db *DB) SearchUsers(query string, excludeUserID int64, limit int) ([]*User, error) {
	query = strings.TrimSpace(query)
	if limit <= 0 {
		limit = 12
	}
	if limit > 50 {
		limit = 50
	}

	args := []interface{}{excludeUserID}
	where := "WHERE id != ?"
	if query != "" {
		where += " AND lower(COALESCE(preferred_username, '')) LIKE ?"
		args = append(args, "%"+strings.ToLower(query)+"%")
	}
	args = append(args, limit)

	rows, err := db.Query(`
		SELECT
			id,
			COALESCE(preferred_username, ''),
			COALESCE(picture, ''),
			COALESCE(email_verified, 0),
			COALESCE(is_moderator, 0),
			COALESCE(is_admin, 0),
			COALESCE(banned_until, ''),
			COALESCE(comments_muted_until, '')
		FROM users
	`+where+`
		ORDER BY lower(COALESCE(preferred_username, '')) ASC, id ASC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]*User, 0, limit)
	for rows.Next() {
		var (
			user             User
			bannedUntilRaw   string
			commentsMutedRaw string
		)
		if err := rows.Scan(
			&user.ID,
			&user.PreferredUsername,
			&user.Picture,
			&user.EmailVerified,
			&user.IsModerator,
			&user.IsAdmin,
			&bannedUntilRaw,
			&commentsMutedRaw,
		); err != nil {
			return nil, err
		}
		if bannedUntilRaw != "" {
			user.BannedUntil, _ = time.Parse(time.RFC3339, bannedUntilRaw)
		}
		if commentsMutedRaw != "" {
			user.CommentsMutedUntil, _ = time.Parse(time.RFC3339, commentsMutedRaw)
		}
		if userIsActivelyBanned(&user) {
			continue
		}
		users = append(users, &user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

func userIsActivelyBanned(user *User) bool {
	return user != nil && !user.BannedUntil.IsZero() && user.BannedUntil.After(time.Now().UTC())
}

func userCommentsAreMuted(user *User) bool {
	return user != nil && !user.CommentsMutedUntil.IsZero() && user.CommentsMutedUntil.After(time.Now().UTC())
}
