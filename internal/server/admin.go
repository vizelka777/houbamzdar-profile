package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/houbamzdar/bff/internal/models"
)

func buildAdminOverviewPayload(overview *models.AdminOverview) map[string]interface{} {
	if overview == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"total_users":                overview.TotalUsers,
		"admin_users":                overview.AdminUsers,
		"moderator_users":            overview.ModeratorUsers,
		"staff_users":                overview.StaffUsers,
		"banned_users":               overview.BannedUsers,
		"comments_muted_users":       overview.CommentsMutedUsers,
		"publishing_suspended_users": overview.PublishingSuspendedUsers,
		"restricted_users":           overview.RestrictedUsers,
	}
}

func buildAdminUserSummaryPayload(user *models.AdminUserSummary) map[string]interface{} {
	if user == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"id":                         user.ID,
		"preferred_username":         user.PreferredUsername,
		"email":                      user.Email,
		"picture":                    user.Picture,
		"is_moderator":               user.IsModerator,
		"is_admin":                   user.IsAdmin,
		"banned_until":               formatOptionalRFC3339(user.BannedUntil),
		"comments_muted_until":       formatOptionalRFC3339(user.CommentsMutedUntil),
		"publishing_suspended_until": formatOptionalRFC3339(user.PublishingSuspendedUntil),
		"is_banned":                  userIsActivelyBanned(&models.User{BannedUntil: user.BannedUntil}),
		"comments_muted":             userCommentsAreMuted(&models.User{CommentsMutedUntil: user.CommentsMutedUntil}),
		"publishing_suspended":       userPublishingIsSuspended(&models.User{PublishingSuspendedUntil: user.PublishingSuspendedUntil}),
		"moderation_note":            user.ModerationNote,
		"moderated_by_user_id":       user.ModeratedByUserID,
		"moderated_at":               formatOptionalRFC3339(user.ModeratedAt),
		"public_posts_count":         user.PublicPostsCount,
		"public_captures_count":      user.PublicCapturesCount,
	}
}

func buildAdminBackupPayload(backup *models.AdminBackup) map[string]interface{} {
	if backup == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"id":                   backup.ID,
		"status":               backup.Status,
		"trigger_kind":         backup.TriggerKind,
		"storage_key":          backup.StorageKey,
		"checksum_sha256":      backup.ChecksumSHA256,
		"size_bytes":           backup.SizeBytes,
		"started_at":           formatOptionalRFC3339(backup.StartedAt),
		"finished_at":          formatOptionalRFC3339(backup.FinishedAt),
		"initiated_by_user_id": backup.InitiatedByUserID,
		"initiated_by_name":    backup.InitiatedByName,
		"error_message":        backup.ErrorMessage,
	}
}

func (s *Server) handleGetAdminOverview(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanAdmin(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	overview, err := s.DB.GetAdminOverview()
	if err != nil {
		http.Error(w, "failed to load admin overview", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"overview": buildAdminOverviewPayload(overview),
	})
}

func (s *Server) handleListAdminUsers(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanAdmin(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	limit, offset := parseLimitOffset(r, 16)
	filters := models.AdminUserListFilters{
		Query:  r.URL.Query().Get("query"),
		Role:   r.URL.Query().Get("role"),
		Status: r.URL.Query().Get("status"),
		Limit:  limit,
		Offset: offset,
	}

	users, err := s.DB.ListAdminUsers(filters)
	if err != nil {
		http.Error(w, "failed to list admin users", http.StatusInternalServerError)
		return
	}
	if users == nil {
		users = []*models.AdminUserSummary{}
	}

	total, err := s.DB.CountAdminUsers(filters)
	if err != nil {
		http.Error(w, "failed to count admin users", http.StatusInternalServerError)
		return
	}

	payload := make([]map[string]interface{}, 0, len(users))
	for _, user := range users {
		payload = append(payload, buildAdminUserSummaryPayload(user))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"users":    payload,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(users) < total,
		"filters": map[string]interface{}{
			"query":  filters.Query,
			"role":   filters.Role,
			"status": filters.Status,
		},
	})
}

func (s *Server) handleListAdminBackups(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanAdmin(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	limit, offset := parseLimitOffset(r, 12)
	backups, err := s.DB.ListAdminBackups(limit, offset)
	if err != nil {
		http.Error(w, "failed to list backups", http.StatusInternalServerError)
		return
	}
	if backups == nil {
		backups = []*models.AdminBackup{}
	}

	total, err := s.DB.CountAdminBackups()
	if err != nil {
		http.Error(w, "failed to count backups", http.StatusInternalServerError)
		return
	}

	payload := make([]map[string]interface{}, 0, len(backups))
	for _, backup := range backups {
		payload = append(payload, buildAdminBackupPayload(backup))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"backups":  payload,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(backups) < total,
	})
}

func (s *Server) handleRunAdminBackup(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanAdmin(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.Backup == nil || !s.Backup.Enabled() {
		http.Error(w, "backup service is not configured", http.StatusServiceUnavailable)
		return
	}

	backupRecord, err := s.Backup.CreateBackup(r.Context(), actor.ID, "manual")
	if err != nil {
		statusCode := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "already running") {
			statusCode = http.StatusConflict
		}
		http.Error(w, err.Error(), statusCode)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"backup": buildAdminBackupPayload(backupRecord),
	})
}
