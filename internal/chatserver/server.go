package chatserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/houbamzdar/bff/internal/chatconfig"
	"github.com/houbamzdar/bff/internal/chatdb"
	"github.com/houbamzdar/bff/internal/chatidentity"
	"github.com/houbamzdar/bff/internal/chattoken"
)

const (
	maxMessageLength      = 1000
	publicRoomMessageKeep = 200
	maxJSONBodyBytes      = 8 << 10
)

type contextKey string

const authContextKey contextKey = "chat-auth-user"

type AuthUser struct {
	Claims *chattoken.Claims
	User   *chatidentity.User
}

type Server struct {
	Config   *chatconfig.Config
	DB       *chatdb.DB
	Identity *chatidentity.DB
	Limiter  *slidingWindowLimiter
	Router   *chi.Mux
}

func New(cfg *chatconfig.Config, chatStore *chatdb.DB, identityStore *chatidentity.DB) *Server {
	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{cfg.FrontOrigin},
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: false,
	}))

	server := &Server{
		Config:   cfg,
		DB:       chatStore,
		Identity: identityStore,
		Limiter:  newSlidingWindowLimiter(),
		Router:   router,
	}
	server.setupRoutes()
	return server
}

func (s *Server) setupRoutes() {
	s.Router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	s.Router.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Use(s.rateLimitMiddleware)
		r.Get("/api/chat/me", s.handleGetMe)
		r.Get("/api/chat/users/search", s.handleSearchUsers)
		r.Get("/api/chat/rooms/public", s.handleListPublicRooms)
		r.Post("/api/chat/rooms/public", s.handleCreatePublicRoom)
		r.Get("/api/chat/rooms/direct", s.handleListDirectRooms)
		r.Post("/api/chat/rooms/direct", s.handleCreateDirectRoom)
		r.Delete("/api/chat/rooms/direct/{roomID}", s.handleDeleteDirectRoom)
		r.Get("/api/chat/rooms/{roomID}/messages", s.handleListRoomMessages)
		r.Post("/api/chat/rooms/{roomID}/messages", s.handleCreateRoomMessage)
		r.Delete("/api/chat/rooms/{roomID}/messages/{messageID}", s.handleDeleteRoomMessage)
		r.Post("/api/chat/rooms/{roomID}/read", s.handleMarkRoomRead)
	})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}

		claims, err := chattoken.Verify(
			s.Config.TokenSecret,
			s.Config.TokenIssuer,
			s.Config.TokenAudience,
			token,
			time.Now().UTC(),
		)
		if err != nil {
			http.Error(w, "invalid chat token", http.StatusUnauthorized)
			return
		}

		user, err := s.Identity.GetUser(claims.UserID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "user not found", http.StatusUnauthorized)
				return
			}
			http.Error(w, "failed to load user", http.StatusInternalServerError)
			return
		}

		ctx := context.WithValue(r.Context(), authContextKey, &AuthUser{
			Claims: claims,
			User:   user,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	authUser := currentAuthUser(r)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":   true,
		"user": authUser.User,
	})
}

func (s *Server) handleSearchUsers(w http.ResponseWriter, r *http.Request) {
	authUser := currentAuthUser(r)
	limit := chatconfig.ParseLimit(r.URL.Query().Get("limit"), 12, 50)
	users, err := s.Identity.SearchUsers(r.URL.Query().Get("q"), authUser.User.ID, limit)
	if err != nil {
		http.Error(w, "failed to search users", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":    true,
		"users": users,
	})
}

func (s *Server) handleListPublicRooms(w http.ResponseWriter, r *http.Request) {
	authUser := currentAuthUser(r)
	rooms, err := s.DB.ListPublicRooms(authUser.User.ID)
	if err != nil {
		http.Error(w, "failed to list public rooms", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":    true,
		"rooms": rooms,
	})
}

func (s *Server) handleCreatePublicRoom(w http.ResponseWriter, r *http.Request) {
	authUser := currentAuthUser(r)
	if !userCanModerate(authUser.User) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		Slug  string `json:"slug"`
		Title string `json:"title"`
	}
	if err := decodeJSONBody(w, r, &req, false); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	slug := normalizeSlug(req.Slug)
	title := strings.TrimSpace(req.Title)
	if slug == "" || title == "" {
		http.Error(w, "slug and title are required", http.StatusBadRequest)
		return
	}
	if !validSlug(slug) {
		http.Error(w, "invalid room slug", http.StatusBadRequest)
		return
	}

	room, err := s.DB.CreatePublicRoom(slug, title, authUser.User.ID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			http.Error(w, "room slug already exists", http.StatusConflict)
			return
		}
		http.Error(w, "failed to create room", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":   true,
		"room": room,
	})
}

func (s *Server) handleListDirectRooms(w http.ResponseWriter, r *http.Request) {
	authUser := currentAuthUser(r)
	limit := chatconfig.ParseLimit(r.URL.Query().Get("limit"), 20, 100)
	offset := parseOffset(r.URL.Query().Get("offset"))
	rooms, total, totalUnread, hasMore, err := s.DB.ListDirectRoomsForUser(authUser.User.ID, limit, offset)
	if err != nil {
		http.Error(w, "failed to list direct rooms", http.StatusInternalServerError)
		return
	}
	for _, room := range rooms {
		s.attachDirectRoomIdentity(room)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"rooms":        rooms,
		"limit":        limit,
		"offset":       offset,
		"has_more":     hasMore,
		"total":        total,
		"total_unread": totalUnread,
	})
}

func (s *Server) handleCreateDirectRoom(w http.ResponseWriter, r *http.Request) {
	authUser := currentAuthUser(r)
	var req struct {
		TargetUserID int64 `json:"target_user_id"`
	}
	if err := decodeJSONBody(w, r, &req, false); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.TargetUserID <= 0 || req.TargetUserID == authUser.User.ID {
		http.Error(w, "invalid target user", http.StatusBadRequest)
		return
	}

	targetUser, err := s.Identity.GetUser(req.TargetUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "target user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load target user", http.StatusInternalServerError)
		return
	}
	if userIsActivelyBanned(targetUser) {
		http.Error(w, "target user is unavailable", http.StatusUnprocessableEntity)
		return
	}

	room, err := s.DB.EnsureDirectRoom(authUser.User.ID, req.TargetUserID)
	if err != nil {
		http.Error(w, "failed to create direct room", http.StatusInternalServerError)
		return
	}
	room.OtherUserID = targetUser.ID
	room.OtherUserName = targetUser.PreferredUsername
	room.OtherUserAvatar = targetUser.Picture
	if strings.TrimSpace(room.Title) == "" || room.Title == "DM" {
		room.Title = targetUser.PreferredUsername
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":   true,
		"room": room,
	})
}

func (s *Server) handleDeleteDirectRoom(w http.ResponseWriter, r *http.Request) {
	authUser := currentAuthUser(r)
	roomID := strings.TrimSpace(chi.URLParam(r, "roomID"))
	if roomID == "" {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}

	room, allowed, err := s.DB.CanUserAccessRoom(authUser.User.ID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "room not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load room", http.StatusInternalServerError)
		return
	}
	if !allowed || room.Kind != "dm" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := s.DB.RemoveDirectRoomForUser(room.ID, authUser.User.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "room not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to delete direct room", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true,
	})
}

func (s *Server) handleListRoomMessages(w http.ResponseWriter, r *http.Request) {
	authUser := currentAuthUser(r)
	roomID, err := roomIDParam(r)
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}

	room, allowed, err := s.DB.CanUserAccessRoom(authUser.User.ID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "room not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load room", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if room.Kind == "dm" {
		s.attachDirectRoomIdentity(room)
	}

	limit := chatconfig.ParseLimit(r.URL.Query().Get("limit"), 50, 100)
	messages, err := s.DB.ListMessagesForUser(room, authUser.User.ID, limit)
	if err != nil {
		http.Error(w, "failed to list messages", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"room":     room,
		"messages": messages,
	})
}

func (s *Server) handleCreateRoomMessage(w http.ResponseWriter, r *http.Request) {
	authUser := currentAuthUser(r)
	if !s.ensureCanSendMessage(w, authUser.User) {
		return
	}

	roomID, err := roomIDParam(r)
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	room, allowed, err := s.DB.CanUserAccessRoom(authUser.User.ID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "room not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load room", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.rejectByRatePolicy(w, r, authUser, sendMessagePolicy(room)) {
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := decodeJSONBody(w, r, &req, false); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	content := strings.TrimSpace(req.Content)
	if content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(content) > maxMessageLength {
		http.Error(w, "message is too long", http.StatusBadRequest)
		return
	}

	message := &chatdb.Message{
		RoomID:             room.ID,
		AuthorUserID:       authUser.User.ID,
		AuthorNameSnapshot: authUser.User.PreferredUsername,
		AuthorAvatar:       authUser.User.Picture,
		Content:            content,
		CreatedAt:          time.Now().UTC(),
	}
	if err := s.DB.CreateMessage(message); err != nil {
		http.Error(w, "failed to create message", http.StatusInternalServerError)
		return
	}
	if room.Kind == "public" {
		if err := s.DB.TrimRoomMessages(room.ID, publicRoomMessageKeep); err != nil {
			http.Error(w, "failed to trim room history", http.StatusInternalServerError)
			return
		}
	}
	_ = s.DB.MarkRoomRead(room.ID, authUser.User.ID, message.ID, message.CreatedAt)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"message": message,
	})
}

func (s *Server) handleDeleteRoomMessage(w http.ResponseWriter, r *http.Request) {
	authUser := currentAuthUser(r)
	roomID, err := roomIDParam(r)
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	room, allowed, err := s.DB.CanUserAccessRoom(authUser.User.ID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "room not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load room", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	messageID := strings.TrimSpace(chi.URLParam(r, "messageID"))
	if messageID == "" {
		http.Error(w, "invalid message id", http.StatusBadRequest)
		return
	}

	message, err := s.DB.GetMessage(messageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "message not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load message", http.StatusInternalServerError)
		return
	}
	if message.RoomID != room.ID {
		http.Error(w, "message not found", http.StatusNotFound)
		return
	}
	if !canDeleteMessage(authUser.User, room, message) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.rejectByRatePolicy(w, r, authUser, deleteMessagePolicy(room)) {
		return
	}
	if message.DeletedAt.IsZero() {
		deletedAt := time.Now().UTC()
		if err := s.DB.SoftDeleteMessage(message.ID, deletedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "message not found", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to delete message", http.StatusInternalServerError)
			return
		}
		message.Content = ""
		message.DeletedAt = deletedAt
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"message": message,
	})
}

func (s *Server) handleMarkRoomRead(w http.ResponseWriter, r *http.Request) {
	authUser := currentAuthUser(r)
	roomID, err := roomIDParam(r)
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}

	if _, allowed, err := s.DB.CanUserAccessRoom(authUser.User.ID, roomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "room not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load room", http.StatusInternalServerError)
		return
	} else if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		LastMessageID string `json:"last_message_id"`
	}
	if err := decodeJSONBody(w, r, &req, true); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.DB.MarkRoomRead(roomID, authUser.User.ID, strings.TrimSpace(req.LastMessageID), time.Now().UTC()); err != nil {
		http.Error(w, "failed to update read state", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true,
	})
}

func (s *Server) attachDirectRoomIdentity(room *chatdb.Room) {
	if room == nil || room.Kind != "dm" || room.OtherUserID <= 0 {
		return
	}
	user, err := s.Identity.GetUser(room.OtherUserID)
	if err != nil {
		return
	}
	room.OtherUserName = user.PreferredUsername
	room.OtherUserAvatar = user.Picture
	if strings.TrimSpace(room.Title) == "" || room.Title == "DM" {
		room.Title = user.PreferredUsername
	}
}

func currentAuthUser(r *http.Request) *AuthUser {
	value := r.Context().Value(authContextKey)
	if authUser, ok := value.(*AuthUser); ok {
		return authUser
	}
	return nil
}

func roomIDParam(r *http.Request) (string, error) {
	roomID := strings.TrimSpace(chi.URLParam(r, "roomID"))
	if roomID == "" {
		return "", nil
	}
	decoded, err := url.PathUnescape(roomID)
	if err != nil {
		return "", err
	}
	return decoded, nil
}

func userCanModerate(user *chatidentity.User) bool {
	return user != nil && (user.IsModerator || user.IsAdmin)
}

func canDeleteMessage(user *chatidentity.User, room *chatdb.Room, message *chatdb.Message) bool {
	if user == nil || room == nil || message == nil {
		return false
	}
	if userCanModerate(user) && room.Kind == "public" {
		return true
	}
	return user.ID > 0 && user.ID == message.AuthorUserID
}

func userIsActivelyBanned(user *chatidentity.User) bool {
	return user != nil && !user.BannedUntil.IsZero() && user.BannedUntil.After(time.Now().UTC())
}

func userCommentsAreMuted(user *chatidentity.User) bool {
	return user != nil && !user.CommentsMutedUntil.IsZero() && user.CommentsMutedUntil.After(time.Now().UTC())
}

func (s *Server) ensureCanSendMessage(w http.ResponseWriter, user *chatidentity.User) bool {
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	if userIsActivelyBanned(user) {
		http.Error(w, "your account is temporarily banned", http.StatusForbidden)
		return false
	}
	if !user.EmailVerified {
		http.Error(w, "confirm your email before using chat", http.StatusForbidden)
		return false
	}
	if userCommentsAreMuted(user) {
		http.Error(w, "chat is temporarily disabled for your account", http.StatusForbidden)
		return false
	}
	restriction, err := s.DB.GetActiveUserRestriction(user.ID, time.Now().UTC())
	if err != nil {
		http.Error(w, "failed to check chat restrictions", http.StatusInternalServerError)
		return false
	}
	if restriction != nil {
		http.Error(w, fmt.Sprintf("chat is temporarily disabled for your account until %s", restriction.MutedUntil.Format(time.RFC3339)), http.StatusForbidden)
		return false
	}
	return true
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func normalizeSlug(slug string) string {
	slug = strings.TrimSpace(strings.ToLower(slug))
	slug = strings.ReplaceAll(slug, " ", "-")
	return slug
}

func validSlug(slug string) bool {
	if slug == "" || len(slug) > 40 {
		return false
	}
	for _, char := range slug {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
			continue
		}
		return false
	}
	return true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}, allowEmpty bool) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("request body must contain a single JSON object")
	} else if !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func parseOffset(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func writeJSON(w http.ResponseWriter, statusCode int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
