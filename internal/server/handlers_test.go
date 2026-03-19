package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/media"
	"github.com/houbamzdar/bff/internal/models"
	"golang.org/x/oauth2"
)

func TestDeleteMeRemovesRegularUserAccount(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:                  "file:" + filepath.Join(t.TempDir(), "test.db"),
		FrontOrigin:            "https://houbamzdar.cz",
		FrontBaseURL:           "https://houbamzdar.cz",
		SessionCookieName:      "hzd_session",
		BunnyStorageHost:       "storage.bunnycdn.com",
		BunnyPrivateZone:       "private-zone",
		BunnyPrivateStorageKey: "private-key",
		BunnyPublicZone:        "public-zone",
		BunnyPublicStorageKey:  "public-key",
		BunnyPublicBaseURL:     "https://foto.houbamzdar.cz",
	}

	database, err := db.New(cfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "delete-me-user",
		PreferredUsername: "delete-me-user",
		Email:             "delete-me@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "delete-me-access",
		RefreshToken: "delete-me-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	sessionID := "delete-me-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    user.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	srv := New(cfg, database, nil, media.NewBunnyStorage(cfg))

	req := httptest.NewRequest(http.MethodDelete, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK                  bool   `json:"ok"`
		DeletedUserID       int64  `json:"deleted_user_id"`
		DeletedUsername     string `json:"deleted_username"`
		DeletedCaptureCount int    `json:"deleted_capture_count"`
		RedirectURL         string `json:"redirect_url"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !payload.OK {
		t.Fatalf("expected ok=true")
	}
	if payload.DeletedUserID != user.ID {
		t.Fatalf("expected deleted user id %d, got %d", user.ID, payload.DeletedUserID)
	}
	if payload.DeletedUsername != user.PreferredUsername {
		t.Fatalf("expected deleted username %q, got %q", user.PreferredUsername, payload.DeletedUsername)
	}
	if payload.DeletedCaptureCount != 0 {
		t.Fatalf("expected deleted capture count 0, got %d", payload.DeletedCaptureCount)
	}
	if payload.RedirectURL != "https://houbamzdar.cz/" {
		t.Fatalf("unexpected redirect URL %q", payload.RedirectURL)
	}

	if _, err := database.GetUser(user.ID); err != sql.ErrNoRows {
		t.Fatalf("expected user to be deleted, got %v", err)
	}
	if _, err := database.GetSession(sessionID); err != sql.ErrNoRows {
		t.Fatalf("expected session to be deleted, got %v", err)
	}

	cleared := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == cfg.SessionCookieName && cookie.Value == "" {
			cleared = true
			break
		}
	}
	if !cleared {
		t.Fatalf("expected response to clear session cookie")
	}
}

func TestDeleteMeRejectsAdminAccount(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:             "file:" + filepath.Join(t.TempDir(), "test.db"),
		FrontOrigin:       "https://houbamzdar.cz",
		FrontBaseURL:      "https://houbamzdar.cz",
		SessionCookieName: "hzd_session",
	}

	database, err := db.New(cfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	adminUser, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "delete-me-admin",
		PreferredUsername: "site-admin",
		Email:             "site-admin@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "admin-access",
		RefreshToken: "admin-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	if _, err := database.Exec(`UPDATE users SET is_admin = 1 WHERE id = ?`, adminUser.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	adminUser, err = database.GetUser(adminUser.ID)
	if err != nil {
		t.Fatalf("reload admin: %v", err)
	}

	sessionID := "delete-me-admin-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    adminUser.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}

	reloaded, err := database.GetUser(adminUser.ID)
	if err != nil {
		t.Fatalf("reload admin user: %v", err)
	}
	if !reloaded.IsAdmin {
		t.Fatalf("expected admin user to remain in database")
	}
}
