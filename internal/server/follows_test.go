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
)

func TestFollowEndpointsAndPublicProfileState(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:             "file:" + filepath.Join(t.TempDir(), "follows.db"),
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

	makeUser := func(sub string) *models.User {
		t.Helper()
		user, _, err := database.UpsertUser(&models.OIDCClaims{
			Iss:               "https://ahoj420.eu",
			Sub:               sub,
			PreferredUsername: sub,
			EmailVerified:     true,
		}, &oauth2.Token{
			AccessToken: "token-" + sub,
			Expiry:      time.Now().Add(time.Hour).UTC(),
		})
		if err != nil {
			t.Fatalf("create user %s: %v", sub, err)
		}
		return user
	}

	follower := makeUser("follower")
	target := makeUser("target")

	sessionID := "follow-session-001"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    follower.ID,
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	guestReq := httptest.NewRequest(http.MethodPost, "/api/users/"+strconv.FormatInt(target.ID, 10)+"/follow", nil)
	guestRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(guestRec, guestReq)
	if guestRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for guest follow, got %d", guestRec.Code)
	}

	followReq := httptest.NewRequest(http.MethodPost, "/api/users/"+strconv.FormatInt(target.ID, 10)+"/follow", nil)
	followReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	followRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(followRec, followReq)
	if followRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for follow, got %d: %s", followRec.Code, followRec.Body.String())
	}

	var followPayload struct {
		OK             bool `json:"ok"`
		IsFollowing    bool `json:"is_following"`
		FollowersCount int  `json:"followers_count"`
	}
	if err := json.NewDecoder(followRec.Body).Decode(&followPayload); err != nil {
		t.Fatalf("decode follow payload: %v", err)
	}
	if !followPayload.OK || !followPayload.IsFollowing || followPayload.FollowersCount != 1 {
		t.Fatalf("unexpected follow payload: %+v", followPayload)
	}

	profileReq := httptest.NewRequest(http.MethodGet, "/api/public/users/"+strconv.FormatInt(target.ID, 10), nil)
	profileReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	profileRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(profileRec, profileReq)
	if profileRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for public profile, got %d: %s", profileRec.Code, profileRec.Body.String())
	}

	var profilePayload struct {
		OK   bool `json:"ok"`
		User struct {
			ID             int64 `json:"id"`
			FollowersCount int   `json:"followers_count"`
			IsFollowedByMe bool  `json:"is_followed_by_me"`
		} `json:"user"`
	}
	if err := json.NewDecoder(profileRec.Body).Decode(&profilePayload); err != nil {
		t.Fatalf("decode public profile payload: %v", err)
	}
	if !profilePayload.OK || !profilePayload.User.IsFollowedByMe || profilePayload.User.FollowersCount != 1 {
		t.Fatalf("unexpected public profile follow state: %+v", profilePayload)
	}

	followingReq := httptest.NewRequest(http.MethodGet, "/api/me/following?limit=10&offset=0", nil)
	followingReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	followingRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(followingRec, followingReq)
	if followingRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for following list, got %d: %s", followingRec.Code, followingRec.Body.String())
	}

	var followingPayload struct {
		OK    bool                         `json:"ok"`
		Users []*models.FollowedUserProfile `json:"users"`
		Total int                          `json:"total"`
	}
	if err := json.NewDecoder(followingRec.Body).Decode(&followingPayload); err != nil {
		t.Fatalf("decode following payload: %v", err)
	}
	if !followingPayload.OK || followingPayload.Total != 1 || len(followingPayload.Users) != 1 {
		t.Fatalf("unexpected following payload: %+v", followingPayload)
	}
	if followingPayload.Users[0].ID != target.ID {
		t.Fatalf("expected followed user id %d, got %d", target.ID, followingPayload.Users[0].ID)
	}

	unfollowReq := httptest.NewRequest(http.MethodDelete, "/api/users/"+strconv.FormatInt(target.ID, 10)+"/follow", nil)
	unfollowReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	unfollowRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(unfollowRec, unfollowReq)
	if unfollowRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for unfollow, got %d: %s", unfollowRec.Code, unfollowRec.Body.String())
	}

	var unfollowPayload struct {
		OK             bool `json:"ok"`
		IsFollowing    bool `json:"is_following"`
		FollowersCount int  `json:"followers_count"`
	}
	if err := json.NewDecoder(unfollowRec.Body).Decode(&unfollowPayload); err != nil {
		t.Fatalf("decode unfollow payload: %v", err)
	}
	if !unfollowPayload.OK || unfollowPayload.IsFollowing || unfollowPayload.FollowersCount != 0 {
		t.Fatalf("unexpected unfollow payload: %+v", unfollowPayload)
	}
}
