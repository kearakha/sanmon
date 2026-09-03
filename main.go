package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	// Baris requests lebih tua dari retentionKeep dihapus (PRD §M3), disapu
	// tiap retentionSweep. Nilai tetap — nggak masuk config (CLAUDE.md: no
	// config buat nilai yang nggak pernah berubah).
	retentionKeep  = 30 * 24 * time.Hour
	retentionSweep = 1 * time.Hour
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := loadConfig("config.yaml")
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	httpClient := &http.Client{}

	db, err := openDB(cfg.DatabaseURL)
	if err != nil {
		slog.Error("open db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	store := NewStore(db)

	hub := NewHub()
	go hub.Run()

	v1 := http.NewServeMux()
	v1.HandleFunc("POST /v1/chat/completions", newChatCompletionsHandler(httpClient, cfg.GeminiAPIKey, cfg.Models, hub, store))
	v1.HandleFunc("POST /v1/embeddings", newEmbeddingsHandler(httpClient, cfg.GeminiAPIKey, cfg.Models, hub, store))
	v1.HandleFunc("GET /v1/models", newModelsHandler(httpClient, cfg.GeminiAPIKey))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.Handle("/v1/", requireBearerToken(cfg.VirtualKey, v1))
	mux.HandleFunc("GET /admin/stream", requireAdminQueryToken(cfg.AdminToken, newStreamHandler(hub)))
	mux.HandleFunc("GET /admin/requests", requireAdminQueryToken(cfg.AdminToken, newRequestsListHandler(db)))
	mux.HandleFunc("GET /admin/requests/{id}", requireAdminQueryToken(cfg.AdminToken, newRequestHandler(db)))

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go store.Run(ctx)
	go store.RunRetention(ctx, retentionSweep, retentionKeep)

	go func() {
		slog.Info("server starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
		os.Exit(1)
	}

	slog.Info("shut down cleanly")
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
