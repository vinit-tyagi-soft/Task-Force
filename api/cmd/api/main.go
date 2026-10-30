// Command api serves the TaskForce /api/v1 HTTP API.
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

	"github.com/vinit-tyagi-soft/taskforce/api/internal/httpapi"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("api exited", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	dbURL := os.Getenv("TF_DATABASE_URL")
	secret := os.Getenv("TF_SESSION_SECRET")
	if dbURL == "" {
		return errors.New("TF_DATABASE_URL is required")
	}
	if len(secret) < 32 {
		return errors.New("TF_SESSION_SECRET must be at least 32 characters")
	}
	addr := os.Getenv("TF_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	devAuth := os.Getenv("TF_DEV_AUTH") == "true"
	if devAuth {
		log.Warn("development login is enabled; never enable TF_DEV_AUTH in production")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	api := httpapi.New(&store.Store{Pool: pool}, httpapi.Config{
		SessionSecret: []byte(secret),
		DevAuth:       devAuth,
		SecureCookies: os.Getenv("TF_SECURE_COOKIES") == "true",
	}, log)

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", addr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}
