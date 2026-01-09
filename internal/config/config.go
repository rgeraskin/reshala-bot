package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram TelegramConfig `yaml:"telegram"`
	Claude   ClaudeConfig   `yaml:"claude"`
	Context  ContextConfig  `yaml:"context"`
	Storage  StorageConfig  `yaml:"storage"`
	Security SecurityConfig `yaml:"security"`
}

type TelegramConfig struct {
	Token          string   `yaml:"token"`
	AllowedChatIDs []string `yaml:"allowed_chat_ids"`
}

type ClaudeConfig struct {
	CLIPath               string        `yaml:"cli_path"`
	ProjectPath           string        `yaml:"project_path"`
	Model                 string        `yaml:"model"`
	QueryTimeout          time.Duration `yaml:"query_timeout"`
	MaxConcurrentSessions int           `yaml:"max_concurrent_sessions"`
}

type ContextConfig struct {
	TTL             time.Duration `yaml:"ttl"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
	ValidationEnabled bool          `yaml:"validation_enabled"`
}

type StorageConfig struct {
	DBPath string `yaml:"db_path"`
}

type SecurityConfig struct {
	SecretPatterns []string `yaml:"secret_patterns"`
}

func Load() (*Config, error) {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "./configs/config.yaml"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	content := expandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Validate configuration
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Telegram.Token == "" {
		return fmt.Errorf("telegram.token is required (check TELEGRAM_BOT_TOKEN env var)")
	}
	if len(c.Telegram.AllowedChatIDs) == 0 {
		return fmt.Errorf("telegram.allowed_chat_ids is required (at least one user or chat ID)")
	}
	if c.Claude.CLIPath == "" {
		return fmt.Errorf("claude.cli_path is required")
	}
	if c.Claude.ProjectPath == "" {
		return fmt.Errorf("claude.project_path is required")
	}
	if c.Claude.QueryTimeout == 0 {
		return fmt.Errorf("claude.query_timeout is required")
	}
	if c.Claude.MaxConcurrentSessions <= 0 {
		return fmt.Errorf("claude.max_concurrent_sessions must be positive")
	}
	if c.Context.TTL == 0 {
		return fmt.Errorf("context.ttl is required")
	}
	if c.Context.CleanupInterval == 0 {
		return fmt.Errorf("context.cleanup_interval is required")
	}
	if c.Storage.DBPath == "" {
		return fmt.Errorf("storage.db_path is required")
	}

	// Validate CLI path exists and is executable
	if info, err := os.Stat(c.Claude.CLIPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("claude.cli_path does not exist: %s", c.Claude.CLIPath)
		}
		return fmt.Errorf("claude.cli_path stat failed: %w", err)
	} else if info.IsDir() {
		return fmt.Errorf("claude.cli_path is a directory, not a file: %s", c.Claude.CLIPath)
	} else if info.Mode()&0111 == 0 {
		return fmt.Errorf("claude.cli_path is not executable: %s", c.Claude.CLIPath)
	}

	// Validate project path exists and is a directory
	if info, err := os.Stat(c.Claude.ProjectPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("claude.project_path does not exist: %s", c.Claude.ProjectPath)
		}
		return fmt.Errorf("claude.project_path stat failed: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("claude.project_path is not a directory: %s", c.Claude.ProjectPath)
	}

	return nil
}

func expandEnv(s string) string {
	return os.Expand(s, func(key string) string {
		return os.Getenv(key)
	})
}

func (c *Config) String() string {
	var sb strings.Builder
	sb.WriteString("Configuration:\n")
	sb.WriteString(fmt.Sprintf("  Telegram Token: %s\n", maskSecret(c.Telegram.Token)))
	sb.WriteString(fmt.Sprintf("  Claude CLI Path: %s\n", c.Claude.CLIPath))
	sb.WriteString(fmt.Sprintf("  Claude Project Path: %s\n", c.Claude.ProjectPath))
	sb.WriteString(fmt.Sprintf("  Claude Model: %s\n", c.Claude.Model))
	sb.WriteString(fmt.Sprintf("  Claude Query Timeout: %s\n", c.Claude.QueryTimeout))
	sb.WriteString(fmt.Sprintf("  Claude Max Sessions: %d\n", c.Claude.MaxConcurrentSessions))
	sb.WriteString(fmt.Sprintf("  Context TTL: %s\n", c.Context.TTL))
	sb.WriteString(fmt.Sprintf("  Context Cleanup Interval: %s\n", c.Context.CleanupInterval))
	sb.WriteString(fmt.Sprintf("  Context Validation: %v\n", c.Context.ValidationEnabled))
	sb.WriteString(fmt.Sprintf("  Storage DB Path: %s\n", c.Storage.DBPath))
	return sb.String()
}

func maskSecret(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "..." + s[len(s)-4:]
}
