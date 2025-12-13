package config

import (
	"crypto/subtle"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// CLIOptions holds command-line argument overrides
type CLIOptions struct {
	SourceType  string // -source-type: log source type (logwatch, drupal_watchdog)
	SourcePath  string // -source-path: path to log source file
	ShowHelp    bool   // -help: show usage
	ShowVersion bool   // -version: show version
}

// ParseCLI parses command-line arguments and returns CLIOptions
func ParseCLI() *CLIOptions {
	opts := &CLIOptions{}

	flag.StringVar(&opts.SourceType, "source-type", "", "Log source type: logwatch, drupal_watchdog")
	flag.StringVar(&opts.SourcePath, "source-path", "", "Path to log source file (overrides LOGWATCH_OUTPUT_PATH or DRUPAL_WATCHDOG_PATH)")
	flag.BoolVar(&opts.ShowHelp, "help", false, "Show usage information")
	flag.BoolVar(&opts.ShowVersion, "version", false, "Show version information")

	// Custom usage message
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Logwatch AI Analyzer - Intelligent log analysis with Claude AI\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -source-type logwatch\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -source-type drupal_watchdog -source-path /tmp/watchdog.json\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nEnvironment variables can be set in .env file or exported directly.\n")
		fmt.Fprintf(os.Stderr, "CLI arguments override environment variables.\n")
	}

	flag.Parse()

	return opts
}

// Config holds all application configuration
type Config struct {
	// AI Provider
	AnthropicAPIKey string
	ClaudeModel     string

	// Telegram
	TelegramBotToken       string
	TelegramArchiveChannel int64
	TelegramAlertsChannel  int64 // Optional

	// Log Source Selection
	LogSourceType string // "logwatch" or "drupal_watchdog"

	// Logwatch Settings (used when LogSourceType = "logwatch")
	LogwatchOutputPath string

	// Drupal Watchdog Settings (used when LogSourceType = "drupal_watchdog")
	DrupalWatchdogPath   string // Path to watchdog export file
	DrupalWatchdogFormat string // "json" or "drush"
	DrupalSiteName       string // Optional: site identifier for multi-site

	// Common Log Settings
	MaxLogSizeMB int

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

	// AI Settings (L-02 fix: make constants configurable)
	AITimeoutSeconds int
	AIMaxTokens      int
}

// Load loads configuration from .env file and environment variables
// Priority: .env file > OS environment variables
// For CLI overrides, use LoadWithCLI instead
func Load() (*Config, error) {
	return LoadWithCLI(nil)
}

// LoadWithCLI loads configuration with CLI argument overrides
// Priority: CLI args > .env file > OS environment variables
func LoadWithCLI(cli *CLIOptions) (*Config, error) {
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
		LogSourceType:          viper.GetString("LOG_SOURCE_TYPE"),
		LogwatchOutputPath:     viper.GetString("LOGWATCH_OUTPUT_PATH"),
		DrupalWatchdogPath:     viper.GetString("DRUPAL_WATCHDOG_PATH"),
		DrupalWatchdogFormat:   viper.GetString("DRUPAL_WATCHDOG_FORMAT"),
		DrupalSiteName:         viper.GetString("DRUPAL_SITE_NAME"),
		MaxLogSizeMB:           viper.GetInt("MAX_LOG_SIZE_MB"),
		LogLevel:               viper.GetString("LOG_LEVEL"),
		EnableDatabase:         viper.GetBool("ENABLE_DATABASE"),
		DatabasePath:           viper.GetString("DATABASE_PATH"),
		EnablePreprocessing:    viper.GetBool("ENABLE_PREPROCESSING"),
		MaxPreprocessingTokens: viper.GetInt("MAX_PREPROCESSING_TOKENS"),
		HTTPProxy:              viper.GetString("HTTP_PROXY"),
		HTTPSProxy:             viper.GetString("HTTPS_PROXY"),
		AITimeoutSeconds:       viper.GetInt("AI_TIMEOUT_SECONDS"),
		AIMaxTokens:            viper.GetInt("AI_MAX_TOKENS"),
	}

	// Apply CLI overrides (highest priority)
	if cli != nil {
		if cli.SourceType != "" {
			config.LogSourceType = cli.SourceType
		}
		if cli.SourcePath != "" {
			// Apply source path based on source type
			switch config.LogSourceType {
			case "drupal_watchdog":
				config.DrupalWatchdogPath = cli.SourcePath
			default:
				config.LogwatchOutputPath = cli.SourcePath
			}
		}
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
	viper.SetDefault("LOG_SOURCE_TYPE", "logwatch")
	viper.SetDefault("LOGWATCH_OUTPUT_PATH", "/tmp/logwatch-output.txt")
	viper.SetDefault("DRUPAL_WATCHDOG_PATH", "/tmp/drupal-watchdog.json")
	viper.SetDefault("DRUPAL_WATCHDOG_FORMAT", "json")
	viper.SetDefault("DRUPAL_SITE_NAME", "")
	viper.SetDefault("MAX_LOG_SIZE_MB", 10)
	viper.SetDefault("LOG_LEVEL", "info")
	viper.SetDefault("ENABLE_DATABASE", true)
	viper.SetDefault("DATABASE_PATH", "./data/summaries.db")
	viper.SetDefault("ENABLE_PREPROCESSING", true)
	viper.SetDefault("MAX_PREPROCESSING_TOKENS", 150000)
	viper.SetDefault("AI_TIMEOUT_SECONDS", 120)
	viper.SetDefault("AI_MAX_TOKENS", 8000)
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate Anthropic API Key
	if c.AnthropicAPIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is required")
	}
	// Use constant-time comparison to prevent timing attacks (M-04 fix)
	if !constantTimePrefixMatch(c.AnthropicAPIKey, "sk-ant-") {
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

	// Validate log source type and source-specific settings
	if err := c.validateLogSource(); err != nil {
		return err
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

	// Validate AI settings (L-02 fix)
	if c.AITimeoutSeconds < 30 || c.AITimeoutSeconds > 600 {
		return fmt.Errorf("AI_TIMEOUT_SECONDS must be between 30 and 600")
	}
	if c.AIMaxTokens < 1000 || c.AIMaxTokens > 16000 {
		return fmt.Errorf("AI_MAX_TOKENS must be between 1000 and 16000")
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

// constantTimePrefixMatch checks if s starts with prefix using constant-time comparison.
// This prevents timing attacks that could leak information about the string content.
// Returns false if s is shorter than prefix.
func constantTimePrefixMatch(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	// Compare only the prefix portion using constant-time comparison
	return subtle.ConstantTimeCompare([]byte(s[:len(prefix)]), []byte(prefix)) == 1
}

// validateLogSource validates log source configuration based on LogSourceType
func (c *Config) validateLogSource() error {
	// Validate log source type
	validSourceTypes := map[string]bool{
		"logwatch":        true,
		"drupal_watchdog": true,
	}

	if !validSourceTypes[c.LogSourceType] {
		return fmt.Errorf("LOG_SOURCE_TYPE must be 'logwatch' or 'drupal_watchdog' (got: %s)", c.LogSourceType)
	}

	// Validate source-specific settings
	switch c.LogSourceType {
	case "logwatch":
		if c.LogwatchOutputPath == "" {
			return fmt.Errorf("LOGWATCH_OUTPUT_PATH is required when LOG_SOURCE_TYPE=logwatch")
		}
	case "drupal_watchdog":
		if c.DrupalWatchdogPath == "" {
			return fmt.Errorf("DRUPAL_WATCHDOG_PATH is required when LOG_SOURCE_TYPE=drupal_watchdog")
		}
		validFormats := map[string]bool{
			"json":  true,
			"drush": true,
		}
		if !validFormats[c.DrupalWatchdogFormat] {
			return fmt.Errorf("DRUPAL_WATCHDOG_FORMAT must be 'json' or 'drush' (got: %s)", c.DrupalWatchdogFormat)
		}
	}

	return nil
}

// GetLogSourcePath returns the path to the log source file based on LogSourceType
func (c *Config) GetLogSourcePath() string {
	switch c.LogSourceType {
	case "drupal_watchdog":
		return c.DrupalWatchdogPath
	default:
		return c.LogwatchOutputPath
	}
}

// IsDrupalWatchdog returns true if the log source type is drupal_watchdog
func (c *Config) IsDrupalWatchdog() bool {
	return c.LogSourceType == "drupal_watchdog"
}

// IsLogwatch returns true if the log source type is logwatch
func (c *Config) IsLogwatch() bool {
	return c.LogSourceType == "logwatch"
}
