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
