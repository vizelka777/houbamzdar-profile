package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
)

func main() {
	username := flag.String("username", "", "preferred_username of the user to promote")
	userID := flag.Int64("user-id", 0, "numeric user id to promote")
	alsoModerator := flag.Bool("moderator", false, "also grant moderator flag during bootstrap")
	flag.Parse()

	if strings.TrimSpace(*username) == "" && *userID <= 0 {
		fmt.Fprintln(os.Stderr, "either -username or -user-id is required")
		flag.Usage()
		os.Exit(2)
	}

	cfg := config.Load()
	database, err := db.New(cfg)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	var updatedUserID int64
	if strings.TrimSpace(*username) != "" {
		user, err := database.BootstrapAdminByPreferredUsername(*username, *alsoModerator)
		if err != nil {
			if err == sql.ErrNoRows {
				log.Fatalf("user %q not found", *username)
			}
			log.Fatalf("bootstrap admin by username: %v", err)
		}
		updatedUserID = user.ID
		fmt.Printf("Admin role granted to %s (#%d). is_admin=%t is_moderator=%t\n", user.PreferredUsername, user.ID, user.IsAdmin, user.IsModerator)
		return
	}

	user, err := database.BootstrapAdminByUserID(*userID, *alsoModerator)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Fatalf("user #%d not found", *userID)
		}
		log.Fatalf("bootstrap admin by id: %v", err)
	}
	updatedUserID = user.ID
	fmt.Printf("Admin role granted to %s (#%d). is_admin=%t is_moderator=%t\n", user.PreferredUsername, updatedUserID, user.IsAdmin, user.IsModerator)
}
