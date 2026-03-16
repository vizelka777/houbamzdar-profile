package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/models"
	"golang.org/x/oauth2"
	_ "modernc.org/sqlite"
)

func TestListModerationUserActionsIncludesUserAndContentActions(t *testing.T) {
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

	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}

	targetUser, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "moderation-target-user",
		PreferredUsername: "target-user",
		Email:             "target@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create target user: %v", err)
	}

	moderator, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "moderation-history-moderator",
		PreferredUsername: "houbamzdar",
		Email:             "moderator@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create moderator: %v", err)
	}
	if !moderator.IsModerator {
		t.Fatalf("expected moderator account to be auto-flagged")
	}

	post := &models.Post{
		ID:        "moderation-history-post",
		UserID:    targetUser.ID,
		Content:   "Post subject to moderation history",
		Status:    "published",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	bannedUntil := time.Now().UTC().Add(2 * time.Hour)
	if err := database.SetUserRestrictions(
		targetUser.ID,
		moderator.ID,
		bannedUntil,
		time.Time{},
		time.Time{},
		"manual_review",
		"temporary ban",
	); err != nil {
		t.Fatalf("set restrictions: %v", err)
	}

	if err := database.SetPostModeratorHidden(post.ID, moderator.ID, true, "manual_review", "hide post"); err != nil {
		t.Fatalf("hide post: %v", err)
	}

	sessionID := "moderation-history-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    moderator.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create moderator session: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/moderation/users/"+strconv.FormatInt(targetUser.ID, 10)+"/actions?limit=10&offset=0",
		nil,
	)
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
		Actions []struct {
			ActionKind   string `json:"action_kind"`
			ActorName    string `json:"actor_name"`
			TargetUserID int64  `json:"target_user_id"`
			TargetPostID string `json:"target_post_id"`
			Note         string `json:"note"`
		} `json:"actions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !payload.OK {
		t.Fatalf("expected ok=true")
	}
	if payload.Total != 2 {
		t.Fatalf("expected total 2, got %d", payload.Total)
	}
	if payload.HasMore {
		t.Fatalf("expected has_more=false")
	}
	if len(payload.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(payload.Actions))
	}

	var (
		foundRestrictions bool
		foundHiddenPost   bool
	)
	for _, action := range payload.Actions {
		if action.ActorName != moderator.PreferredUsername {
			t.Fatalf("expected actor name %q, got %q", moderator.PreferredUsername, action.ActorName)
		}
		if action.TargetUserID != targetUser.ID {
			t.Fatalf("expected target user id %d, got %d", targetUser.ID, action.TargetUserID)
		}
		switch action.ActionKind {
		case "user_restrictions_updated":
			foundRestrictions = true
		case "post_visibility_updated":
			if action.TargetPostID != post.ID {
				t.Fatalf("expected target post id %q, got %q", post.ID, action.TargetPostID)
			}
			if action.Note != "hide post" {
				t.Fatalf("expected post moderation note to round-trip, got %q", action.Note)
			}
			foundHiddenPost = true
		}
	}

	if !foundRestrictions {
		t.Fatalf("expected user_restrictions_updated action in payload")
	}
	if !foundHiddenPost {
		t.Fatalf("expected post_visibility_updated action in payload")
	}
}

func TestListModerationHiddenContentEndpoints(t *testing.T) {
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

	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}

	author, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "hidden-content-author",
		PreferredUsername: "autor-hidden",
		Email:             "hidden-author@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	commenter, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "hidden-content-commenter",
		PreferredUsername: "komentar-hidden",
		Email:             "hidden-commenter@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create commenter: %v", err)
	}

	moderator, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "hidden-content-moderator",
		PreferredUsername: "houbamzdar",
		Email:             "hidden-moderator@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create moderator: %v", err)
	}
	if !moderator.IsModerator {
		t.Fatalf("expected moderator account to be auto-flagged")
	}

	capturedAt := time.Date(2026, time.March, 16, 11, 0, 0, 0, time.UTC)
	lat := 49.1951
	lon := 16.6068
	capture := &models.Capture{
		ID:                "hidden-dashboard-capture-001",
		UserID:            author.ID,
		OriginalFileName:  "hidden-dashboard-capture.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         4096,
		Width:             1600,
		Height:            1200,
		CapturedAt:        capturedAt,
		UploadedAt:        capturedAt,
		Latitude:          &lat,
		Longitude:         &lon,
		Status:            "published",
		PrivateStorageKey: "captures/private/hidden-dashboard-capture-001.jpg",
		PublicStorageKey:  "captures/public/hidden-dashboard-capture-001.jpg",
		PublishedAt:       capturedAt.Add(2 * time.Minute),
	}
	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create capture: %v", err)
	}

	post := &models.Post{
		ID:        "hidden-dashboard-post-001",
		UserID:    author.ID,
		Content:   "Skrytý příspěvek pro moderation dashboard",
		Status:    "published",
		CreatedAt: capturedAt.Add(3 * time.Minute),
		UpdatedAt: capturedAt.Add(3 * time.Minute),
		Captures:  []*models.Capture{capture},
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	comment := &models.Comment{
		ID:        "hidden-dashboard-comment-001",
		PostID:    post.ID,
		UserID:    commenter.ID,
		Content:   "Skrytý komentář pro moderation dashboard",
		CreatedAt: capturedAt.Add(4 * time.Minute),
		UpdatedAt: capturedAt.Add(4 * time.Minute),
	}
	if err := database.CreatePostComment(comment); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	if err := database.SetCaptureModeratorHidden(capture.ID, moderator.ID, true, "manual_review", "hide capture"); err != nil {
		t.Fatalf("hide capture: %v", err)
	}
	if err := database.SetPostModeratorHidden(post.ID, moderator.ID, true, "manual_review", "hide post"); err != nil {
		t.Fatalf("hide post: %v", err)
	}
	if err := database.SetCommentModeratorHidden(comment.ID, moderator.ID, true, "manual_review", "hide comment"); err != nil {
		t.Fatalf("hide comment: %v", err)
	}

	sessionID := "hidden-dashboard-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    moderator.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create moderator session: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	t.Run("hidden captures", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/moderation/hidden-captures?limit=10&offset=0", nil)
		req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
		rec := httptest.NewRecorder()
		srv.Router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var payload struct {
			OK       bool              `json:"ok"`
			Total    int               `json:"total"`
			HasMore  bool              `json:"has_more"`
			Captures []*models.Capture `json:"captures"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode captures payload: %v", err)
		}

		if !payload.OK || payload.Total != 1 || payload.HasMore || len(payload.Captures) != 1 {
			t.Fatalf("unexpected captures payload: %+v", payload)
		}
		if payload.Captures[0].ID != capture.ID {
			t.Fatalf("expected capture id %q, got %q", capture.ID, payload.Captures[0].ID)
		}
		if payload.Captures[0].AuthorName != author.PreferredUsername {
			t.Fatalf("expected author name %q, got %q", author.PreferredUsername, payload.Captures[0].AuthorName)
		}
		if payload.Captures[0].ModeratedByName != moderator.PreferredUsername {
			t.Fatalf("expected moderator name %q, got %q", moderator.PreferredUsername, payload.Captures[0].ModeratedByName)
		}
	})

	t.Run("hidden posts", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/moderation/hidden-posts?limit=10&offset=0", nil)
		req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
		rec := httptest.NewRecorder()
		srv.Router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var payload struct {
			OK      bool           `json:"ok"`
			Total   int            `json:"total"`
			HasMore bool           `json:"has_more"`
			Posts   []*models.Post `json:"posts"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode posts payload: %v", err)
		}

		if !payload.OK || payload.Total != 1 || payload.HasMore || len(payload.Posts) != 1 {
			t.Fatalf("unexpected posts payload: %+v", payload)
		}
		if payload.Posts[0].ID != post.ID {
			t.Fatalf("expected post id %q, got %q", post.ID, payload.Posts[0].ID)
		}
		if payload.Posts[0].AuthorName != author.PreferredUsername {
			t.Fatalf("expected post author name %q, got %q", author.PreferredUsername, payload.Posts[0].AuthorName)
		}
		if payload.Posts[0].ModeratedByName != moderator.PreferredUsername {
			t.Fatalf("expected moderator name %q, got %q", moderator.PreferredUsername, payload.Posts[0].ModeratedByName)
		}
		if len(payload.Posts[0].Captures) != 1 || payload.Posts[0].Captures[0].ID != capture.ID {
			t.Fatalf("expected hidden post to include capture %q, got %+v", capture.ID, payload.Posts[0].Captures)
		}
	})

	t.Run("hidden comments", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/moderation/hidden-comments?limit=10&offset=0", nil)
		req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
		rec := httptest.NewRecorder()
		srv.Router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var payload struct {
			OK       bool                              `json:"ok"`
			Total    int                               `json:"total"`
			HasMore  bool                              `json:"has_more"`
			Comments []*models.ModerationHiddenComment `json:"comments"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode comments payload: %v", err)
		}

		if !payload.OK || payload.Total != 1 || payload.HasMore || len(payload.Comments) != 1 {
			t.Fatalf("unexpected comments payload: %+v", payload)
		}
		if payload.Comments[0].ID != comment.ID {
			t.Fatalf("expected comment id %q, got %q", comment.ID, payload.Comments[0].ID)
		}
		if payload.Comments[0].PostID != post.ID {
			t.Fatalf("expected post id %q, got %q", post.ID, payload.Comments[0].PostID)
		}
		if payload.Comments[0].AuthorName != commenter.PreferredUsername {
			t.Fatalf("expected comment author name %q, got %q", commenter.PreferredUsername, payload.Comments[0].AuthorName)
		}
		if payload.Comments[0].ModeratedByName != moderator.PreferredUsername {
			t.Fatalf("expected moderator name %q, got %q", moderator.PreferredUsername, payload.Comments[0].ModeratedByName)
		}
	})
}

func TestModerationUserRolesRouteDisabled(t *testing.T) {
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

	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}

	targetUser, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "roles-disabled-target",
		PreferredUsername: "target-role-disabled",
		Email:             "target-role-disabled@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create target user: %v", err)
	}

	moderator, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "roles-disabled-moderator",
		PreferredUsername: "houbamzdar",
		Email:             "roles-disabled-moderator@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create moderator: %v", err)
	}
	if !moderator.IsModerator {
		t.Fatalf("expected moderator account to be auto-flagged")
	}
	if moderator.IsAdmin {
		t.Fatalf("expected moderator bootstrap account to stay non-admin")
	}

	sessionID := "roles-disabled-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    moderator.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create moderator session: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/moderation/users/"+strconv.FormatInt(targetUser.ID, 10)+"/roles",
		nil,
	)
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for disabled role-management route, got %d: %s", rec.Code, rec.Body.String())
	}
}
