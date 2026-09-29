package main

import (
	"context"
	"os"

	"open-prs/internal/openprs"
)

// main prints the open requests from the repositories configured in REPOS.
func main() {
	os.Exit(openprs.Run(context.Background(), os.Getenv("REPOS"), os.Stdout, os.Stderr))
}
