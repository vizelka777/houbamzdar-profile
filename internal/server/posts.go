package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
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

	if len(req.CaptureIDs) > 9 {
		http.Error(w, "maximum 9 photos allowed per post", http.StatusBadRequest)
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

func (s *Server) handleGetPost(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	postID := chi.URLParam(r, "postID")

	post, err := s.DB.GetPost(postID, user.ID)
	if err != nil {
		http.Error(w, "post not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":   true,
		"post": post,
	})
}

func (s *Server) handleUpdatePost(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	postID := chi.URLParam(r, "postID")

	post, err := s.DB.GetPost(postID, user.ID)
	if err != nil {
		http.Error(w, "post not found", http.StatusNotFound)
		return
	}

	var req models.CreatePostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}

	if len(req.CaptureIDs) > 9 {
		http.Error(w, "maximum 9 photos allowed per post", http.StatusBadRequest)
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

	post.Content = req.Content
	post.Captures = captures

	if err := s.DB.UpdatePost(post); err != nil {
		http.Error(w, "failed to update post", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":   true,
		"post": post,
	})
}

func (s *Server) handleDeletePost(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	postID := chi.URLParam(r, "postID")

	if err := s.DB.DeletePost(postID, user.ID); err != nil {
		http.Error(w, "failed to delete post", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true,
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
	} else {
		for _, post := range posts {
			attachPublicURLs(post.Captures, s.Media)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"posts": posts,
	})
}

func (s *Server) handleListPublicPosts(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 10
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	offset := 0
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	posts, err := s.DB.ListPublicPosts(limit, offset)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"posts": posts,
	})
}
