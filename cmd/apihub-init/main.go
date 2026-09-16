package main

import (
	"context"
	"log"
	"os"
	"time"

	"apihub-go/internal/catalog"
	"apihub-go/internal/config"
	"apihub-go/internal/store"
)

func main() {
	databaseURL := os.Getenv("APIHUB_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("APIHUB_DATABASE_URL")
	}
	if databaseURL == "" {
		log.Fatal("APIHUB_DATABASE_URL is required")
	}
	workspaceID := env("APIHUB_WORKSPACE_ID", config.DefaultWorkspaceID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database, err := store.Open(ctx, databaseURL, workspaceID)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		log.Fatal(err)
	}
	providerCatalog, err := catalog.Load(os.Getenv("APIHUB_CATALOG_DIR"))
	if err != nil {
		log.Fatal(err)
	}
	err = database.Bootstrap(ctx, store.BootstrapInput{WorkspaceID: workspaceID, WorkspaceSlug: env("APIHUB_WORKSPACE_SLUG", "default"), WorkspaceName: env("APIHUB_WORKSPACE_NAME", "APIHub"), AdminUserID: env("APIHUB_ADMIN_USER_ID", config.DefaultAdminUserID), AdminEmail: env("APIHUB_ADMIN_EMAIL", "admin@localhost"), AdminPasswordHash: os.Getenv("APIHUB_ADMIN_PASSWORD_HASH"), PublicBaseURL: env("APIHUB_PUBLIC_BASE_URL", "http://127.0.0.1:8080"), Providers: providerCatalog.Providers()})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("APIHub database initialized for workspace %s", workspaceID)
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
