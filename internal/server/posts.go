package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/houbamzdar/bff/internal/models"
)

func (s *Server) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)

	var req models.CreatePostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}

	var captures []*models.Capture
	if len(req.CaptureIDs) > 0 {
		for _, cid := range req.CaptureIDs {
			c, err := s.DB.GetCaptureForUser(cid, user.ID)
			if err != nil {
				http.Error(w, "capture not found or unauthorized", http.StatusBadRequest)
				return
			}
			
			if c.Status != "published" {
				if s.Media != nil && s.Media.Enabled() {
					publicKey, publicURL, err := s.Media.PublishCapture(r.Context(), c)
					if err == nil {
						if err := s.DB.PublishCapture(c.ID, user.ID, publicKey); err == nil {
							c.Status = "published"
							c.PublicStorageKey = publicKey
							c.PublicURL = publicURL
							c.PublishedAt = time.Now()
						}
					}
				}
			}
			captures = append(captures, c)
		}
	}

	post := &models.Post{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Content:   req.Content,
		Status:    "published",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Captures:  captures,
	}

	if err := s.DB.CreatePost(post); err != nil {
		http.Error(w, "failed to create post", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":   true,
		"post": post,
	})
}

func (s *Server) handleListPosts(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)

	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 20
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	offset := 0
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	posts, err := s.DB.ListPosts(user.ID, limit, offset)
	if err != nil {
		http.Error(w, "failed to list posts", http.StatusInternalServerError)
		return
	}

	if posts == nil {
		posts = []*models.Post{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"posts": posts,
	})
}
