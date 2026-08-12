/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

// Command agentgw runs the Agent Gateway HTTP server.
//
// Usage:
//
//	agentgw --config ./config/services.yaml --addr :8080
package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Clawdlinux/agentgate/internal/gateway"
	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

var version = "0.1.0-dev"

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	configPath := flag.String("config", "./config/services.yaml", "path to service registry YAML")
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

	// Initialize vault.
	// MVP: random key per process (tokens don't survive restart).
	// Production: load from VAULT_ENCRYPTION_KEY env var or KMS.
	encKey := make([]byte, 32)
	if envKey := os.Getenv("VAULT_ENCRYPTION_KEY"); len(envKey) == 32 {
		copy(encKey, []byte(envKey))
	} else {
		if _, err := rand.Read(encKey); err != nil {
			logger.Error("generate encryption key", "error", err)
			os.Exit(1)
		}
		logger.Warn("using random encryption key — tokens will not survive restart. Set VAULT_ENCRYPTION_KEY for persistence.")
	}

	store, err := vault.NewMemoryStore(encKey)
	if err != nil {
		logger.Error("init vault", "error", err)
		os.Exit(1)
	}

	// Agent API keys.
	// MVP: from environment. Production: from database.
	agentKeys := make(map[string]string)
	if key := os.Getenv("AGENT_API_KEY"); key != "" {
		agentKeys[key] = "default-agent"
	} else {
		logger.Warn("no AGENT_API_KEY set — all requests will be rejected")
	}

	srv := gateway.New(gateway.Config{
		Registry:  reg,
		Vault:     store,
		Logger:    logger,
		AgentKeys: agentKeys,
	})

	httpSrv := &http.Server{
		Addr:         *addr,
		Handler:      srv,
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
