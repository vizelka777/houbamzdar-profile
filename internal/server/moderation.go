package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/houbamzdar/bff/internal/models"
)

func userCanModerate(user *models.User) bool {
	return user != nil && (user.IsModerator || user.IsAdmin)
}

func userCanAdmin(user *models.User) bool {
	return user != nil && user.IsAdmin
}

func userCanViewAllMapMarkers(user *models.User) bool {
	return userCanAdmin(user) && user.ID == 20
}

func userIsActivelyBanned(user *models.User) bool {
	return user != nil && !user.BannedUntil.IsZero() && user.BannedUntil.After(time.Now().UTC())
}

func userCommentsAreMuted(user *models.User) bool {
	return user != nil && !user.CommentsMutedUntil.IsZero() && user.CommentsMutedUntil.After(time.Now().UTC())
}

func userPublishingIsSuspended(user *models.User) bool {
	return user != nil && !user.PublishingSuspendedUntil.IsZero() && user.PublishingSuspendedUntil.After(time.Now().UTC())
}

func formatOptionalRFC3339(value time.Time) interface{} {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

func parseOptionalRFC3339(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func buildModerationUserPayload(user *models.User) map[string]interface{} {
	if user == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"id":                         user.ID,
		"preferred_username":         user.PreferredUsername,
		"is_moderator":               user.IsModerator,
		"is_admin":                   user.IsAdmin,
		"banned_until":               formatOptionalRFC3339(user.BannedUntil),
		"comments_muted_until":       formatOptionalRFC3339(user.CommentsMutedUntil),
		"publishing_suspended_until": formatOptionalRFC3339(user.PublishingSuspendedUntil),
		"is_banned":                  userIsActivelyBanned(user),
		"comments_muted":             userCommentsAreMuted(user),
		"publishing_suspended":       userPublishingIsSuspended(user),
		"moderation_note":            user.ModerationNote,
		"moderated_by_user_id":       user.ModeratedByUserID,
		"moderated_at":               formatOptionalRFC3339(user.ModeratedAt),
	}
}

func buildModerationActionPayload(action *models.ModerationAction) map[string]interface{} {
	if action == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"id":                action.ID,
		"actor_user_id":     action.ActorUserID,
		"actor_name":        action.ActorName,
		"target_user_id":    action.TargetUserID,
		"target_capture_id": action.TargetCaptureID,
		"target_post_id":    action.TargetPostID,
		"target_comment_id": action.TargetCommentID,
		"action_kind":       action.ActionKind,
		"reason_code":       action.ReasonCode,
		"note":              action.Note,
		"meta_json":         action.MetaJSON,
		"created_at":        formatOptionalRFC3339(action.CreatedAt),
	}
}

func (s *Server) ensureNotBanned(w http.ResponseWriter, user *models.User) bool {
	if !userIsActivelyBanned(user) {
		return true
	}
	http.Error(w, "your account is temporarily banned", http.StatusForbidden)
	return false
}

func (s *Server) ensureCanPublish(w http.ResponseWriter, user *models.User) bool {
	if !s.ensureNotBanned(w, user) {
		return false
	}
	if !userPublishingIsSuspended(user) {
		return true
	}
	http.Error(w, "publishing is temporarily suspended for your account", http.StatusForbidden)
	return false
}

func (s *Server) ensureCanComment(w http.ResponseWriter, user *models.User) bool {
	if !s.ensureNotBanned(w, user) {
		return false
	}
	if !userCommentsAreMuted(user) {
		return true
	}
	http.Error(w, "comments are temporarily disabled for your account", http.StatusForbidden)
	return false
}

func (s *Server) handleGetModerationUser(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanModerate(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	userID, err := parseURLInt64Param(r, "userID")
	if err != nil || userID <= 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	targetUser, err := s.DB.GetUser(userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":   true,
		"user": buildModerationUserPayload(targetUser),
	})
}

func (s *Server) handleSetModerationUserRestrictions(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanModerate(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	userID, err := parseURLInt64Param(r, "userID")
	if err != nil || userID <= 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	if userID == actor.ID {
		http.Error(w, "you cannot moderate your own account", http.StatusUnprocessableEntity)
		return
	}

	var req struct {
		BannedUntil              string `json:"banned_until"`
		CommentsMutedUntil       string `json:"comments_muted_until"`
		PublishingSuspendedUntil string `json:"publishing_suspended_until"`
		ReasonCode               string `json:"reason_code"`
		Note                     string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	bannedUntil, err := parseOptionalRFC3339(req.BannedUntil)
	if err != nil {
		http.Error(w, "invalid banned_until", http.StatusBadRequest)
		return
	}
	commentsMutedUntil, err := parseOptionalRFC3339(req.CommentsMutedUntil)
	if err != nil {
		http.Error(w, "invalid comments_muted_until", http.StatusBadRequest)
		return
	}
	publishingSuspendedUntil, err := parseOptionalRFC3339(req.PublishingSuspendedUntil)
	if err != nil {
		http.Error(w, "invalid publishing_suspended_until", http.StatusBadRequest)
		return
	}

	if err := s.DB.SetUserRestrictions(
		userID,
		actor.ID,
		bannedUntil,
		commentsMutedUntil,
		publishingSuspendedUntil,
		req.ReasonCode,
		req.Note,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to update user restrictions", http.StatusInternalServerError)
		return
	}

	targetUser, err := s.DB.GetUser(userID)
	if err != nil {
		http.Error(w, "failed to reload user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":   true,
		"user": buildModerationUserPayload(targetUser),
	})
}

func (s *Server) handleListModerationUserActions(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanModerate(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	userID, err := parseURLInt64Param(r, "userID")
	if err != nil || userID <= 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	if _, err := s.DB.GetUser(userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}

	limit, offset := parseLimitOffset(r, 10)
	actions, err := s.DB.ListModerationActionsByTargetUser(userID, limit, offset)
	if err != nil {
		http.Error(w, "failed to list moderation actions", http.StatusInternalServerError)
		return
	}
	if actions == nil {
		actions = []*models.ModerationAction{}
	}

	total, err := s.DB.CountModerationActionsByTargetUser(userID)
	if err != nil {
		http.Error(w, "failed to count moderation actions", http.StatusInternalServerError)
		return
	}

	payload := make([]map[string]interface{}, 0, len(actions))
	for _, action := range actions {
		payload = append(payload, buildModerationActionPayload(action))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"actions":  payload,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(actions) < total,
	})
}

func (s *Server) handleListModerationHiddenCaptures(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanModerate(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	limit, offset := parseLimitOffset(r, 12)
	captures, err := s.DB.ListModerationHiddenCaptures(limit, offset)
	if err != nil {
		http.Error(w, "failed to list hidden captures", http.StatusInternalServerError)
		return
	}
	if captures == nil {
		captures = []*models.Capture{}
	} else {
		attachPublicURLs(captures, s.Media)
	}

	total, err := s.DB.CountModerationHiddenCaptures()
	if err != nil {
		http.Error(w, "failed to count hidden captures", http.StatusInternalServerError)
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

func (s *Server) handleListModerationHiddenPosts(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanModerate(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	limit, offset := parseLimitOffset(r, 8)
	posts, err := s.DB.ListModerationHiddenPosts(limit, offset, actor.ID)
	if err != nil {
		http.Error(w, "failed to list hidden posts", http.StatusInternalServerError)
		return
	}
	if posts == nil {
		posts = []*models.Post{}
	} else {
		for _, post := range posts {
			attachPublicURLs(post.Captures, s.Media)
		}
	}

	total, err := s.DB.CountModerationHiddenPosts()
	if err != nil {
		http.Error(w, "failed to count hidden posts", http.StatusInternalServerError)
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

func (s *Server) handleListModerationHiddenComments(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanModerate(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	limit, offset := parseLimitOffset(r, 12)
	comments, err := s.DB.ListModerationHiddenComments(limit, offset)
	if err != nil {
		http.Error(w, "failed to list hidden comments", http.StatusInternalServerError)
		return
	}
	if comments == nil {
		comments = []*models.ModerationHiddenComment{}
	}

	total, err := s.DB.CountModerationHiddenComments()
	if err != nil {
		http.Error(w, "failed to count hidden comments", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"comments": comments,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(comments) < total,
	})
}

func (s *Server) handleSetModerationUserRoles(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanAdmin(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	userID, err := parseURLInt64Param(r, "userID")
	if err != nil || userID <= 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	if userID == actor.ID {
		http.Error(w, "you cannot change your own roles", http.StatusUnprocessableEntity)
		return
	}

	var req struct {
		IsModerator bool   `json:"is_moderator"`
		IsAdmin     bool   `json:"is_admin"`
		ReasonCode  string `json:"reason_code"`
		Note        string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.DB.SetUserRoles(userID, actor.ID, req.IsModerator, req.IsAdmin, req.ReasonCode, req.Note); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to update user roles", http.StatusInternalServerError)
		return
	}

	targetUser, err := s.DB.GetUser(userID)
	if err != nil {
		http.Error(w, "failed to reload user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":   true,
		"user": buildModerationUserPayload(targetUser),
	})
}

func decodeVisibilityModerationRequest(r *http.Request) (bool, string, string, error) {
	var req struct {
		Hidden     bool   `json:"hidden"`
		ReasonCode string `json:"reason_code"`
		Note       string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return false, "", "", err
	}
	return req.Hidden, req.ReasonCode, req.Note, nil
}

func (s *Server) handleSetModerationCaptureVisibility(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanModerate(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	captureID := chi.URLParam(r, "captureID")
	hidden, reasonCode, note, err := decodeVisibilityModerationRequest(r)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.DB.SetCaptureModeratorHidden(captureID, actor.ID, hidden, reasonCode, note); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "capture not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to update capture visibility", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"hidden": hidden,
	})
}

func (s *Server) handleSetModerationPostVisibility(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanModerate(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	postID := chi.URLParam(r, "postID")
	hidden, reasonCode, note, err := decodeVisibilityModerationRequest(r)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.DB.SetPostModeratorHidden(postID, actor.ID, hidden, reasonCode, note); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "post not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to update post visibility", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"hidden": hidden,
	})
}

func (s *Server) handleSetModerationCommentVisibility(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanModerate(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	commentID := chi.URLParam(r, "commentID")
	hidden, reasonCode, note, err := decodeVisibilityModerationRequest(r)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.DB.SetCommentModeratorHidden(commentID, actor.ID, hidden, reasonCode, note); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "comment not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to update comment visibility", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"hidden": hidden,
	})
}
