package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/models"
	"golang.org/x/oauth2"
)

func newEmailVerificationTestServer(t *testing.T, emailVerified bool) (*Server, *config.Config, *db.DB, *models.User, string) {
	t.Helper()

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

	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "email-verification-user",
		PreferredUsername: "email-verification-user",
		Email:             "email-verification@example.test",
		EmailVerified:     emailVerified,
	}, &oauth2.Token{
		AccessToken:  "email-verification-access",
		RefreshToken: "email-verification-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	sessionID := "email-verification-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    user.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	return New(cfg, database, nil, nil, nil), cfg, database, user, sessionID
}

func TestCreatePostRequiresVerifiedEmail(t *testing.T) {
	t.Parallel()

	srv, cfg, _, _, sessionID := newEmailVerificationTestServer(t, false)

	req := httptest.NewRequest(http.MethodPost, "/api/posts", bytes.NewBufferString(`{"content":"Ahoj z lesa"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "confirm your email") {
		t.Fatalf("expected email verification message, got %q", rec.Body.String())
	}
}

func TestCreateCaptureRejectsUnverifiedEmailBeforeStorageChecks(t *testing.T) {
	t.Parallel()

	srv, cfg, _, _, sessionID := newEmailVerificationTestServer(t, false)

	req := httptest.NewRequest(http.MethodPost, "/api/captures", nil)
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateCaptureAllowsVerifiedEmailPastVerificationGate(t *testing.T) {
	t.Parallel()

	srv, cfg, _, _, sessionID := newEmailVerificationTestServer(t, true)

	req := httptest.NewRequest(http.MethodPost, "/api/captures", nil)
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503 after passing email verification gate, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFollowRequiresVerifiedEmail(t *testing.T) {
	t.Parallel()

	srv, cfg, database, _, sessionID := newEmailVerificationTestServer(t, false)

	targetUser, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "email-verification-target",
		PreferredUsername: "email-verification-target",
		Email:             "target@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "target-access",
		RefreshToken: "target-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create target user: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/users/"+strconv.FormatInt(targetUser.ID, 10)+"/follow", nil)
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}
