package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/chattoken"
	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/media"
	"github.com/houbamzdar/bff/internal/models"
	"golang.org/x/oauth2"
)

func TestCreateChatTokenReturnsSignedIdentityToken(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:               "file:" + filepath.Join(t.TempDir(), "test.db"),
		FrontOrigin:         "https://houbamzdar.cz",
		SessionCookieName:   "hzd_session",
		ChatTokenSecret:     "chat-secret-123",
		ChatTokenIssuer:     "https://api.houbamzdar.cz",
		ChatTokenAudience:   "houbamzdar-chat",
		ChatTokenTTLSeconds: 300,
		ChatAPIBaseURL:      "https://chat.houbamzdar.cz",
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
		Sub:               "chat-token-user",
		PreferredUsername: "chatuser",
		Email:             "chat@example.test",
		EmailVerified:     true,
		Picture:           "https://img.example.test/chatuser.png",
	}, &oauth2.Token{
		AccessToken:  "chat-access",
		RefreshToken: "chat-refresh",
		Expiry:       time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	sessionID := "chat-token-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    user.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	srv := New(cfg, database, nil, nil, media.NewBunnyStorage(cfg))

	req := httptest.NewRequest(http.MethodPost, "/api/chat/token", nil)
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK         bool   `json:"ok"`
		Token      string `json:"token"`
		ExpiresAt  string `json:"expires_at"`
		APIBaseURL string `json:"api_base_url"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.OK || payload.Token == "" {
		t.Fatalf("expected signed token payload, got %+v", payload)
	}
	if payload.APIBaseURL != "https://chat.houbamzdar.cz" {
		t.Fatalf("unexpected chat api base url %q", payload.APIBaseURL)
	}

	claims, err := chattoken.Verify(
		cfg.ChatTokenSecret,
		cfg.ChatTokenIssuer,
		cfg.ChatTokenAudience,
		payload.Token,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("verify chat token: %v", err)
	}
	if claims.UserID != user.ID {
		t.Fatalf("expected token user id %d, got %d", user.ID, claims.UserID)
	}
	if claims.PreferredUsername != user.PreferredUsername {
		t.Fatalf("expected username %q, got %q", user.PreferredUsername, claims.PreferredUsername)
	}
	if claims.Picture != user.Picture {
		t.Fatalf("expected picture %q, got %q", user.Picture, claims.Picture)
	}
}
