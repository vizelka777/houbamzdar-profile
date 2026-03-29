package chattoken

import (
	"testing"
	"time"
)

func TestSignAndVerify(t *testing.T) {
	now := time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)
	token, err := Sign("secret-123", &Claims{
		Issuer:            "https://api.houbamzdar.cz",
		Audience:          "houbamzdar-chat",
		UserID:            42,
		Subject:           "42",
		PreferredUsername: "mykolog42",
		ExpiresAtUnix:     now.Add(5 * time.Minute).Unix(),
		IssuedAtUnix:      now.Unix(),
	})
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	claims, err := Verify("secret-123", "https://api.houbamzdar.cz", "houbamzdar-chat", token, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if claims.UserID != 42 {
		t.Fatalf("expected user id 42, got %d", claims.UserID)
	}
	if claims.PreferredUsername != "mykolog42" {
		t.Fatalf("unexpected username: %q", claims.PreferredUsername)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	now := time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)
	token, err := Sign("secret-123", &Claims{
		Issuer:        "https://api.houbamzdar.cz",
		Audience:      "houbamzdar-chat",
		UserID:        7,
		Subject:       "7",
		ExpiresAtUnix: now.Add(1 * time.Minute).Unix(),
		IssuedAtUnix:  now.Unix(),
	})
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := Verify("secret-123", "https://api.houbamzdar.cz", "houbamzdar-chat", token, now.Add(2*time.Minute)); err != ErrTokenExpired {
		t.Fatalf("expected expired error, got %v", err)
	}
}
