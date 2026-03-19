package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	flag.String("username", "", "preferred_username of the user to promote")
	flag.Int64("user-id", 0, "numeric user id to promote")
	flag.Bool("moderator", false, "also grant moderator flag during bootstrap")
	flag.Parse()

	fmt.Fprintln(os.Stderr, "setting is_admin via code is disabled; update users.is_admin directly in the database")
	os.Exit(1)
}
