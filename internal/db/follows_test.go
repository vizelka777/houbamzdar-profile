package db

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/models"
	"golang.org/x/oauth2"
)

func TestFollowUsersLifecycle(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:             "file:" + filepath.Join(t.TempDir(), "follow.db"),
		FrontOrigin:       "https://houbamzdar.cz",
		SessionCookieName: "hzd_session",
	}

	database, err := New(cfg)
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

	alice := makeUser("alice")
	bob := makeUser("bob")
	charlie := makeUser("charlie")

	if err := database.FollowUser(alice.ID, bob.ID); err != nil {
		t.Fatalf("alice follows bob: %v", err)
	}
	if err := database.FollowUser(alice.ID, charlie.ID); err != nil {
		t.Fatalf("alice follows charlie: %v", err)
	}
	if err := database.FollowUser(bob.ID, charlie.ID); err != nil {
		t.Fatalf("bob follows charlie: %v", err)
	}

	if err := database.FollowUser(alice.ID, alice.ID); !errors.Is(err, ErrCannotFollowSelf) {
		t.Fatalf("expected ErrCannotFollowSelf, got %v", err)
	}

	total, err := database.CountFollowingUsers(alice.ID)
	if err != nil {
		t.Fatalf("count alice following: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected alice following count 2, got %d", total)
	}

	following, err := database.ListFollowingUsers(alice.ID, 10, 0)
	if err != nil {
		t.Fatalf("list alice following: %v", err)
	}
	if len(following) != 2 {
		t.Fatalf("expected 2 followed users, got %d", len(following))
	}

	foundBob := false
	foundCharlie := false
	for _, user := range following {
		switch user.ID {
		case bob.ID:
			foundBob = true
		case charlie.ID:
			foundCharlie = true
			if user.FollowersCount != 2 {
				t.Fatalf("expected charlie to have 2 followers, got %d", user.FollowersCount)
			}
		}
	}
	if !foundBob || !foundCharlie {
		t.Fatalf("expected bob and charlie in following list, got %+v", following)
	}

	profile, err := database.GetPublicUserProfileForViewer(charlie.ID, alice.ID)
	if err != nil {
		t.Fatalf("get charlie public profile for alice: %v", err)
	}
	if !profile.IsFollowedByMe {
		t.Fatalf("expected alice to follow charlie")
	}
	if profile.FollowersCount != 2 {
		t.Fatalf("expected charlie followers count 2, got %d", profile.FollowersCount)
	}

	if err := database.UnfollowUser(alice.ID, bob.ID); err != nil {
		t.Fatalf("alice unfollows bob: %v", err)
	}

	total, err = database.CountFollowingUsers(alice.ID)
	if err != nil {
		t.Fatalf("count alice following after unfollow: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected alice following count 1 after unfollow, got %d", total)
	}
}
