package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/houbamzdar/bff/internal/models"
)

const maxPostCommentLength = 1000

func validatePostCommentContent(content string) (string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", errCommentContentRequired
	}
	if utf8.RuneCountInString(trimmed) > maxPostCommentLength {
		return "", errCommentTooLong
	}
	return trimmed, nil
}

var (
	errCommentContentRequired = errors.New("comment content is required")
	errCommentTooLong         = errors.New("comment is too long")
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

	attachPublicURLs(post.Captures, s.Media)

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

func (s *Server) handleCreatePostComment(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	postID := chi.URLParam(r, "postID")

	var req models.CreateCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	content, err := validatePostCommentContent(req.Content)
	if err != nil {
		switch err {
		case errCommentContentRequired:
			http.Error(w, "content is required", http.StatusBadRequest)
		case errCommentTooLong:
			http.Error(w, "comment is too long", http.StatusBadRequest)
		default:
			http.Error(w, "invalid comment content", http.StatusBadRequest)
		}
		return
	}

	exists, err := s.DB.PublicPostExists(postID)
	if err != nil {
		http.Error(w, "failed to validate post", http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "post not found", http.StatusNotFound)
		return
	}

	now := time.Now().UTC()
	comment := &models.Comment{
		ID:           uuid.New().String(),
		PostID:       postID,
		UserID:       user.ID,
		AuthorUserID: user.ID,
		AuthorName:   user.PreferredUsername,
		AuthorAvatar: user.Picture,
		Content:      content,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.DB.CreatePostComment(comment); err != nil {
		http.Error(w, "failed to create comment", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"comment": comment,
	})
}

func (s *Server) handleUpdatePostComment(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	postID := chi.URLParam(r, "postID")
	commentID := chi.URLParam(r, "commentID")

	var req models.CreateCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	content, err := validatePostCommentContent(req.Content)
	if err != nil {
		switch err {
		case errCommentContentRequired:
			http.Error(w, "content is required", http.StatusBadRequest)
		case errCommentTooLong:
			http.Error(w, "comment is too long", http.StatusBadRequest)
		default:
			http.Error(w, "invalid comment content", http.StatusBadRequest)
		}
		return
	}

	comment, err := s.DB.UpdatePostComment(postID, commentID, user.ID, content, time.Now().UTC())
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "comment not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to update comment", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"comment": comment,
	})
}

func (s *Server) handleDeletePostComment(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	postID := chi.URLParam(r, "postID")
	commentID := chi.URLParam(r, "commentID")

	if err := s.DB.DeletePostComment(postID, commentID, user.ID); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "comment not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to delete comment", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true,
	})
}

func (s *Server) handleListPosts(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	limit, offset := parseLimitOffset(r, 20)

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
	limit, offset := parseLimitOffset(r, 10)

	currentUserID := s.currentUserIDFromOptionalSession(r)

	posts, err := s.DB.ListPublicPosts(limit, offset, currentUserID)
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

func (s *Server) handleTogglePostLike(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	postID := chi.URLParam(r, "postID")

	newCount, isLiked, err := s.DB.TogglePostLike(postID, user.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "post not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to toggle like", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"likes_count": newCount,
		"is_liked":    isLiked,
	})
}
