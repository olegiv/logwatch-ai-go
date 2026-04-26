// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

// Package config loads runtime configuration from environment variables
// and optional side-files (drupal-sites.json, ocms-sites.json, exclusions.json).
package config

import (
	"crypto/subtle"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"

	"github.com/olegiv/logwatch-ai-go/internal/exclusions"
)

// CLIOptions holds command-line argument overrides
type CLIOptions struct {
	SourceType        string // -source-type: log source type (logwatch, drupal_watchdog, ocms)
	SourcePath        string // -source-path: path to log source file
	DrupalSite        string // -drupal-site: Drupal site ID from drupal-sites.json
	DrupalSitesConfig string // -drupal-sites-config: path to drupal-sites.json
	ListDrupalSites   bool   // -list-drupal-sites: list available sites and exit
	OCMSSite          string // -ocms-site: OCMS site ID from ocms-sites.json
	OCMSSitesConfig   string // -ocms-sites-config: path to ocms-sites.json
	OCMSSitesRegistry string // -ocms-sites-registry: path to OCMS sites.conf
	OCMSLogKind       string // -ocms-log-kind: main, error, or all
	ListOCMSSites     bool   // -list-ocms-sites: list available OCMS sites and exit
	ExclusionsConfig  string // -exclusions-config: path to exclusions.json
	ShowHelp          bool   // -help: show usage
	ShowVersion       bool   // -version: show version
}

// ParseCLI parses command-line arguments and returns CLIOptions
func ParseCLI() *CLIOptions {
	opts := &CLIOptions{}

	flag.StringVar(&opts.SourceType, "source-type", "", "Log source type: logwatch, drupal_watchdog, ocms")
	flag.StringVar(&opts.SourcePath, "source-path", "", "Path to log source file (overrides config)")
	flag.StringVar(&opts.DrupalSite, "drupal-site", "", "Drupal site ID from drupal-sites.json (for multi-site deployments)")
	flag.StringVar(&opts.DrupalSitesConfig, "drupal-sites-config", "", "Path to drupal-sites.json configuration file")
	flag.BoolVar(&opts.ListDrupalSites, "list-drupal-sites", false, "List available Drupal sites from drupal-sites.json and exit")
	flag.StringVar(&opts.OCMSSite, "ocms-site", "", "OCMS site ID from ocms-sites.json (for multi-site deployments)")
	flag.StringVar(&opts.OCMSSitesConfig, "ocms-sites-config", "", "Path to ocms-sites.json configuration file")
	flag.StringVar(&opts.OCMSSitesRegistry, "ocms-sites-registry", "", "Path to OCMS sites.conf registry (default: /etc/ocms/sites.conf)")
	flag.StringVar(&opts.OCMSLogKind, "ocms-log-kind", "", "OCMS log kind for site registry mode: main, error, or all (default: main)")
	flag.BoolVar(&opts.ListOCMSSites, "list-ocms-sites", false, "List available OCMS sites from ocms-sites.json and exit")
	flag.StringVar(&opts.ExclusionsConfig, "exclusions-config", "", "Path to exclusions.json configuration file")
	flag.BoolVar(&opts.ShowHelp, "help", false, "Show usage information")
	flag.BoolVar(&opts.ShowHelp, "h", false, "Show usage information (shorthand)")
	flag.BoolVar(&opts.ShowVersion, "version", false, "Show version information")
	flag.BoolVar(&opts.ShowVersion, "v", false, "Show version information (shorthand)")

	// Custom usage message
	flag.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, "Logwatch AI Analyzer - Intelligent log analysis with Claude AI\n\n")
		_, _ = fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		_, _ = fmt.Fprintf(os.Stderr, "\nExamples:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  %s -source-type logwatch\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "  %s -source-type ocms -source-path /tmp/ocms.log\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "  %s -source-type ocms -ocms-site example_com\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "  %s -source-type drupal_watchdog -source-path /tmp/watchdog.json\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "  %s -source-type drupal_watchdog -drupal-site production\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "  %s -list-drupal-sites\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "  %s -list-ocms-sites\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "\nMulti-site Drupal:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  Create drupal-sites.json with site configurations.\n")
		_, _ = fmt.Fprintf(os.Stderr, "  Use -drupal-site to select which site to analyze.\n")
		_, _ = fmt.Fprintf(os.Stderr, "\nMulti-site OCMS:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  Create ocms-sites.json with site IDs matching /etc/ocms/sites.conf.\n")
		_, _ = fmt.Fprintf(os.Stderr, "  Use -ocms-site to select which site to analyze.\n")
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
	LogSourceType string // "logwatch", "drupal_watchdog", or "ocms"

	// Logwatch Settings (used when LogSourceType = "logwatch")
	LogwatchOutputPath string

	// OCMS Settings (used when LogSourceType = "ocms")
	OCMSLogsPath string
	OCMSLogKind  string
	OCMSLogPaths []OCMSLogPath

	// Drupal Watchdog Settings (used when LogSourceType = "drupal_watchdog")
	DrupalWatchdogPath   string // Path to watchdog export file
	DrupalWatchdogFormat string // "json" or "drush"
	DrupalSiteName       string // Optional: site identifier for multi-site

	// Selected site metadata shared by multi-site log sources
	SiteID   string
	SiteName string

	// Multi-site Drupal configuration (loaded from drupal-sites.json)
	DrupalSiteID          string             // Selected site ID from drupal-sites.json
	DrupalSitesConfig     *DrupalSitesConfig // Loaded multi-site config (nil if single-site mode)
	DrupalSitesConfigPath string             // Path to drupal-sites.json (if used)

	// Multi-site OCMS configuration (loaded from ocms-sites.json and /etc/ocms/sites.conf)
	OCMSSiteID            string             // Selected site ID from ocms-sites.json
	OCMSSiteName          string             // Display name for OCMS reports
	OCMSSitesConfig       *OCMSSitesConfig   // Loaded multi-site config (nil if single-site mode)
	OCMSSitesConfigPath   string             // Path to ocms-sites.json (if used)
	OCMSSitesRegistry     *OCMSSitesRegistry // Loaded OCMS registry (nil in single-site mode)
	OCMSSitesRegistryPath string             // Path to sites.conf (if used)

	// Finding exclusions (loaded from exclusions.json, nil if feature not used)
	Exclusions           *exclusions.Config
	ExclusionsConfigPath string

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
		OCMSLogsPath:       viper.GetString("OCMS_LOGS_PATH"),
		OCMSLogKind:        OCMSLogKindMain,
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
			case "ocms":
				config.OCMSLogsPath = cli.SourcePath
			default:
				config.LogwatchOutputPath = cli.SourcePath
			}
		}
		if cli.OCMSLogKind != "" {
			config.OCMSLogKind = cli.OCMSLogKind
		}
	}

	// Handle multi-site Drupal configuration
	if err := config.applyDrupalMultiSiteConfig(cli); err != nil {
		return nil, err
	}

	// Handle multi-site OCMS configuration
	if err := config.applyOCMSMultiSiteConfig(cli); err != nil {
		return nil, err
	}

	// Load optional finding exclusions
	if err := config.applyExclusionsConfig(cli); err != nil {
		return nil, err
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// applyExclusionsConfig loads exclusions.json (if present) and attaches
// the parsed Config. A missing file without an explicit CLI path is not
// an error: the feature is opt-in. An explicit -exclusions-config path
// that cannot be read is a hard error so typos fail fast.
func (c *Config) applyExclusionsConfig(cli *CLIOptions) error {
	var explicitPath string
	if cli != nil {
		explicitPath = cli.ExclusionsConfig
	}

	cfg, foundPath, err := exclusions.Load(explicitPath)
	if err != nil {
		return fmt.Errorf("failed to load exclusions config: %w", err)
	}
	if cfg == nil {
		return nil
	}

	c.Exclusions = cfg
	c.ExclusionsConfigPath = foundPath
	return nil
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
	c.SiteID = siteID

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
	c.SiteName = c.DrupalSiteName

	return nil
}

// applyOCMSMultiSiteConfig loads and applies OCMS site configuration from ocms-sites.json.
func (c *Config) applyOCMSMultiSiteConfig(cli *CLIOptions) error {
	if c.LogSourceType != "ocms" {
		return nil
	}

	configPath, cliSiteID, registryPath, cliLogKind, cliSourcePath := readOCMSCLI(cli)
	if cliSourcePath != "" {
		return c.applyOCMSSourcePathOverride(cliSourcePath, cliLogKind)
	}

	sitesConfig, configFoundPath, err := LoadOCMSSitesConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load OCMS sites config: %w", err)
	}

	if sitesConfig == nil {
		return c.applyOCMSSingleSiteFallback(cliSiteID, configPath)
	}

	c.OCMSSitesConfig = sitesConfig
	c.OCMSSitesConfigPath = configFoundPath

	siteID, err := resolveOCMSSiteID(cliSiteID, sitesConfig.DefaultSite)
	if err != nil {
		return err
	}

	siteConfig, err := sitesConfig.GetSite(siteID)
	if err != nil {
		return fmt.Errorf("failed to get OCMS site '%s': %w", siteID, err)
	}

	logKind, err := resolveOCMSLogKind(sitesConfig, siteConfig, cliLogKind)
	if err != nil {
		return err
	}
	c.OCMSLogKind = logKind

	registrySite, registry, foundPath, err := loadOCMSRegistrySite(registryPath, sitesConfig.RegistryPath, siteID)
	if err != nil {
		return err
	}

	c.OCMSSiteID = siteID
	c.OCMSSiteName = siteID
	if siteConfig.Name != "" {
		c.OCMSSiteName = siteConfig.Name
	}
	c.SiteID = siteID
	c.SiteName = c.OCMSSiteName
	c.OCMSSitesRegistry = registry
	c.OCMSSitesRegistryPath = foundPath

	if cliSourcePath == "" {
		logPaths, err := registrySite.LogPaths(logKind)
		if err != nil {
			return err
		}
		c.OCMSLogPaths = logPaths
		if len(logPaths) > 0 {
			c.OCMSLogsPath = logPaths[0].Path
		}
	}

	return nil
}

// readOCMSCLI extracts OCMS-related CLI fields with nil-safety.
func readOCMSCLI(cli *CLIOptions) (configPath, siteID, registryPath, logKind, sourcePath string) {
	if cli == nil {
		return
	}
	return cli.OCMSSitesConfig, cli.OCMSSite, cli.OCMSSitesRegistry, cli.OCMSLogKind, cli.SourcePath
}

// applyOCMSSourcePathOverride keeps -source-path as the final explicit path
// override and avoids loading registry-backed OCMS multisite configuration.
func (c *Config) applyOCMSSourcePathOverride(sourcePath, cliLogKind string) error {
	c.OCMSLogsPath = sourcePath
	if cliLogKind != "" {
		c.OCMSLogKind = cliLogKind
	}
	logKind, err := NormalizeOCMSLogKind(c.OCMSLogKind)
	if err != nil {
		return err
	}
	c.OCMSLogKind = logKind
	c.OCMSLogPaths = nil
	return nil
}

// applyOCMSSingleSiteFallback handles the case where no ocms-sites.json was found.
func (c *Config) applyOCMSSingleSiteFallback(cliSiteID, configPath string) error {
	if cliSiteID != "" || configPath != "" {
		return fmt.Errorf("ocms-sites.json is required when -ocms-site or -ocms-sites-config is used. " +
			"Create ocms-sites.json in one of: ./ocms-sites.json, ./configs/ocms-sites.json, " +
			"/opt/logwatch-ai/ocms-sites.json, or ~/.config/logwatch-ai/ocms-sites.json. " +
			"See configs/ocms-sites.json.example for format")
	}
	logKind, err := NormalizeOCMSLogKind(c.OCMSLogKind)
	if err != nil {
		return err
	}
	c.OCMSLogKind = logKind
	return nil
}

// resolveOCMSSiteID returns the selected site ID, falling back to the JSON default.
func resolveOCMSSiteID(cliSiteID, defaultSite string) (string, error) {
	if cliSiteID != "" {
		return cliSiteID, nil
	}
	if defaultSite != "" {
		return defaultSite, nil
	}
	return "", fmt.Errorf("no OCMS site specified. Use -ocms-site <site_id> or set default_site in ocms-sites.json. " +
		"Available sites: use -list-ocms-sites to see options")
}

// resolveOCMSLogKind applies CLI override on top of the site/config-derived log kind.
func resolveOCMSLogKind(sitesConfig *OCMSSitesConfig, siteConfig *OCMSSiteConfig, cliLogKind string) (string, error) {
	logKind, err := sitesConfig.EffectiveLogKind(siteConfig)
	if err != nil {
		return "", err
	}
	if cliLogKind != "" {
		return NormalizeOCMSLogKind(cliLogKind)
	}
	return logKind, nil
}

// loadOCMSRegistrySite loads the registry and returns the matching site entry.
func loadOCMSRegistrySite(cliRegistryPath, jsonRegistryPath, siteID string) (*OCMSSite, *OCMSSitesRegistry, string, error) {
	registryPath := cliRegistryPath
	if registryPath == "" {
		registryPath = jsonRegistryPath
	}

	registry, foundPath, err := LoadOCMSSitesRegistry(registryPath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to load OCMS sites registry: %w", err)
	}
	if registry == nil {
		return nil, nil, "", fmt.Errorf("OCMS sites registry is required when ocms-sites.json is used. Expected %s or use -ocms-sites-registry <path>",
			DefaultOCMSSitesRegistryPath)
	}

	registrySite, err := registry.GetSite(siteID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to get OCMS site '%s': %w", siteID, err)
	}
	return registrySite, registry, foundPath, nil
}

// setDefaults sets default configuration values
func setDefaults() {
	// LLM Provider defaults
	viper.SetDefault("LLM_PROVIDER", "anthropic")
	viper.SetDefault("CLAUDE_MODEL", "claude-haiku-4-5-20251001")
	viper.SetDefault("OLLAMA_BASE_URL", "http://localhost:11434")
	viper.SetDefault("OLLAMA_MODEL", "llama3.3:latest")
	viper.SetDefault("LMSTUDIO_BASE_URL", "http://localhost:1234")
	viper.SetDefault("LMSTUDIO_MODEL", "local-model")

	// Log source defaults
	viper.SetDefault("LOG_SOURCE_TYPE", "logwatch")
	viper.SetDefault("LOGWATCH_OUTPUT_PATH", "/tmp/logwatch-output.txt")
	viper.SetDefault("OCMS_LOGS_PATH", "/tmp/ocms.log")
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
		// Enforce a conservative model-ID shape so a mis-set credential
		// (e.g. operator pastes an API key into CLAUDE_MODEL) cannot reach
		// log output or the API. Dated snapshots like
		// "claude-haiku-4-5-20251001" and aliases like "claude-sonnet-4-6"
		// both fit this pattern.
		claudeModelRegex := regexp.MustCompile(`^claude-[a-z0-9-]+$`)
		if !claudeModelRegex.MatchString(c.ClaudeModel) {
			return fmt.Errorf("CLAUDE_MODEL has invalid format (expected model ID like 'claude-haiku-4-5-20251001')")
		}

	case "ollama":
		// Validate Ollama settings
		if c.OllamaModel == "" {
			return fmt.Errorf("OLLAMA_MODEL is required when LLM_PROVIDER=ollama")
		}
		if c.OllamaBaseURL == "" {
			return fmt.Errorf("OLLAMA_BASE_URL is required when LLM_PROVIDER=ollama")
		}
		if err := validateLLMBaseURL("OLLAMA_BASE_URL", c.OllamaBaseURL); err != nil {
			return err
		}

	case "lmstudio":
		// Validate LM Studio settings
		if c.LMStudioBaseURL == "" {
			return fmt.Errorf("LMSTUDIO_BASE_URL is required when LLM_PROVIDER=lmstudio")
		}
		if err := validateLLMBaseURL("LMSTUDIO_BASE_URL", c.LMStudioBaseURL); err != nil {
			return err
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
		"ocms":            true,
	}

	if !validSourceTypes[c.LogSourceType] {
		return fmt.Errorf("LOG_SOURCE_TYPE must be 'logwatch', 'drupal_watchdog', or 'ocms' (got: %s)", c.LogSourceType)
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
	case "ocms":
		if _, err := NormalizeOCMSLogKind(c.OCMSLogKind); err != nil {
			return err
		}
		if c.OCMSLogsPath == "" {
			return fmt.Errorf("OCMS_LOGS_PATH is required when LOG_SOURCE_TYPE=ocms")
		}
	}

	return nil
}

// GetLogSourcePath returns the path to the log source file based on LogSourceType
func (c *Config) GetLogSourcePath() string {
	switch c.LogSourceType {
	case "drupal_watchdog":
		return c.DrupalWatchdogPath
	case "ocms":
		return c.OCMSLogsPath
	default:
		return c.LogwatchOutputPath
	}
}

// GetOCMSLogPaths returns resolved OCMS log paths for the active run.
func (c *Config) GetOCMSLogPaths() []OCMSLogPath {
	if len(c.OCMSLogPaths) > 0 {
		out := make([]OCMSLogPath, len(c.OCMSLogPaths))
		copy(out, c.OCMSLogPaths)
		return out
	}
	if c.OCMSLogsPath == "" {
		return nil
	}
	return []OCMSLogPath{{Kind: c.OCMSLogKind, Path: c.OCMSLogsPath}}
}

// SelectedSiteID returns the selected multi-site ID for the active source.
func (c *Config) SelectedSiteID() string {
	if c.SiteID != "" {
		return c.SiteID
	}
	if c.DrupalSiteID != "" {
		return c.DrupalSiteID
	}
	return c.OCMSSiteID
}

// SelectedSiteName returns the selected multi-site display name for the active source.
func (c *Config) SelectedSiteName() string {
	if c.SiteName != "" {
		return c.SiteName
	}
	if c.DrupalSiteName != "" {
		return c.DrupalSiteName
	}
	return c.OCMSSiteName
}

// IsDrupalWatchdog returns true if the log source type is drupal_watchdog
func (c *Config) IsDrupalWatchdog() bool {
	return c.LogSourceType == "drupal_watchdog"
}

// IsLogwatch returns true if the log source type is logwatch
func (c *Config) IsLogwatch() bool {
	return c.LogSourceType == "logwatch"
}

// IsOCMS returns true if the log source type is ocms
func (c *Config) IsOCMS() bool {
	return c.LogSourceType == "ocms"
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

// validateLLMBaseURL parses the configured LLM endpoint and rejects obviously
// unsafe shapes: non-http(s) schemes, and IP literals in loopback, link-local,
// or RFC-1918 private ranges (prevents SSRF to cloud metadata endpoints such
// as 169.254.169.254 or to internal services on the deployment network when
// the operator intended a real remote inference host).
//
// The localhost case (Ollama/LM Studio on the same machine) is common and
// legitimate; operators can opt in via ALLOW_LOCAL_LLM=true. Hostnames are
// not resolved at config time - DNS rebinding is not addressed here, and the
// common case is that operators configure either an explicit IP literal or a
// hostname that resolves stably to a trusted host at request time.
//
// A cleartext-http warning is emitted for non-loopback http:// URLs so log
// content (which may contain PII) is not silently transmitted unencrypted.
func validateLLMBaseURL(envName, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s has invalid URL: %w", envName, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s must use http:// or https:// scheme (got: %q)", envName, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%s must include a host", envName)
	}

	host := u.Hostname()
	isLocalName := host == "localhost" || strings.EqualFold(host, "localhost.localdomain")
	allowLocal := strings.EqualFold(os.Getenv("ALLOW_LOCAL_LLM"), "true")

	// Strip an IPv6 zone identifier ("fe80::1%eth0") before parsing. Without
	// this, net.ParseIP returns nil for any scoped IPv6 literal and the
	// loopback/link-local guard below is silently bypassed - a link-local
	// endpoint could then be accepted even without ALLOW_LOCAL_LLM.
	hostForIP := host
	if i := strings.IndexByte(hostForIP, '%'); i != -1 {
		hostForIP = hostForIP[:i]
	}
	ip := net.ParseIP(hostForIP)

	if ip != nil {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() || ip.IsUnspecified() {
			if !allowLocal && !ip.IsLoopback() {
				return fmt.Errorf(
					"%s resolves to a private/link-local address (%s); set ALLOW_LOCAL_LLM=true to permit",
					envName, ip.String())
			}
		}
	}

	if u.Scheme == "http" {
		switch {
		case ip != nil && !ip.IsLoopback():
			fmt.Fprintf(os.Stderr, "config: %s uses cleartext http:// to %s - log content will be transmitted unencrypted\n", envName, host)
		case ip == nil && !isLocalName:
			fmt.Fprintf(os.Stderr, "config: %s uses cleartext http:// to a remote host - log content will be transmitted unencrypted\n", envName)
		}
	}

	return nil
}
