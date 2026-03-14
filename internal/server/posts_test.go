package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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

func TestUpdateAndDeletePostComment(t *testing.T) {
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

	createUser := func(sub, username, email string) *models.User {
		user, _, err := database.UpsertUser(&models.OIDCClaims{
			Iss:               "https://ahoj420.eu",
			Sub:               sub,
			PreferredUsername: username,
			Email:             email,
			EmailVerified:     true,
		}, &oauth2.Token{
			AccessToken:  sub + "-access",
			RefreshToken: sub + "-refresh",
			Expiry:       time.Now().Add(time.Hour).UTC(),
		})
		if err != nil {
			t.Fatalf("create user %s: %v", username, err)
		}
		return user
	}

	author := createUser("post-owner", "autor", "author@example.test")
	commenter := createUser("post-commenter", "komentator", "commenter@example.test")
	other := createUser("post-other", "jiny", "other@example.test")

	post := &models.Post{
		ID:        "comment-edit-post",
		UserID:    author.ID,
		Content:   "Prispevek pro upravu komentare",
		Status:    "published",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	createSession := func(id string, userID int64) {
		if err := database.CreateSession(&models.Session{
			SessionID: id,
			UserID:    userID,
			ExpiresAt: time.Now().Add(time.Hour).UTC(),
		}); err != nil {
			t.Fatalf("create session %s: %v", id, err)
		}
	}
	createSession("commenter-session", commenter.ID)
	createSession("other-session", other.ID)

	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/posts/"+post.ID+"/comments",
		strings.NewReader(`{"content":"Puvodni komentar"}`),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: "commenter-session"})
	createRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for comment creation, got %d: %s", createRec.Code, createRec.Body.String())
	}

	var createPayload struct {
		Comment struct {
			ID string `json:"id"`
		} `json:"comment"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create payload: %v", err)
	}
	if createPayload.Comment.ID == "" {
		t.Fatalf("expected created comment id")
	}

	updateReq := httptest.NewRequest(
		http.MethodPut,
		"/api/posts/"+post.ID+"/comments/"+createPayload.Comment.ID,
		strings.NewReader(`{"content":"Upraveny komentar"}`),
	)
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: "commenter-session"})
	updateRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for comment update, got %d: %s", updateRec.Code, updateRec.Body.String())
	}

	var updatePayload struct {
		OK      bool `json:"ok"`
		Comment struct {
			ID           string `json:"id"`
			AuthorUserID int64  `json:"author_user_id"`
			Content      string `json:"content"`
		} `json:"comment"`
	}
	if err := json.NewDecoder(updateRec.Body).Decode(&updatePayload); err != nil {
		t.Fatalf("decode update payload: %v", err)
	}
	if !updatePayload.OK {
		t.Fatalf("expected ok=true for update")
	}
	if updatePayload.Comment.Content != "Upraveny komentar" {
		t.Fatalf("unexpected updated content: %q", updatePayload.Comment.Content)
	}
	if updatePayload.Comment.AuthorUserID != commenter.ID {
		t.Fatalf("unexpected author_user_id %d", updatePayload.Comment.AuthorUserID)
	}

	otherDeleteReq := httptest.NewRequest(
		http.MethodDelete,
		"/api/posts/"+post.ID+"/comments/"+createPayload.Comment.ID,
		nil,
	)
	otherDeleteReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: "other-session"})
	otherDeleteRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(otherDeleteRec, otherDeleteReq)
	if otherDeleteRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for deleting someone else's comment, got %d", otherDeleteRec.Code)
	}

	publicReq := httptest.NewRequest(http.MethodGet, "/api/public/posts?limit=10&offset=0", nil)
	publicRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(publicRec, publicReq)
	if publicRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for public posts after update, got %d: %s", publicRec.Code, publicRec.Body.String())
	}

	var publicPayload struct {
		Posts []struct {
			Comments []struct {
				ID           string `json:"id"`
				AuthorUserID int64  `json:"author_user_id"`
				Content      string `json:"content"`
			} `json:"comments"`
		} `json:"posts"`
	}
	if err := json.NewDecoder(publicRec.Body).Decode(&publicPayload); err != nil {
		t.Fatalf("decode public payload: %v", err)
	}
	if len(publicPayload.Posts) != 1 || len(publicPayload.Posts[0].Comments) != 1 {
		t.Fatalf("expected updated comment in public feed, got %+v", publicPayload)
	}
	if publicPayload.Posts[0].Comments[0].Content != "Upraveny komentar" {
		t.Fatalf("expected updated public comment, got %q", publicPayload.Posts[0].Comments[0].Content)
	}
	if publicPayload.Posts[0].Comments[0].AuthorUserID != commenter.ID {
		t.Fatalf("expected public comment author id %d, got %d", commenter.ID, publicPayload.Posts[0].Comments[0].AuthorUserID)
	}

	deleteReq := httptest.NewRequest(
		http.MethodDelete,
		"/api/posts/"+post.ID+"/comments/"+createPayload.Comment.ID,
		nil,
	)
	deleteReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: "commenter-session"})
	deleteRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for comment delete, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}

	publicReq = httptest.NewRequest(http.MethodGet, "/api/public/posts?limit=10&offset=0", nil)
	publicRec = httptest.NewRecorder()
	srv.Router.ServeHTTP(publicRec, publicReq)
	if publicRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for public posts after delete, got %d: %s", publicRec.Code, publicRec.Body.String())
	}

	var publicPayloadAfterDelete struct {
		Posts []struct {
			Comments []struct {
				ID string `json:"id"`
			} `json:"comments"`
		} `json:"posts"`
	}
	if err := json.NewDecoder(publicRec.Body).Decode(&publicPayloadAfterDelete); err != nil {
		t.Fatalf("decode public payload after delete: %v", err)
	}
	if len(publicPayloadAfterDelete.Posts) != 1 {
		t.Fatalf("expected 1 public post after delete, got %d", len(publicPayloadAfterDelete.Posts))
	}
	if len(publicPayloadAfterDelete.Posts[0].Comments) != 0 {
		t.Fatalf("expected no comments after delete, got %d", len(publicPayloadAfterDelete.Posts[0].Comments))
	}
}

func TestPublicUserProfileRoutes(t *testing.T) {
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

	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:                 "https://ahoj420.eu",
		Sub:                 "public-profile-user",
		PreferredUsername:   "atlas",
		Email:               "atlas@example.test",
		EmailVerified:       true,
		PhoneNumberVerified: true,
		Picture:             "https://img.example.test/avatar.jpg",
	}, &oauth2.Token{
		AccessToken:  "atlas-access",
		RefreshToken: "atlas-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := database.UpdateAboutMe(user.ID, "Lovim hlavne v jizerskych lesich."); err != nil {
		t.Fatalf("update about me: %v", err)
	}

	capturedAt := time.Date(2026, time.March, 14, 8, 15, 0, 0, time.UTC)
	capture := &models.Capture{
		ID:                "public-user-capture",
		UserID:            user.ID,
		OriginalFileName:  "atlas.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         5120,
		Width:             1600,
		Height:            1200,
		CapturedAt:        capturedAt,
		UploadedAt:        capturedAt.Add(2 * time.Minute),
		Latitude:          func() *float64 { value := 50.12345; return &value }(),
		Longitude:         func() *float64 { value := 14.98765; return &value }(),
		CoordinatesFree:   true,
		Status:            "published",
		PrivateStorageKey: "captures/private/public-user-capture.jpg",
		PublicStorageKey:  "captures/public/public-user-capture.jpg",
		PublishedAt:       capturedAt.Add(3 * time.Minute),
	}
	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create capture: %v", err)
	}

	post := &models.Post{
		ID:        "public-user-post",
		UserID:    user.ID,
		Content:   "Moje verejna publikace",
		Status:    "published",
		CreatedAt: capturedAt.Add(4 * time.Minute),
		UpdatedAt: capturedAt.Add(4 * time.Minute),
		Captures:  []*models.Capture{capture},
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	srv := New(cfg, database, nil, media.NewBunnyStorage(cfg))

	profileReq := httptest.NewRequest(http.MethodGet, "/api/public/users/"+strconv.FormatInt(user.ID, 10), nil)
	profileRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(profileRec, profileReq)
	if profileRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for public profile, got %d: %s", profileRec.Code, profileRec.Body.String())
	}

	var profilePayload struct {
		OK   bool `json:"ok"`
		User struct {
			ID                  int64  `json:"id"`
			PreferredUsername   string `json:"preferred_username"`
			AboutMe             string `json:"about_me"`
			PublicPostsCount    int    `json:"public_posts_count"`
			PublicCapturesCount int    `json:"public_captures_count"`
		} `json:"user"`
	}
	if err := json.NewDecoder(profileRec.Body).Decode(&profilePayload); err != nil {
		t.Fatalf("decode public profile payload: %v", err)
	}
	if !profilePayload.OK {
		t.Fatalf("expected ok=true for public profile")
	}
	if profilePayload.User.ID != user.ID {
		t.Fatalf("unexpected public user id %d", profilePayload.User.ID)
	}
	if profilePayload.User.PreferredUsername != user.PreferredUsername {
		t.Fatalf("unexpected public username %q", profilePayload.User.PreferredUsername)
	}
	if profilePayload.User.AboutMe != "Lovim hlavne v jizerskych lesich." {
		t.Fatalf("unexpected about me %q", profilePayload.User.AboutMe)
	}
	if profilePayload.User.PublicPostsCount != 1 || profilePayload.User.PublicCapturesCount != 1 {
		t.Fatalf("unexpected public counters: %+v", profilePayload.User)
	}

	postsReq := httptest.NewRequest(http.MethodGet, "/api/public/users/"+strconv.FormatInt(user.ID, 10)+"/posts?limit=6&offset=0", nil)
	postsRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(postsRec, postsReq)
	if postsRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for public user posts, got %d: %s", postsRec.Code, postsRec.Body.String())
	}

	var postsPayload struct {
		OK      bool `json:"ok"`
		Total   int  `json:"total"`
		HasMore bool `json:"has_more"`
		Posts   []struct {
			ID           string `json:"id"`
			AuthorUserID int64  `json:"author_user_id"`
			Captures     []struct {
				ID           string `json:"id"`
				AuthorUserID int64  `json:"author_user_id"`
				PublicURL    string `json:"public_url"`
			} `json:"captures"`
		} `json:"posts"`
	}
	if err := json.NewDecoder(postsRec.Body).Decode(&postsPayload); err != nil {
		t.Fatalf("decode public user posts payload: %v", err)
	}
	if !postsPayload.OK || postsPayload.Total != 1 || postsPayload.HasMore {
		t.Fatalf("unexpected public posts meta: %+v", postsPayload)
	}
	if len(postsPayload.Posts) != 1 || len(postsPayload.Posts[0].Captures) != 1 {
		t.Fatalf("unexpected public posts payload: %+v", postsPayload)
	}
	if postsPayload.Posts[0].AuthorUserID != user.ID {
		t.Fatalf("unexpected post author_user_id %d", postsPayload.Posts[0].AuthorUserID)
	}
	if postsPayload.Posts[0].Captures[0].AuthorUserID != user.ID {
		t.Fatalf("unexpected capture author_user_id %d", postsPayload.Posts[0].Captures[0].AuthorUserID)
	}
	if postsPayload.Posts[0].Captures[0].PublicURL == "" {
		t.Fatalf("expected public capture URL in public user posts")
	}

	capturesReq := httptest.NewRequest(http.MethodGet, "/api/public/users/"+strconv.FormatInt(user.ID, 10)+"/captures?limit=24&offset=0", nil)
	capturesRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(capturesRec, capturesReq)
	if capturesRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for public user captures, got %d: %s", capturesRec.Code, capturesRec.Body.String())
	}

	var capturesPayload struct {
		OK       bool `json:"ok"`
		Total    int  `json:"total"`
		Captures []struct {
			ID           string  `json:"id"`
			AuthorUserID int64   `json:"author_user_id"`
			PublicURL    string  `json:"public_url"`
			Latitude     float64 `json:"latitude"`
			Longitude    float64 `json:"longitude"`
		} `json:"captures"`
	}
	if err := json.NewDecoder(capturesRec.Body).Decode(&capturesPayload); err != nil {
		t.Fatalf("decode public user captures payload: %v", err)
	}
	if !capturesPayload.OK || capturesPayload.Total != 1 || len(capturesPayload.Captures) != 1 {
		t.Fatalf("unexpected public captures payload: %+v", capturesPayload)
	}
	if capturesPayload.Captures[0].AuthorUserID != user.ID {
		t.Fatalf("unexpected capture author_user_id %d", capturesPayload.Captures[0].AuthorUserID)
	}
	if capturesPayload.Captures[0].PublicURL == "" {
		t.Fatalf("expected public capture URL in public user captures")
	}
	if capturesPayload.Captures[0].Latitude == 0 || capturesPayload.Captures[0].Longitude == 0 {
		t.Fatalf("expected public coordinates in user captures payload")
	}
}
