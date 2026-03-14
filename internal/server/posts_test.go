package server

import (
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
	_ "modernc.org/sqlite"
)

func TestListPublicPostsIncludesCapturePublicURLs(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:                  "file:" + filepath.Join(t.TempDir(), "test.db"),
		FrontOrigin:            "https://houbamzdar.cz",
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

	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}
	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "guest-feed-user",
		PreferredUsername: "mykolog",
		Email:             "mykolog@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	capturedAt := time.Date(2026, time.March, 12, 14, 9, 1, 0, time.UTC)
	capture := &models.Capture{
		ID:                "capture-001",
		UserID:            user.ID,
		ClientLocalID:     "local-001",
		OriginalFileName:  "lesni-nalez.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         2048,
		Width:             1600,
		Height:            1200,
		CapturedAt:        capturedAt,
		UploadedAt:        capturedAt.Add(2 * time.Minute),
		Status:            "published",
		PrivateStorageKey: "captures/1/2026/03/capture-001.jpg",
		PublicStorageKey:  "captures/published/1/2026/03/capture-001.jpg",
		PublicURL:         "",
		PublishedAt:       capturedAt.Add(3 * time.Minute),
	}
	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create published capture: %v", err)
	}

	post := &models.Post{
		ID:        "post-001",
		UserID:    user.ID,
		Content:   "Public post with a capture",
		Status:    "published",
		CreatedAt: capturedAt.Add(4 * time.Minute),
		UpdatedAt: capturedAt.Add(4 * time.Minute),
		Captures:  []*models.Capture{capture},
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	srv := New(cfg, database, nil, media.NewBunnyStorage(cfg))

	req := httptest.NewRequest(http.MethodGet, "/api/public/posts?limit=10&offset=0", nil)
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK    bool `json:"ok"`
		Posts []struct {
			ID       string `json:"id"`
			Captures []struct {
				ID        string `json:"id"`
				PublicURL string `json:"public_url"`
			} `json:"captures"`
		} `json:"posts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !payload.OK {
		t.Fatalf("expected ok=true in response")
	}
	if len(payload.Posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(payload.Posts))
	}
	if len(payload.Posts[0].Captures) != 1 {
		t.Fatalf("expected 1 capture, got %d", len(payload.Posts[0].Captures))
	}

	wantPublicURL := "https://foto.houbamzdar.cz/captures/published/1/2026/03/capture-001.jpg"
	if got := payload.Posts[0].Captures[0].PublicURL; got != wantPublicURL {
		t.Fatalf("expected public_url %q, got %q", wantPublicURL, got)
	}
}
