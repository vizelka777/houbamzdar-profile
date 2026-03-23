package models

import (
	"strings"
	"time"
)

type User struct {
	ID                       int64     `json:"id"`
	IDPIssuer                string    `json:"-"`
	IDPSub                   string    `json:"-"`
	PreferredUsername        string    `json:"preferred_username"`
	IsModerator              bool      `json:"is_moderator"`
	IsAdmin                  bool      `json:"is_admin"`
	Email                    string    `json:"email"`
	EmailVerified            bool      `json:"email_verified"`
	PhoneNumber              string    `json:"phone_number"`
	PhoneNumberVerified      bool      `json:"phone_number_verified"`
	Picture                  string    `json:"picture"`
	AboutMe                  string    `json:"about_me"`
	BannedUntil              time.Time `json:"banned_until,omitempty"`
	CommentsMutedUntil       time.Time `json:"comments_muted_until,omitempty"`
	PublishingSuspendedUntil time.Time `json:"publishing_suspended_until,omitempty"`
	ModerationNote           string    `json:"moderation_note,omitempty"`
	ModeratedByUserID        int64     `json:"moderated_by_user_id,omitempty"`
	ModeratedAt              time.Time `json:"moderated_at,omitempty"`
	AccessToken              string    `json:"-"`
	RefreshToken             string    `json:"-"`
	TokenExpiresAt           time.Time `json:"-"`
	HoubickaBalance          int64     `json:"houbicka_balance"`
	CreatedAt                time.Time `json:"-"`
	UpdatedAt                time.Time `json:"-"`
	LastIDPSyncAt            time.Time `json:"-"`
}

type Capture struct {
	ID                             string                    `json:"id"`
	UserID                         int64                     `json:"-"`
	AuthorUserID                   int64                     `json:"author_user_id,omitempty"`
	AuthorName                     string                    `json:"author_name,omitempty"`
	AuthorAvatar                   string                    `json:"author_avatar,omitempty"`
	ClientLocalID                  string                    `json:"client_local_id,omitempty"`
	OriginalFileName               string                    `json:"original_file_name"`
	ContentType                    string                    `json:"content_type"`
	SizeBytes                      int64                     `json:"size_bytes"`
	Width                          int                       `json:"width"`
	Height                         int                       `json:"height"`
	CapturedAt                     time.Time                 `json:"captured_at"`
	UploadedAt                     time.Time                 `json:"uploaded_at"`
	Latitude                       *float64                  `json:"latitude,omitempty"`
	Longitude                      *float64                  `json:"longitude,omitempty"`
	AccuracyMeters                 *float64                  `json:"accuracy_meters,omitempty"`
	CoordinatesFree                bool                      `json:"coordinates_free"`
	CoordinatesLocked              bool                      `json:"coordinates_locked"`
	Status                         string                    `json:"status"`
	PublicationReviewStatus        string                    `json:"publication_review_status,omitempty"`
	PublicationReviewReasonCode    string                    `json:"publication_review_reason_code,omitempty"`
	PublicationReviewLastError     string                    `json:"publication_review_last_error,omitempty"`
	PublicationReviewCheckedAt     time.Time                 `json:"publication_review_checked_at,omitempty"`
	PublicationRequestedAt         time.Time                 `json:"publication_requested_at,omitempty"`
	PublicationReviewAttempts      int                       `json:"publication_review_attempts,omitempty"`
	PublicationReviewNextAttemptAt time.Time                 `json:"publication_review_next_attempt_at,omitempty"`
	PrivateStorageKey              string                    `json:"-"`
	PublicStorageKey               string                    `json:"-"`
	ImageURL                       string                    `json:"image_url,omitempty"`
	PublicURL                      string                    `json:"public_url,omitempty"`
	PublishedAt                    time.Time                 `json:"published_at,omitempty"`
	UnlockedAt                     time.Time                 `json:"unlocked_at,omitempty"`
	HasMushrooms                   bool                      `json:"has_mushrooms,omitempty"`
	MushroomPrimaryLatinName       string                    `json:"mushroom_primary_latin_name,omitempty"`
	MushroomPrimaryCzechName       string                    `json:"mushroom_primary_czech_name,omitempty"`
	MushroomPrimaryProbability     float64                   `json:"mushroom_primary_probability,omitempty"`
	MushroomAnalysisAt             time.Time                 `json:"mushroom_analysis_at,omitempty"`
	MushroomSpecies                []*CaptureMushroomSpecies `json:"mushroom_species,omitempty"`
	CountryCode                    string                    `json:"country_code,omitempty"`
	KrajName                       string                    `json:"kraj_name,omitempty"`
	OkresName                      string                    `json:"okres_name,omitempty"`
	ObecName                       string                    `json:"obec_name,omitempty"`
	GeoResolvedAt                  time.Time                 `json:"geo_resolved_at,omitempty"`
	ModeratorHidden                bool                      `json:"moderator_hidden,omitempty"`
	ModerationReasonCode           string                    `json:"moderation_reason_code,omitempty"`
	ModerationNote                 string                    `json:"moderation_note,omitempty"`
	ModeratedByUserID              int64                     `json:"moderated_by_user_id,omitempty"`
	ModeratedByName                string                    `json:"moderated_by_name,omitempty"`
	ModeratedAt                    time.Time                 `json:"moderated_at,omitempty"`
}

type CaptureMushroomAnalysis struct {
	CaptureID                string    `json:"capture_id"`
	HasMushrooms             bool      `json:"has_mushrooms"`
	PrimaryLatinName         string    `json:"primary_latin_name,omitempty"`
	PrimaryCzechOfficialName string    `json:"primary_czech_official_name,omitempty"`
	PrimaryProbability       float64   `json:"primary_probability,omitempty"`
	ModelCode                string    `json:"model_code,omitempty"`
	ReviewSource             string    `json:"review_source,omitempty"`
	ReviewedByUserID         int64     `json:"reviewed_by_user_id,omitempty"`
	ReviewedAt               time.Time `json:"reviewed_at,omitempty"`
	RawJSON                  string    `json:"-"`
	AnalyzedAt               time.Time `json:"analyzed_at"`
}

type CaptureMushroomSpecies struct {
	ID                int64   `json:"id,omitempty"`
	CaptureID         string  `json:"capture_id,omitempty"`
	LatinName         string  `json:"latin_name"`
	CzechOfficialName string  `json:"czech_official_name,omitempty"`
	Probability       float64 `json:"probability"`
}

type CaptureGeoIndex struct {
	CaptureID   string    `json:"capture_id"`
	CountryCode string    `json:"country_code,omitempty"`
	KrajName    string    `json:"kraj_name,omitempty"`
	OkresName   string    `json:"okres_name,omitempty"`
	ObecName    string    `json:"obec_name,omitempty"`
	RawJSON     string    `json:"-"`
	ResolvedAt  time.Time `json:"resolved_at"`
}

type Post struct {
	ID                   string     `json:"id"`
	UserID               int64      `json:"-"`
	AuthorUserID         int64      `json:"author_user_id,omitempty"`
	AuthorName           string     `json:"author_name,omitempty"`
	AuthorAvatar         string     `json:"author_avatar,omitempty"`
	Content              string     `json:"content"`
	Status               string     `json:"status"`
	LikesCount           int        `json:"likes_count"`
	IsLikedByMe          bool       `json:"is_liked_by_me"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	ModeratorHidden      bool       `json:"moderator_hidden,omitempty"`
	ModerationReasonCode string     `json:"moderation_reason_code,omitempty"`
	ModerationNote       string     `json:"moderation_note,omitempty"`
	ModeratedByUserID    int64      `json:"moderated_by_user_id,omitempty"`
	ModeratedByName      string     `json:"moderated_by_name,omitempty"`
	ModeratedAt          time.Time  `json:"moderated_at,omitempty"`
	Captures             []*Capture `json:"captures,omitempty"`
	Comments             []*Comment `json:"comments,omitempty"`
}

type CreatePostRequest struct {
	Content    string   `json:"content"`
	CaptureIDs []string `json:"capture_ids"`
}

func (c *Capture) PublishedStorageKey() string {
	if c == nil {
		return ""
	}
	if key := strings.TrimSpace(c.PrivateStorageKey); key != "" {
		return key
	}
	return strings.TrimSpace(c.PublicStorageKey)
}

func (c *Capture) IsPubliclyShared() bool {
	return c != nil && c.Status == "published" && c.PublishedStorageKey() != ""
}

type Comment struct {
	ID                   string    `json:"id"`
	PostID               string    `json:"-"`
	UserID               int64     `json:"-"`
	AuthorUserID         int64     `json:"author_user_id,omitempty"`
	AuthorName           string    `json:"author_name,omitempty"`
	AuthorAvatar         string    `json:"author_avatar,omitempty"`
	Content              string    `json:"content"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	ModeratorHidden      bool      `json:"moderator_hidden,omitempty"`
	ModerationReasonCode string    `json:"moderation_reason_code,omitempty"`
	ModerationNote       string    `json:"moderation_note,omitempty"`
	ModeratedByUserID    int64     `json:"moderated_by_user_id,omitempty"`
	ModeratedByName      string    `json:"moderated_by_name,omitempty"`
	ModeratedAt          time.Time `json:"moderated_at,omitempty"`
}

type ModerationHiddenComment struct {
	ID                   string    `json:"id"`
	PostID               string    `json:"post_id"`
	PostContent          string    `json:"post_content,omitempty"`
	PostAuthorUserID     int64     `json:"post_author_user_id,omitempty"`
	PostAuthorName       string    `json:"post_author_name,omitempty"`
	AuthorUserID         int64     `json:"author_user_id,omitempty"`
	AuthorName           string    `json:"author_name,omitempty"`
	AuthorAvatar         string    `json:"author_avatar,omitempty"`
	Content              string    `json:"content"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	ModerationReasonCode string    `json:"moderation_reason_code,omitempty"`
	ModerationNote       string    `json:"moderation_note,omitempty"`
	ModeratedByUserID    int64     `json:"moderated_by_user_id,omitempty"`
	ModeratedByName      string    `json:"moderated_by_name,omitempty"`
	ModeratedAt          time.Time `json:"moderated_at,omitempty"`
}

type CreateCommentRequest struct {
	Content string `json:"content"`
}

type Session struct {
	SessionID string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

type OIDCLoginState struct {
	State             string
	Nonce             string
	PKCEVerifier      string
	CreatedAt         time.Time
	ExpiresAt         time.Time
	PostLoginRedirect string
}

type OIDCClaims struct {
	Iss                 string `json:"iss"`
	Sub                 string `json:"sub"`
	PreferredUsername   string `json:"preferred_username"`
	Email               string `json:"email"`
	EmailVerified       bool   `json:"email_verified"`
	PhoneNumber         string `json:"phone_number"`
	PhoneNumberVerified bool   `json:"phone_number_verified"`
	Picture             string `json:"picture"`
}

type HoubickaOperation struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	ReasonCode     string    `json:"reason_code"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	ActorUserID    int64     `json:"actor_user_id,omitempty"`
	TargetUserID   int64     `json:"target_user_id,omitempty"`
	CaptureID      string    `json:"capture_id,omitempty"`
	MetaJSON       string    `json:"meta_json,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type HoubickaEntry struct {
	ID           int64     `json:"id"`
	OperationID  string    `json:"operation_id"`
	UserID       int64     `json:"user_id"`
	AmountSigned int64     `json:"amount_signed"`
	CreatedAt    time.Time `json:"created_at"`
}

type PublicUserProfile struct {
	ID                  int64     `json:"id"`
	PreferredUsername   string    `json:"preferred_username"`
	Picture             string    `json:"picture"`
	AboutMe             string    `json:"about_me"`
	EmailVerified       bool      `json:"email_verified"`
	PhoneVerified       bool      `json:"phone_verified"`
	PublicPostsCount    int       `json:"public_posts_count"`
	PublicCapturesCount int       `json:"public_captures_count"`
	FollowersCount      int       `json:"followers_count"`
	FollowingCount      int       `json:"following_count"`
	IsFollowedByMe      bool      `json:"is_followed_by_me"`
	JoinedAt            time.Time `json:"joined_at"`
}

type FollowedUserProfile struct {
	ID                  int64     `json:"id"`
	PreferredUsername   string    `json:"preferred_username"`
	Picture             string    `json:"picture"`
	AboutMe             string    `json:"about_me"`
	PublicPostsCount    int       `json:"public_posts_count"`
	PublicCapturesCount int       `json:"public_captures_count"`
	FollowersCount      int       `json:"followers_count"`
	FollowedAt          time.Time `json:"followed_at"`
}

type ModerationAction struct {
	ID              string    `json:"id"`
	ActorUserID     int64     `json:"actor_user_id"`
	ActorName       string    `json:"actor_name,omitempty"`
	TargetUserID    int64     `json:"target_user_id,omitempty"`
	TargetCaptureID string    `json:"target_capture_id,omitempty"`
	TargetPostID    string    `json:"target_post_id,omitempty"`
	TargetCommentID string    `json:"target_comment_id,omitempty"`
	ActionKind      string    `json:"action_kind"`
	ReasonCode      string    `json:"reason_code,omitempty"`
	Note            string    `json:"note,omitempty"`
	MetaJSON        string    `json:"meta_json,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type AdminOverview struct {
	TotalUsers                int `json:"total_users"`
	AdminUsers                int `json:"admin_users"`
	ModeratorUsers            int `json:"moderator_users"`
	StaffUsers                int `json:"staff_users"`
	BannedUsers               int `json:"banned_users"`
	CommentsMutedUsers        int `json:"comments_muted_users"`
	PublishingSuspendedUsers  int `json:"publishing_suspended_users"`
	RestrictedUsers           int `json:"restricted_users"`
	PublicPosts               int `json:"public_posts"`
	PublicCaptures            int `json:"public_captures"`
	HiddenPosts               int `json:"hidden_posts"`
	HiddenComments            int `json:"hidden_comments"`
	HiddenCaptures            int `json:"hidden_captures"`
	PendingPublicationReview  int `json:"pending_publication_review"`
	FailedPublicationReview   int `json:"failed_publication_review"`
	RejectedPublicationReview int `json:"rejected_publication_review"`
}

type AdminUserSummary struct {
	ID                       int64     `json:"id"`
	PreferredUsername        string    `json:"preferred_username"`
	Picture                  string    `json:"picture"`
	IsModerator              bool      `json:"is_moderator"`
	IsAdmin                  bool      `json:"is_admin"`
	BannedUntil              time.Time `json:"banned_until,omitempty"`
	CommentsMutedUntil       time.Time `json:"comments_muted_until,omitempty"`
	PublishingSuspendedUntil time.Time `json:"publishing_suspended_until,omitempty"`
	ModerationNote           string    `json:"moderation_note,omitempty"`
	ModeratedByUserID        int64     `json:"moderated_by_user_id,omitempty"`
	ModeratedAt              time.Time `json:"moderated_at,omitempty"`
	PublicPostsCount         int       `json:"public_posts_count"`
	PublicCapturesCount      int       `json:"public_captures_count"`
}

type AdminUserListFilters struct {
	Query  string `json:"query,omitempty"`
	Role   string `json:"role,omitempty"`
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

type AdminBackup struct {
	ID                string    `json:"id"`
	Status            string    `json:"status"`
	TriggerKind       string    `json:"trigger_kind"`
	StorageKey        string    `json:"storage_key,omitempty"`
	ChecksumSHA256    string    `json:"checksum_sha256,omitempty"`
	SizeBytes         int64     `json:"size_bytes,omitempty"`
	StartedAt         time.Time `json:"started_at,omitempty"`
	FinishedAt        time.Time `json:"finished_at,omitempty"`
	InitiatedByUserID int64     `json:"initiated_by_user_id,omitempty"`
	InitiatedByName   string    `json:"initiated_by_name,omitempty"`
	ErrorMessage      string    `json:"error_message,omitempty"`
}

type AdminSystemStatus struct {
	BackupEnabled            bool         `json:"backup_enabled"`
	BackupSchedulerEnabled   bool         `json:"backup_scheduler_enabled"`
	BackupIntervalHours      int          `json:"backup_interval_hours"`
	BackupRetentionDays      int          `json:"backup_retention_days"`
	BackupMaxCompleted       int          `json:"backup_max_completed"`
	BackupStorageBackend     string       `json:"backup_storage_backend,omitempty"`
	LatestCompletedBackup    *AdminBackup `json:"latest_completed_backup,omitempty"`
	PublishDefaultModel      string       `json:"publish_default_model,omitempty"`
	ModeratorDefaultModel    string       `json:"moderator_default_model,omitempty"`
	ValidatorConfigReachable bool         `json:"validator_config_reachable"`
	ValidatorConfigError     string       `json:"validator_config_error,omitempty"`
}
