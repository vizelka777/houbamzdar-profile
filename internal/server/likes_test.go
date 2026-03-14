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
	"github.com/houbamzdar/bff/internal/models"
	"golang.org/x/oauth2"
	_ "modernc.org/sqlite"
)

func TestPostLikes(t *testing.T) {
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

	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "like-test-user",
		PreferredUsername: "liker",
		Email:             "liker@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken: "token",
		Expiry:      time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	post := &models.Post{
		ID:        "post-to-like",
		UserID:    user.ID,
		Content:   "Test post content",
		Status:    "published",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	guestReq := httptest.NewRequest(http.MethodPost, "/api/posts/"+post.ID+"/like", nil)
	guestRec := httptest.NewRecorder()
	srv := New(cfg, database, nil, nil)
	srv.Router.ServeHTTP(guestRec, guestReq)
	if guestRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for guest like, got %d", guestRec.Code)
	}

	sessionID := "session-like-001"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// 1. Missing post should return 404 even for authenticated users.
	missingReq := httptest.NewRequest(http.MethodPost, "/api/posts/missing-post/like", nil)
	missingReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, missingReq)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing post, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Like the post.
	likeReq := httptest.NewRequest(http.MethodPost, "/api/posts/"+post.ID+"/like", nil)
	likeReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec = httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, likeReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for like, got %d: %s", rec.Code, rec.Body.String())
	}

	var res struct {
		OK         bool `json:"ok"`
		LikesCount int  `json:"likes_count"`
		IsLiked    bool `json:"is_liked"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !res.OK || res.LikesCount != 1 || !res.IsLiked {
		t.Fatalf("unexpected like result: %+v", res)
	}

	// 3. Verify in public feed for the liker.
	feedReq := httptest.NewRequest(http.MethodGet, "/api/public/posts?limit=10", nil)
	feedReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec = httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, feedReq)

	var feedRes struct {
		OK    bool           `json:"ok"`
		Posts []*models.Post `json:"posts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&feedRes); err != nil {
		t.Fatalf("decode feed response: %v", err)
	}

	if len(feedRes.Posts) != 1 || feedRes.Posts[0].LikesCount != 1 || !feedRes.Posts[0].IsLikedByMe {
		t.Fatalf("unexpected feed state after like: %+v", feedRes.Posts[0])
	}

	guestFeedReq := httptest.NewRequest(http.MethodGet, "/api/public/posts?limit=10", nil)
	rec = httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, guestFeedReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for guest feed, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&feedRes); err != nil {
		t.Fatalf("decode guest feed response: %v", err)
	}
	if len(feedRes.Posts) != 1 || feedRes.Posts[0].LikesCount != 1 || feedRes.Posts[0].IsLikedByMe {
		t.Fatalf("unexpected guest feed state after like: %+v", feedRes.Posts[0])
	}

	// 4. Unlike the post.
	rec = httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, likeReq) // Toggle again

	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode response again: %v", err)
	}

	if !res.OK || res.LikesCount != 0 || res.IsLiked {
		t.Fatalf("unexpected unlike result: %+v", res)
	}

	// 5. Verify in public feed after unlike.
	rec = httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, feedReq)
	if err := json.NewDecoder(rec.Body).Decode(&feedRes); err != nil {
		t.Fatalf("decode feed response again: %v", err)
	}

	if feedRes.Posts[0].LikesCount != 0 || feedRes.Posts[0].IsLikedByMe {
		t.Fatalf("unexpected feed state after unlike: %+v", feedRes.Posts[0])
	}
}
