package models

import "time"

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
	ModeratedByUserID              int64                     `json:"moderated_by_user_id,omitempty"`
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
	ModeratedByUserID    int64      `json:"moderated_by_user_id,omitempty"`
	ModeratedAt          time.Time  `json:"moderated_at,omitempty"`
	Captures             []*Capture `json:"captures,omitempty"`
	Comments             []*Comment `json:"comments,omitempty"`
}

type CreatePostRequest struct {
	Content    string   `json:"content"`
	CaptureIDs []string `json:"capture_ids"`
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
	ModeratedByUserID    int64     `json:"moderated_by_user_id,omitempty"`
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
	JoinedAt            time.Time `json:"joined_at"`
}

type ModerationAction struct {
	ID              string    `json:"id"`
	ActorUserID     int64     `json:"actor_user_id"`
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
