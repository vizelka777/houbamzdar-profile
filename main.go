package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/houbamzdar/bff/internal/auth"
	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/enrichment"
	"github.com/houbamzdar/bff/internal/media"
	"github.com/houbamzdar/bff/internal/server"
)

func main() {
	cfg := config.Load()

	database, err := db.New(cfg)
	if err != nil {
		log.Fatalf("failed to initialize db: %v", err)
	}
	defer database.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	oidcProvider, err := auth.NewOIDC(ctx, cfg)
	if err != nil {
		log.Fatalf("failed to initialize oidc provider: %v", err)
	}

	mediaStorage := media.NewBunnyStorage(cfg)
	srv := server.New(cfg, database, oidcProvider, mediaStorage)
	enrichmentService := enrichment.New(cfg, database, mediaStorage)

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()

	if enrichmentService.Enabled() {
		go enrichmentService.Run(workerCtx)
	}

	go func() {
		log.Printf("Starting server on port %s", cfg.Port)
		if err := srv.Start(); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down gracefully...")
	workerCancel()
}
