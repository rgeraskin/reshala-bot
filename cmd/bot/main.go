package main

import (
	"context"
	"log"
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
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting aiops bot...")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("%s", cfg)

	store, err := storage.NewStorage(cfg.Storage.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer store.Close()
	log.Println("Database initialized successfully")

	sanitizer := security.NewSanitizer(cfg.Security.SecretPatterns)
	log.Printf("Security sanitizer initialized with %d patterns", len(cfg.Security.SecretPatterns))

	contextManager := ctx.NewManager(store, cfg.Context.TTL)
	log.Printf("Context manager initialized (TTL: %v)", cfg.Context.TTL)

	validator, err := ctx.NewValidator(store, cfg.Claude.ProjectPath, cfg.Context.ValidationEnabled)
	if err != nil {
		log.Printf("Warning: validator initialization failed: %v", err)
	}
	log.Printf("Context validator initialized (enabled: %v)", cfg.Context.ValidationEnabled)

	processManager := claude.NewProcessManager(
		cfg.Claude.CLIPath,
		cfg.Claude.ProjectPath,
		cfg.Claude.MaxConcurrentSessions,
		cfg.Claude.QueryTimeout,
	)
	log.Printf("Process manager initialized (max sessions: %d, timeout: %v)",
		cfg.Claude.MaxConcurrentSessions, cfg.Claude.QueryTimeout)

	executor := claude.NewExecutor(processManager, cfg.Claude.ProjectPath, cfg.Claude.QueryTimeout)
	log.Println("Claude executor initialized")

	expiryWorker := ctx.NewExpiryWorker(store, processManager, cfg.Context.CleanupInterval)
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()

	go expiryWorker.Start(workerCtx)
	log.Printf("Expiry worker started (interval: %v)", cfg.Context.CleanupInterval)

	platform, err := telegram.NewClient(cfg.Telegram.Token)
	if err != nil {
		log.Fatalf("Failed to create Telegram client: %v", err)
	}
	log.Println("Telegram client initialized")

	handler := bot.NewHandler(
		platform,
		contextManager,
		validator,
		processManager,
		executor,
		sanitizer,
		store,
		cfg.Telegram.AllowedChatIDs,
	)
	log.Printf("Bot handler initialized (whitelist: %d allowed IDs)", len(cfg.Telegram.AllowedChatIDs))

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, shutting down gracefully...", sig)
		cancelWorker()

		activeCount := processManager.GetActiveProcessCount()
		log.Printf("Cleaning up %d active processes...", activeCount)

		os.Exit(0)
	}()

	log.Println("Bot is ready to receive messages!")
	log.Println("Add the bot to a Telegram group/channel to start using it")

	if err := platform.Start(handler.HandleMessage); err != nil {
		log.Fatalf("Bot stopped with error: %v", err)
	}
}
