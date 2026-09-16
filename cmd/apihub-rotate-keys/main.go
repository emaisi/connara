package main

import (
	"apihub-go/internal/config"
	"apihub-go/internal/secret"
	"apihub-go/internal/store"
	"context"
	"log"
	"os"
	"strconv"
	"time"
)

func main() {
	version, err := strconv.Atoi(os.Getenv("APIHUB_ENCRYPTION_KEY_VERSION"))
	if err != nil {
		log.Fatal("APIHUB_ENCRYPTION_KEY_VERSION is required")
	}
	codec, err := secret.FromConfig(os.Getenv("APIHUB_ENCRYPTION_KEY"), os.Getenv("APIHUB_PREVIOUS_ENCRYPTION_KEYS"), version)
	if err != nil {
		log.Fatal(err)
	}
	workspace := os.Getenv("APIHUB_WORKSPACE_ID")
	if workspace == "" {
		workspace = config.DefaultWorkspaceID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	database, err := store.Open(ctx, os.Getenv("APIHUB_DATABASE_URL"), workspace)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	if err := database.CheckSchema(ctx); err != nil {
		log.Fatal(err)
	}
	count, err := database.RotateSecrets(ctx, codec)
	if err != nil {
		log.Fatalf("rotated %d rows before stopping: %v", count, err)
	}
	log.Printf("rotated %d encrypted rows to key version %d", count, codec.Version())
}
