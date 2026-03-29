package chatconfig

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port             string
	AppBaseURL       string
	FrontOrigin      string
	ChatDBURL        string
	ChatDBToken      string
	IdentityDBURL    string
	IdentityDBToken  string
	TokenSecret      string
	TokenIssuer      string
	TokenAudience    string
	GeneralRoomSlug  string
	GeneralRoomTitle string
}

func Load() *Config {
	_ = godotenv.Load()

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}

	appBaseURL := strings.TrimSpace(os.Getenv("APP_BASE_URL"))
	frontOrigin := strings.TrimSpace(os.Getenv("FRONT_ORIGIN"))
	if frontOrigin == "" {
		frontOrigin = strings.TrimSpace(os.Getenv("CHAT_FRONT_ORIGIN"))
	}

	return &Config{
		Port:             port,
		AppBaseURL:       appBaseURL,
		FrontOrigin:      frontOrigin,
		ChatDBURL:        strings.TrimSpace(os.Getenv("CHAT_DB_URL")),
		ChatDBToken:      strings.TrimSpace(os.Getenv("CHAT_DB_TOKEN")),
		IdentityDBURL:    fallbackEnv("IDENTITY_DB_URL", os.Getenv("DB_URL")),
		IdentityDBToken:  fallbackEnv("IDENTITY_DB_TOKEN", os.Getenv("DB_TOKEN")),
		TokenSecret:      strings.TrimSpace(os.Getenv("CHAT_TOKEN_SECRET")),
		TokenIssuer:      fallbackEnv("CHAT_TOKEN_ISSUER", os.Getenv("APP_BASE_URL")),
		TokenAudience:    fallbackEnv("CHAT_TOKEN_AUDIENCE", "houbamzdar-chat"),
		GeneralRoomSlug:  fallbackEnv("CHAT_GENERAL_ROOM_SLUG", "general"),
		GeneralRoomTitle: fallbackEnv("CHAT_GENERAL_ROOM_TITLE", "Obecný chat"),
	}
}

func fallbackEnv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func ParseLimit(raw string, fallback int, max int) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	if max > 0 && parsed > max {
		return max
	}
	return parsed
}
