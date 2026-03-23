package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/houbamzdar/bff/internal/models"
)

func parseLimitOffset(r *http.Request, defaultLimit int) (int, int) {
	limit := defaultLimit
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 && value <= 100 {
		limit = value
	}

	offset := 0
	if value, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && value >= 0 {
		offset = value
	}

	return limit, offset
}

func parseURLInt64Param(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, name), 10, 64)
}

func (s *Server) handleGetPublicUserProfile(w http.ResponseWriter, r *http.Request) {
	userID, err := parseURLInt64Param(r, "userID")
	if err != nil || userID <= 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	profile, err := s.DB.GetPublicUserProfileForViewer(userID, s.currentUserIDFromOptionalSession(r))
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":   true,
		"user": profile,
	})
}

func (s *Server) handleListPublicUserPosts(w http.ResponseWriter, r *http.Request) {
	userID, err := parseURLInt64Param(r, "userID")
	if err != nil || userID <= 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	if _, err := s.DB.GetPublicUserProfile(userID); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}

	limit, offset := parseLimitOffset(r, 6)
	currentUserID := s.currentUserIDFromOptionalSession(r)

	posts, err := s.DB.ListPublicPostsByUser(userID, limit, offset, currentUserID)
	if err != nil {
		http.Error(w, "failed to list public posts", http.StatusInternalServerError)
		return
	}

	if posts == nil {
		posts = []*models.Post{}
	} else {
		for _, post := range posts {
			attachPublicURLs(post.Captures, s.Media)
		}
	}

	total, err := s.DB.CountPublicPostsByUser(userID)
	if err != nil {
		http.Error(w, "failed to count public posts", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"posts":    posts,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(posts) < total,
	})
}

func (s *Server) handleListPublicUserCaptures(w http.ResponseWriter, r *http.Request) {
	userID, err := parseURLInt64Param(r, "userID")
	if err != nil || userID <= 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	if _, err := s.DB.GetPublicUserProfile(userID); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}

	limit, offset := parseLimitOffset(r, 48)
	currentUserID := s.currentUserIDFromOptionalSession(r)

	captures, err := s.DB.ListPublicCapturesByUser(userID, limit, offset, currentUserID)
	if err != nil {
		http.Error(w, "failed to list public captures", http.StatusInternalServerError)
		return
	}

	if captures == nil {
		captures = []*models.Capture{}
	} else {
		attachPublicURLs(captures, s.Media)
	}

	total, err := s.DB.CountPublicCapturesByUser(userID)
	if err != nil {
		http.Error(w, "failed to count public captures", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"captures": captures,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(captures) < total,
	})
}

func (s *Server) handleListPublicUserMapCaptures(w http.ResponseWriter, r *http.Request) {
	userID, err := parseURLInt64Param(r, "userID")
	if err != nil || userID <= 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	if _, err := s.DB.GetPublicUserProfile(userID); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}

	currentUserID := s.currentUserIDFromOptionalSession(r)
	captures, err := s.DB.ListPublicMapCapturesByUser(userID, currentUserID)
	if err != nil {
		http.Error(w, "failed to list public map captures", http.StatusInternalServerError)
		return
	}

	if captures == nil {
		captures = []*models.Capture{}
	} else {
		attachPublicURLs(captures, s.Media)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"captures": captures,
	})
}
