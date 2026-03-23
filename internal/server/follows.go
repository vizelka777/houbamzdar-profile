package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	dbpkg "github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/models"
)

func (s *Server) handleFollowUser(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	targetUserID, err := parseURLInt64Param(r, "userID")
	if err != nil || targetUserID <= 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	if err := s.DB.FollowUser(user.ID, targetUserID); err != nil {
		if errors.Is(err, dbpkg.ErrCannotFollowSelf) {
			http.Error(w, "cannot follow self", http.StatusBadRequest)
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to follow user", http.StatusInternalServerError)
		return
	}

	profile, err := s.DB.GetPublicUserProfileForViewer(targetUserID, user.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load public profile", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":              true,
		"is_following":    profile.IsFollowedByMe,
		"followers_count": profile.FollowersCount,
		"following_count": profile.FollowingCount,
	})
}

func (s *Server) handleUnfollowUser(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	targetUserID, err := parseURLInt64Param(r, "userID")
	if err != nil || targetUserID <= 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	if err := s.DB.UnfollowUser(user.ID, targetUserID); err != nil {
		http.Error(w, "failed to unfollow user", http.StatusInternalServerError)
		return
	}

	profile, err := s.DB.GetPublicUserProfileForViewer(targetUserID, user.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "failed to load public profile", http.StatusInternalServerError)
		return
	}

	followersCount := 0
	followingCount := 0
	if profile != nil {
		followersCount = profile.FollowersCount
		followingCount = profile.FollowingCount
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":              true,
		"is_following":    false,
		"followers_count": followersCount,
		"following_count": followingCount,
	})
}

func (s *Server) handleListMyFollowingUsers(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	limit, offset := parseLimitOffset(r, 24)

	users, err := s.DB.ListFollowingUsers(user.ID, limit, offset)
	if err != nil {
		http.Error(w, "failed to list following users", http.StatusInternalServerError)
		return
	}
	if users == nil {
		users = []*models.FollowedUserProfile{}
	}

	total, err := s.DB.CountFollowingUsers(user.ID)
	if err != nil {
		http.Error(w, "failed to count following users", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"users":    users,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(users) < total,
	})
}
