package assistantadmin

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/houbamzdar/bff/internal/models"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func New(url string, token string) (*DB, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, fmt.Errorf("assistant db url is required")
	}
	if strings.TrimSpace(token) != "" {
		url = fmt.Sprintf("%s?authToken=%s", url, token)
	}

	sqlDB, err := sql.Open("libsql", url)
	if err != nil {
		return nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	return &DB{DB: sqlDB}, nil
}

func parseOptionalTime(raw string) time.Time {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func trimPreview(value string, max int) string {
	text := strings.TrimSpace(strings.ReplaceAll(value, "\r", ""))
	text = strings.Join(strings.Fields(text), " ")
	if max <= 0 || len(text) <= max {
		return text
	}
	return strings.TrimSpace(text[:max])
}

func (db *DB) GetOverview() (*models.AssistantAdminOverview, error) {
	if db == nil || db.DB == nil {
		return nil, sql.ErrConnDone
	}

	today := time.Now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339)
	row := db.QueryRow(`
		SELECT
			COALESCE((SELECT COUNT(*) FROM assistant_threads), 0),
			COALESCE((SELECT COUNT(*) FROM assistant_threads WHERE COALESCE(status, 'open') = 'open'), 0),
			COALESCE((SELECT COUNT(DISTINCT client_id) FROM assistant_threads WHERE COALESCE(client_id, '') <> ''), 0),
			COALESCE((SELECT COUNT(*) FROM assistant_messages), 0),
			COALESCE((SELECT COUNT(*) FROM assistant_messages WHERE role = 'user'), 0),
			COALESCE((SELECT COUNT(*) FROM assistant_messages WHERE role = 'assistant'), 0),
			COALESCE((SELECT COUNT(*) FROM assistant_feedback WHERE vote = 'up'), 0),
			COALESCE((SELECT COUNT(*) FROM assistant_feedback WHERE vote = 'down'), 0),
			COALESCE((SELECT COUNT(*) FROM assistant_threads WHERE created_at >= ?), 0),
			COALESCE((SELECT COUNT(*) FROM assistant_messages WHERE created_at >= ?), 0),
			COALESCE((SELECT MAX(created_at) FROM assistant_messages), '')
	`, today, today)

	var overview models.AssistantAdminOverview
	var lastMessageAt string
	if err := row.Scan(
		&overview.TotalThreads,
		&overview.OpenThreads,
		&overview.UniqueClients,
		&overview.TotalMessages,
		&overview.UserMessages,
		&overview.AssistantPosts,
		&overview.FeedbackUp,
		&overview.FeedbackDown,
		&overview.ThreadsToday,
		&overview.MessagesToday,
		&lastMessageAt,
	); err != nil {
		return nil, err
	}
	overview.LastMessageAt = parseOptionalTime(lastMessageAt)
	return &overview, nil
}

func (db *DB) ListFrequentQuestions(limit int) ([]*models.AssistantFrequentQuestion, error) {
	if db == nil || db.DB == nil {
		return nil, sql.ErrConnDone
	}
	if limit <= 0 {
		limit = 8
	}

	rows, err := db.Query(`
		WITH grouped AS (
			SELECT
				lower(trim(content)) AS normalized_question,
				COUNT(*) AS question_count,
				MAX(created_at) AS last_asked_at
			FROM assistant_messages
			WHERE role = 'user'
				AND trim(content) <> ''
			GROUP BY lower(trim(content))
		),
		latest AS (
			SELECT
				lower(trim(content)) AS normalized_question,
				content,
				created_at,
				ROW_NUMBER() OVER (
					PARTITION BY lower(trim(content))
					ORDER BY created_at DESC, id DESC
				) AS row_no
			FROM assistant_messages
			WHERE role = 'user'
				AND trim(content) <> ''
		)
		SELECT
			grouped.normalized_question,
			latest.content,
			grouped.question_count,
			grouped.last_asked_at
		FROM grouped
		JOIN latest
			ON latest.normalized_question = grouped.normalized_question
		   AND latest.row_no = 1
		ORDER BY grouped.question_count DESC, grouped.last_asked_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*models.AssistantFrequentQuestion, 0, limit)
	for rows.Next() {
		item := &models.AssistantFrequentQuestion{}
		var lastAskedAt string
		if err := rows.Scan(&item.NormalizedQuestion, &item.Question, &item.Count, &lastAskedAt); err != nil {
			return nil, err
		}
		item.Question = trimPreview(item.Question, 240)
		item.LastAskedAt = parseOptionalTime(lastAskedAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (db *DB) CountThreads() (int, error) {
	if db == nil || db.DB == nil {
		return 0, sql.ErrConnDone
	}
	row := db.QueryRow(`SELECT COUNT(*) FROM assistant_threads`)
	var total int
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (db *DB) ListThreads(limit int, offset int) ([]*models.AssistantThreadSummary, error) {
	if db == nil || db.DB == nil {
		return nil, sql.ErrConnDone
	}
	if limit <= 0 {
		limit = 12
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := db.Query(`
		SELECT
			t.id,
			COALESCE(t.client_id, ''),
			COALESCE(t.page_context, ''),
			COALESCE(t.locale, ''),
			COALESCE(t.status, 'open'),
			COALESCE(t.created_at, ''),
			COALESCE(t.updated_at, ''),
			COALESCE(t.last_message_at, ''),
			COALESCE(stats.total_messages, 0),
			COALESCE(stats.user_messages, 0),
			COALESCE(stats.assistant_messages, 0),
			COALESCE(last_user.content, ''),
			COALESCE(last_assistant.content, ''),
			COALESCE(last_feedback.vote, '')
		FROM assistant_threads t
		LEFT JOIN (
			SELECT
				thread_id,
				COUNT(*) AS total_messages,
				SUM(CASE WHEN role = 'user' THEN 1 ELSE 0 END) AS user_messages,
				SUM(CASE WHEN role = 'assistant' THEN 1 ELSE 0 END) AS assistant_messages
			FROM assistant_messages
			GROUP BY thread_id
		) stats
			ON stats.thread_id = t.id
		LEFT JOIN assistant_messages last_user
			ON last_user.id = (
				SELECT id
				FROM assistant_messages
				WHERE thread_id = t.id AND role = 'user'
				ORDER BY created_at DESC, id DESC
				LIMIT 1
			)
		LEFT JOIN assistant_messages last_assistant
			ON last_assistant.id = (
				SELECT id
				FROM assistant_messages
				WHERE thread_id = t.id AND role = 'assistant'
				ORDER BY created_at DESC, id DESC
				LIMIT 1
			)
		LEFT JOIN assistant_feedback last_feedback
			ON last_feedback.id = (
				SELECT id
				FROM assistant_feedback
				WHERE thread_id = t.id
				ORDER BY updated_at DESC, created_at DESC, id DESC
				LIMIT 1
			)
		ORDER BY COALESCE(t.last_message_at, t.updated_at, t.created_at) DESC, t.id DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*models.AssistantThreadSummary, 0, limit)
	for rows.Next() {
		item := &models.AssistantThreadSummary{}
		var createdAt string
		var updatedAt string
		var lastMessageAt string
		if err := rows.Scan(
			&item.ID,
			&item.ClientID,
			&item.PageContext,
			&item.Locale,
			&item.Status,
			&createdAt,
			&updatedAt,
			&lastMessageAt,
			&item.TotalMessages,
			&item.UserMessages,
			&item.AssistantMessages,
			&item.LastUserMessage,
			&item.LastAssistantMessage,
			&item.LastFeedbackVote,
		); err != nil {
			return nil, err
		}
		item.CreatedAt = parseOptionalTime(createdAt)
		item.UpdatedAt = parseOptionalTime(updatedAt)
		item.LastMessageAt = parseOptionalTime(lastMessageAt)
		item.LastUserMessage = trimPreview(item.LastUserMessage, 200)
		item.LastAssistantMessage = trimPreview(item.LastAssistantMessage, 200)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (db *DB) GetThread(threadID string) (*models.AssistantThreadDetail, error) {
	if db == nil || db.DB == nil {
		return nil, sql.ErrConnDone
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, sql.ErrNoRows
	}

	row := db.QueryRow(`
		SELECT
			t.id,
			COALESCE(t.client_id, ''),
			COALESCE(t.page_context, ''),
			COALESCE(t.locale, ''),
			COALESCE(t.status, 'open'),
			COALESCE(t.created_at, ''),
			COALESCE(t.updated_at, ''),
			COALESCE(t.last_message_at, ''),
			COALESCE(stats.total_messages, 0),
			COALESCE(stats.user_messages, 0),
			COALESCE(stats.assistant_messages, 0),
			COALESCE(last_user.content, ''),
			COALESCE(last_assistant.content, ''),
			COALESCE(last_feedback.vote, '')
		FROM assistant_threads t
		LEFT JOIN (
			SELECT
				thread_id,
				COUNT(*) AS total_messages,
				SUM(CASE WHEN role = 'user' THEN 1 ELSE 0 END) AS user_messages,
				SUM(CASE WHEN role = 'assistant' THEN 1 ELSE 0 END) AS assistant_messages
			FROM assistant_messages
			GROUP BY thread_id
		) stats
			ON stats.thread_id = t.id
		LEFT JOIN assistant_messages last_user
			ON last_user.id = (
				SELECT id
				FROM assistant_messages
				WHERE thread_id = t.id AND role = 'user'
				ORDER BY created_at DESC, id DESC
				LIMIT 1
			)
		LEFT JOIN assistant_messages last_assistant
			ON last_assistant.id = (
				SELECT id
				FROM assistant_messages
				WHERE thread_id = t.id AND role = 'assistant'
				ORDER BY created_at DESC, id DESC
				LIMIT 1
			)
		LEFT JOIN assistant_feedback last_feedback
			ON last_feedback.id = (
				SELECT id
				FROM assistant_feedback
				WHERE thread_id = t.id
				ORDER BY updated_at DESC, created_at DESC, id DESC
				LIMIT 1
			)
		WHERE t.id = ?
		LIMIT 1
	`, threadID)

	thread := &models.AssistantThreadSummary{}
	var createdAt string
	var updatedAt string
	var lastMessageAt string
	if err := row.Scan(
		&thread.ID,
		&thread.ClientID,
		&thread.PageContext,
		&thread.Locale,
		&thread.Status,
		&createdAt,
		&updatedAt,
		&lastMessageAt,
		&thread.TotalMessages,
		&thread.UserMessages,
		&thread.AssistantMessages,
		&thread.LastUserMessage,
		&thread.LastAssistantMessage,
		&thread.LastFeedbackVote,
	); err != nil {
		return nil, err
	}
	thread.CreatedAt = parseOptionalTime(createdAt)
	thread.UpdatedAt = parseOptionalTime(updatedAt)
	thread.LastMessageAt = parseOptionalTime(lastMessageAt)
	thread.LastUserMessage = trimPreview(thread.LastUserMessage, 200)
	thread.LastAssistantMessage = trimPreview(thread.LastAssistantMessage, 200)

	rows, err := db.Query(`
		SELECT
			m.id,
			m.thread_id,
			m.role,
			COALESCE(m.content, ''),
			COALESCE(m.model, ''),
			COALESCE(m.response_id, ''),
			COALESCE(m.created_at, ''),
			COALESCE(f.vote, '')
		FROM assistant_messages m
		LEFT JOIN assistant_feedback f
			ON f.thread_id = m.thread_id
		   AND f.message_id = m.id
		WHERE m.thread_id = ?
		ORDER BY m.created_at ASC, m.id ASC
	`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]*models.AssistantThreadMessage, 0, thread.TotalMessages)
	for rows.Next() {
		message := &models.AssistantThreadMessage{}
		var created string
		if err := rows.Scan(
			&message.ID,
			&message.ThreadID,
			&message.Role,
			&message.Content,
			&message.Model,
			&message.ResponseID,
			&created,
			&message.FeedbackVote,
		); err != nil {
			return nil, err
		}
		message.CreatedAt = parseOptionalTime(created)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &models.AssistantThreadDetail{
		Thread:   thread,
		Messages: messages,
	}, nil
}
