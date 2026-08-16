/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

// Command agentgw runs the Agent Gateway HTTP server.
//
// Usage:
//
//	agentgw --config ./config/services.yaml --db ./data/agentgate.db --addr :8080
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Clawdlinux/agentgate/internal/admin"
	"github.com/Clawdlinux/agentgate/internal/auth"
	agentgatedb "github.com/Clawdlinux/agentgate/internal/db"
	"github.com/Clawdlinux/agentgate/internal/gateway"
	"github.com/Clawdlinux/agentgate/internal/oauth"
	"github.com/Clawdlinux/agentgate/internal/ratelimit"
	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/signer"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

var version = "0.1.0-dev"

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	configPath := flag.String("config", "./config/services.yaml", "path to service registry YAML")
	dbPath := flag.String("db", "./data/agentgate.db", "path to the SQLite database file")
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Println(version)
		os.Exit(0)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Load service registry.
	reg := registry.New()
	if err := reg.LoadFile(*configPath); err != nil {
		logger.Error("load service config", "error", err)
		os.Exit(1)
	}
	logger.Info("loaded services", "count", reg.Count(), "services", reg.List())

	// Open the persistent database and apply every migration. This is the
	// real composition root: vault, auth, and receipt state all live here
	// now, not in memory (LEDG-02).
	database, err := agentgatedb.Open(*dbPath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	if err := agentgatedb.RunMigrations(database); err != nil {
		logger.Error("run migrations", "error", err)
		os.Exit(1)
	}

	masterKey, err := loadMasterKey()
	if err != nil {
		logger.Error("load vault key", "error", err)
		os.Exit(1)
	}

	vaultStore, err := vault.NewSQLiteStore(database, masterKey)
	if err != nil {
		logger.Error("init vault", "error", err)
		os.Exit(1)
	}

	keyStore := auth.NewKeyStore(database)
	if err := bootstrapAgentKey(context.Background(), keyStore, logger); err != nil {
		logger.Error("bootstrap agent key", "error", err)
		os.Exit(1)
	}

	// Signer derives its own purpose-specific encryption key from
	// masterKey — it never uses masterKey directly (internal/signer's own
	// guarantee), so reusing the vault's master secret here does not reuse
	// the vault's actual encryption key.
	signerStore, err := signer.NewStore(database, masterKey)
	if err != nil {
		logger.Error("init signer", "error", err)
		os.Exit(1)
	}
	if _, _, err := signerStore.LoadOrCreateActive(1); err != nil {
		logger.Error("load or create signing key", "error", err)
		os.Exit(1)
	}

	ledger := receipt.NewLedger(database, signerStore)
	limiter := ratelimit.New(nil) // no per-service limits configured yet; Allow() is a pass-through

	srv := gateway.New(gateway.Config{
		Registry:   reg,
		Vault:      vaultStore,
		Logger:     logger,
		Authorizer: keyStore,
		Receipts:   ledger,
		Limiter:    limiter,
	})

	adminSecret := os.Getenv("AGENTGATE_ADMIN_SECRET")
	if adminSecret == "" {
		logger.Error("AGENTGATE_ADMIN_SECRET must be set")
		os.Exit(1)
	}
	publicURL := os.Getenv("AGENTGATE_PUBLIC_URL")
	if publicURL == "" {
		publicURL = "http://localhost:8080"
	}

	oauthHandler := oauth.NewCallbackHandler(buildOAuthProviders(reg, logger), vaultStore, masterKey, publicURL, logger)
	adminHandler := admin.NewHandler(keyStore, oauthHandler, vaultStore, adminSecret, logger)

	mux := http.NewServeMux()
	mux.Handle("/", srv)
	mux.HandleFunc("GET /v1/receipts/pubkey", signer.PubkeyHandler(signerStore))
	mux.Handle("POST /admin/keys", adminHandler.RequireAdmin(http.HandlerFunc(adminHandler.CreateKey)))
	mux.Handle("DELETE /admin/keys/{id}", adminHandler.RequireAdmin(http.HandlerFunc(adminHandler.RevokeKey)))
	mux.Handle("POST /admin/link", adminHandler.RequireAdmin(http.HandlerFunc(adminHandler.LinkAccount)))
	mux.Handle("GET /admin/tokens/{user_id}", adminHandler.RequireAdmin(http.HandlerFunc(adminHandler.ListTokens)))
	mux.HandleFunc("GET /auth/callback/{service}", oauthHandler.ServeHTTP)

	httpSrv := &http.Server{
		Addr:         *addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("agent-gateway starting", "addr", *addr, "version", version)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("listen", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "error", err)
	}
}

// loadMasterKey reads the 32-byte master secret used to derive both the
// vault's token-encryption key and the signer's storage-encryption key.
// AGENTGATE_VAULT_KEY is the name docker-compose.yaml and .env.example
// already document; the previous VAULT_ENCRYPTION_KEY read in this command
// never matched either file.
func loadMasterKey() ([]byte, error) {
	envKey := os.Getenv("AGENTGATE_VAULT_KEY")
	if len(envKey) != 32 {
		return nil, fmt.Errorf("AGENTGATE_VAULT_KEY must be set to exactly 32 bytes (got %d) — this key encrypts vault tokens and the receipt signing key at rest", len(envKey))
	}
	return []byte(envKey), nil
}

// bootstrapAgentKey creates one agent API key on an empty database and
// logs its plaintext value once. There is no import path for an
// externally supplied plaintext key under the bcrypt-hashed key store, so
// the previous AGENT_API_KEY env var — a plaintext MVP map lookup — has no
// equivalent here; an operator now retrieves the bootstrap key from this
// one-time log line, or an admin API caller creates additional keys via
// internal/auth.KeyStore.Create.
func bootstrapAgentKey(ctx context.Context, keyStore *auth.KeyStore, logger *slog.Logger) error {
	count, err := keyStore.Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, plaintext, err := keyStore.Create(ctx, "bootstrap-agent", []string{"*"}, []string{"*"})
	if err != nil {
		return err
	}
	logger.Warn("bootstrapped a new agent API key — this is the only time it is shown; store it now",
		"agent_key", plaintext,
	)
	return nil
}

// buildOAuthProviders constructs one oauth.Provider per registered service
// whose auth.type is oauth2 and whose <SERVICE>_CLIENT_ID/_CLIENT_SECRET
// env vars are both set. A service missing either value is skipped with a
// warning, not a startup failure — an operator connecting only GitHub
// should not be forced to configure every registered service.
func buildOAuthProviders(reg *registry.Registry, logger *slog.Logger) map[string]*oauth.Provider {
	providers := make(map[string]*oauth.Provider)
	for _, name := range reg.List() {
		svc, err := reg.Get(name)
		if err != nil || svc.Auth.Type != "oauth2" {
			continue
		}
		prefix := strings.ToUpper(name)
		clientID := os.Getenv(prefix + "_CLIENT_ID")
		clientSecret := os.Getenv(prefix + "_CLIENT_SECRET")
		if clientID == "" || clientSecret == "" {
			logger.Warn("oauth provider not configured; account linking is disabled for this service", "service", name)
			continue
		}
		providers[name] = &oauth.Provider{
			Name:         name,
			ClientID:     clientID,
			ClientSecret: clientSecret,
			AuthorizeURL: svc.Auth.AuthorizeURL,
			TokenURL:     svc.Auth.TokenURL,
			Scopes:       svc.Auth.Scopes,
		}
	}
	return providers
}
