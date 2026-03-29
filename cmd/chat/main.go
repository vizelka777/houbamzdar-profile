package main

import (
	"log"
	"net/http"

	"github.com/houbamzdar/bff/internal/chatconfig"
	"github.com/houbamzdar/bff/internal/chatdb"
	"github.com/houbamzdar/bff/internal/chatidentity"
	"github.com/houbamzdar/bff/internal/chatserver"
)

func main() {
	cfg := chatconfig.Load()

	chatStore, err := chatdb.New(cfg.ChatDBURL, cfg.ChatDBToken, cfg.GeneralRoomSlug, cfg.GeneralRoomTitle)
	if err != nil {
		log.Fatalf("failed to initialize chat db: %v", err)
	}
	defer chatStore.Close()

	identityStore, err := chatidentity.New(cfg.IdentityDBURL, cfg.IdentityDBToken)
	if err != nil {
		log.Fatalf("failed to initialize identity db: %v", err)
	}
	defer identityStore.Close()

	server := chatserver.New(cfg, chatStore, identityStore)
	log.Printf("Starting chat service on port %s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, server.Router); err != nil {
		log.Fatalf("chat server error: %v", err)
	}
}
