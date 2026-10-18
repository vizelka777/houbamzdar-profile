package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/media"
	"github.com/houbamzdar/bff/internal/models"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

func main() {
	apply := flag.Bool("apply", false, "create derived images; default only reports count")
	workers := flag.Int("workers", 4, "number of concurrent image preparations, 1-16")
	flag.Parse()
	if *workers < 1 || *workers > 16 {
		log.Fatal("workers must be between 1 and 16")
	}
	cfg := config.Load()
	if cfg.DBURL == "" {
		log.Fatal("DB_URL is required")
	}
	url := cfg.DBURL
	if cfg.DBToken != "" {
		url += "?authToken=" + cfg.DBToken
	}
	database, err := sql.Open("libsql", url)
	if err != nil {
		log.Fatal("cannot open database")
	}
	defer database.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	rows, err := database.QueryContext(ctx, `SELECT id, private_storage_key, COALESCE(public_storage_key, '') FROM photo_captures WHERE private_storage_key IS NOT NULL AND private_storage_key != '' ORDER BY id`)
	if err != nil {
		log.Fatal("cannot list capture storage keys")
	}
	var captures []*models.Capture
	for rows.Next() {
		capture := &models.Capture{}
		if err := rows.Scan(&capture.ID, &capture.PrivateStorageKey, &capture.PublicStorageKey); err != nil {
			log.Fatal(err)
		}
		captures = append(captures, capture)
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
	rows.Close()
	fmt.Printf("captures=%d apply=%t\n", len(captures), *apply)
	if !*apply {
		return
	}
	storage := media.NewBunnyStorage(cfg)
	if storage == nil {
		log.Fatal("storage is not configured")
	}
	var failed, processed atomic.Int32
	jobs := make(chan *models.Capture)
	var group sync.WaitGroup
	for worker := 0; worker < *workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for capture := range jobs {
				if ctx.Err() != nil {
					continue
				}
				itemCtx, itemCancel := context.WithTimeout(ctx, 3*time.Minute)
				err := storage.BackfillCaptureImageVariants(itemCtx, capture)
				itemCancel()
				if err != nil {
					if failed.Add(1) >= 10 {
						cancel()
					}
					log.Printf("capture %s: %v", capture.ID, err)
				}
				count := processed.Add(1)
				if count%25 == 0 || int(count) == len(captures) {
					fmt.Printf("processed=%d/%d failed=%d\n", count, len(captures), failed.Load())
				}
			}
		}()
	}
	for _, capture := range captures {
		if ctx.Err() != nil {
			break
		}
		jobs <- capture
	}
	close(jobs)
	group.Wait()
	if ctx.Err() != nil || failed.Load() != 0 {
		os.Exit(1)
	}
}
