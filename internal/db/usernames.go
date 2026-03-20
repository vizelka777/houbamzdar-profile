package db

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxPreferredUsernameLength = 12
const defaultPreferredUsernameSeed = "houbar"

var ErrPreferredUsernameRequired = errors.New("preferred username is required")
var ErrPreferredUsernameTooLong = errors.New("preferred username is too long")
var ErrPreferredUsernameInvalid = errors.New("preferred username may contain only letters and digits")
var ErrPreferredUsernameTaken = errors.New("preferred username is already taken")

func ValidatePreferredUsername(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ErrPreferredUsernameRequired
	}
	if utf8.RuneCountInString(value) > maxPreferredUsernameLength {
		return "", ErrPreferredUsernameTooLong
	}
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return "", ErrPreferredUsernameInvalid
		}
	}
	return value, nil
}

func normalizedPreferredUsername(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func sanitizePreferredUsernameSeed(raw string) string {
	value := strings.TrimSpace(raw)
	if at := strings.Index(value, "@"); at > 0 {
		value = value[:at]
	}

	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}

	seed := b.String()
	if seed == "" {
		seed = defaultPreferredUsernameSeed
	}
	return truncateRunes(seed, maxPreferredUsernameLength)
}

func truncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}

	var b strings.Builder
	count := 0
	for _, r := range value {
		if count >= maxRunes {
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}

func preferredUsernameCandidate(rawSeed string, attempt int) string {
	seed := sanitizePreferredUsernameSeed(rawSeed)
	if attempt <= 0 {
		return truncateRunes(seed, maxPreferredUsernameLength)
	}

	suffix := strconv.Itoa(attempt)
	prefixLimit := maxPreferredUsernameLength - utf8.RuneCountInString(suffix)
	if prefixLimit <= 0 {
		return truncateRunes(suffix, maxPreferredUsernameLength)
	}

	prefix := truncateRunes(seed, prefixLimit)
	if prefix == "" {
		prefix = truncateRunes(defaultPreferredUsernameSeed, prefixLimit)
	}
	return prefix + suffix
}

func isPreferredUsernameUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "unique") && strings.Contains(lower, "preferred_username_norm")
}

func isPreferredUsernameAvailableTx(tx *sql.Tx, normalized string, excludeUserID int64) (bool, error) {
	var existingID int64
	err := tx.QueryRow(`
		SELECT id
		FROM users
		WHERE preferred_username_norm = ?
			AND id != ?
		LIMIT 1
	`, normalized, excludeUserID).Scan(&existingID)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func (db *DB) isPreferredUsernameAvailable(normalized string, excludeUserID int64) (bool, error) {
	var existingID int64
	err := db.QueryRow(`
		SELECT id
		FROM users
		WHERE preferred_username_norm = ?
			AND id != ?
		LIMIT 1
	`, normalized, excludeUserID).Scan(&existingID)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func findAvailablePreferredUsernameTx(tx *sql.Tx, rawSeed string, excludeUserID int64) (string, error) {
	for attempt := 0; attempt < 5000; attempt++ {
		candidate := preferredUsernameCandidate(rawSeed, attempt)
		if candidate == "" {
			continue
		}

		available, err := isPreferredUsernameAvailableTx(tx, normalizedPreferredUsername(candidate), excludeUserID)
		if err != nil {
			return "", err
		}
		if available {
			return candidate, nil
		}
	}

	return "", ErrPreferredUsernameTaken
}

func backfillPreferredUsernameNormTx(tx *sql.Tx) error {
	rows, err := tx.Query(`
		SELECT id, COALESCE(preferred_username, '')
		FROM users
		ORDER BY id ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type userRow struct {
		ID                int64
		PreferredUsername string
	}

	users := make([]userRow, 0, 32)
	for rows.Next() {
		var row userRow
		if err := rows.Scan(&row.ID, &row.PreferredUsername); err != nil {
			return err
		}
		users = append(users, row)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, user := range users {
		candidate := strings.TrimSpace(user.PreferredUsername)
		if _, err := ValidatePreferredUsername(candidate); err != nil {
			candidate = ""
		}
		if candidate == "" {
			candidate = sanitizePreferredUsernameSeed(user.PreferredUsername)
		}

		candidate, err = findAvailablePreferredUsernameTx(tx, candidate, user.ID)
		if err != nil {
			return err
		}

		if _, err := tx.Exec(`
			UPDATE users
			SET preferred_username = ?,
				preferred_username_norm = ?,
				updated_at = datetime('now')
			WHERE id = ?
		`, candidate, normalizedPreferredUsername(candidate), user.ID); err != nil {
			return err
		}
	}

	return nil
}

func (db *DB) SuggestPreferredUsernames(raw string, excludeUserID int64, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 3
	}

	suggestions := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	for attempt := 0; len(suggestions) < limit && attempt < 5000; attempt++ {
		candidate := preferredUsernameCandidate(raw, attempt)
		if candidate == "" {
			continue
		}

		normalized := normalizedPreferredUsername(candidate)
		if _, ok := seen[normalized]; ok {
			continue
		}

		available, err := db.isPreferredUsernameAvailable(normalized, excludeUserID)
		if err != nil {
			return nil, err
		}
		if !available {
			continue
		}

		seen[normalized] = struct{}{}
		suggestions = append(suggestions, candidate)
	}

	return suggestions, nil
}

func (db *DB) UpdatePreferredUsername(userID int64, raw string) (string, error) {
	value, err := ValidatePreferredUsername(raw)
	if err != nil {
		return "", err
	}

	result, err := db.Exec(`
		UPDATE users
		SET preferred_username = ?,
			preferred_username_norm = ?,
			updated_at = datetime('now')
		WHERE id = ?
	`, value, normalizedPreferredUsername(value), userID)
	if err != nil {
		if isPreferredUsernameUniqueConstraintError(err) {
			return "", ErrPreferredUsernameTaken
		}
		return "", err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if rowsAffected == 0 {
		return "", sql.ErrNoRows
	}

	return value, nil
}
