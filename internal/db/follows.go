package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/houbamzdar/bff/internal/models"
)

var ErrCannotFollowSelf = errors.New("cannot follow self")

func (db *DB) FollowUser(followerUserID int64, followedUserID int64) error {
	if followerUserID <= 0 || followedUserID <= 0 {
		return sql.ErrNoRows
	}
	if followerUserID == followedUserID {
		return ErrCannotFollowSelf
	}

	now := moderationNowRFC3339()
	var exists int
	if err := db.QueryRow(
		fmt.Sprintf(`SELECT 1 FROM users WHERE id = ? AND %s`, publicUserNotBannedClause("users")),
		followedUserID,
		now,
	).Scan(&exists); err != nil {
		return err
	}

	_, err := db.Exec(`
		INSERT OR IGNORE INTO user_follows (follower_user_id, followed_user_id, created_at)
		VALUES (?, ?, ?)
	`, followerUserID, followedUserID, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (db *DB) UnfollowUser(followerUserID int64, followedUserID int64) error {
	if followerUserID <= 0 || followedUserID <= 0 {
		return sql.ErrNoRows
	}

	_, err := db.Exec(`
		DELETE FROM user_follows
		WHERE follower_user_id = ? AND followed_user_id = ?
	`, followerUserID, followedUserID)
	return err
}

func (db *DB) CountFollowingUsers(followerUserID int64) (int, error) {
	now := moderationNowRFC3339()
	var total int
	err := db.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*)
		FROM user_follows uf
		JOIN users followed_users ON followed_users.id = uf.followed_user_id
		WHERE uf.follower_user_id = ? AND %s
	`, publicUserNotBannedClause("followed_users")), followerUserID, now).Scan(&total)
	return total, err
}

func (db *DB) ListFollowingUsers(followerUserID int64, limit int, offset int) ([]*models.FollowedUserProfile, error) {
	now := moderationNowRFC3339()
	rows, err := db.Query(fmt.Sprintf(`
		SELECT
			u.id,
			COALESCE(u.preferred_username, ''),
			COALESCE(u.picture, ''),
			COALESCE(u.about_me, ''),
			uf.created_at,
			(SELECT COUNT(*) FROM posts WHERE user_id = u.id AND status = 'published' AND COALESCE(moderator_hidden, 0) = 0),
			(SELECT COUNT(*) FROM photo_captures WHERE user_id = u.id AND status = 'published' AND COALESCE(moderator_hidden, 0) = 0 AND COALESCE(private_storage_key, '') != ''),
			(
				SELECT COUNT(*)
				FROM user_follows uf2
				JOIN users follower_users ON follower_users.id = uf2.follower_user_id
				WHERE uf2.followed_user_id = u.id AND %s
			)
		FROM user_follows uf
		JOIN users u ON u.id = uf.followed_user_id
		WHERE uf.follower_user_id = ? AND %s
		ORDER BY uf.created_at DESC, u.id DESC
		LIMIT ? OFFSET ?
	`, publicUserNotBannedClause("follower_users"), publicUserNotBannedClause("u")), now, followerUserID, now, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]*models.FollowedUserProfile, 0, limit)
	for rows.Next() {
		var (
			user          models.FollowedUserProfile
			followedAtRaw string
		)
		if err := rows.Scan(
			&user.ID,
			&user.PreferredUsername,
			&user.Picture,
			&user.AboutMe,
			&followedAtRaw,
			&user.PublicPostsCount,
			&user.PublicCapturesCount,
			&user.FollowersCount,
		); err != nil {
			return nil, err
		}
		user.FollowedAt, _ = time.Parse(time.RFC3339, followedAtRaw)
		users = append(users, &user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}
