package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/houbamzdar/bff/internal/chattoken"
	"github.com/houbamzdar/bff/internal/models"
)

func (s *Server) handleCreateChatToken(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	if !s.ensureNotBanned(w, user) {
		return
	}
	if s.Config == nil || s.Config.ChatTokenSecret == "" {
		http.Error(w, "chat integration is not configured", http.StatusServiceUnavailable)
		return
	}

	now := time.Now().UTC()
	ttlSeconds := s.Config.ChatTokenTTLSeconds
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}

	claims := &chattoken.Claims{
		Issuer:            s.Config.ChatTokenIssuer,
		Audience:          s.Config.ChatTokenAudience,
		Subject:           strconv.FormatInt(user.ID, 10),
		ExpiresAtUnix:     now.Add(time.Duration(ttlSeconds) * time.Second).Unix(),
		IssuedAtUnix:      now.Unix(),
		UserID:            user.ID,
		PreferredUsername: user.PreferredUsername,
		Picture:           user.Picture,
		EmailVerified:     user.EmailVerified,
		IsModerator:       user.IsModerator,
		IsAdmin:           user.IsAdmin,
	}

	token, err := chattoken.Sign(s.Config.ChatTokenSecret, claims)
	if err != nil {
		http.Error(w, "failed to create chat token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":           true,
		"token":        token,
		"expires_at":   now.Add(time.Duration(ttlSeconds) * time.Second).Format(time.RFC3339),
		"api_base_url": s.Config.ChatAPIBaseURL,
	})
}
