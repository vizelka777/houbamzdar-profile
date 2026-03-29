package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/assistantadmin"
	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/models"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"golang.org/x/oauth2"
)

func seedAssistantAdminTestDatabase(t *testing.T, path string) {
	t.Helper()

	sqlDB, err := sql.Open("libsql", "file:"+path)
	if err != nil {
		t.Fatalf("open assistant test db: %v", err)
	}
	defer sqlDB.Close()

	queries := []string{
		`CREATE TABLE assistant_threads (
			id TEXT PRIMARY KEY,
			client_id TEXT NOT NULL,
			origin TEXT,
			page_context TEXT,
			locale TEXT,
			status TEXT NOT NULL DEFAULT 'open',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_message_at TEXT
		);`,
		`CREATE TABLE assistant_messages (
			id TEXT PRIMARY KEY,
			thread_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			model TEXT,
			response_id TEXT,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE assistant_feedback (
			id TEXT PRIMARY KEY,
			thread_id TEXT NOT NULL,
			message_id TEXT NOT NULL,
			client_id TEXT NOT NULL,
			vote TEXT NOT NULL,
			note TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE assistant_events (
			id TEXT PRIMARY KEY,
			thread_id TEXT,
			client_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			path TEXT,
			payload_json TEXT,
			created_at TEXT NOT NULL
		);`,
	}
	for _, query := range queries {
		if _, err := sqlDB.Exec(query); err != nil {
			t.Fatalf("seed assistant schema: %v", err)
		}
	}

	now := time.Now().UTC()
	thread1Created := now.Add(-2 * time.Hour).Format(time.RFC3339)
	thread1Updated := now.Add(-90 * time.Minute).Format(time.RFC3339)
	thread1Last := now.Add(-80 * time.Minute).Format(time.RFC3339)
	thread2Created := now.Add(-30 * time.Minute).Format(time.RFC3339)
	thread2Updated := now.Add(-20 * time.Minute).Format(time.RFC3339)
	thread2Last := now.Add(-10 * time.Minute).Format(time.RFC3339)

	if _, err := sqlDB.Exec(`
		INSERT INTO assistant_threads (id, client_id, origin, page_context, locale, status, created_at, updated_at, last_message_at)
		VALUES
			('thread-1', 'client-one', 'https://houbamzdar.cz', 'Registrace', 'cs', 'open', ?, ?, ?),
			('thread-2', 'client-two', 'https://houbamzdar.cz', 'Chat', 'cs', 'open', ?, ?, ?)
	`, thread1Created, thread1Updated, thread1Last, thread2Created, thread2Updated, thread2Last); err != nil {
		t.Fatalf("seed assistant threads: %v", err)
	}

	if _, err := sqlDB.Exec(`
		INSERT INTO assistant_messages (id, thread_id, role, content, model, response_id, created_at)
		VALUES
			('m1', 'thread-1', 'user', 'Jak se přihlásím?', '', '', ?),
			('m2', 'thread-1', 'assistant', 'Klikněte na Přihlásit a zvolte poskytovatele.', 'gpt-5-mini', 'resp-1', ?),
			('m3', 'thread-2', 'user', 'Jak se přihlásím?', '', '', ?),
			('m4', 'thread-2', 'assistant', 'Použijte stejného poskytovatele jako při registraci.', 'gpt-5-mini', 'resp-2', ?),
			('m5', 'thread-2', 'user', 'Kde najdu veřejný chat?', '', '', ?)
	`, now.Add(-110*time.Minute).Format(time.RFC3339),
		now.Add(-100*time.Minute).Format(time.RFC3339),
		now.Add(-25*time.Minute).Format(time.RFC3339),
		now.Add(-24*time.Minute).Format(time.RFC3339),
		now.Add(-10*time.Minute).Format(time.RFC3339),
	); err != nil {
		t.Fatalf("seed assistant messages: %v", err)
	}

	if _, err := sqlDB.Exec(`
		INSERT INTO assistant_feedback (id, thread_id, message_id, client_id, vote, note, created_at, updated_at)
		VALUES ('f1', 'thread-2', 'm4', 'client-two', 'up', NULL, ?, ?)
	`, now.Add(-5*time.Minute).Format(time.RFC3339), now.Add(-5*time.Minute).Format(time.RFC3339)); err != nil {
		t.Fatalf("seed assistant feedback: %v", err)
	}
}

func TestAssistantAdminEndpoints(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:             "file:" + filepath.Join(t.TempDir(), "main.db"),
		FrontOrigin:       "https://houbamzdar.cz",
		SessionCookieName: "hzd_session",
	}

	mainDB, err := db.New(cfg)
	if err != nil {
		t.Fatalf("open main db: %v", err)
	}
	t.Cleanup(func() {
		_ = mainDB.Close()
	})

	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}

	adminUser, _, err := mainDB.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "assistant-admin",
		PreferredUsername: "site-admin",
		Email:             "admin@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	adminUser, err = mainDB.BootstrapAdminByUserID(adminUser.ID, false)
	if err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}

	sessionID := "assistant-admin-session"
	if err := mainDB.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    adminUser.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	assistantPath := filepath.Join(t.TempDir(), "assistant.db")
	seedAssistantAdminTestDatabase(t, assistantPath)
	assistantDB, err := assistantadmin.New("file:"+assistantPath, "")
	if err != nil {
		t.Fatalf("open assistant admin db: %v", err)
	}
	t.Cleanup(func() {
		_ = assistantDB.Close()
	})

	srv := New(cfg, mainDB, assistantDB, nil, nil)

	t.Run("overview", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/assistant/overview", nil)
		req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
		rec := httptest.NewRecorder()
		srv.Router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var payload struct {
			OK       bool `json:"ok"`
			Overview struct {
				TotalThreads    int `json:"total_threads"`
				TotalMessages   int `json:"total_messages"`
				UniqueClients   int `json:"unique_clients"`
				FeedbackUp      int `json:"feedback_up"`
				AssistantFields int `json:"assistant_messages"`
			} `json:"overview"`
			FrequentQuestions []struct {
				Question string `json:"question"`
				Count    int    `json:"count"`
			} `json:"frequent_questions"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if !payload.OK {
			t.Fatalf("expected ok=true")
		}
		if payload.Overview.TotalThreads != 2 || payload.Overview.TotalMessages != 5 || payload.Overview.UniqueClients != 2 {
			t.Fatalf("unexpected overview: %+v", payload.Overview)
		}
		if payload.Overview.FeedbackUp != 1 {
			t.Fatalf("expected feedback_up=1, got %+v", payload.Overview)
		}
		if len(payload.FrequentQuestions) == 0 || payload.FrequentQuestions[0].Count != 2 {
			t.Fatalf("unexpected frequent questions: %+v", payload.FrequentQuestions)
		}
	})

	t.Run("threads list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/assistant/threads?limit=1&offset=0", nil)
		req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
		rec := httptest.NewRecorder()
		srv.Router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var payload struct {
			OK      bool `json:"ok"`
			Total   int  `json:"total"`
			HasMore bool `json:"has_more"`
			Threads []struct {
				ID                string `json:"id"`
				LastUserMessage   string `json:"last_user_message"`
				LastFeedbackVote  string `json:"last_feedback_vote"`
				AssistantMessages int    `json:"assistant_messages"`
				TotalMessages     int    `json:"total_messages"`
			} `json:"threads"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if !payload.OK || payload.Total != 2 || !payload.HasMore {
			t.Fatalf("unexpected thread list meta: %+v", payload)
		}
		if len(payload.Threads) != 1 || payload.Threads[0].ID != "thread-2" {
			t.Fatalf("unexpected threads ordering: %+v", payload.Threads)
		}
	})

	t.Run("thread detail", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/assistant/threads/thread-2", nil)
		req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
		rec := httptest.NewRecorder()
		srv.Router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var payload struct {
			OK     bool `json:"ok"`
			Thread struct {
				ID               string `json:"id"`
				TotalMessages    int    `json:"total_messages"`
				LastFeedbackVote string `json:"last_feedback_vote"`
			} `json:"thread"`
			Messages []struct {
				ID           string `json:"id"`
				Role         string `json:"role"`
				FeedbackVote string `json:"feedback_vote"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if !payload.OK || payload.Thread.ID != "thread-2" || payload.Thread.TotalMessages != 3 {
			t.Fatalf("unexpected detail thread payload: %+v", payload.Thread)
		}
		if len(payload.Messages) != 3 {
			t.Fatalf("expected 3 messages, got %+v", payload.Messages)
		}
		if payload.Messages[1].FeedbackVote != "up" {
			t.Fatalf("expected assistant feedback to be returned, got %+v", payload.Messages)
		}
	})
}
