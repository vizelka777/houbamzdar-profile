package server

import (
	"bytes"
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
	adminUser, err = database.BootstrapAdminByUserID(adminUser.ID, false)
	if err != nil {
		t.Fatalf("bootstrap admin: %v", err)
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

func TestPostMeNicknameSuggestsAlternativesForTakenValue(t *testing.T) {
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

	if _, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "nickname-owner",
		PreferredUsername: "atlas",
	}, &oauth2.Token{
		AccessToken:  "atlas-access",
		RefreshToken: "atlas-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("create owner of taken nickname: %v", err)
	}

	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "nickname-editor",
		PreferredUsername: "boris",
	}, &oauth2.Token{
		AccessToken:  "boris-access",
		RefreshToken: "boris-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create editing user: %v", err)
	}

	sessionID := "nickname-editor-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    user.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/me/nickname", bytes.NewBufferString(`{"preferred_username":"atlas"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK                   bool     `json:"ok"`
		Code                 string   `json:"code"`
		Message              string   `json:"message"`
		PreferredSuggestions []string `json:"preferred_suggestions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode nickname conflict payload: %v", err)
	}

	if payload.OK {
		t.Fatalf("expected ok=false for conflict payload")
	}
	if payload.Code != "preferred_username_taken" {
		t.Fatalf("unexpected conflict code %q", payload.Code)
	}
	if len(payload.PreferredSuggestions) == 0 {
		t.Fatalf("expected nickname suggestions for taken nickname")
	}
}

func TestPostMeNicknameUpdatesPreferredUsername(t *testing.T) {
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

	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "nickname-save-user",
		PreferredUsername: "oldnick",
	}, &oauth2.Token{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	sessionID := "nickname-save-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    user.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/me/nickname", bytes.NewBufferString(`{"preferred_username":"Žaneta7"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK                bool   `json:"ok"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode nickname update payload: %v", err)
	}

	if !payload.OK {
		t.Fatalf("expected ok=true for nickname update")
	}
	if payload.PreferredUsername != "Žaneta7" {
		t.Fatalf("unexpected preferred_username %q", payload.PreferredUsername)
	}

	reloaded, err := database.GetUser(user.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.PreferredUsername != "Žaneta7" {
		t.Fatalf("expected stored preferred_username to update, got %q", reloaded.PreferredUsername)
	}
}
