package config

import (
	"os"
	"strconv"
	"strings"

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

	BunnyStorageHost       string
	BunnyPrivateZone       string
	BunnyPrivateStorageKey string
	BunnyPublicZone        string
	BunnyPublicStorageKey  string
	BunnyPublicBaseURL     string

	CaptureAIValidatorURL   string
	CaptureAIValidatorToken string
	NominatimBaseURL        string
	NominatimUserAgent      string

	AdminBackupIntervalHours int
	AdminBackupRetentionDays int
	AdminBackupMaxCompleted  int
}

func Load() *Config {
	_ = godotenv.Load()

	secure, _ := strconv.ParseBool(os.Getenv("SESSION_COOKIE_SECURE"))
	ttl := getEnvInt("SESSION_TTL_HOURS", 720)
	if ttl == 0 {
		ttl = 720
	}
	backupIntervalHours := getEnvInt("ADMIN_BACKUP_INTERVAL_HOURS", 0)
	backupRetentionDays := getEnvInt("ADMIN_BACKUP_RETENTION_DAYS", 0)
	backupMaxCompleted := getEnvInt("ADMIN_BACKUP_MAX_COMPLETED", 0)
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
		scopes = parseScopes(scopesStr)
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

		BunnyStorageHost:       getEnv("BUNNY_STORAGE_HOST", "storage.bunnycdn.com"),
		BunnyPrivateZone:       os.Getenv("BUNNY_PRIVATE_STORAGE_ZONE"),
		BunnyPrivateStorageKey: os.Getenv("BUNNY_PRIVATE_STORAGE_KEY"),
		BunnyPublicZone:        os.Getenv("BUNNY_PUBLIC_STORAGE_ZONE"),
		BunnyPublicStorageKey:  os.Getenv("BUNNY_PUBLIC_STORAGE_KEY"),
		BunnyPublicBaseURL:     os.Getenv("BUNNY_PUBLIC_BASE_URL"),

		CaptureAIValidatorURL:   os.Getenv("CAPTURE_AI_VALIDATOR_URL"),
		CaptureAIValidatorToken: os.Getenv("CAPTURE_AI_VALIDATOR_TOKEN"),
		NominatimBaseURL:        getEnv("NOMINATIM_BASE_URL", "https://nominatim.openstreetmap.org"),
		NominatimUserAgent:      getEnv("NOMINATIM_USER_AGENT", "houbamzdar-mvp/1.0"),

		AdminBackupIntervalHours: backupIntervalHours,
		AdminBackupRetentionDays: backupRetentionDays,
		AdminBackupMaxCompleted:  backupMaxCompleted,
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseScopes(raw string) []string {
	parts := strings.Split(raw, ",")
	scopes := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))

	for _, part := range parts {
		scope := strings.TrimSpace(part)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		scopes = append(scopes, scope)
	}

	if len(scopes) == 0 {
		return []string{"openid", "profile", "email", "phone", "offline_access"}
	}
	return scopes
}
