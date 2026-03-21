package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/houbamzdar/bff/internal/models"
)

func buildAdminOverviewPayload(overview *models.AdminOverview) map[string]interface{} {
	if overview == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"total_users":                 overview.TotalUsers,
		"admin_users":                 overview.AdminUsers,
		"moderator_users":             overview.ModeratorUsers,
		"staff_users":                 overview.StaffUsers,
		"banned_users":                overview.BannedUsers,
		"comments_muted_users":        overview.CommentsMutedUsers,
		"publishing_suspended_users":  overview.PublishingSuspendedUsers,
		"restricted_users":            overview.RestrictedUsers,
		"public_posts":                overview.PublicPosts,
		"public_captures":             overview.PublicCaptures,
		"hidden_posts":                overview.HiddenPosts,
		"hidden_comments":             overview.HiddenComments,
		"hidden_captures":             overview.HiddenCaptures,
		"pending_publication_review":  overview.PendingPublicationReview,
		"failed_publication_review":   overview.FailedPublicationReview,
		"rejected_publication_review": overview.RejectedPublicationReview,
	}
}

func buildAdminUserSummaryPayload(user *models.AdminUserSummary) map[string]interface{} {
	if user == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"id":                         user.ID,
		"preferred_username":         user.PreferredUsername,
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

func buildAdminSystemPayload(status *models.AdminSystemStatus) map[string]interface{} {
	if status == nil {
		return map[string]interface{}{}
	}
	payload := map[string]interface{}{
		"backup_enabled":             status.BackupEnabled,
		"backup_scheduler_enabled":   status.BackupSchedulerEnabled,
		"backup_interval_hours":      status.BackupIntervalHours,
		"backup_retention_days":      status.BackupRetentionDays,
		"backup_max_completed":       status.BackupMaxCompleted,
		"backup_storage_backend":     status.BackupStorageBackend,
		"publish_default_model":      status.PublishDefaultModel,
		"moderator_default_model":    status.ModeratorDefaultModel,
		"validator_config_reachable": status.ValidatorConfigReachable,
		"validator_config_error":     status.ValidatorConfigError,
	}
	if status.LatestCompletedBackup != nil {
		payload["latest_completed_backup"] = buildAdminBackupPayload(status.LatestCompletedBackup)
	} else {
		payload["latest_completed_backup"] = nil
	}
	return payload
}

func (s *Server) loadAdminSystemStatus(r *http.Request) *models.AdminSystemStatus {
	status := &models.AdminSystemStatus{}
	if s.Backup != nil {
		status.BackupEnabled = s.Backup.Enabled()
		status.BackupSchedulerEnabled = s.Backup.SchedulerEnabled()
		status.BackupIntervalHours = s.Backup.IntervalHours()
		status.BackupRetentionDays = s.Backup.RetentionDays()
		status.BackupMaxCompleted = s.Backup.MaxCompletedBackups()
		status.BackupStorageBackend = s.Backup.StorageBackendName()
		latestBackup, err := s.DB.GetLatestCompletedAdminBackup()
		if err == nil {
			status.LatestCompletedBackup = latestBackup
		}
	}

	publishModel, moderatorModel, reachable, errMessage := s.fetchAdminValidatorConfig(r)
	status.PublishDefaultModel = publishModel
	status.ModeratorDefaultModel = moderatorModel
	status.ValidatorConfigReachable = reachable
	status.ValidatorConfigError = errMessage
	return status
}

func (s *Server) fetchAdminValidatorConfig(r *http.Request) (string, string, bool, string) {
	rawURL := strings.TrimSpace(s.Config.CaptureAIValidatorURL)
	if rawURL == "" {
		return "", "", false, "validator není nakonfigurovaný"
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", "", false, "neplatná validator URL"
	}
	parsedURL.Path = "/config"
	parsedURL.RawQuery = ""

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return "", "", false, "nepodařilo se připravit dotaz na validator"
	}

	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", false, "validator config není dostupný"
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", false, "validator config vrátil chybu"
	}

	var payload struct {
		PublishDefaultModel   string `json:"publish_default_model"`
		ModeratorDefaultModel string `json:"moderator_default_model"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", false, "validator config má neplatný formát"
	}
	return payload.PublishDefaultModel, payload.ModeratorDefaultModel, true, ""
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
		"system":   buildAdminSystemPayload(s.loadAdminSystemStatus(r)),
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

func isMissingStorageObjectError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "status 404")
}

func (s *Server) deleteUserAccountData(ctx context.Context, userID int64) (*models.User, int, error) {
	if s.Media == nil || !s.Media.Enabled() {
		return nil, 0, http.ErrNotSupported
	}

	targetUser, err := s.DB.GetUser(userID)
	if err != nil {
		return nil, 0, err
	}

	captureRefs, err := s.DB.ListUserCaptureStorageRefs(userID)
	if err != nil {
		return nil, 0, err
	}
	for _, captureRef := range captureRefs {
		if captureRef == nil {
			continue
		}
		if s.Media.PublicObjectNeedsDelete(captureRef.PrivateStorageKey, captureRef.PublicStorageKey) {
			if err := s.Media.DeletePublic(ctx, captureRef.PublicStorageKey); err != nil && !isMissingStorageObjectError(err) {
				return nil, 0, err
			}
		}
		if captureRef.PrivateStorageKey != "" {
			if err := s.Media.DeletePrivate(ctx, captureRef.PrivateStorageKey); err != nil && !isMissingStorageObjectError(err) {
				return nil, 0, err
			}
		}
	}

	if err := s.DB.DeleteUserByAdmin(userID); err != nil {
		return nil, 0, err
	}

	return targetUser, len(captureRefs), nil
}

func (s *Server) handleDeleteAdminUser(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanAdmin(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.Media == nil || !s.Media.Enabled() {
		http.Error(w, "capture storage is not configured", http.StatusServiceUnavailable)
		return
	}

	userID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil || userID <= 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	if userID == actor.ID {
		http.Error(w, "cannot delete your own account", http.StatusForbidden)
		return
	}
	if userID == 20 {
		http.Error(w, "cannot delete the primary admin account", http.StatusForbidden)
		return
	}

	targetUser, err := s.DB.GetUser(userID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if targetUser.IsAdmin {
		http.Error(w, "cannot delete an admin account", http.StatusForbidden)
		return
	}

	deletedUser, deletedCaptureCount, err := s.deleteUserAccountData(r.Context(), userID)
	if err != nil {
		if err == http.ErrNotSupported {
			http.Error(w, "capture storage is not configured", http.StatusServiceUnavailable)
			return
		}
		if isMissingStorageObjectError(err) {
			http.Error(w, "failed to delete capture files", http.StatusBadGateway)
			return
		}
		http.Error(w, "failed to delete user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":                    true,
		"deleted_user_id":       userID,
		"deleted_username":      deletedUser.PreferredUsername,
		"deleted_capture_count": deletedCaptureCount,
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

func (s *Server) handlePruneAdminBackups(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanAdmin(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.Backup == nil || !s.Backup.Enabled() {
		http.Error(w, "backup service is not configured", http.StatusServiceUnavailable)
		return
	}

	deletedCount, err := s.Backup.PruneExpiredBackups(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":            true,
		"deleted_count": deletedCount,
	})
}
