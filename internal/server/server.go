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
)

type Server struct {
	Config *config.Config
	DB     *db.DB
	OIDC   *auth.OIDC
	Router *chi.Mux
}

func New(cfg *config.Config, database *db.DB, oidc *auth.OIDC) *Server {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{cfg.FrontOrigin},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
	}))

	s := &Server{
		Config: cfg,
		DB:     database,
		OIDC:   oidc,
		Router: r,
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.Router.Get("/auth/login", s.handleLogin)
	s.Router.Get("/auth/callback", s.handleCallback)
	s.Router.Post("/auth/logout", s.handleLogout)

	s.Router.Get("/api/session", s.handleSession)
	
	s.Router.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Get("/api/me", s.handleGetMe)
		r.Post("/api/me/about", s.handlePostMeAbout)
	})
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
