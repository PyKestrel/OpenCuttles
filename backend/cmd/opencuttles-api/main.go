package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/api"
	"github.com/opencuttles/opencuttles/backend/internal/auth"
	"github.com/opencuttles/opencuttles/backend/internal/devicecontrol"
	"github.com/opencuttles/opencuttles/backend/internal/orchestrator"
	"github.com/opencuttles/opencuttles/backend/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dbPath := env("OPENCUTTLES_DB", "opencuttles.db")
	listenAddr := env("OPENCUTTLES_LISTEN", ":8080")
	allowedOrigin := os.Getenv("OPENCUTTLES_ALLOWED_ORIGIN")
	secureCookies := env("OPENCUTTLES_SECURE_COOKIES", "1") != "0"

	db, err := store.OpenSQLite(dbPath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	authService := auth.NewService(db)
	service := orchestrator.NewService(db, orchestrator.NewExecRunner(logger), logger)
	devices := devicecontrol.NewService(db, nil, logger)
	if err := db.PruneExpiredSessions(context.Background()); err != nil {
		logger.Error("prune sessions failed", "error", err)
		os.Exit(1)
	}
	if err := service.Reconcile(context.Background()); err != nil {
		logger.Error("reconcile failed", "error", err)
		os.Exit(1)
	}
	// Nothing resumes a test cycle across a restart, and a cycle run left
	// 'running' blocks its schedule from ever firing again.
	if swept, err := db.FailStrandedCycleRuns(context.Background(), "interrupted by API restart"); err != nil {
		logger.Error("sweep stranded cycle runs failed", "error", err)
		os.Exit(1)
	} else if swept > 0 {
		logger.Warn("marked interrupted cycle runs as failed", "count", swept)
	}
	apiServer := api.New(db, service, authService, devices, logger, secureCookies, allowedOrigin)
	server := &http.Server{
		Addr:              listenAddr,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("starting api", "addr", listenAddr, "db", dbPath)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("api failed", "error", err)
			os.Exit(1)
		}
	}()

	// Optional runner mutual-TLS listener. Off unless
	// OPENCUTTLES_RUNNER_MTLS_LISTEN is set. It terminates TLS here rather than
	// behind Caddy so the client certificate is verified by the process that acts
	// on it, instead of being asserted by a forwarded header.
	var mtlsServer *http.Server
	if mtls, err := apiServer.MTLSServer(context.Background()); err != nil {
		// Misconfigured mTLS must not silently fall back to token-only auth: the
		// operator asked for it, and every runner would otherwise be refused with
		// no explanation.
		logger.Error("runner mTLS is enabled but could not start", "error", err)
		os.Exit(1)
	} else if mtls != nil {
		mtlsServer = mtls
		go func() {
			logger.Info("starting runner mTLS listener", "addr", mtls.Addr)
			// Certificates come from TLSConfig, so the file arguments are empty.
			if err := mtls.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				logger.Error("runner mTLS listener failed", "error", err)
				os.Exit(1)
			}
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if mtlsServer != nil {
		_ = mtlsServer.Shutdown(shutdownCtx)
	}
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("api stopped")
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
