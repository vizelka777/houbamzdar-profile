package db

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/houbamzdar/bff/internal/models"
)

func (db *DB) GetAdminOverview() (*models.AdminOverview, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	overview := &models.AdminOverview{}
	err := db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN COALESCE(is_admin, 0) = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN COALESCE(is_moderator, 0) = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN COALESCE(is_admin, 0) = 1 OR COALESCE(is_moderator, 0) = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN COALESCE(banned_until, '') != '' AND banned_until > ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN COALESCE(comments_muted_until, '') != '' AND comments_muted_until > ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN COALESCE(publishing_suspended_until, '') != '' AND publishing_suspended_until > ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE
				WHEN (COALESCE(banned_until, '') != '' AND banned_until > ?)
					OR (COALESCE(comments_muted_until, '') != '' AND comments_muted_until > ?)
					OR (COALESCE(publishing_suspended_until, '') != '' AND publishing_suspended_until > ?)
				THEN 1
				ELSE 0
			END), 0),
			COALESCE((
				SELECT COUNT(*)
				FROM posts
				WHERE status = 'published'
					AND COALESCE(moderator_hidden, 0) = 0
			), 0),
			COALESCE((
				SELECT COUNT(*)
				FROM photo_captures
				WHERE status = 'published'
					AND COALESCE(moderator_hidden, 0) = 0
					AND public_storage_key IS NOT NULL
					AND public_storage_key != ''
			), 0),
			COALESCE((
				SELECT COUNT(*)
				FROM posts
				WHERE COALESCE(moderator_hidden, 0) = 1
			), 0),
			COALESCE((
				SELECT COUNT(*)
				FROM post_comments
				WHERE COALESCE(moderator_hidden, 0) = 1
			), 0),
			COALESCE((
				SELECT COUNT(*)
				FROM photo_captures
				WHERE COALESCE(moderator_hidden, 0) = 1
			), 0),
			COALESCE((
				SELECT COUNT(*)
				FROM photo_captures
				WHERE COALESCE(publication_review_status, 'none') = 'pending_validation'
			), 0),
			COALESCE((
				SELECT COUNT(*)
				FROM photo_captures
				WHERE COALESCE(publication_review_status, 'none') = 'error'
			), 0),
			COALESCE((
				SELECT COUNT(*)
				FROM photo_captures
				WHERE COALESCE(publication_review_status, 'none') = 'rejected'
			), 0)
		FROM users
	`, now, now, now, now, now, now).Scan(
		&overview.TotalUsers,
		&overview.AdminUsers,
		&overview.ModeratorUsers,
		&overview.StaffUsers,
		&overview.BannedUsers,
		&overview.CommentsMutedUsers,
		&overview.PublishingSuspendedUsers,
		&overview.RestrictedUsers,
		&overview.PublicPosts,
		&overview.PublicCaptures,
		&overview.HiddenPosts,
		&overview.HiddenComments,
		&overview.HiddenCaptures,
		&overview.PendingPublicationReview,
		&overview.FailedPublicationReview,
		&overview.RejectedPublicationReview,
	)
	if err != nil {
		return nil, err
	}
	return overview, nil
}

func buildAdminUsersWhereClause(filters models.AdminUserListFilters) (string, []interface{}) {
	clauses := []string{"1 = 1"}
	params := make([]interface{}, 0, 8)

	query := strings.TrimSpace(filters.Query)
	if query != "" {
		searchClauses := []string{
			"lower(COALESCE(preferred_username, '')) LIKE ?",
			"lower(COALESCE(email, '')) LIKE ?",
		}
		searchValue := "%" + strings.ToLower(query) + "%"
		params = append(params, searchValue, searchValue)
		if numericID, err := strconv.ParseInt(query, 10, 64); err == nil && numericID > 0 {
			searchClauses = append(searchClauses, "id = ?")
			params = append(params, numericID)
		}
		clauses = append(clauses, "("+strings.Join(searchClauses, " OR ")+")")
	}

	switch strings.TrimSpace(filters.Role) {
	case "admin":
		clauses = append(clauses, "COALESCE(is_admin, 0) = 1")
	case "moderator":
		clauses = append(clauses, "COALESCE(is_moderator, 0) = 1 AND COALESCE(is_admin, 0) = 0")
	case "staff":
		clauses = append(clauses, "(COALESCE(is_admin, 0) = 1 OR COALESCE(is_moderator, 0) = 1)")
	case "user":
		clauses = append(clauses, "COALESCE(is_admin, 0) = 0 AND COALESCE(is_moderator, 0) = 0")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	switch strings.TrimSpace(filters.Status) {
	case "restricted":
		clauses = append(clauses, `(
			(COALESCE(banned_until, '') != '' AND banned_until > ?)
			OR (COALESCE(comments_muted_until, '') != '' AND comments_muted_until > ?)
			OR (COALESCE(publishing_suspended_until, '') != '' AND publishing_suspended_until > ?)
		)`)
		params = append(params, now, now, now)
	case "banned":
		clauses = append(clauses, "COALESCE(banned_until, '') != '' AND banned_until > ?")
		params = append(params, now)
	case "comments_muted":
		clauses = append(clauses, "COALESCE(comments_muted_until, '') != '' AND comments_muted_until > ?")
		params = append(params, now)
	case "publishing_suspended":
		clauses = append(clauses, "COALESCE(publishing_suspended_until, '') != '' AND publishing_suspended_until > ?")
		params = append(params, now)
	}

	return " WHERE " + strings.Join(clauses, " AND "), params
}

func scanAdminUserSummary(rows scanner) (*models.AdminUserSummary, error) {
	var (
		user                        models.AdminUserSummary
		bannedUntilRaw              string
		commentsMutedUntilRaw       string
		publishingSuspendedUntilRaw string
		moderatedAtRaw              string
	)
	if err := rows.Scan(
		&user.ID,
		&user.PreferredUsername,
		&user.Email,
		&user.Picture,
		&user.IsModerator,
		&user.IsAdmin,
		&bannedUntilRaw,
		&commentsMutedUntilRaw,
		&publishingSuspendedUntilRaw,
		&user.ModerationNote,
		&user.ModeratedByUserID,
		&moderatedAtRaw,
		&user.PublicPostsCount,
		&user.PublicCapturesCount,
	); err != nil {
		return nil, err
	}

	user.BannedUntil = parseRFC3339Value(bannedUntilRaw)
	user.CommentsMutedUntil = parseRFC3339Value(commentsMutedUntilRaw)
	user.PublishingSuspendedUntil = parseRFC3339Value(publishingSuspendedUntilRaw)
	user.ModeratedAt = parseRFC3339Value(moderatedAtRaw)
	return &user, nil
}

func (db *DB) ListAdminUsers(filters models.AdminUserListFilters) ([]*models.AdminUserSummary, error) {
	whereClause, params := buildAdminUsersWhereClause(filters)
	query := `
		SELECT
			id,
			COALESCE(preferred_username, ''),
			COALESCE(email, ''),
			COALESCE(picture, ''),
			COALESCE(is_moderator, 0),
			COALESCE(is_admin, 0),
			COALESCE(banned_until, ''),
			COALESCE(comments_muted_until, ''),
			COALESCE(publishing_suspended_until, ''),
			COALESCE(moderation_note, ''),
			COALESCE(moderated_by_user_id, 0),
			COALESCE(moderated_at, ''),
			COALESCE((
				SELECT COUNT(*)
				FROM posts
				WHERE posts.user_id = users.id
					AND posts.status = 'published'
					AND COALESCE(posts.moderator_hidden, 0) = 0
			), 0),
			COALESCE((
				SELECT COUNT(*)
				FROM photo_captures
				WHERE photo_captures.user_id = users.id
					AND photo_captures.status = 'published'
					AND COALESCE(photo_captures.moderator_hidden, 0) = 0
					AND photo_captures.public_storage_key IS NOT NULL
					AND photo_captures.public_storage_key != ''
			), 0)
		FROM users
	` + whereClause + `
		ORDER BY COALESCE(is_admin, 0) DESC,
			COALESCE(is_moderator, 0) DESC,
			lower(COALESCE(preferred_username, '')) ASC,
			id ASC
		LIMIT ? OFFSET ?
	`
	params = append(params, filters.Limit, filters.Offset)

	rows, err := db.Query(query, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]*models.AdminUserSummary, 0, filters.Limit)
	for rows.Next() {
		user, err := scanAdminUserSummary(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

func (db *DB) CountAdminUsers(filters models.AdminUserListFilters) (int, error) {
	whereClause, params := buildAdminUsersWhereClause(filters)
	query := `SELECT COUNT(*) FROM users` + whereClause

	var total int
	if err := db.QueryRow(query, params...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (db *DB) BootstrapAdminRole(targetUserID int64, alsoModerator bool) error {
	result, err := db.Exec(`
		UPDATE users
		SET is_admin = 1,
			is_moderator = CASE
				WHEN ? THEN 1
				ELSE COALESCE(is_moderator, 0)
			END,
			moderated_at = ?
		WHERE id = ?
	`, alsoModerator, time.Now().UTC().Format(time.RFC3339), targetUserID)
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

func (db *DB) BootstrapAdminByPreferredUsername(username string, alsoModerator bool) (*models.User, error) {
	user, err := db.GetUserByPreferredUsername(username)
	if err != nil {
		return nil, err
	}
	if err := db.BootstrapAdminRole(user.ID, alsoModerator); err != nil {
		return nil, err
	}
	return db.GetUser(user.ID)
}

func (db *DB) BootstrapAdminByUserID(userID int64, alsoModerator bool) (*models.User, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user id")
	}
	if err := db.BootstrapAdminRole(userID, alsoModerator); err != nil {
		return nil, err
	}
	return db.GetUser(userID)
}
