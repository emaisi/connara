package main

import (
	"apihub-go/internal/config"
	"apihub-go/internal/store"
	"context"
	"flag"
	"log"
	"os"
	"time"
)

func main() {
	at := flag.String("resume-at", "", "fixed UTC RFC3339 cutoff; stop all schedulers before running")
	scope := flag.String("scope", "v2", "v2 or code")
	flag.Parse()
	resume, err := time.Parse(time.RFC3339Nano, *at)
	if err != nil || resume.Location() != time.UTC {
		log.Fatal("--resume-at must be UTC RFC3339")
	}
	if *scope != "v2" && *scope != "code" {
		log.Fatal("--scope must be v2 or code")
	}
	workspace := os.Getenv("APIHUB_WORKSPACE_ID")
	if workspace == "" {
		workspace = config.DefaultWorkspaceID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	db, err := store.Open(ctx, os.Getenv("APIHUB_DATABASE_URL"), workspace)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.CheckSchema(ctx); err != nil {
		log.Fatal(err)
	}
	count, err := db.RescheduleWorkflowPlans(ctx, resume, *scope == "code")
	if err != nil {
		log.Fatalf("reordered %d plans; keep schedulers stopped and repeat the same cutoff: %v", count, err)
	}
	log.Printf("reordered %d plans after %s", count, resume.Format(time.RFC3339Nano))
}
