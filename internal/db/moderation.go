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

func listModerationActionsQuery(limitClause string) string {
	return `
		SELECT
			a.id,
			a.actor_user_id,
			COALESCE(actor.preferred_username, '') AS actor_name,
			COALESCE(a.target_user_id, 0),
			COALESCE(a.target_capture_id, ''),
			COALESCE(a.target_post_id, ''),
			COALESCE(a.target_comment_id, ''),
			a.action_kind,
			COALESCE(a.reason_code, ''),
			COALESCE(a.note, ''),
			COALESCE(a.meta_json, ''),
			a.created_at
		FROM moderation_actions a
		LEFT JOIN users actor ON actor.id = a.actor_user_id
		WHERE a.target_user_id = ?
		ORDER BY a.created_at DESC
	` + limitClause
}

func scanModerationAction(rows scanner) (*models.ModerationAction, error) {
	var (
		action       models.ModerationAction
		targetUserID sql.NullInt64
		createdAtRaw string
	)
	if err := rows.Scan(
		&action.ID,
		&action.ActorUserID,
		&action.ActorName,
		&targetUserID,
		&action.TargetCaptureID,
		&action.TargetPostID,
		&action.TargetCommentID,
		&action.ActionKind,
		&action.ReasonCode,
		&action.Note,
		&action.MetaJSON,
		&createdAtRaw,
	); err != nil {
		return nil, err
	}
	if targetUserID.Valid {
		action.TargetUserID = targetUserID.Int64
	}
	if createdAtRaw != "" {
		parsed, err := time.Parse(time.RFC3339, createdAtRaw)
		if err != nil {
			return nil, err
		}
		action.CreatedAt = parsed.UTC()
	}
	return &action, nil
}

func (db *DB) ListModerationActionsByTargetUser(targetUserID int64, limit int, offset int) ([]*models.ModerationAction, error) {
	rows, err := db.Query(listModerationActionsQuery(` LIMIT ? OFFSET ?`), targetUserID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []*models.ModerationAction
	for rows.Next() {
		action, err := scanModerationAction(rows)
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return actions, nil
}

func (db *DB) CountModerationActionsByTargetUser(targetUserID int64) (int, error) {
	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM moderation_actions WHERE target_user_id = ?`, targetUserID).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func loadTargetUserIDForContentTx(tx *sql.Tx, tableName string, idColumn string, targetID string) (int64, error) {
	var targetUserID int64
	query := fmt.Sprintf(`SELECT user_id FROM %s WHERE %s = ?`, tableName, idColumn)
	if err := tx.QueryRow(query, targetID).Scan(&targetUserID); err != nil {
		return 0, err
	}
	return targetUserID, nil
}

func (db *DB) ApplyCaptureManualModeratorTaxonomy(
	captureID string,
	moderatorUserID int64,
	species []*models.CaptureMushroomSpecies,
	note string,
) (*models.CaptureMushroomAnalysis, error) {
	if len(species) == 0 {
		return nil, fmt.Errorf("at least one species is required")
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	targetUserID, err := loadTargetUserIDForContentTx(tx, "photo_captures", "id", captureID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}

	now := time.Now().UTC()
	first := species[0]
	rawJSON := moderationMetaJSON(map[string]interface{}{
		"species": species,
		"source":  CaptureAnalysisSourceModeratorManualOverride,
	})
	analysis := &models.CaptureMushroomAnalysis{
		CaptureID:                captureID,
		HasMushrooms:             true,
		PrimaryLatinName:         strings.TrimSpace(first.LatinName),
		PrimaryCzechOfficialName: strings.TrimSpace(first.CzechOfficialName),
		PrimaryProbability:       first.Probability,
		ModelCode:                "manual_override",
		ReviewSource:             CaptureAnalysisSourceModeratorManualOverride,
		ReviewedByUserID:         moderatorUserID,
		ReviewedAt:               now,
		RawJSON:                  rawJSON,
		AnalyzedAt:               now,
	}

	if err := upsertCaptureMushroomAnalysisTx(tx, analysis); err != nil {
		return nil, err
	}
	if err := replaceCaptureMushroomSpeciesTx(tx, captureID, species); err != nil {
		return nil, err
	}
	if err := recordModerationActionTx(tx, &models.ModerationAction{
		ActorUserID:     moderatorUserID,
		TargetUserID:    targetUserID,
		TargetCaptureID: captureID,
		ActionKind:      "capture_taxonomy_updated",
		ReasonCode:      "manual_taxonomy_override",
		Note:            strings.TrimSpace(note),
		MetaJSON: moderationMetaJSON(map[string]interface{}{
			"species_count": len(species),
			"model_code":    analysis.ModelCode,
			"review_source": analysis.ReviewSource,
		}),
		CreatedAt: now,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return analysis, nil
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

	targetUserID, err := loadTargetUserIDForContentTx(tx, tableName, idColumn, targetID)
	if err != nil {
		if err == sql.ErrNoRows {
			return sql.ErrNoRows
		}
		return err
	}

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
		ActorUserID:  actorUserID,
		TargetUserID: targetUserID,
		ActionKind:   actionKind,
		ReasonCode:   reasonCode,
		Note:         note,
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
