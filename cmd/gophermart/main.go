// Package main starts the Gophermart loyalty service.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/accrual"
	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/auth"
	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/config"
	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/server"
	"github.com/Dja-tiger/go-musthave-diploma-tpl/internal/storage"
)

func main() {
	cfg := config.Load()
	if cfg.DatabaseURI == "" {
		log.Fatal("DATABASE_URI or -d is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := storage.New(ctx, cfg.DatabaseURI)
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	defer store.Close()

	tokens, err := auth.NewManager()
	if err != nil {
		log.Fatalf("create auth manager: %v", err)
	}

	go accrual.NewWorker(store, accrual.NewClient(cfg.AccrualSystemAddress)).Run(ctx)

	httpServer := &http.Server{
		Addr:              cfg.RunAddress,
		Handler:           server.New(store, tokens).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("gophermart listening on %s", cfg.RunAddress)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve http: %v", err)
	}
}
