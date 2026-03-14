package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"golang.org/x/oauth2"
)

func TestExchangeCodeUsesSingleBasicAuthRequest(t *testing.T) {
	t.Parallel()

	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		if r.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", r.Method)
		}

		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("client-id:client-secret"))
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Fatalf("expected Authorization header %q, got %q", wantAuth, got)
		}

		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}

		if got := r.Form.Get("grant_type"); got != "authorization_code" {
			t.Fatalf("expected authorization_code grant, got %q", got)
		}
		if got := r.Form.Get("code"); got != "test-code" {
			t.Fatalf("expected code test-code, got %q", got)
		}
		if got := r.Form.Get("code_verifier"); got != "test-verifier" {
			t.Fatalf("expected PKCE verifier test-verifier, got %q", got)
		}
		if got := r.Form.Get("redirect_uri"); got != "https://api.houbamzdar.cz/auth/callback" {
			t.Fatalf("unexpected redirect_uri %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "access-token",
			"token_type":    "Bearer",
			"refresh_token": "refresh-token",
			"expires_in":    3600,
			"id_token":      "id-token",
			"scope":         "openid profile",
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		OIDCClientID:     "client-id",
		OIDCClientSecret: "client-secret",
		OIDCRedirectURL:  "https://api.houbamzdar.cz/auth/callback",
		OIDCScopes:       []string{"openid", "profile"},
	}

	oidcClient := &OIDC{
		OAuth2Config: newOAuth2Config(cfg, oauth2.Endpoint{
			AuthURL:  "https://example.com/authorize",
			TokenURL: server.URL,
		}),
	}

	token, err := oidcClient.ExchangeCode(context.Background(), "test-code", "test-verifier")
	if err != nil {
		t.Fatalf("exchange token: %v", err)
	}

	if token.AccessToken != "access-token" {
		t.Fatalf("unexpected access token %q", token.AccessToken)
	}

	if requestCount != 1 {
		t.Fatalf("expected exactly one token request, got %d", requestCount)
	}

	if token.TokenType != "Bearer" {
		t.Fatalf("unexpected token type %q", token.TokenType)
	}

	if token.RefreshToken != "refresh-token" {
		t.Fatalf("unexpected refresh token %q", token.RefreshToken)
	}

	if got := token.Extra("id_token"); got != "id-token" {
		t.Fatalf("unexpected id_token %v", got)
	}

	if got := token.Extra("scope"); got != "openid profile" {
		t.Fatalf("unexpected scope extra %v", got)
	}

	if time.Until(token.Expiry) <= 0 {
		t.Fatalf("expected token expiry to be set")
	}
}
