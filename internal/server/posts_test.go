package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

func TestCreatePostCommentRequiresAuthAndAppearsInPublicFeed(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:             "file:" + filepath.Join(t.TempDir(), "test.db"),
		FrontOrigin:       "https://houbamzdar.cz",
		SessionCookieName: "hzd_session",
	}

	database, err := db.New(cfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	author, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "comment-post-author",
		PreferredUsername: "autor",
		Email:             "author@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "author-access",
		RefreshToken: "author-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	commenter, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "comment-post-commenter",
		PreferredUsername: "komentator",
		Email:             "commenter@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "commenter-access",
		RefreshToken: "commenter-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create commenter: %v", err)
	}

	postCreatedAt := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.UTC)
	post := &models.Post{
		ID:        "post-with-comments",
		UserID:    author.ID,
		Content:   "Prispevek k testu komentaru",
		Status:    "published",
		CreatedAt: postCreatedAt,
		UpdatedAt: postCreatedAt,
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	guestReq := httptest.NewRequest(
		http.MethodPost,
		"/api/posts/"+post.ID+"/comments",
		strings.NewReader(`{"content":"Host nema povoleno komentovat"}`),
	)
	guestReq.Header.Set("Content-Type", "application/json")
	guestRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(guestRec, guestReq)
	if guestRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for guest comment, got %d", guestRec.Code)
	}

	sessionID := "session-comment-001"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    commenter.ID,
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	authReq := httptest.NewRequest(
		http.MethodPost,
		"/api/posts/"+post.ID+"/comments",
		strings.NewReader(`{"content":"Skvela publikace."}`),
	)
	authReq.Header.Set("Content-Type", "application/json")
	authReq.AddCookie(&http.Cookie{
		Name:  cfg.SessionCookieName,
		Value: sessionID,
	})
	authRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(authRec, authReq)
	if authRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated comment, got %d: %s", authRec.Code, authRec.Body.String())
	}

	var createPayload struct {
		OK      bool `json:"ok"`
		Comment struct {
			Content    string `json:"content"`
			AuthorName string `json:"author_name"`
		} `json:"comment"`
	}
	if err := json.NewDecoder(authRec.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create comment payload: %v", err)
	}
	if !createPayload.OK {
		t.Fatalf("expected ok=true for comment creation")
	}
	if createPayload.Comment.Content != "Skvela publikace." {
		t.Fatalf("unexpected created comment content: %q", createPayload.Comment.Content)
	}
	if createPayload.Comment.AuthorName != commenter.PreferredUsername {
		t.Fatalf("unexpected created comment author: %q", createPayload.Comment.AuthorName)
	}

	publicReq := httptest.NewRequest(http.MethodGet, "/api/public/posts?limit=10&offset=0", nil)
	publicRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(publicRec, publicReq)
	if publicRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for public feed, got %d: %s", publicRec.Code, publicRec.Body.String())
	}

	var publicPayload struct {
		OK    bool `json:"ok"`
		Posts []struct {
			ID       string `json:"id"`
			Comments []struct {
				Content    string `json:"content"`
				AuthorName string `json:"author_name"`
			} `json:"comments"`
		} `json:"posts"`
	}
	if err := json.NewDecoder(publicRec.Body).Decode(&publicPayload); err != nil {
		t.Fatalf("decode public posts payload: %v", err)
	}
	if !publicPayload.OK {
		t.Fatalf("expected ok=true for public posts")
	}
	if len(publicPayload.Posts) != 1 {
		t.Fatalf("expected 1 public post, got %d", len(publicPayload.Posts))
	}
	if len(publicPayload.Posts[0].Comments) != 1 {
		t.Fatalf("expected 1 comment in public feed, got %d", len(publicPayload.Posts[0].Comments))
	}
	if publicPayload.Posts[0].Comments[0].Content != "Skvela publikace." {
		t.Fatalf("unexpected public comment content: %q", publicPayload.Posts[0].Comments[0].Content)
	}
}

func TestUnlockCaptureCoordinatesEndpointIsIdempotent(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:             "file:" + filepath.Join(t.TempDir(), "test.db"),
		FrontOrigin:       "https://houbamzdar.cz",
		SessionCookieName: "hzd_session",
	}

	database, err := db.New(cfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	owner, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "unlock-endpoint-owner",
		PreferredUsername: "owner",
		Email:             "owner@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{AccessToken: "owner-access", RefreshToken: "owner-refresh", Expiry: time.Now().Add(time.Hour).UTC()})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	viewer, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "unlock-endpoint-viewer",
		PreferredUsername: "viewer",
		Email:             "viewer@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{AccessToken: "viewer-access", RefreshToken: "viewer-refresh", Expiry: time.Now().Add(time.Hour).UTC()})
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}

	if _, err := database.Exec(`UPDATE users SET balance = 2 WHERE id = ?`, viewer.ID); err != nil {
		t.Fatalf("seed viewer balance: %v", err)
	}

	lat := 49.123
	lng := 17.456
	acc := 3.2
	capture := &models.Capture{
		ID:                "unlock-endpoint-cap",
		UserID:            owner.ID,
		OriginalFileName:  "capture.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         111,
		Width:             1000,
		Height:            700,
		CapturedAt:        time.Now().UTC().Add(-2 * time.Hour),
		UploadedAt:        time.Now().UTC().Add(-2 * time.Hour),
		Latitude:          &lat,
		Longitude:         &lng,
		AccuracyMeters:    &acc,
		Status:            "published",
		PrivateStorageKey: "captures/private/unlock-endpoint-cap.jpg",
		PublicStorageKey:  "captures/public/unlock-endpoint-cap.jpg",
		PublishedAt:       time.Now().UTC().Add(-time.Hour),
	}
	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create capture: %v", err)
	}

	if err := database.CreateSession(&models.Session{
		SessionID: "unlock-session",
		UserID:    viewer.ID,
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	firstReq := httptest.NewRequest(http.MethodPost, "/api/captures/"+capture.ID+"/unlock-coordinates", nil)
	firstReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: "unlock-session"})
	firstRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("expected first unlock status 200, got %d: %s", firstRec.Code, firstRec.Body.String())
	}

	var firstPayload struct {
		OK              bool    `json:"ok"`
		AlreadyUnlocked bool    `json:"already_unlocked"`
		Latitude        float64 `json:"latitude"`
		Longitude       float64 `json:"longitude"`
	}
	if err := json.NewDecoder(firstRec.Body).Decode(&firstPayload); err != nil {
		t.Fatalf("decode first unlock payload: %v", err)
	}
	if !firstPayload.OK || firstPayload.AlreadyUnlocked {
		t.Fatalf("unexpected first unlock payload: %+v", firstPayload)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/captures/"+capture.ID+"/unlock-coordinates", nil)
	secondReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: "unlock-session"})
	secondRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("expected second unlock status 200, got %d: %s", secondRec.Code, secondRec.Body.String())
	}

	var secondPayload struct {
		OK              bool `json:"ok"`
		AlreadyUnlocked bool `json:"already_unlocked"`
	}
	if err := json.NewDecoder(secondRec.Body).Decode(&secondPayload); err != nil {
		t.Fatalf("decode second unlock payload: %v", err)
	}
	if !secondPayload.OK || !secondPayload.AlreadyUnlocked {
		t.Fatalf("unexpected second unlock payload: %+v", secondPayload)
	}

	var viewerBalance int
	if err := database.QueryRow(`SELECT balance FROM users WHERE id = ?`, viewer.ID).Scan(&viewerBalance); err != nil {
		t.Fatalf("select viewer balance: %v", err)
	}
	if viewerBalance != 1 {
		t.Fatalf("expected viewer balance=1 after idempotent unlock, got %d", viewerBalance)
	}
}
