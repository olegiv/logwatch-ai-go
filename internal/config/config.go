package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Config holds all application configuration
type Config struct {
	// AI Provider
	AnthropicAPIKey string
	ClaudeModel     string

	// Telegram
	TelegramBotToken       string
	TelegramArchiveChannel int64
	TelegramAlertsChannel  int64 // Optional

	// Paths
	LogwatchOutputPath string
	MaxLogSizeMB       int

	// Application
	LogLevel       string
	EnableDatabase bool
	DatabasePath   string

	// Preprocessing
	EnablePreprocessing    bool
	MaxPreprocessingTokens int

	// Proxy
	HTTPProxy  string
	HTTPSProxy string
}

// Load loads configuration from .env file and environment variables
// Priority: .env file > OS environment variables
func Load() (*Config, error) {
	// Set up viper first to read OS environment variables
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Load .env file to override OS environment variables
	// godotenv.Load() sets OS env vars from .env, which viper will then read
	_ = godotenv.Load()

	// Set defaults
	setDefaults()

	config := &Config{
		AnthropicAPIKey:        viper.GetString("ANTHROPIC_API_KEY"),
		ClaudeModel:            viper.GetString("CLAUDE_MODEL"),
		TelegramBotToken:       viper.GetString("TELEGRAM_BOT_TOKEN"),
		TelegramArchiveChannel: viper.GetInt64("TELEGRAM_CHANNEL_ARCHIVE_ID"),
		TelegramAlertsChannel:  viper.GetInt64("TELEGRAM_CHANNEL_ALERTS_ID"),
		LogwatchOutputPath:     viper.GetString("LOGWATCH_OUTPUT_PATH"),
		MaxLogSizeMB:           viper.GetInt("MAX_LOG_SIZE_MB"),
		LogLevel:               viper.GetString("LOG_LEVEL"),
		EnableDatabase:         viper.GetBool("ENABLE_DATABASE"),
		DatabasePath:           viper.GetString("DATABASE_PATH"),
		EnablePreprocessing:    viper.GetBool("ENABLE_PREPROCESSING"),
		MaxPreprocessingTokens: viper.GetInt("MAX_PREPROCESSING_TOKENS"),
		HTTPProxy:              viper.GetString("HTTP_PROXY"),
		HTTPSProxy:             viper.GetString("HTTPS_PROXY"),
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// setDefaults sets default configuration values
func setDefaults() {
	viper.SetDefault("CLAUDE_MODEL", "claude-sonnet-4-5-20250929")
	viper.SetDefault("LOGWATCH_OUTPUT_PATH", "/tmp/logwatch-output.txt")
	viper.SetDefault("MAX_LOG_SIZE_MB", 10)
	viper.SetDefault("LOG_LEVEL", "info")
	viper.SetDefault("ENABLE_DATABASE", true)
	viper.SetDefault("DATABASE_PATH", "./data/summaries.db")
	viper.SetDefault("ENABLE_PREPROCESSING", true)
	viper.SetDefault("MAX_PREPROCESSING_TOKENS", 150000)
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate Anthropic API Key
	if c.AnthropicAPIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is required")
	}
	if !strings.HasPrefix(c.AnthropicAPIKey, "sk-ant-") {
		return fmt.Errorf("ANTHROPIC_API_KEY must start with 'sk-ant-'")
	}

	// Validate Telegram Bot Token
	if c.TelegramBotToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	telegramTokenRegex := regexp.MustCompile(`^\d+:[A-Za-z0-9_-]+$`)
	if !telegramTokenRegex.MatchString(c.TelegramBotToken) {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN has invalid format (expected: 'number:token')")
	}

	// Validate Telegram Archive Channel (required)
	if c.TelegramArchiveChannel == 0 {
		return fmt.Errorf("TELEGRAM_CHANNEL_ARCHIVE_ID is required")
	}
	if c.TelegramArchiveChannel > -100 {
		return fmt.Errorf("TELEGRAM_CHANNEL_ARCHIVE_ID must be a supergroup/channel ID (starts with -100)")
	}

	// Validate Telegram Alerts Channel (optional, but if set must be valid)
	if c.TelegramAlertsChannel != 0 && c.TelegramAlertsChannel > -100 {
		return fmt.Errorf("TELEGRAM_CHANNEL_ALERTS_ID must be a supergroup/channel ID (starts with -100)")
	}

	// Validate logwatch output path
	if c.LogwatchOutputPath == "" {
		return fmt.Errorf("LOGWATCH_OUTPUT_PATH is required")
	}

	// Validate max log size
	if c.MaxLogSizeMB < 1 || c.MaxLogSizeMB > 100 {
		return fmt.Errorf("MAX_LOG_SIZE_MB must be between 1 and 100")
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLogLevels[strings.ToLower(c.LogLevel)] {
		return fmt.Errorf("LOG_LEVEL must be one of: debug, info, warn, error")
	}

	// Validate preprocessing tokens
	if c.EnablePreprocessing && c.MaxPreprocessingTokens < 10000 {
		return fmt.Errorf("MAX_PREPROCESSING_TOKENS must be at least 10000")
	}

	return nil
}

// HasAlertsChannel returns true if alerts channel is configured
func (c *Config) HasAlertsChannel() bool {
	return c.TelegramAlertsChannel != 0
}

// GetProxyURL returns the appropriate proxy URL for HTTP/HTTPS requests
func (c *Config) GetProxyURL(isHTTPS bool) string {
	if isHTTPS && c.HTTPSProxy != "" {
		return c.HTTPSProxy
	}
	if c.HTTPProxy != "" {
		return c.HTTPProxy
	}
	return ""
}
