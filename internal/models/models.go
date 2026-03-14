package models

import "time"

const (
	HoubickaTransactionTypeCredit = "credit"
	HoubickaTransactionTypeDebit  = "debit"
)

const (
	HoubickaReasonSignupBonus        = "signup_bonus"
	HoubickaReasonEmailVerifiedBonus = "email_verified_bonus"
	HoubickaReasonPhoneVerifiedBonus = "phone_verified_bonus"
	HoubickaReasonCoordsViewDebit    = "coords_view_debit"
	HoubickaReasonCoordsViewCredit   = "coords_view_credit"
	HoubickaReasonGift               = "gift"
	HoubickaReasonPurchase           = "purchase"
	HoubickaReasonAdminGrant         = "admin_grant"
)

var HoubickaReasonCodes = []string{
	HoubickaReasonSignupBonus,
	HoubickaReasonEmailVerifiedBonus,
	HoubickaReasonPhoneVerifiedBonus,
	HoubickaReasonCoordsViewDebit,
	HoubickaReasonCoordsViewCredit,
	HoubickaReasonGift,
	HoubickaReasonPurchase,
	HoubickaReasonAdminGrant,
}

type User struct {
	ID                  int64     `json:"id"`
	IDPIssuer           string    `json:"-"`
	IDPSub              string    `json:"-"`
	PreferredUsername   string    `json:"preferred_username"`
	Email               string    `json:"email"`
	EmailVerified       bool      `json:"email_verified"`
	PhoneNumber         string    `json:"phone_number"`
	PhoneNumberVerified bool      `json:"phone_number_verified"`
	Picture             string    `json:"picture"`
	AboutMe             string    `json:"about_me"`
	AccessToken         string    `json:"-"`
	RefreshToken        string    `json:"-"`
	TokenExpiresAt      time.Time `json:"-"`
	CreatedAt           time.Time `json:"-"`
	UpdatedAt           time.Time `json:"-"`
	LastIDPSyncAt       time.Time `json:"-"`
}

type Capture struct {
	ID                string    `json:"id"`
	UserID            int64     `json:"-"`
	AuthorName        string    `json:"author_name,omitempty"`
	AuthorAvatar      string    `json:"author_avatar,omitempty"`
	ClientLocalID     string    `json:"client_local_id,omitempty"`
	OriginalFileName  string    `json:"original_file_name"`
	ContentType       string    `json:"content_type"`
	SizeBytes         int64     `json:"size_bytes"`
	Width             int       `json:"width"`
	Height            int       `json:"height"`
	CapturedAt        time.Time `json:"captured_at"`
	UploadedAt        time.Time `json:"uploaded_at"`
	Latitude          *float64  `json:"latitude,omitempty"`
	Longitude         *float64  `json:"longitude,omitempty"`
	AccuracyMeters    *float64  `json:"accuracy_meters,omitempty"`
	Status            string    `json:"status"`
	PrivateStorageKey string    `json:"-"`
	PublicStorageKey  string    `json:"-"`
	PublicURL         string    `json:"public_url,omitempty"`
	PublishedAt       time.Time `json:"published_at,omitempty"`
}

type Post struct {
	ID           string     `json:"id"`
	UserID       int64      `json:"-"`
	AuthorName   string     `json:"author_name,omitempty"`
	AuthorAvatar string     `json:"author_avatar,omitempty"`
	Content      string     `json:"content"`
	Status       string     `json:"status"`
	LikesCount   int        `json:"likes_count"`
	IsLikedByMe  bool       `json:"is_liked_by_me"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	Captures     []*Capture `json:"captures,omitempty"`
	Comments     []*Comment `json:"comments,omitempty"`
}

type CreatePostRequest struct {
	Content    string   `json:"content"`
	CaptureIDs []string `json:"capture_ids"`
}

type Comment struct {
	ID           string    `json:"id"`
	PostID       string    `json:"-"`
	UserID       int64     `json:"-"`
	AuthorName   string    `json:"author_name,omitempty"`
	AuthorAvatar string    `json:"author_avatar,omitempty"`
	Content      string    `json:"content"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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

type HoubickaWallet struct {
	UserID    int64     `json:"user_id"`
	Balance   int64     `json:"balance"`
	UpdatedAt time.Time `json:"updated_at"`
}

type HoubickaTransaction struct {
	ID             int64     `json:"id"`
	UserID         int64     `json:"user_id"`
	Type           string    `json:"type"`
	AmountSigned   int64     `json:"amount_signed"`
	ReasonCode     string    `json:"reason_code"`
	IdempotencyKey string    `json:"idempotency_key"`
	MetadataJSON   string    `json:"metadata_json"`
	CreatedAt      time.Time `json:"created_at"`
}
