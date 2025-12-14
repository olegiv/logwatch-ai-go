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
	SourceType        string // -source-type: log source type (logwatch, drupal_watchdog)
	SourcePath        string // -source-path: path to log source file
	DrupalSite        string // -drupal-site: Drupal site ID from drupal-sites.json
	DrupalSitesConfig string // -drupal-sites-config: path to drupal-sites.json
	ListDrupalSites   bool   // -list-drupal-sites: list available sites and exit
	ShowHelp          bool   // -help: show usage
	ShowVersion       bool   // -version: show version
}

// ParseCLI parses command-line arguments and returns CLIOptions
func ParseCLI() *CLIOptions {
	opts := &CLIOptions{}

	flag.StringVar(&opts.SourceType, "source-type", "", "Log source type: logwatch, drupal_watchdog")
	flag.StringVar(&opts.SourcePath, "source-path", "", "Path to log source file (overrides config)")
	flag.StringVar(&opts.DrupalSite, "drupal-site", "", "Drupal site ID from drupal-sites.json (for multi-site deployments)")
	flag.StringVar(&opts.DrupalSitesConfig, "drupal-sites-config", "", "Path to drupal-sites.json configuration file")
	flag.BoolVar(&opts.ListDrupalSites, "list-drupal-sites", false, "List available Drupal sites from drupal-sites.json and exit")
	flag.BoolVar(&opts.ShowHelp, "help", false, "Show usage information")
	flag.BoolVar(&opts.ShowVersion, "version", false, "Show version information")

	// Custom usage message
	flag.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, "Logwatch AI Analyzer - Intelligent log analysis with Claude AI\n\n")
		_, _ = fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		_, _ = fmt.Fprintf(os.Stderr, "\nExamples:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  %s -source-type logwatch\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "  %s -source-type drupal_watchdog -source-path /tmp/watchdog.json\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "  %s -source-type drupal_watchdog -drupal-site production\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "  %s -list-drupal-sites\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "\nMulti-site Drupal:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  Create drupal-sites.json with site configurations.\n")
		_, _ = fmt.Fprintf(os.Stderr, "  Use -drupal-site to select which site to analyze.\n")
		_, _ = fmt.Fprintf(os.Stderr, "\nEnvironment variables can be set in .env file or exported directly.\n")
		_, _ = fmt.Fprintf(os.Stderr, "CLI arguments override environment variables.\n")
	}

	flag.Parse()

	return opts
}

// PrintUsage prints the command-line usage information
func PrintUsage() {
	flag.Usage()
}

// Config holds all application configuration
type Config struct {
	// LLM Provider Selection
	LLMProvider string // "anthropic" (default) or "ollama"

	// Anthropic/Claude Settings (used when LLMProvider = "anthropic")
	AnthropicAPIKey string
	ClaudeModel     string

	// Ollama Settings (used when LLMProvider = "ollama")
	OllamaBaseURL string // e.g., "http://localhost:11434"
	OllamaModel   string // e.g., "llama3.3:latest"

	// LM Studio Settings (used when LLMProvider = "lmstudio")
	LMStudioBaseURL string // e.g., "http://localhost:1234"
	LMStudioModel   string // e.g., "local-model" or specific model name

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

	// Multi-site Drupal configuration (loaded from drupal-sites.json)
	DrupalSiteID          string             // Selected site ID from drupal-sites.json
	DrupalSitesConfig     *DrupalSitesConfig // Loaded multi-site config (nil if single-site mode)
	DrupalSitesConfigPath string             // Path to drupal-sites.json (if used)

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
		// LLM Provider settings
		LLMProvider:     viper.GetString("LLM_PROVIDER"),
		AnthropicAPIKey: viper.GetString("ANTHROPIC_API_KEY"),
		ClaudeModel:     viper.GetString("CLAUDE_MODEL"),
		OllamaBaseURL:   viper.GetString("OLLAMA_BASE_URL"),
		OllamaModel:     viper.GetString("OLLAMA_MODEL"),
		LMStudioBaseURL: viper.GetString("LMSTUDIO_BASE_URL"),
		LMStudioModel:   viper.GetString("LMSTUDIO_MODEL"),

		// Telegram settings
		TelegramBotToken:       viper.GetString("TELEGRAM_BOT_TOKEN"),
		TelegramArchiveChannel: viper.GetInt64("TELEGRAM_CHANNEL_ARCHIVE_ID"),
		TelegramAlertsChannel:  viper.GetInt64("TELEGRAM_CHANNEL_ALERTS_ID"),

		// Log source settings
		LogSourceType:      viper.GetString("LOG_SOURCE_TYPE"),
		LogwatchOutputPath: viper.GetString("LOGWATCH_OUTPUT_PATH"),
		// Drupal settings are loaded from drupal-sites.json, not env vars
		DrupalWatchdogFormat: "json", // default, overridden by site config
		MaxLogSizeMB:         viper.GetInt("MAX_LOG_SIZE_MB"),

		// Application settings
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

	// Handle multi-site Drupal configuration
	if err := config.applyDrupalMultiSiteConfig(cli); err != nil {
		return nil, err
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// applyDrupalMultiSiteConfig loads and applies Drupal site configuration from drupal-sites.json
func (c *Config) applyDrupalMultiSiteConfig(cli *CLIOptions) error {
	// Only process for drupal_watchdog source type
	if c.LogSourceType != "drupal_watchdog" {
		return nil
	}

	// Determine config path from CLI or auto-detect
	var configPath string
	if cli != nil && cli.DrupalSitesConfig != "" {
		configPath = cli.DrupalSitesConfig
	}

	// Try to load drupal-sites.json (required for drupal_watchdog)
	sitesConfig, foundPath, err := LoadDrupalSitesConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load drupal sites config: %w", err)
	}

	// drupal-sites.json is required for drupal_watchdog source type
	if sitesConfig == nil {
		return fmt.Errorf("drupal-sites.json is required when LOG_SOURCE_TYPE=drupal_watchdog. " +
			"Create drupal-sites.json in one of: ./drupal-sites.json, ./configs/drupal-sites.json, " +
			"/opt/logwatch-ai/drupal-sites.json, or ~/.config/logwatch-ai/drupal-sites.json. " +
			"See configs/drupal-sites.json.example for format")
	}

	// Store the loaded config
	c.DrupalSitesConfig = sitesConfig
	c.DrupalSitesConfigPath = foundPath

	// Determine which site to use
	var siteID string
	if cli != nil && cli.DrupalSite != "" {
		siteID = cli.DrupalSite
	} else if sitesConfig.DefaultSite != "" {
		siteID = sitesConfig.DefaultSite
	}

	// A site must be selected (either via CLI or default_site in config)
	if siteID == "" {
		return fmt.Errorf("no Drupal site specified. Use -drupal-site <site_id> or set default_site in drupal-sites.json. " +
			"Available sites: use -list-drupal-sites to see options")
	}

	// Get the site configuration
	site, err := sitesConfig.GetSite(siteID)
	if err != nil {
		return fmt.Errorf("failed to get drupal site '%s': %w", siteID, err)
	}

	// Store the selected site ID
	c.DrupalSiteID = siteID

	// Apply site-specific configuration (CLI -source-path takes precedence)
	if cli == nil || cli.SourcePath == "" {
		c.DrupalWatchdogPath = site.WatchdogPath
	}

	// Apply format from site config (default to json if not specified)
	if site.WatchdogFormat != "" {
		c.DrupalWatchdogFormat = site.WatchdogFormat
	}

	// Apply site name for display
	if site.Name != "" {
		c.DrupalSiteName = site.Name
	} else {
		c.DrupalSiteName = siteID
	}

	return nil
}

// setDefaults sets default configuration values
func setDefaults() {
	// LLM Provider defaults
	viper.SetDefault("LLM_PROVIDER", "anthropic")
	viper.SetDefault("CLAUDE_MODEL", "claude-sonnet-4-5-20250929")
	viper.SetDefault("OLLAMA_BASE_URL", "http://localhost:11434")
	viper.SetDefault("OLLAMA_MODEL", "llama3.3:latest")
	viper.SetDefault("LMSTUDIO_BASE_URL", "http://localhost:1234")
	viper.SetDefault("LMSTUDIO_MODEL", "local-model")

	// Log source defaults
	viper.SetDefault("LOG_SOURCE_TYPE", "logwatch")
	viper.SetDefault("LOGWATCH_OUTPUT_PATH", "/tmp/logwatch-output.txt")
	// Drupal settings come from drupal-sites.json, not env vars
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
	// Validate LLM Provider
	if err := c.validateLLMProvider(); err != nil {
		return err
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

// validateLLMProvider validates LLM provider configuration
func (c *Config) validateLLMProvider() error {
	validProviders := map[string]bool{
		"anthropic": true,
		"ollama":    true,
		"lmstudio":  true,
	}

	if !validProviders[c.LLMProvider] {
		return fmt.Errorf("LLM_PROVIDER must be 'anthropic', 'ollama', or 'lmstudio' (got: %s)", c.LLMProvider)
	}

	switch c.LLMProvider {
	case "anthropic":
		// Validate Anthropic API Key
		if c.AnthropicAPIKey == "" {
			return fmt.Errorf("ANTHROPIC_API_KEY is required when LLM_PROVIDER=anthropic")
		}
		// Use constant-time comparison to prevent timing attacks (M-04 fix)
		if !constantTimePrefixMatch(c.AnthropicAPIKey, "sk-ant-") {
			return fmt.Errorf("ANTHROPIC_API_KEY must start with 'sk-ant-'")
		}
		if c.ClaudeModel == "" {
			return fmt.Errorf("CLAUDE_MODEL is required when LLM_PROVIDER=anthropic")
		}

	case "ollama":
		// Validate Ollama settings
		if c.OllamaModel == "" {
			return fmt.Errorf("OLLAMA_MODEL is required when LLM_PROVIDER=ollama")
		}
		if c.OllamaBaseURL == "" {
			return fmt.Errorf("OLLAMA_BASE_URL is required when LLM_PROVIDER=ollama")
		}
		// Validate URL format (basic check)
		if !strings.HasPrefix(c.OllamaBaseURL, "http://") && !strings.HasPrefix(c.OllamaBaseURL, "https://") {
			return fmt.Errorf("OLLAMA_BASE_URL must start with 'http://' or 'https://'")
		}

	case "lmstudio":
		// Validate LM Studio settings
		if c.LMStudioBaseURL == "" {
			return fmt.Errorf("LMSTUDIO_BASE_URL is required when LLM_PROVIDER=lmstudio")
		}
		// Validate URL format (basic check)
		if !strings.HasPrefix(c.LMStudioBaseURL, "http://") && !strings.HasPrefix(c.LMStudioBaseURL, "https://") {
			return fmt.Errorf("LMSTUDIO_BASE_URL must start with 'http://' or 'https://'")
		}
		// Model is optional for LM Studio (defaults to "local-model")
	}

	return nil
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
			return fmt.Errorf("watchdog_path is required in drupal-sites.json site configuration")
		}
		validFormats := map[string]bool{
			"json":  true,
			"drush": true,
		}
		if !validFormats[c.DrupalWatchdogFormat] {
			return fmt.Errorf("watchdog_format must be 'json' or 'drush' in drupal-sites.json (got: %s)", c.DrupalWatchdogFormat)
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

// IsOllama returns true if the LLM provider is Ollama
func (c *Config) IsOllama() bool {
	return c.LLMProvider == "ollama"
}

// IsAnthropic returns true if the LLM provider is Anthropic
func (c *Config) IsAnthropic() bool {
	return c.LLMProvider == "anthropic"
}

// IsLMStudio returns true if the LLM provider is LM Studio
func (c *Config) IsLMStudio() bool {
	return c.LLMProvider == "lmstudio"
}

// GetLLMModel returns the model name for the current LLM provider
func (c *Config) GetLLMModel() string {
	switch c.LLMProvider {
	case "ollama":
		return c.OllamaModel
	case "lmstudio":
		return c.LMStudioModel
	default:
		return c.ClaudeModel
	}
}
