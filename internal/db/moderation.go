package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/houbamzdar/bff/internal/models"
)

func moderationMetaJSON(payload map[string]interface{}) string {
	if len(payload) == 0 {
		return ""
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func recordModerationActionTx(tx *sql.Tx, action *models.ModerationAction) error {
	if tx == nil {
		return fmt.Errorf("transaction is required")
	}
	if action == nil {
		return fmt.Errorf("moderation action is required")
	}
	if action.ActorUserID == 0 {
		return fmt.Errorf("actor_user_id is required")
	}
	if strings.TrimSpace(action.ActionKind) == "" {
		return fmt.Errorf("action_kind is required")
	}
	if action.ID == "" {
		action.ID = uuid.New().String()
	}
	if action.CreatedAt.IsZero() {
		action.CreatedAt = time.Now().UTC()
	}

	_, err := tx.Exec(`
		INSERT INTO moderation_actions (
			id, actor_user_id, target_user_id, target_capture_id, target_post_id, target_comment_id,
			action_kind, reason_code, note, meta_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		action.ID,
		action.ActorUserID,
		nullIfZeroInt64(action.TargetUserID),
		nullIfEmpty(action.TargetCaptureID),
		nullIfEmpty(action.TargetPostID),
		nullIfEmpty(action.TargetCommentID),
		action.ActionKind,
		nullIfEmpty(strings.TrimSpace(action.ReasonCode)),
		nullIfEmpty(strings.TrimSpace(action.Note)),
		nullIfEmpty(action.MetaJSON),
		action.CreatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (db *DB) SetUserRestrictions(
	targetUserID int64,
	actorUserID int64,
	bannedUntil time.Time,
	commentsMutedUntil time.Time,
	publishingSuspendedUntil time.Time,
	reasonCode string,
	note string,
) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	result, err := tx.Exec(`
		UPDATE users
		SET banned_until = ?,
			comments_muted_until = ?,
			publishing_suspended_until = ?,
			moderation_note = ?,
			moderated_by_user_id = ?,
			moderated_at = ?
		WHERE id = ?
	`,
		nullIfZeroTime(bannedUntil),
		nullIfZeroTime(commentsMutedUntil),
		nullIfZeroTime(publishingSuspendedUntil),
		nullIfEmpty(strings.TrimSpace(note)),
		nullIfZeroInt64(actorUserID),
		now.Format(time.RFC3339),
		targetUserID,
	)
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

	action := &models.ModerationAction{
		ActorUserID:  actorUserID,
		TargetUserID: targetUserID,
		ActionKind:   "user_restrictions_updated",
		ReasonCode:   strings.TrimSpace(reasonCode),
		Note:         strings.TrimSpace(note),
		MetaJSON: moderationMetaJSON(map[string]interface{}{
			"banned_until":               nullIfZeroTime(bannedUntil),
			"comments_muted_until":       nullIfZeroTime(commentsMutedUntil),
			"publishing_suspended_until": nullIfZeroTime(publishingSuspendedUntil),
		}),
		CreatedAt: now,
	}
	if err := recordModerationActionTx(tx, action); err != nil {
		return err
	}

	return tx.Commit()
}

func (db *DB) SetUserRoles(
	targetUserID int64,
	actorUserID int64,
	isModerator bool,
	isAdmin bool,
	reasonCode string,
	note string,
) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	result, err := tx.Exec(`
		UPDATE users
		SET is_moderator = ?,
			is_admin = ?,
			moderation_note = ?,
			moderated_by_user_id = ?,
			moderated_at = ?
		WHERE id = ?
	`,
		isModerator,
		isAdmin,
		nullIfEmpty(strings.TrimSpace(note)),
		nullIfZeroInt64(actorUserID),
		now.Format(time.RFC3339),
		targetUserID,
	)
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

	action := &models.ModerationAction{
		ActorUserID:  actorUserID,
		TargetUserID: targetUserID,
		ActionKind:   "user_roles_updated",
		ReasonCode:   strings.TrimSpace(reasonCode),
		Note:         strings.TrimSpace(note),
		MetaJSON: moderationMetaJSON(map[string]interface{}{
			"is_moderator": isModerator,
			"is_admin":     isAdmin,
		}),
		CreatedAt: now,
	}
	if err := recordModerationActionTx(tx, action); err != nil {
		return err
	}

	return tx.Commit()
}

func (db *DB) SetCaptureModeratorHidden(captureID string, actorUserID int64, hidden bool, reasonCode string, note string) error {
	return db.setContentModeratorHidden(
		"photo_captures",
		"id",
		captureID,
		actorUserID,
		hidden,
		strings.TrimSpace(reasonCode),
		strings.TrimSpace(note),
		"capture_visibility_updated",
		func(action *models.ModerationAction) {
			action.TargetCaptureID = captureID
		},
	)
}

func (db *DB) SetPostModeratorHidden(postID string, actorUserID int64, hidden bool, reasonCode string, note string) error {
	return db.setContentModeratorHidden(
		"posts",
		"id",
		postID,
		actorUserID,
		hidden,
		strings.TrimSpace(reasonCode),
		strings.TrimSpace(note),
		"post_visibility_updated",
		func(action *models.ModerationAction) {
			action.TargetPostID = postID
		},
	)
}

func (db *DB) SetCommentModeratorHidden(commentID string, actorUserID int64, hidden bool, reasonCode string, note string) error {
	return db.setContentModeratorHidden(
		"post_comments",
		"id",
		commentID,
		actorUserID,
		hidden,
		strings.TrimSpace(reasonCode),
		strings.TrimSpace(note),
		"comment_visibility_updated",
		func(action *models.ModerationAction) {
			action.TargetCommentID = commentID
		},
	)
}

func (db *DB) setContentModeratorHidden(
	tableName string,
	idColumn string,
	targetID string,
	actorUserID int64,
	hidden bool,
	reasonCode string,
	note string,
	actionKind string,
	bindTarget func(*models.ModerationAction),
) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	query := fmt.Sprintf(`
		UPDATE %s
		SET moderator_hidden = ?,
			moderation_reason_code = ?,
			moderated_by_user_id = ?,
			moderated_at = ?
		WHERE %s = ?
	`, tableName, idColumn)
	result, err := tx.Exec(
		query,
		hidden,
		nullIfEmpty(reasonCode),
		nullIfZeroInt64(actorUserID),
		now.Format(time.RFC3339),
		targetID,
	)
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

	action := &models.ModerationAction{
		ActorUserID: actorUserID,
		ActionKind:  actionKind,
		ReasonCode:  reasonCode,
		Note:        note,
		MetaJSON: moderationMetaJSON(map[string]interface{}{
			"hidden": hidden,
		}),
		CreatedAt: now,
	}
	if bindTarget != nil {
		bindTarget(action)
	}
	if err := recordModerationActionTx(tx, action); err != nil {
		return err
	}

	return tx.Commit()
}
