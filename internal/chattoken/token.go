package chattoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrTokenMalformed      = errors.New("chat token is malformed")
	ErrTokenSignature      = errors.New("chat token signature is invalid")
	ErrTokenExpired        = errors.New("chat token is expired")
	ErrTokenIssuerMismatch = errors.New("chat token issuer mismatch")
	ErrTokenAudience       = errors.New("chat token audience mismatch")
	ErrTokenSubject        = errors.New("chat token subject is invalid")
)

var encoding = base64.RawURLEncoding

type Claims struct {
	Issuer            string `json:"iss,omitempty"`
	Audience          string `json:"aud,omitempty"`
	Subject           string `json:"sub,omitempty"`
	ExpiresAtUnix     int64  `json:"exp,omitempty"`
	IssuedAtUnix      int64  `json:"iat,omitempty"`
	UserID            int64  `json:"uid"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Picture           string `json:"picture,omitempty"`
	EmailVerified     bool   `json:"email_verified,omitempty"`
	IsModerator       bool   `json:"is_moderator,omitempty"`
	IsAdmin           bool   `json:"is_admin,omitempty"`
}

func (c *Claims) Validate(now time.Time, expectedIssuer string, expectedAudience string) error {
	if c == nil {
		return ErrTokenMalformed
	}
	if c.Subject == "" || c.UserID <= 0 {
		return ErrTokenSubject
	}
	if expectedIssuer != "" && c.Issuer != expectedIssuer {
		return ErrTokenIssuerMismatch
	}
	if expectedAudience != "" && c.Audience != expectedAudience {
		return ErrTokenAudience
	}
	if c.ExpiresAtUnix > 0 && now.UTC().Unix() >= c.ExpiresAtUnix {
		return ErrTokenExpired
	}
	return nil
}

func Sign(secret string, claims *Claims) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", errors.New("chat token secret is required")
	}
	if claims == nil {
		return "", errors.New("claims are required")
	}
	if claims.Subject == "" && claims.UserID > 0 {
		claims.Subject = strconv.FormatInt(claims.UserID, 10)
	}

	headerSegment, err := encodeSegment(map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	})
	if err != nil {
		return "", err
	}
	payloadSegment, err := encodeSegment(claims)
	if err != nil {
		return "", err
	}

	unsigned := headerSegment + "." + payloadSegment
	signature := signHS256(secret, unsigned)
	return unsigned + "." + encoding.EncodeToString(signature), nil
}

func Verify(secret string, expectedIssuer string, expectedAudience string, token string, now time.Time) (*Claims, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("chat token secret is required")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrTokenMalformed
	}

	unsigned := parts[0] + "." + parts[1]
	signature, err := encoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrTokenMalformed
	}
	expectedSignature := signHS256(secret, unsigned)
	if !hmac.Equal(signature, expectedSignature) {
		return nil, ErrTokenSignature
	}

	var header struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return nil, ErrTokenMalformed
	}
	if header.Algorithm != "HS256" || header.Type != "JWT" {
		return nil, ErrTokenMalformed
	}

	var claims Claims
	if err := decodeSegment(parts[1], &claims); err != nil {
		return nil, ErrTokenMalformed
	}
	if claims.Subject == "" && claims.UserID > 0 {
		claims.Subject = strconv.FormatInt(claims.UserID, 10)
	}
	if claims.UserID <= 0 && claims.Subject != "" {
		userID, err := strconv.ParseInt(claims.Subject, 10, 64)
		if err != nil || userID <= 0 {
			return nil, ErrTokenSubject
		}
		claims.UserID = userID
	}
	if claims.UserID > 0 && claims.Subject != strconv.FormatInt(claims.UserID, 10) {
		return nil, ErrTokenSubject
	}

	if err := claims.Validate(now, expectedIssuer, expectedAudience); err != nil {
		return nil, err
	}
	return &claims, nil
}

func signHS256(secret string, unsigned string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(unsigned))
	return mac.Sum(nil)
}

func encodeSegment(value interface{}) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal token segment: %w", err)
	}
	return encoding.EncodeToString(payload), nil
}

func decodeSegment(segment string, dest interface{}) error {
	payload, err := encoding.DecodeString(segment)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, dest)
}
