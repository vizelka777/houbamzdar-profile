package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/models"
	"golang.org/x/oauth2"
)

func TestAdminManualGrantRequiresAdminAuthorization(t *testing.T) {
	t.Parallel()

	database, cfg, user := setupAdminManualGrantTest(t, nil)
	srv := New(cfg, database, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/bonus/grant", strings.NewReader(`{"user_id":42,"idempotency_key":"grant-no-admin"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: "session-admin-grant"})

	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}

	if user.Email == "" {
		t.Fatalf("test setup failed: expected seeded user email")
	}
}

func TestAdminManualGrantAllowsConfiguredAdminEmail(t *testing.T) {
	t.Parallel()

	database, cfg, _ := setupAdminManualGrantTest(t, []string{"admin@example.test"})
	srv := New(cfg, database, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/bonus/grant", strings.NewReader(`{"user_id":42,"idempotency_key":"grant-admin-ok"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: "session-admin-grant"})

	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func setupAdminManualGrantTest(t *testing.T, adminEmails []string) (*db.DB, *config.Config, *models.User) {
	t.Helper()

	cfg := &config.Config{
		DBURL:             "file:" + filepath.Join(t.TempDir(), "test.db"),
		FrontOrigin:       "https://houbamzdar.cz",
		SessionCookieName: "hzd_session",
		AdminEmails:       adminEmails,
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
		Sub:               "manual-grant-admin",
		PreferredUsername: "admin",
		Email:             "admin@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "admin-access",
		RefreshToken: "admin-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := database.CreateSession(&models.Session{
		SessionID: "session-admin-grant",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	return database, cfg, user
}
