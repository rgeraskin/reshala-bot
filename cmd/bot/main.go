package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rg/aiops/internal/bot"
	"github.com/rg/aiops/internal/claude"
	"github.com/rg/aiops/internal/config"
	ctx "github.com/rg/aiops/internal/context"
	"github.com/rg/aiops/internal/messaging/telegram"
	"github.com/rg/aiops/internal/security"
	"github.com/rg/aiops/internal/storage"
)

func main() {
	// Initialize structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		AddSource: true,
	}))
	slog.SetDefault(logger)

	slog.Info("Starting aiops bot")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	slog.Info("Configuration loaded", "config", cfg)

	store, err := storage.NewStorage(cfg.Storage.DBPath)
	if err != nil {
		slog.Error("Failed to initialize storage", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	slog.Info("Database initialized successfully")

	sanitizer, err := security.NewSanitizer(cfg.Security.SecretPatterns)
	if err != nil {
		slog.Error("Failed to initialize sanitizer", "error", err)
		os.Exit(1)
	}
	slog.Info("Security sanitizer initialized", "patterns_count", len(cfg.Security.SecretPatterns))

	// SessionManager must be created before ContextManager (used to cleanup orphaned sessions)
	sessionManager := claude.NewSessionManager(
		cfg.Claude.CLIPath,
		cfg.Claude.ProjectPath,
		cfg.Claude.Model,
		cfg.Claude.MaxConcurrentSessions,
		cfg.Claude.QueryTimeout,
	)
	slog.Info("Session manager initialized",
		"max_sessions", cfg.Claude.MaxConcurrentSessions,
		"timeout", cfg.Claude.QueryTimeout)

	contextManager := ctx.NewManager(store, sessionManager, cfg.Context.TTL)
	slog.Info("Context manager initialized", "ttl", cfg.Context.TTL)

	validator, err := ctx.NewValidator(store, cfg.Claude.ProjectPath, cfg.Context.ValidationEnabled)
	if err != nil {
		slog.Warn("Validator initialization failed", "error", err)
	}
	slog.Info("Context validator initialized", "enabled", cfg.Context.ValidationEnabled)

	executor := claude.NewExecutor(sessionManager, cfg.Claude.ProjectPath, cfg.Claude.QueryTimeout)
	slog.Info("Claude executor initialized")

	// Validate Claude CLI is available
	if err := sessionManager.ValidateCLI(); err != nil {
		slog.Error("Claude CLI validation failed", "error", err)
		os.Exit(1)
	}
	slog.Info("Claude CLI validated successfully")

	expiryWorker := ctx.NewExpiryWorker(store, sessionManager, cfg.Context.CleanupInterval)
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()

	go expiryWorker.Start(workerCtx)
	slog.Info("Expiry worker started", "interval", cfg.Context.CleanupInterval)

	platform, err := telegram.NewClient(cfg.Telegram.Token)
	if err != nil {
		slog.Error("Failed to create Telegram client", "error", err)
		os.Exit(1)
	}
	slog.Info("Telegram client initialized")

	handler := bot.NewHandler(
		platform,
		contextManager,
		expiryWorker,
		validator,
		sessionManager,
		executor,
		sanitizer,
		store,
		cfg.Telegram.AllowedChatIDs,
	)
	slog.Info("Bot handler initialized", "allowed_chats", len(cfg.Telegram.AllowedChatIDs))

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		slog.Info("Received shutdown signal", "signal", sig)
		cancelWorker()

		activeCount := sessionManager.GetActiveSessionCount()
		slog.Info("Cleaning up active sessions", "count", activeCount)

		// Stop Telegram client gracefully
		platform.Stop()

		os.Exit(0)
	}()

	slog.Info("Bot is ready to receive messages")

	if err := platform.Start(handler.HandleMessage); err != nil {
		slog.Error("Bot stopped with error", "error", err)
		os.Exit(1)
	}
}
