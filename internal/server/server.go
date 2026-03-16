package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/houbamzdar/bff/internal/auth"
	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/media"
)

type Server struct {
	Config *config.Config
	DB     *db.DB
	OIDC   *auth.OIDC
	Media  *media.BunnyStorage
	Router *chi.Mux
}

func New(cfg *config.Config, database *db.DB, oidc *auth.OIDC, mediaStorage *media.BunnyStorage) *Server {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{cfg.FrontOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
	}))

	s := &Server{
		Config: cfg,
		DB:     database,
		OIDC:   oidc,
		Media:  mediaStorage,
		Router: r,
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.Router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	s.Router.Get("/auth/login", s.handleLogin)
	s.Router.Get("/auth/callback", s.handleCallback)
	s.Router.Post("/auth/logout", s.handleLogout)

	s.Router.Get("/api/session", s.handleSession)
	s.Router.Get("/api/public/users/{userID}", s.handleGetPublicUserProfile)
	s.Router.Get("/api/public/users/{userID}/posts", s.handleListPublicUserPosts)
	s.Router.Get("/api/public/users/{userID}/captures", s.handleListPublicUserCaptures)
	s.Router.Get("/api/public/posts", s.handleListPublicPosts)
	s.Router.Get("/api/public/captures", s.handleListPublicCaptures)
	s.Router.Get("/api/public/map-captures", s.handleListPublicMapCaptures)

	s.Router.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Get("/api/me", s.handleGetMe)
		r.Get("/api/me/viewed-captures", s.handleListViewedCaptures)
		r.Post("/api/me/about", s.handlePostMeAbout)
		r.Get("/api/moderation/ai-models", s.handleListModeratorAIModels)
		r.Get("/api/moderation/users/{userID}", s.handleGetModerationUser)
		r.Get("/api/moderation/users/{userID}/actions", s.handleListModerationUserActions)
		r.Get("/api/moderation/hidden-captures", s.handleListModerationHiddenCaptures)
		r.Get("/api/moderation/hidden-posts", s.handleListModerationHiddenPosts)
		r.Get("/api/moderation/hidden-comments", s.handleListModerationHiddenComments)
		r.Get("/api/moderation/captures/{captureID}/taxonomy", s.handleGetModerationCaptureTaxonomy)
		r.Post("/api/moderation/captures/{captureID}/taxonomy", s.handleSetModerationCaptureTaxonomy)
		r.Get("/api/moderation/captures/{captureID}/geo", s.handleGetModerationCaptureGeo)
		r.Post("/api/moderation/captures/{captureID}/geo", s.handleSetModerationCaptureGeo)
		r.Post("/api/moderation/users/{userID}/restrictions", s.handleSetModerationUserRestrictions)
		r.Post("/api/moderation/users/{userID}/roles", s.handleSetModerationUserRoles)
		r.Post("/api/moderation/captures/{captureID}/visibility", s.handleSetModerationCaptureVisibility)
		r.Post("/api/moderation/posts/{postID}/visibility", s.handleSetModerationPostVisibility)
		r.Post("/api/moderation/comments/{commentID}/visibility", s.handleSetModerationCommentVisibility)
		r.Get("/api/captures", s.handleListCaptures)
		r.Get("/api/captures/{captureID}/preview", s.handlePreviewCapture)
		r.Post("/api/captures", s.handleCreateCapture)
		r.Post("/api/captures/{captureID}/publish", s.handlePublishCapture)
		r.Post("/api/captures/{captureID}/moderator-recheck", s.handleModeratorRecheckCapture)
		r.Post("/api/captures/{captureID}/unpublish", s.handleUnpublishCapture)
		r.Post("/api/captures/{captureID}/coordinates-free", s.handleSetCaptureCoordinatesFree)
		r.Post("/api/captures/{captureID}/unlock-coordinates", s.handleUnlockCaptureCoordinates)
		r.Delete("/api/captures/{captureID}", s.handleDeleteCapture)

		r.Get("/api/posts", s.handleListPosts)
		r.Post("/api/posts", s.handleCreatePost)
		r.Get("/api/posts/{postID}", s.handleGetPost)
		r.Post("/api/posts/{postID}/comments", s.handleCreatePostComment)
		r.Put("/api/posts/{postID}/comments/{commentID}", s.handleUpdatePostComment)
		r.Delete("/api/posts/{postID}/comments/{commentID}", s.handleDeletePostComment)
		r.Post("/api/posts/{postID}/like", s.handleTogglePostLike)
		r.Put("/api/posts/{postID}", s.handleUpdatePost)
		r.Delete("/api/posts/{postID}", s.handleDeletePost)
	})
}

func (s *Server) currentUserIDFromOptionalSession(r *http.Request) int64 {
	cookie, err := r.Cookie(s.Config.SessionCookieName)
	if err != nil {
		return 0
	}

	session, err := s.DB.GetSession(cookie.Value)
	if err != nil {
		return 0
	}

	return session.UserID
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.Config.SessionCookieName)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		session, err := s.DB.GetSession(cookie.Value)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		user, err := s.DB.GetUser(session.UserID)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), "user", user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) Start() error {
	return http.ListenAndServe(":"+s.Config.Port, s.Router)
}
