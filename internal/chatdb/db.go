package chatdb

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func New(url string, token string, generalRoomSlug string, generalRoomTitle string) (*DB, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, fmt.Errorf("chat db url is required")
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

	db := &DB{DB: sqlDB}
	if err := db.migrate(generalRoomSlug, generalRoomTitle); err != nil {
		return nil, err
	}
	return db, nil
}

func (db *DB) migrate(generalRoomSlug string, generalRoomTitle string) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS chat_rooms (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			slug TEXT,
			title TEXT NOT NULL,
			created_by_user_id INTEGER NOT NULL,
			direct_pair_key TEXT,
			created_at TEXT NOT NULL,
			archived_at TEXT
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_rooms_slug ON chat_rooms(slug);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_rooms_direct_pair_key ON chat_rooms(direct_pair_key);`,
		`CREATE TABLE IF NOT EXISTS chat_room_members (
			room_id TEXT NOT NULL,
			user_id INTEGER NOT NULL,
			role TEXT NOT NULL DEFAULT 'member',
			joined_at TEXT NOT NULL,
			PRIMARY KEY (room_id, user_id),
			FOREIGN KEY(room_id) REFERENCES chat_rooms(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_chat_room_members_user_room ON chat_room_members(user_id, room_id);`,
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id TEXT PRIMARY KEY,
			room_id TEXT NOT NULL,
			author_user_id INTEGER NOT NULL,
			author_name_snapshot TEXT NOT NULL,
			author_avatar_snapshot TEXT,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			edited_at TEXT,
			deleted_at TEXT,
			FOREIGN KEY(room_id) REFERENCES chat_rooms(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_room_created ON chat_messages(room_id, created_at DESC, id DESC);`,
		`CREATE TABLE IF NOT EXISTS chat_room_reads (
			room_id TEXT NOT NULL,
			user_id INTEGER NOT NULL,
			last_read_message_id TEXT,
			last_read_at TEXT NOT NULL,
			PRIMARY KEY (room_id, user_id),
			FOREIGN KEY(room_id) REFERENCES chat_rooms(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS chat_user_restrictions (
			user_id INTEGER PRIMARY KEY,
			muted_until TEXT,
			reason TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS chat_abuse_events (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			ip TEXT,
			action TEXT NOT NULL,
			escalating INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_chat_abuse_events_user_created ON chat_abuse_events(user_id, created_at DESC);`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}

	if generalRoomSlug == "" {
		generalRoomSlug = "general"
	}
	if generalRoomTitle == "" {
		generalRoomTitle = "Obecný chat"
	}
	_, err := db.Exec(`
		INSERT OR IGNORE INTO chat_rooms (
			id, kind, slug, title, created_by_user_id, direct_pair_key, created_at, archived_at
		) VALUES (?, 'public', ?, ?, 0, NULL, ?, NULL)
	`, "room:"+generalRoomSlug, generalRoomSlug, generalRoomTitle, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (db *DB) CreatePublicRoom(slug string, title string, createdByUserID int64) (*Room, error) {
	now := time.Now().UTC()
	slug = normalizeSlug(slug)
	title = strings.TrimSpace(title)
	room := &Room{
		ID:              uuid.NewString(),
		Kind:            "public",
		Slug:            slug,
		Title:           title,
		CreatedByUserID: createdByUserID,
		CreatedAt:       now,
	}
	if _, err := db.Exec(`
		INSERT INTO chat_rooms (id, kind, slug, title, created_by_user_id, direct_pair_key, created_at, archived_at)
		VALUES (?, ?, ?, ?, ?, NULL, ?, NULL)
	`, room.ID, room.Kind, room.Slug, room.Title, room.CreatedByUserID, room.CreatedAt.Format(time.RFC3339)); err != nil {
		return nil, err
	}
	return room, nil
}

func (db *DB) EnsureDirectRoom(userA int64, userB int64) (*Room, error) {
	if userA <= 0 || userB <= 0 {
		return nil, sql.ErrNoRows
	}
	if userA == userB {
		return nil, fmt.Errorf("cannot create direct room with self")
	}

	pairKey := directPairKey(userA, userB)
	if existing, err := db.getRoomByDirectPairKey(pairKey); err == nil {
		if err := db.ensureDirectRoomMembers(existing.ID, userA, userB); err != nil {
			return nil, err
		}
		return existing, nil
	} else if err != sql.ErrNoRows {
		return nil, err
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	room := &Room{
		ID:              uuid.NewString(),
		Kind:            "dm",
		Title:           "DM",
		CreatedByUserID: userA,
		DirectPairKey:   pairKey,
		CreatedAt:       now,
	}
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO chat_rooms (id, kind, slug, title, created_by_user_id, direct_pair_key, created_at, archived_at)
		VALUES (?, 'dm', NULL, ?, ?, ?, ?, NULL)
	`, room.ID, room.Title, room.CreatedByUserID, room.DirectPairKey, room.CreatedAt.Format(time.RFC3339)); err != nil {
		return nil, err
	}

	createdRoom, err := scanRoomRow(tx.QueryRow(`
		SELECT id, kind, COALESCE(slug, ''), title, created_by_user_id, COALESCE(direct_pair_key, ''), created_at, COALESCE(archived_at, '')
		FROM chat_rooms
		WHERE direct_pair_key = ?
		LIMIT 1
	`, pairKey))
	if err != nil {
		return nil, err
	}

	if err := ensureDirectRoomMembersTx(tx, createdRoom.ID, now, userA, userB); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return createdRoom, nil
}

func (db *DB) ListPublicRooms(userID int64) ([]*Room, error) {
	rows, err := db.Query(`
		SELECT id, kind, COALESCE(slug, ''), title, created_by_user_id, COALESCE(direct_pair_key, ''), created_at, COALESCE(archived_at, '')
		FROM chat_rooms
		WHERE kind = 'public' AND COALESCE(archived_at, '') = ''
		ORDER BY lower(title) ASC, created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rooms := make([]*Room, 0)
	for rows.Next() {
		room, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		if err := db.attachRoomState(room, userID); err != nil {
			return nil, err
		}
		rooms = append(rooms, room)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return rooms, nil
}

func (db *DB) ListDirectRoomsForUser(userID int64, limit int, offset int) ([]*Room, int, int, bool, error) {
	rows, err := db.Query(`
		SELECT r.id, r.kind, COALESCE(r.slug, ''), r.title, r.created_by_user_id, COALESCE(r.direct_pair_key, ''), r.created_at, COALESCE(r.archived_at, '')
		FROM chat_rooms r
		JOIN chat_room_members rm ON rm.room_id = r.id
		WHERE r.kind = 'dm' AND COALESCE(r.archived_at, '') = '' AND rm.user_id = ?
	`, userID)
	if err != nil {
		return nil, 0, 0, false, err
	}
	defer rows.Close()

	rooms := make([]*Room, 0)
	totalUnread := 0
	for rows.Next() {
		room, err := scanRoom(rows)
		if err != nil {
			return nil, 0, 0, false, err
		}
		room.OtherUserID, _ = db.getOtherUserID(room.ID, userID)
		if err := db.attachRoomState(room, userID); err != nil {
			return nil, 0, 0, false, err
		}
		totalUnread += room.UnreadCount
		rooms = append(rooms, room)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, false, err
	}

	sort.SliceStable(rooms, func(leftIndex int, rightIndex int) bool {
		left := directRoomLastActivityAt(rooms[leftIndex])
		right := directRoomLastActivityAt(rooms[rightIndex])
		if !left.Equal(right) {
			return left.After(right)
		}
		if !rooms[leftIndex].CreatedAt.Equal(rooms[rightIndex].CreatedAt) {
			return rooms[leftIndex].CreatedAt.After(rooms[rightIndex].CreatedAt)
		}
		return rooms[leftIndex].ID > rooms[rightIndex].ID
	})

	total := len(rooms)
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = total
	}
	if offset >= total {
		return []*Room{}, total, totalUnread, false, nil
	}

	end := offset + limit
	if end > total {
		end = total
	}
	hasMore := end < total
	return rooms[offset:end], total, totalUnread, hasMore, nil
}

func (db *DB) GetRoom(roomID string) (*Room, error) {
	return scanRoomRow(db.QueryRow(`
		SELECT id, kind, COALESCE(slug, ''), title, created_by_user_id, COALESCE(direct_pair_key, ''), created_at, COALESCE(archived_at, '')
		FROM chat_rooms
		WHERE id = ?
		LIMIT 1
	`, roomID))
}

func (db *DB) CanUserAccessRoom(userID int64, roomID string) (*Room, bool, error) {
	room, err := db.GetRoom(roomID)
	if err != nil {
		return nil, false, err
	}
	if room.Kind == "public" {
		return room, true, nil
	}

	var exists int
	err = db.QueryRow(`
		SELECT 1
		FROM chat_room_members
		WHERE room_id = ? AND user_id = ?
		LIMIT 1
	`, roomID, userID).Scan(&exists)
	if err == sql.ErrNoRows {
		return room, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return room, true, nil
}

func (db *DB) ListMessages(roomID string, limit int) ([]*Message, error) {
	return db.listMessagesAfter(roomID, limit, time.Time{})
}

func (db *DB) ListMessagesForUser(room *Room, userID int64, limit int) ([]*Message, error) {
	if room == nil {
		return nil, sql.ErrNoRows
	}
	visibleSince, err := db.visibleSinceForRoomUser(room, userID)
	if err != nil {
		return nil, err
	}
	return db.listMessagesAfter(room.ID, limit, visibleSince)
}

func (db *DB) listMessagesAfter(roomID string, limit int, visibleSince time.Time) ([]*Message, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	query := `
		SELECT id, room_id, author_user_id, author_name_snapshot, COALESCE(author_avatar_snapshot, ''), content, created_at, COALESCE(edited_at, ''), COALESCE(deleted_at, '')
		FROM chat_messages
		WHERE room_id = ?
	`
	args := []interface{}{roomID}
	if !visibleSince.IsZero() {
		query += ` AND created_at >= ?`
		args = append(args, visibleSince.Format(time.RFC3339))
	}
	query += `
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	descending := make([]*Message, 0, limit)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		descending = append(descending, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	messages := make([]*Message, 0, len(descending))
	for index := len(descending) - 1; index >= 0; index-- {
		messages = append(messages, descending[index])
	}
	return messages, nil
}

func (db *DB) CreateMessage(message *Message) error {
	if message == nil {
		return fmt.Errorf("message is required")
	}
	if message.ID == "" {
		message.ID = uuid.NewString()
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}
	_, err := db.Exec(`
		INSERT INTO chat_messages (
			id, room_id, author_user_id, author_name_snapshot, author_avatar_snapshot, content, created_at, edited_at, deleted_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL)
	`, message.ID, message.RoomID, message.AuthorUserID, message.AuthorNameSnapshot, message.AuthorAvatar, message.Content, message.CreatedAt.Format(time.RFC3339))
	return err
}

func (db *DB) GetMessage(messageID string) (*Message, error) {
	return scanMessageRow(db.QueryRow(`
		SELECT id, room_id, author_user_id, author_name_snapshot, COALESCE(author_avatar_snapshot, ''), content, created_at, COALESCE(edited_at, ''), COALESCE(deleted_at, '')
		FROM chat_messages
		WHERE id = ?
		LIMIT 1
	`, messageID))
}

func (db *DB) SoftDeleteMessage(messageID string, deletedAt time.Time) error {
	if deletedAt.IsZero() {
		deletedAt = time.Now().UTC()
	}
	result, err := db.Exec(`
		UPDATE chat_messages
		SET content = '', deleted_at = ?, edited_at = NULL
		WHERE id = ?
	`, deletedAt.Format(time.RFC3339), messageID)
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

func (db *DB) TrimRoomMessages(roomID string, keep int) error {
	if keep <= 0 {
		return nil
	}
	_, err := db.Exec(`
		DELETE FROM chat_messages
		WHERE room_id = ?
			AND id IN (
				SELECT id
				FROM chat_messages
				WHERE room_id = ?
				ORDER BY created_at DESC, id DESC
				LIMIT -1 OFFSET ?
			)
	`, roomID, roomID, keep)
	return err
}

func (db *DB) RemoveDirectRoomForUser(roomID string, userID int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		DELETE FROM chat_room_reads
		WHERE room_id = ? AND user_id = ?
	`, roomID, userID); err != nil {
		return err
	}
	result, err := tx.Exec(`
		DELETE FROM chat_room_members
		WHERE room_id = ? AND user_id = ?
	`, roomID, userID)
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

	var remainingMembers int
	if err := tx.QueryRow(`
		SELECT COUNT(*)
		FROM chat_room_members
		WHERE room_id = ?
	`, roomID).Scan(&remainingMembers); err != nil {
		return err
	}
	if remainingMembers == 0 {
		if _, err := tx.Exec(`
			DELETE FROM chat_rooms
			WHERE id = ?
		`, roomID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *DB) ensureDirectRoomMembers(roomID string, userIDs ...int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := ensureDirectRoomMembersTx(tx, roomID, time.Now().UTC(), userIDs...); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureDirectRoomMembersTx(tx *sql.Tx, roomID string, joinedAt time.Time, userIDs ...int64) error {
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO chat_room_members (room_id, user_id, role, joined_at)
			VALUES (?, ?, 'member', ?)
		`, roomID, userID, joinedAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) MarkRoomRead(roomID string, userID int64, lastMessageID string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	_, err := db.Exec(`
		INSERT INTO chat_room_reads (room_id, user_id, last_read_message_id, last_read_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(room_id, user_id) DO UPDATE SET
			last_read_message_id = excluded.last_read_message_id,
			last_read_at = excluded.last_read_at
	`, roomID, userID, nullableString(lastMessageID), at.Format(time.RFC3339))
	return err
}

func (db *DB) attachRoomState(room *Room, userID int64) error {
	if room == nil {
		return nil
	}
	visibleSince, err := db.visibleSinceForRoomUser(room, userID)
	if err != nil {
		return err
	}
	lastMessage, err := db.getLastMessageAfter(room.ID, visibleSince)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		room.LastMessage = lastMessage
	}
	unreadCount, err := db.countUnreadMessagesAfter(room.ID, userID, visibleSince)
	if err != nil {
		return err
	}
	room.UnreadCount = unreadCount
	return nil
}

func (db *DB) getLastMessageAfter(roomID string, visibleSince time.Time) (*Message, error) {
	query := `
		SELECT id, room_id, author_user_id, author_name_snapshot, COALESCE(author_avatar_snapshot, ''), content, created_at, COALESCE(edited_at, ''), COALESCE(deleted_at, '')
		FROM chat_messages
		WHERE room_id = ?
	`
	args := []interface{}{roomID}
	if !visibleSince.IsZero() {
		query += ` AND created_at >= ?`
		args = append(args, visibleSince.Format(time.RFC3339))
	}
	query += `
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`
	return scanMessageRow(db.QueryRow(query, args...))
}

func (db *DB) countUnreadMessagesAfter(roomID string, userID int64, visibleSince time.Time) (int, error) {
	var total int
	query := `
		SELECT COUNT(*)
		FROM chat_messages m
		LEFT JOIN chat_room_reads rr ON rr.room_id = m.room_id AND rr.user_id = ?
		WHERE m.room_id = ?
			AND (
				COALESCE(rr.last_read_at, '') = ''
				OR m.created_at > rr.last_read_at
			)
	`
	args := []interface{}{userID, roomID}
	if !visibleSince.IsZero() {
		query += ` AND m.created_at >= ?`
		args = append(args, visibleSince.Format(time.RFC3339))
	}
	err := db.QueryRow(query, args...).Scan(&total)
	return total, err
}

func (db *DB) getOtherUserID(roomID string, currentUserID int64) (int64, error) {
	var otherUserID int64
	err := db.QueryRow(`
		SELECT user_id
		FROM chat_room_members
		WHERE room_id = ? AND user_id != ?
		ORDER BY user_id ASC
		LIMIT 1
	`, roomID, currentUserID).Scan(&otherUserID)
	return otherUserID, err
}

func directRoomLastActivityAt(room *Room) time.Time {
	if room == nil {
		return time.Time{}
	}
	if room.LastMessage != nil && !room.LastMessage.CreatedAt.IsZero() {
		return room.LastMessage.CreatedAt
	}
	return room.CreatedAt
}

func (db *DB) GetUserRestriction(userID int64) (*UserRestriction, error) {
	var (
		restriction  UserRestriction
		mutedUntil   sql.NullString
		reason       sql.NullString
		createdAtRaw string
		updatedAtRaw string
	)
	err := db.QueryRow(`
		SELECT user_id, muted_until, reason, created_at, updated_at
		FROM chat_user_restrictions
		WHERE user_id = ?
		LIMIT 1
	`, userID).Scan(
		&restriction.UserID,
		&mutedUntil,
		&reason,
		&createdAtRaw,
		&updatedAtRaw,
	)
	if err != nil {
		return nil, err
	}
	if mutedUntil.Valid && mutedUntil.String != "" {
		restriction.MutedUntil, _ = time.Parse(time.RFC3339, mutedUntil.String)
	}
	if reason.Valid {
		restriction.Reason = reason.String
	}
	restriction.CreatedAt, _ = time.Parse(time.RFC3339, createdAtRaw)
	restriction.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtRaw)
	return &restriction, nil
}

func (db *DB) GetActiveUserRestriction(userID int64, now time.Time) (*UserRestriction, error) {
	restriction, err := db.GetUserRestriction(userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if restriction.MutedUntil.IsZero() || !restriction.MutedUntil.After(now) {
		return nil, nil
	}
	return restriction, nil
}

func (db *DB) UpsertUserRestriction(userID int64, mutedUntil time.Time, reason string, now time.Time) error {
	if userID <= 0 || mutedUntil.IsZero() {
		return nil
	}
	existing, err := db.GetUserRestriction(userID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if existing != nil && existing.MutedUntil.After(mutedUntil) {
		mutedUntil = existing.MutedUntil
	}

	createdAt := now
	if existing != nil && !existing.CreatedAt.IsZero() {
		createdAt = existing.CreatedAt
	}

	_, err = db.Exec(`
		INSERT INTO chat_user_restrictions (user_id, muted_until, reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			muted_until = excluded.muted_until,
			reason = excluded.reason,
			updated_at = excluded.updated_at
	`, userID, mutedUntil.Format(time.RFC3339), strings.TrimSpace(reason), createdAt.Format(time.RFC3339), now.Format(time.RFC3339))
	return err
}

func (db *DB) RecordAbuseEvent(userID int64, ip string, action string, escalating bool, now time.Time) error {
	if userID <= 0 || strings.TrimSpace(action) == "" {
		return nil
	}
	_, err := db.Exec(`
		INSERT INTO chat_abuse_events (id, user_id, ip, action, escalating, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), userID, strings.TrimSpace(ip), strings.TrimSpace(action), boolToInt(escalating), now.Format(time.RFC3339))
	return err
}

func (db *DB) CountRecentAbuseEvents(userID int64, since time.Time, escalatingOnly bool) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM chat_abuse_events
		WHERE user_id = ? AND created_at >= ?
	`
	args := []interface{}{userID, since.Format(time.RFC3339)}
	if escalatingOnly {
		query += ` AND escalating = 1`
	}

	var total int
	err := db.QueryRow(query, args...).Scan(&total)
	return total, err
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (db *DB) visibleSinceForRoomUser(room *Room, userID int64) (time.Time, error) {
	if room == nil || room.Kind != "dm" || userID <= 0 {
		return time.Time{}, nil
	}

	var joinedAtRaw string
	if err := db.QueryRow(`
		SELECT joined_at
		FROM chat_room_members
		WHERE room_id = ? AND user_id = ?
		LIMIT 1
	`, room.ID, userID).Scan(&joinedAtRaw); err != nil {
		return time.Time{}, err
	}

	joinedAt, err := time.Parse(time.RFC3339, joinedAtRaw)
	if err != nil {
		return time.Time{}, err
	}
	return joinedAt, nil
}

func (db *DB) getRoomByDirectPairKey(pairKey string) (*Room, error) {
	return scanRoomRow(db.QueryRow(`
		SELECT id, kind, COALESCE(slug, ''), title, created_by_user_id, COALESCE(direct_pair_key, ''), created_at, COALESCE(archived_at, '')
		FROM chat_rooms
		WHERE direct_pair_key = ?
		LIMIT 1
	`, pairKey))
}

func directPairKey(userA int64, userB int64) string {
	if userA < userB {
		return fmt.Sprintf("%d:%d", userA, userB)
	}
	return fmt.Sprintf("%d:%d", userB, userA)
}

func normalizeSlug(slug string) string {
	slug = strings.TrimSpace(strings.ToLower(slug))
	slug = strings.ReplaceAll(slug, " ", "-")
	return slug
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanRoomRow(row scanner) (*Room, error) {
	return scanRoom(row)
}

func scanRoom(row scanner) (*Room, error) {
	var (
		room          Room
		createdAtRaw  string
		archivedAtRaw string
	)
	if err := row.Scan(
		&room.ID,
		&room.Kind,
		&room.Slug,
		&room.Title,
		&room.CreatedByUserID,
		&room.DirectPairKey,
		&createdAtRaw,
		&archivedAtRaw,
	); err != nil {
		return nil, err
	}
	room.CreatedAt, _ = time.Parse(time.RFC3339, createdAtRaw)
	if archivedAtRaw != "" {
		room.ArchivedAt, _ = time.Parse(time.RFC3339, archivedAtRaw)
	}
	return &room, nil
}

func scanMessageRow(row scanner) (*Message, error) {
	return scanMessage(row)
}

func scanMessage(row scanner) (*Message, error) {
	var (
		message      Message
		createdAtRaw string
		editedAtRaw  string
		deletedAtRaw string
	)
	if err := row.Scan(
		&message.ID,
		&message.RoomID,
		&message.AuthorUserID,
		&message.AuthorNameSnapshot,
		&message.AuthorAvatar,
		&message.Content,
		&createdAtRaw,
		&editedAtRaw,
		&deletedAtRaw,
	); err != nil {
		return nil, err
	}
	message.CreatedAt, _ = time.Parse(time.RFC3339, createdAtRaw)
	if editedAtRaw != "" {
		message.EditedAt, _ = time.Parse(time.RFC3339, editedAtRaw)
	}
	if deletedAtRaw != "" {
		message.DeletedAt, _ = time.Parse(time.RFC3339, deletedAtRaw)
	}
	return &message, nil
}

func nullableString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
