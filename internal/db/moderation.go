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

func parseRFC3339Value(raw string) time.Time {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339, raw)
	return parsed.UTC()
}

func (db *DB) ListModerationHiddenCaptures(limit int, offset int) ([]*models.Capture, error) {
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
			c.status,
			COALESCE(c.public_storage_key, ''),
			COALESCE(c.published_at, ''),
			COALESCE(c.coordinates_free, 0),
			COALESCE(gi.kraj_name, ''),
			COALESCE(ma.primary_latin_name, ''),
			COALESCE(ma.primary_czech_name, ''),
			COALESCE(ma.primary_probability, 0),
			COALESCE(c.moderation_reason_code, ''),
			COALESCE((
				SELECT COALESCE(a.note, '')
				FROM moderation_actions a
				WHERE a.target_capture_id = c.id
					AND a.action_kind = 'capture_visibility_updated'
				ORDER BY a.created_at DESC
				LIMIT 1
			), ''),
			COALESCE(c.moderated_by_user_id, 0),
			COALESCE(mod.preferred_username, ''),
			COALESCE(c.moderated_at, '')
		FROM photo_captures c
		JOIN users u ON u.id = c.user_id
		LEFT JOIN users mod ON mod.id = c.moderated_by_user_id
		LEFT JOIN capture_geo_index gi ON gi.capture_id = c.id
		LEFT JOIN capture_mushroom_analysis ma ON ma.capture_id = c.id
		WHERE c.status = 'published'
			AND c.public_storage_key IS NOT NULL
			AND c.public_storage_key != ''
			AND COALESCE(c.moderator_hidden, 0) = 1
		ORDER BY COALESCE(c.moderated_at, c.published_at, c.uploaded_at) DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var captures []*models.Capture
	for rows.Next() {
		var (
			c               models.Capture
			capturedAtRaw   string
			uploadedAtRaw   string
			publishedAtRaw  string
			coordinatesFree int
			moderatedAtRaw  string
		)
		if err := rows.Scan(
			&c.ID,
			&c.UserID,
			&c.AuthorName,
			&c.AuthorAvatar,
			&c.ClientLocalID,
			&c.OriginalFileName,
			&c.ContentType,
			&c.SizeBytes,
			&c.Width,
			&c.Height,
			&capturedAtRaw,
			&uploadedAtRaw,
			&c.Status,
			&c.PublicStorageKey,
			&publishedAtRaw,
			&coordinatesFree,
			&c.KrajName,
			&c.MushroomPrimaryLatinName,
			&c.MushroomPrimaryCzechName,
			&c.MushroomPrimaryProbability,
			&c.ModerationReasonCode,
			&c.ModerationNote,
			&c.ModeratedByUserID,
			&c.ModeratedByName,
			&moderatedAtRaw,
		); err != nil {
			return nil, err
		}
		c.AuthorUserID = c.UserID
		c.CoordinatesFree = coordinatesFree == 1
		c.CapturedAt = parseRFC3339Value(capturedAtRaw)
		c.UploadedAt = parseRFC3339Value(uploadedAtRaw)
		c.PublishedAt = parseRFC3339Value(publishedAtRaw)
		c.ModeratedAt = parseRFC3339Value(moderatedAtRaw)
		captures = append(captures, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return captures, nil
}

func (db *DB) CountModerationHiddenCaptures() (int, error) {
	var total int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM photo_captures
		WHERE status = 'published'
			AND public_storage_key IS NOT NULL
			AND public_storage_key != ''
			AND COALESCE(moderator_hidden, 0) = 1
	`).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (db *DB) ListModerationHiddenPosts(limit int, offset int, viewerUserID int64) ([]*models.Post, error) {
	rows, err := db.Query(`
		SELECT
			p.id,
			p.user_id,
			COALESCE(u.preferred_username, ''),
			COALESCE(u.picture, ''),
			p.content,
			p.status,
			p.created_at,
			p.updated_at,
			COALESCE(p.moderation_reason_code, ''),
			COALESCE((
				SELECT COALESCE(a.note, '')
				FROM moderation_actions a
				WHERE a.target_post_id = p.id
					AND a.action_kind = 'post_visibility_updated'
				ORDER BY a.created_at DESC
				LIMIT 1
			), ''),
			COALESCE(p.moderated_by_user_id, 0),
			COALESCE(mod.preferred_username, ''),
			COALESCE(p.moderated_at, '')
		FROM posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN users mod ON mod.id = p.moderated_by_user_id
		WHERE p.status = 'published'
			AND COALESCE(p.moderator_hidden, 0) = 1
		ORDER BY COALESCE(p.moderated_at, p.updated_at, p.created_at) DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []*models.Post
	for rows.Next() {
		var (
			post           models.Post
			createdAtRaw   string
			updatedAtRaw   string
			moderatedAtRaw string
		)
		if err := rows.Scan(
			&post.ID,
			&post.UserID,
			&post.AuthorName,
			&post.AuthorAvatar,
			&post.Content,
			&post.Status,
			&createdAtRaw,
			&updatedAtRaw,
			&post.ModerationReasonCode,
			&post.ModerationNote,
			&post.ModeratedByUserID,
			&post.ModeratedByName,
			&moderatedAtRaw,
		); err != nil {
			return nil, err
		}
		post.AuthorUserID = post.UserID
		post.CreatedAt = parseRFC3339Value(createdAtRaw)
		post.UpdatedAt = parseRFC3339Value(updatedAtRaw)
		post.ModeratedAt = parseRFC3339Value(moderatedAtRaw)
		posts = append(posts, &post)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, post := range posts {
		captures, err := db.getCapturesForPost(post.ID, viewerUserID, true)
		if err != nil {
			return nil, err
		}
		post.Captures = captures
	}

	return posts, nil
}

func (db *DB) CountModerationHiddenPosts() (int, error) {
	var total int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM posts
		WHERE status = 'published'
			AND COALESCE(moderator_hidden, 0) = 1
	`).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (db *DB) ListModerationHiddenComments(limit int, offset int) ([]*models.ModerationHiddenComment, error) {
	rows, err := db.Query(`
		SELECT
			pc.id,
			pc.post_id,
			COALESCE(p.content, ''),
			COALESCE(p.user_id, 0),
			COALESCE(post_author.preferred_username, ''),
			pc.user_id,
			COALESCE(comment_author.preferred_username, ''),
			COALESCE(comment_author.picture, ''),
			pc.content,
			pc.created_at,
			pc.updated_at,
			COALESCE(pc.moderation_reason_code, ''),
			COALESCE((
				SELECT COALESCE(a.note, '')
				FROM moderation_actions a
				WHERE a.target_comment_id = pc.id
					AND a.action_kind = 'comment_visibility_updated'
				ORDER BY a.created_at DESC
				LIMIT 1
			), ''),
			COALESCE(pc.moderated_by_user_id, 0),
			COALESCE(mod.preferred_username, ''),
			COALESCE(pc.moderated_at, '')
		FROM post_comments pc
		JOIN users comment_author ON comment_author.id = pc.user_id
		JOIN posts p ON p.id = pc.post_id
		JOIN users post_author ON post_author.id = p.user_id
		LEFT JOIN users mod ON mod.id = pc.moderated_by_user_id
		WHERE COALESCE(pc.moderator_hidden, 0) = 1
		ORDER BY COALESCE(pc.moderated_at, pc.updated_at, pc.created_at) DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []*models.ModerationHiddenComment
	for rows.Next() {
		var (
			comment        models.ModerationHiddenComment
			createdAtRaw   string
			updatedAtRaw   string
			moderatedAtRaw string
		)
		if err := rows.Scan(
			&comment.ID,
			&comment.PostID,
			&comment.PostContent,
			&comment.PostAuthorUserID,
			&comment.PostAuthorName,
			&comment.AuthorUserID,
			&comment.AuthorName,
			&comment.AuthorAvatar,
			&comment.Content,
			&createdAtRaw,
			&updatedAtRaw,
			&comment.ModerationReasonCode,
			&comment.ModerationNote,
			&comment.ModeratedByUserID,
			&comment.ModeratedByName,
			&moderatedAtRaw,
		); err != nil {
			return nil, err
		}
		comment.CreatedAt = parseRFC3339Value(createdAtRaw)
		comment.UpdatedAt = parseRFC3339Value(updatedAtRaw)
		comment.ModeratedAt = parseRFC3339Value(moderatedAtRaw)
		comments = append(comments, &comment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return comments, nil
}

func (db *DB) CountModerationHiddenComments() (int, error) {
	var total int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM post_comments
		WHERE COALESCE(moderator_hidden, 0) = 1
	`).Scan(&total); err != nil {
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
			moderated_by_user_id = ?,
			moderated_at = ?
		WHERE id = ?
	`,
		isModerator,
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
