package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/houbamzdar/bff/internal/models"
)

const (
	CapturePublicationReviewNone              = "none"
	CapturePublicationReviewPendingValidation = "pending_validation"
	CapturePublicationReviewApproved          = "approved"
	CapturePublicationReviewRejected          = "rejected"
	CapturePublicationReviewError             = "error"
)

const (
	CapturePublicationRejectMissingCoordinates = "missing_coordinates"
	CapturePublicationRejectOutsideCzechia     = "outside_czechia"
	CapturePublicationRejectNoMushrooms        = "no_mushrooms_detected"
)

const (
	CaptureAnalysisSourceAutomaticPublishValidation = "automatic_publish_validation"
	CaptureAnalysisSourceModeratorRecheck           = "moderator_recheck"
)

type PublicCaptureFilters struct {
	Limit        int
	Offset       int
	HasMushrooms *bool
	SpeciesQuery string
	KrajQuery    string
	OkresQuery   string
	ObecQuery    string
	Sort         string
}

func normalizeSearchText(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	replacer := strings.NewReplacer(
		"á", "a",
		"ä", "a",
		"č", "c",
		"ď", "d",
		"é", "e",
		"ě", "e",
		"ë", "e",
		"í", "i",
		"ľ", "l",
		"ĺ", "l",
		"ň", "n",
		"ó", "o",
		"ô", "o",
		"ö", "o",
		"ř", "r",
		"ŕ", "r",
		"š", "s",
		"ť", "t",
		"ú", "u",
		"ů", "u",
		"ü", "u",
		"ý", "y",
		"ž", "z",
	)
	return replacer.Replace(value)
}

func normalizePublicCaptureFilters(filters PublicCaptureFilters) PublicCaptureFilters {
	if filters.Limit <= 0 {
		filters.Limit = 24
	}
	if filters.Limit > 200 {
		filters.Limit = 200
	}
	if filters.Offset < 0 {
		filters.Offset = 0
	}

	filters.SpeciesQuery = normalizeSearchText(filters.SpeciesQuery)
	filters.KrajQuery = normalizeSearchText(filters.KrajQuery)
	filters.OkresQuery = normalizeSearchText(filters.OkresQuery)
	filters.ObecQuery = normalizeSearchText(filters.ObecQuery)

	switch filters.Sort {
	case "captured_desc", "probability_desc", "kraj_asc", "okres_asc", "obec_asc":
	default:
		filters.Sort = "published_desc"
	}

	return filters
}

func (db *DB) RequestCapturePublicationValidation(id string, userID int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		UPDATE photo_captures
		SET publication_review_status = ?,
			publication_review_reason_code = NULL,
			publication_review_last_error = NULL,
			publication_review_checked_at = NULL,
			publication_requested_at = ?,
			publication_review_attempts = 0,
			publication_review_next_attempt_at = ?
		WHERE id = ? AND user_id = ?
	`, CapturePublicationReviewPendingValidation, now, now, id, userID)
	return err
}

func (db *DB) GetCaptureByID(id string) (*models.Capture, error) {
	row := db.QueryRow(captureSelectColumns+` WHERE id = ? LIMIT 1`, id)
	return scanCaptureRow(row)
}

func (db *DB) ListCapturesPendingPublicationValidation(limit int) ([]*models.Capture, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := db.Query(
		captureSelectColumns+`
		WHERE status = 'private'
			AND publication_review_status = ?
			AND (
				publication_review_next_attempt_at IS NULL
				OR publication_review_next_attempt_at = ''
				OR publication_review_next_attempt_at <= ?
			)
		ORDER BY COALESCE(publication_requested_at, uploaded_at) ASC
		LIMIT ?`,
		CapturePublicationReviewPendingValidation,
		time.Now().UTC().Format(time.RFC3339),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var captures []*models.Capture
	for rows.Next() {
		capture, scanErr := scanCapture(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		captures = append(captures, capture)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return captures, nil
}

func (db *DB) MarkCapturePublicationValidationRetry(id string, lastError string, nextAttemptAt time.Time, attempts int) error {
	_, err := db.Exec(`
		UPDATE photo_captures
		SET publication_review_status = ?,
			publication_review_last_error = ?,
			publication_review_attempts = ?,
			publication_review_next_attempt_at = ?
		WHERE id = ?
	`, CapturePublicationReviewPendingValidation, lastError, attempts, nextAttemptAt.UTC().Format(time.RFC3339), id)
	return err
}

func (db *DB) MarkCapturePublicationValidationError(id string, lastError string, attempts int) error {
	_, err := db.Exec(`
		UPDATE photo_captures
		SET publication_review_status = ?,
			publication_review_last_error = ?,
			publication_review_attempts = ?,
			publication_review_checked_at = ?,
			publication_review_next_attempt_at = NULL
		WHERE id = ?
	`, CapturePublicationReviewError, lastError, attempts, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (db *DB) RejectCapturePublication(id string, reasonCode string) error {
	_, err := db.Exec(`
		UPDATE photo_captures
		SET publication_review_status = ?,
			publication_review_reason_code = ?,
			publication_review_last_error = NULL,
			publication_review_checked_at = ?,
			publication_review_next_attempt_at = NULL
		WHERE id = ?
	`, CapturePublicationReviewRejected, reasonCode, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func upsertCaptureMushroomAnalysisTx(tx *sql.Tx, analysis *models.CaptureMushroomAnalysis) error {
	if analysis == nil {
		return fmt.Errorf("mushroom analysis is required")
	}

	reviewSource := strings.TrimSpace(analysis.ReviewSource)
	if reviewSource == "" {
		reviewSource = CaptureAnalysisSourceAutomaticPublishValidation
	}

	if _, err := tx.Exec(`
		INSERT INTO capture_mushroom_analysis (
			capture_id, has_mushrooms, primary_latin_name, primary_latin_name_norm,
			primary_czech_name, primary_czech_name_norm, primary_probability, model_code,
			review_source, reviewed_by_user_id, reviewed_at, raw_json, analyzed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(capture_id) DO UPDATE SET
			has_mushrooms = excluded.has_mushrooms,
			primary_latin_name = excluded.primary_latin_name,
			primary_latin_name_norm = excluded.primary_latin_name_norm,
			primary_czech_name = excluded.primary_czech_name,
			primary_czech_name_norm = excluded.primary_czech_name_norm,
			primary_probability = excluded.primary_probability,
			model_code = excluded.model_code,
			review_source = excluded.review_source,
			reviewed_by_user_id = excluded.reviewed_by_user_id,
			reviewed_at = excluded.reviewed_at,
			raw_json = excluded.raw_json,
			analyzed_at = excluded.analyzed_at
	`,
		analysis.CaptureID,
		analysis.HasMushrooms,
		nullIfEmpty(analysis.PrimaryLatinName),
		nullIfEmpty(normalizeSearchText(analysis.PrimaryLatinName)),
		nullIfEmpty(analysis.PrimaryCzechOfficialName),
		nullIfEmpty(normalizeSearchText(analysis.PrimaryCzechOfficialName)),
		analysis.PrimaryProbability,
		nullIfEmpty(analysis.ModelCode),
		reviewSource,
		nullIfZeroInt64(analysis.ReviewedByUserID),
		nullIfZeroTime(analysis.ReviewedAt),
		nullIfEmpty(analysis.RawJSON),
		analysis.AnalyzedAt.UTC().Format(time.RFC3339),
	); err != nil {
		return err
	}

	return nil
}

func replaceCaptureMushroomSpeciesTx(tx *sql.Tx, captureID string, species []*models.CaptureMushroomSpecies) error {
	if _, err := tx.Exec(`DELETE FROM capture_mushroom_species WHERE capture_id = ?`, captureID); err != nil {
		return err
	}
	for _, item := range species {
		if _, err := tx.Exec(`
			INSERT INTO capture_mushroom_species (
				capture_id, latin_name, latin_name_norm, czech_official_name, czech_official_name_norm, probability
			) VALUES (?, ?, ?, ?, ?, ?)
		`,
			captureID,
			item.LatinName,
			normalizeSearchText(item.LatinName),
			nullIfEmpty(item.CzechOfficialName),
			nullIfEmpty(normalizeSearchText(item.CzechOfficialName)),
			item.Probability,
		); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) FinalizeCapturePublicationApproved(
	id string,
	userID int64,
	publicStorageKey string,
	analysis *models.CaptureMushroomAnalysis,
	species []*models.CaptureMushroomSpecies,
	geo *models.CaptureGeoIndex,
) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	analysis.ReviewSource = CaptureAnalysisSourceAutomaticPublishValidation
	analysis.ReviewedByUserID = 0
	analysis.ReviewedAt = time.Time{}
	if err := upsertCaptureMushroomAnalysisTx(tx, analysis); err != nil {
		return err
	}

	if err := replaceCaptureMushroomSpeciesTx(tx, id, species); err != nil {
		return err
	}

	if _, err := tx.Exec(`
		INSERT INTO capture_geo_index (
			capture_id, country_code, kraj_name, kraj_name_norm, okres_name, okres_name_norm,
			obec_name, obec_name_norm, raw_json, resolved_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(capture_id) DO UPDATE SET
			country_code = excluded.country_code,
			kraj_name = excluded.kraj_name,
			kraj_name_norm = excluded.kraj_name_norm,
			okres_name = excluded.okres_name,
			okres_name_norm = excluded.okres_name_norm,
			obec_name = excluded.obec_name,
			obec_name_norm = excluded.obec_name_norm,
			raw_json = excluded.raw_json,
			resolved_at = excluded.resolved_at
	`,
		id,
		nullIfEmpty(strings.ToUpper(strings.TrimSpace(geo.CountryCode))),
		nullIfEmpty(geo.KrajName),
		nullIfEmpty(normalizeSearchText(geo.KrajName)),
		nullIfEmpty(geo.OkresName),
		nullIfEmpty(normalizeSearchText(geo.OkresName)),
		nullIfEmpty(geo.ObecName),
		nullIfEmpty(normalizeSearchText(geo.ObecName)),
		nullIfEmpty(geo.RawJSON),
		geo.ResolvedAt.UTC().Format(time.RFC3339),
	); err != nil {
		return err
	}

	if _, err := tx.Exec(`
		UPDATE photo_captures
		SET status = 'published',
			public_storage_key = ?,
			published_at = ?,
			publication_review_status = ?,
			publication_review_reason_code = NULL,
			publication_review_last_error = NULL,
			publication_review_checked_at = ?,
			publication_review_next_attempt_at = NULL
		WHERE id = ? AND user_id = ?
	`, publicStorageKey, now, CapturePublicationReviewApproved, now, id, userID); err != nil {
		return err
	}

	return tx.Commit()
}

func (db *DB) ApplyCaptureModeratorReview(
	captureID string,
	moderatorUserID int64,
	analysis *models.CaptureMushroomAnalysis,
	species []*models.CaptureMushroomSpecies,
) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	analysis.ReviewSource = CaptureAnalysisSourceModeratorRecheck
	analysis.ReviewedByUserID = moderatorUserID
	analysis.ReviewedAt = time.Now().UTC()

	if err := upsertCaptureMushroomAnalysisTx(tx, analysis); err != nil {
		return err
	}
	if err := replaceCaptureMushroomSpeciesTx(tx, captureID, species); err != nil {
		return err
	}

	return tx.Commit()
}

func (db *DB) ListPublicCapturesWithFilters(filters PublicCaptureFilters, viewerUserID int64) ([]*models.Capture, error) {
	return db.listPublicCapturesWithFilters(filters, viewerUserID, false)
}

func (db *DB) ListPublicMapCapturesWithFilters(filters PublicCaptureFilters, viewerUserID int64) ([]*models.Capture, error) {
	return db.listPublicCapturesWithFilters(filters, viewerUserID, true)
}

func (db *DB) listPublicCapturesWithFilters(filters PublicCaptureFilters, viewerUserID int64, requireVisibleCoordinates bool) ([]*models.Capture, error) {
	filters = normalizePublicCaptureFilters(filters)

	var (
		whereClauses = []string{
			"c.status = 'published'",
			"c.public_storage_key IS NOT NULL",
			"c.public_storage_key != ''",
		}
		args []interface{}
	)

	if requireVisibleCoordinates {
		whereClauses = append(whereClauses, "c.latitude IS NOT NULL", "c.longitude IS NOT NULL")
		if viewerUserID > 0 {
			whereClauses = append(whereClauses, `
				(
					c.user_id = ?
					OR COALESCE(c.coordinates_free, 0) = 1
					OR EXISTS (
						SELECT 1
						FROM capture_coordinate_unlocks cu
						WHERE cu.capture_id = c.id AND cu.viewer_user_id = ?
					)
				)
			`)
			args = append(args, viewerUserID, viewerUserID)
		} else {
			whereClauses = append(whereClauses, "COALESCE(c.coordinates_free, 0) = 1")
		}
	}

	if filters.HasMushrooms != nil {
		whereClauses = append(whereClauses, "COALESCE(ma.has_mushrooms, 0) = ?")
		if *filters.HasMushrooms {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}

	if filters.SpeciesQuery != "" {
		like := "%" + filters.SpeciesQuery + "%"
		whereClauses = append(whereClauses, `
			EXISTS (
				SELECT 1
				FROM capture_mushroom_species cms
				WHERE cms.capture_id = c.id
				  AND (
					  cms.latin_name_norm LIKE ?
					  OR COALESCE(cms.czech_official_name_norm, '') LIKE ?
				  )
			)
		`)
		args = append(args, like, like)
	}

	if filters.KrajQuery != "" {
		args = append(args, "%"+filters.KrajQuery+"%")
		whereClauses = append(whereClauses, "COALESCE(gi.kraj_name_norm, '') LIKE ?")
	}

	if filters.OkresQuery != "" {
		args = append(args, "%"+filters.OkresQuery+"%")
		whereClauses = append(whereClauses, "COALESCE(c.coordinates_free, 0) = 1")
		whereClauses = append(whereClauses, "COALESCE(gi.okres_name_norm, '') LIKE ?")
	}

	if filters.ObecQuery != "" {
		args = append(args, "%"+filters.ObecQuery+"%")
		whereClauses = append(whereClauses, "COALESCE(c.coordinates_free, 0) = 1")
		whereClauses = append(whereClauses, "COALESCE(gi.obec_name_norm, '') LIKE ?")
	}

	orderBy := "c.published_at DESC, c.captured_at DESC"
	switch filters.Sort {
	case "captured_desc":
		orderBy = "c.captured_at DESC, c.published_at DESC"
	case "probability_desc":
		orderBy = "COALESCE(ma.primary_probability, 0) DESC, c.published_at DESC"
	case "kraj_asc":
		orderBy = "COALESCE(gi.kraj_name_norm, ''), c.published_at DESC"
	case "okres_asc":
		orderBy = "CASE WHEN COALESCE(c.coordinates_free, 0) = 1 THEN COALESCE(gi.okres_name_norm, '') ELSE '' END, c.published_at DESC"
	case "obec_asc":
		orderBy = "CASE WHEN COALESCE(c.coordinates_free, 0) = 1 THEN COALESCE(gi.obec_name_norm, '') ELSE '' END, c.published_at DESC"
	}

	query := fmt.Sprintf(`
		SELECT c.id, c.user_id, u.preferred_username, COALESCE(u.picture, ''), COALESCE(c.client_local_id, ''), c.original_file_name, c.content_type, c.size_bytes, c.width, c.height,
			c.captured_at, c.uploaded_at, c.latitude, c.longitude, c.accuracy_meters, c.status, c.private_storage_key,
			COALESCE(c.public_storage_key, ''), COALESCE(c.published_at, ''), COALESCE(c.coordinates_free, 0),
			COALESCE(gi.country_code, ''), COALESCE(gi.kraj_name, ''),
			CASE WHEN COALESCE(c.coordinates_free, 0) = 1 THEN COALESCE(gi.okres_name, '') ELSE '' END,
			CASE WHEN COALESCE(c.coordinates_free, 0) = 1 THEN COALESCE(gi.obec_name, '') ELSE '' END,
			COALESCE(ma.has_mushrooms, 0), COALESCE(ma.primary_latin_name, ''), COALESCE(ma.primary_czech_name, ''),
			COALESCE(ma.primary_probability, 0), COALESCE(ma.analyzed_at, '')
		FROM photo_captures c
		JOIN users u ON c.user_id = u.id
		LEFT JOIN capture_geo_index gi ON gi.capture_id = c.id
		LEFT JOIN capture_mushroom_analysis ma ON ma.capture_id = c.id
		WHERE %s
		ORDER BY %s
		LIMIT ? OFFSET ?
	`, strings.Join(whereClauses, " AND "), orderBy)

	args = append(args, filters.Limit, filters.Offset)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var captures []*models.Capture
	for rows.Next() {
		var (
			c                models.Capture
			capturedAt       string
			uploadedAt       string
			publishedAt      string
			countryCode      string
			krajName         string
			okresName        string
			obecName         string
			hasMushrooms     int
			primaryLatinName string
			primaryCzechName string
			analysisAt       sql.NullString
			coordinatesFree  int
		)
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.AuthorName, &c.AuthorAvatar, &c.ClientLocalID, &c.OriginalFileName, &c.ContentType, &c.SizeBytes, &c.Width, &c.Height,
			&capturedAt, &uploadedAt, &c.Latitude, &c.Longitude, &c.AccuracyMeters, &c.Status, &c.PrivateStorageKey,
			&c.PublicStorageKey, &publishedAt, &coordinatesFree,
			&countryCode, &krajName, &okresName, &obecName,
			&hasMushrooms, &primaryLatinName, &primaryCzechName, &c.MushroomPrimaryProbability, &analysisAt,
		); err != nil {
			return nil, err
		}
		c.AuthorUserID = c.UserID
		c.CoordinatesFree = coordinatesFree == 1
		c.CapturedAt, _ = time.Parse(time.RFC3339, capturedAt)
		c.UploadedAt, _ = time.Parse(time.RFC3339, uploadedAt)
		if publishedAt != "" {
			c.PublishedAt, _ = time.Parse(time.RFC3339, publishedAt)
		}
		if analysisAt.Valid && analysisAt.String != "" {
			c.MushroomAnalysisAt, _ = time.Parse(time.RFC3339, analysisAt.String)
		}
		c.CountryCode = strings.ToUpper(countryCode)
		c.KrajName = krajName
		c.OkresName = okresName
		c.ObecName = obecName
		c.HasMushrooms = hasMushrooms == 1
		c.MushroomPrimaryLatinName = primaryLatinName
		c.MushroomPrimaryCzechName = primaryCzechName
		captures = append(captures, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if requireVisibleCoordinates {
		for _, capture := range captures {
			if capture == nil {
				continue
			}
			capture.CoordinatesLocked = false
		}
		return captures, nil
	}

	if err := db.maskCaptureCoordinatesForViewer(viewerUserID, captures); err != nil {
		return nil, err
	}

	return captures, nil
}
