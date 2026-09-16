package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"apihub-go/internal/authn"
	"apihub-go/internal/background"
	"apihub-go/internal/catalog"
	"apihub-go/internal/config"
	"apihub-go/internal/executor"
	"apihub-go/internal/httpapi"
	"apihub-go/internal/rediscache"
	"apihub-go/internal/secret"
	"apihub-go/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	settings, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	codec, err := secret.FromConfig(settings.EncryptionKey, settings.PreviousEncryptionKeys, settings.EncryptionKeyVersion)
	if err != nil {
		logger.Error("create credential codec", "error", err)
		os.Exit(1)
	}
	startup, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStartup()
	database, err := store.Open(startup, settings.DatabaseURL, settings.WorkspaceID)
	if err != nil {
		logger.Error("open PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	cache, err := rediscache.Open(startup, settings.RedisAddress, settings.RedisPassword, settings.RedisDB)
	if err != nil {
		logger.Warn("Redis unavailable; control plane will start degraded", "error", err)
	}
	defer cache.Close()
	providerCatalog, err := catalog.Load(settings.CatalogDirectory)
	if err != nil {
		logger.Error("load provider catalog", "error", err)
		os.Exit(1)
	}
	if err := database.CheckSchema(startup); err != nil {
		logger.Error("check schema", "error", err)
		os.Exit(1)
	}
	client := executor.NewGuardedClient(settings.AllowedCIDRs...)
	authService := authn.New(database, codec, cache, client)
	actionExecutor := executor.New(client)
	handler := httpapi.New(httpapi.Dependencies{Store: database, Catalog: providerCatalog, Codec: codec, Auth: authService, Executor: executor.New(client), Cache: cache, Logger: logger, PublicBaseURL: settings.PublicBaseURL})
	runContext, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	backgroundService := background.New(database, authService, actionExecutor, codec, client, logger, settings.WorkspaceID+":"+settings.Role)
	var workers sync.WaitGroup
	if settings.Role == "all" || settings.Role == "worker" {
		workers.Add(1)
		go func() { defer workers.Done(); backgroundService.RunWorker(runContext) }()
	}
	if settings.Role == "all" || settings.Role == "scheduler" {
		workers.Add(1)
		go func() { defer workers.Done(); backgroundService.RunScheduler(runContext) }()
	}
	server := &http.Server{Addr: settings.Address, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 40 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		logger.Info("APIHub started", "address", settings.Address, "role", settings.Role, "workspace", settings.WorkspaceID)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server stopped", "error", err)
			os.Exit(1)
		}
	}()
	stop, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	<-stop.Done()
	backgroundService.StopClaiming()
	ctx, shutdown := context.WithTimeout(context.Background(), 45*time.Second)
	defer shutdown()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		_ = server.Close()
	}
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		cancelRun()
		<-done
	}
}
