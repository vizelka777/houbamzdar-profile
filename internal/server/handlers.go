package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/houbamzdar/bff/internal/models"
	"golang.org/x/oauth2"
)

func generateRandomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	state := generateRandomString(32)
	nonce := generateRandomString(32)
	verifier := oauth2.GenerateVerifier()

	postLoginRedirect := s.Config.FrontBaseURL + "/"
	if r.URL.Query().Get("next") == "me" {
		postLoginRedirect = s.Config.FrontBaseURL + "/me.html"
	}

	err := s.DB.SaveState(&models.OIDCLoginState{
		State:             state,
		Nonce:             nonce,
		PKCEVerifier:      verifier,
		ExpiresAt:         time.Now().Add(10 * time.Minute),
		PostLoginRedirect: postLoginRedirect,
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.S256ChallengeOption(verifier),
	}

	// Add custom parameter if requested
	if r.URL.Query().Get("edit_profile") == "1" {
		opts = append(opts, oauth2.SetAuthURLParam("edit_profile", "1"))
	}

	authURL := s.OIDC.OAuth2Config.AuthCodeURL(state, opts...)

	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	stateStr := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	stateObj, err := s.DB.GetAndRemoveState(stateStr)
	if err != nil {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	oauth2Token, err := s.OIDC.ExchangeCode(ctx, code, stateObj.PKCEVerifier)
	if err != nil {
		http.Error(w, "failed to exchange token: "+err.Error(), http.StatusBadRequest)
		return
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "no id_token in response", http.StatusBadRequest)
		return
	}

	idToken, err := s.OIDC.Verifier.Verify(ctx, rawIDToken)
	if err != nil {
		http.Error(w, "failed to verify id_token", http.StatusBadRequest)
		return
	}

	if idToken.Nonce != stateObj.Nonce {
		http.Error(w, "nonce mismatch", http.StatusBadRequest)
		return
	}

	var claims models.OIDCClaims
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "failed to parse claims", http.StatusBadRequest)
		return
	}

	// Make UserInfo request if some claims are still missing (fallback)
	if claims.Email == "" || claims.PreferredUsername == "" {
		userInfo, err := s.OIDC.Provider.UserInfo(ctx, oauth2.StaticTokenSource(oauth2Token))
		if err == nil {
			userInfo.Claims(&claims)
		}
	}

	claims.Iss = idToken.Issuer
	claims.Sub = idToken.Subject

	user, isNew, err := s.DB.UpsertUser(&claims, oauth2Token)
	if err != nil {
		http.Error(w, "failed to upsert user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	sessionID := uuid.New().String()
	ttl := time.Duration(s.Config.SessionTTLHours) * time.Hour
	err = s.DB.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(ttl),
	})
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     s.Config.SessionCookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  time.Now().Add(ttl),
		HttpOnly: true,
		Secure:   s.Config.SessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	redirectURL := stateObj.PostLoginRedirect
	if isNew {
		redirectURL = s.Config.FrontBaseURL + "/me.html"
	}

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(s.Config.SessionCookieName)
	if err == nil {
		s.DB.DeleteSession(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     s.Config.SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   s.Config.SessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	logoutURL := fmt.Sprintf("%s/logout?post_logout_redirect_uri=%s", s.Config.OIDCIssuer, url.QueryEscape(s.Config.FrontBaseURL+"/"))
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":             true,
		"idp_logout_url": logoutURL,
	})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(s.Config.SessionCookieName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"logged_in": false,
			"user":      nil,
		})
		return
	}

	session, err := s.DB.GetSession(cookie.Value)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"logged_in": false,
			"user":      nil,
		})
		return
	}

	user, err := s.DB.GetUser(session.UserID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"logged_in": false,
			"user":      nil,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"logged_in": true,
		"user": map[string]interface{}{
			"preferred_username": user.PreferredUsername,
			"picture":            user.Picture,
		},
	})
}

func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (s *Server) handlePostMeAbout(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)

	var req struct {
		AboutMe string `json:"about_me"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	aboutMe := strings.TrimSpace(req.AboutMe)
	if len(aboutMe) > 2000 {
		http.Error(w, "about_me is too long (max 2000 chars)", http.StatusBadRequest)
		return
	}

	if err := s.DB.UpdateAboutMe(user.ID, aboutMe); err != nil {
		http.Error(w, "failed to update about_me", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"about_me": aboutMe,
	})
}
