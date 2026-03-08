package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                string
	AppBaseURL          string
	FrontBaseURL        string
	FrontOrigin         string
	SessionCookieName   string
	SessionCookieSecure bool
	SessionTTLHours     int

	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	OIDCScopes       []string

	DBURL   string
	DBToken string
}

func Load() *Config {
	_ = godotenv.Load()

	secure, _ := strconv.ParseBool(os.Getenv("SESSION_COOKIE_SECURE"))
	ttl, _ := strconv.Atoi(os.Getenv("SESSION_TTL_HOURS"))
	if ttl == 0 {
		ttl = 720
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	sessionCookieName := os.Getenv("SESSION_COOKIE_NAME")
	if sessionCookieName == "" {
		sessionCookieName = "hzd_session"
	}
	scopesStr := os.Getenv("OIDC_SCOPES")
	scopes := []string{"openid", "profile", "email", "phone", "offline_access"}
	if scopesStr != "" {
		// Just using defaults for simplicity
	}

	return &Config{
		Port:                port,
		AppBaseURL:          os.Getenv("APP_BASE_URL"),
		FrontBaseURL:        os.Getenv("FRONT_BASE_URL"),
		FrontOrigin:         os.Getenv("FRONT_ORIGIN"),
		SessionCookieName:   sessionCookieName,
		SessionCookieSecure: secure,
		SessionTTLHours:     ttl,

		OIDCIssuer:       os.Getenv("OIDC_ISSUER"),
		OIDCClientID:     os.Getenv("OIDC_CLIENT_ID"),
		OIDCClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:  os.Getenv("OIDC_REDIRECT_URL"),
		OIDCScopes:       scopes,

		DBURL:   os.Getenv("DB_URL"),
		DBToken: os.Getenv("DB_TOKEN"),
	}
}
