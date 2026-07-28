package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-api-server/internal/config"
	"go-api-server/internal/database"
	"go-api-server/internal/server"
)

func gracefulShutdown(apiServer *http.Server, done chan bool) {
	// Create context that listens for the interrupt signal from the OS.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Listen for the interrupt signal.
	<-ctx.Done()

	slog.Info("shutting down gracefully, press Ctrl+C again to force")
	stop() // Allow Ctrl+C to force shutdown

	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := apiServer.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown with error", "err", err)
	}

	slog.Info("server exiting")

	// Notify the main goroutine that the shutdown is complete
	done <- true
}

func main() {
	config.LoggerConfig()
	slog.Info("logger initialized")

	path := os.Getenv("ENV_FILE")
	if err := config.LoadEnvFile(path); err != nil {
		slog.Info("no env file loaded, using process environment", "path", path, "err", err)
	}
	slog.Info("loaded env file", "path", path)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "err", err)
		os.Exit(1)
	}
	slog.Info("env variables configured")

	db, err := database.New(cfg)
	if err != nil {
		slog.Error("database connection failed", "err", err)
		os.Exit(1)
	}
	slog.Info("database connected")
	defer db.Close()

	apiServer, err := server.NewServer(cfg, db)
	if err != nil {
		slog.Error("server initialization error", "err", err)
		os.Exit(1)
	}
	slog.Info("server initialized and listening")

	// Create a done channel to signal when the shutdown is complete
	done := make(chan bool, 1)

	// Run graceful shutdown in a separate goroutine
	go gracefulShutdown(apiServer, done)

	if err = apiServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("http server error", "err", err)
		os.Exit(1)
	}

	// Wait for the graceful shutdown to complete
	<-done
	slog.Info("graceful shutdown complete")
}
