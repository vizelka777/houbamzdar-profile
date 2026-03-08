package models

import "time"

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
	Iss               string `json:"iss"`
	Sub               string `json:"sub"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	PhoneNumber       string `json:"phone_number"`
	PhoneNumberVerified bool `json:"phone_number_verified"`
	Picture           string `json:"picture"`
}
