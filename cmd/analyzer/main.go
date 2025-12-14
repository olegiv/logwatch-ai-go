package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/olegiv/go-logger"
	"github.com/olegiv/logwatch-ai-go/internal/ai"
	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
	"github.com/olegiv/logwatch-ai-go/internal/config"
	"github.com/olegiv/logwatch-ai-go/internal/drupal"
	"github.com/olegiv/logwatch-ai-go/internal/logging"
	"github.com/olegiv/logwatch-ai-go/internal/logwatch"
	"github.com/olegiv/logwatch-ai-go/internal/notification"
	"github.com/olegiv/logwatch-ai-go/internal/storage"
)

const (
	exitSuccess = 0
	exitFailure = 1
)

// Version information - injected at build time via ldflags
var (
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Parse CLI arguments first
	cli := config.ParseCLI()

	// Handle -help flag
	if cli.ShowHelp {
		config.PrintUsage()
		return exitSuccess
	}

	// Handle -version flag
	if cli.ShowVersion {
		fmt.Printf("logwatch-analyzer %s (commit: %s, built: %s)\n", version, gitCommit, buildTime)
		return exitSuccess
	}

	// Handle -list-drupal-sites flag
	if cli.ListDrupalSites {
		return handleListDrupalSites(cli)
	}

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	// Load configuration with CLI overrides
	cfg, err := config.LoadWithCLI(cli)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		return exitFailure
	}

	// Initialize logger with credential sanitization (M-02 fix)
	baseLog := logger.New(logger.Config{
		Level:      cfg.LogLevel,
		LogDir:     "./logs",
		Filename:   "analyzer.log",
		MaxSizeMB:  10,
		MaxBackups: 5,
		Console:    true,
	})
	log := logging.NewSecure(baseLog)
	defer func() {
		if err := log.Close(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", err)
		}
	}()

	// Log startup info with optional site details
	logEvent := log.Info().Str("source_type", cfg.LogSourceType)
	if cfg.DrupalSiteID != "" {
		logEvent = logEvent.Str("drupal_site", cfg.DrupalSiteID)
	}
	if cfg.DrupalSiteName != "" && cfg.DrupalSiteName != cfg.DrupalSiteID {
		logEvent = logEvent.Str("site_name", cfg.DrupalSiteName)
	}
	logEvent.Msg("Starting Log AI Analyzer")
	log.Info().
		Str("provider", cfg.LLMProvider).
		Str("model", cfg.GetLLMModel()).
		Msg("Configured LLM")

	// Run the analyzer
	if err := runAnalyzer(ctx, cfg, log); err != nil {
		log.Error().Err(err).Msg("Analysis failed")
		return exitFailure
	}

	log.Info().Msg("Analysis completed successfully")
	return exitSuccess
}

func runAnalyzer(ctx context.Context, cfg *config.Config, log *logging.SecureLogger) error {
	startTime := time.Now()

	// Initialize components
	log.Info().Msg("Initializing components...")

	// 1. Initialize storage (if enabled)
	var store *storage.Storage
	var err error

	if cfg.EnableDatabase {
		store, err = storage.New(cfg.DatabasePath)
		if err != nil {
			return fmt.Errorf("failed to initialize storage: %w", err)
		}
		defer func(store *storage.Storage) {
			err = store.Close()
			if err != nil {
				log.Warn().Err(err).Msg("Failed to close database")
			}
		}(store)
		log.Info().Str("path", cfg.DatabasePath).Msg("Database initialized")
	}

	// 2. Initialize Telegram client
	telegramClient, err := notification.NewTelegramClient(
		cfg.TelegramBotToken,
		cfg.TelegramArchiveChannel,
		cfg.TelegramAlertsChannel,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize Telegram client: %w", err)
	}
	defer func(telegramClient *notification.TelegramClient) {
		err = telegramClient.Close()
		if err != nil {
			log.Warn().Err(err).Msg("Failed to close Telegram client")
		}
	}(telegramClient)

	botInfo := telegramClient.GetBotInfo()
	log.Info().
		Str("username", botInfo["username"].(string)).
		Msg("Telegram bot initialized")

	// 3. Initialize LLM client based on provider
	llmClient, err := createLLMClient(ctx, cfg, log)
	if err != nil {
		return fmt.Errorf("failed to initialize LLM client: %w", err)
	}

	modelInfo := llmClient.GetModelInfo()
	log.Info().
		Str("provider", llmClient.GetProviderName()).
		Str("model", modelInfo["model"].(string)).
		Int("max_tokens", modelInfo["max_tokens"].(int)).
		Msg("LLM client initialized")

	// 4. Initialize log source based on configuration
	logSource, err := createLogSource(cfg)
	if err != nil {
		return fmt.Errorf("failed to create log source: %w", err)
	}

	// Get source path
	sourcePath := cfg.GetLogSourcePath()

	// Read log content
	log.Info().
		Str("path", sourcePath).
		Str("type", cfg.LogSourceType).
		Msg("Reading log content...")

	logContent, err := logSource.Reader.Read(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to read log content: %w", err)
	}

	sourceInfo, err := logSource.Reader.GetSourceInfo(sourcePath)
	if err != nil {
		log.Warn().Err(err).Msg("Could not get source file info")
		log.Info().Msg("Log file read successfully")
	} else {
		log.Info().
			Float64("size_mb", sourceInfo["size_mb"].(float64)).
			Float64("age_hours", sourceInfo["age_hours"].(float64)).
			Msg("Log file read successfully")
	}

	// Check for no entries (Drupal watchdog specific)
	// When there are no log entries for the time period, skip AI analysis
	// and send an informational notification instead
	if cfg.LogSourceType == "drupal_watchdog" && drupal.IsNoEntriesContent(logContent) {
		log.Info().Msg("No watchdog entries found for the time period - skipping AI analysis")

		// Send informational Telegram notification
		if err := telegramClient.SendNoEntriesReport(cfg.LogSourceType, cfg.DrupalSiteName); err != nil {
			return fmt.Errorf("failed to send no-entries notification: %w", err)
		}

		log.Info().Msg("No-entries notification sent to Telegram")
		return nil
	}

	// Get historical context (if database enabled)
	// Filter by source type and site to get relevant historical data only
	var historicalContext string
	sourceFilter := &storage.SourceFilter{
		LogSourceType: cfg.LogSourceType,
		SiteName:      cfg.DrupalSiteName, // Empty for logwatch
	}
	if store != nil {
		log.Info().Msg("Retrieving historical context...")
		historicalContext, err = store.GetHistoricalContext(7, sourceFilter) // Last 7 days
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get historical context, continuing without it")
		} else if historicalContext != "" {
			log.Info().Msg("Historical context retrieved")
		}
	}

	// Build prompts using the log source's prompt builder
	systemPrompt := logSource.PromptBuilder.GetSystemPrompt()
	userPrompt := logSource.PromptBuilder.GetUserPrompt(logContent, historicalContext)

	// Analyze with LLM
	log.Info().
		Str("log_type", logSource.PromptBuilder.GetLogType()).
		Str("provider", llmClient.GetProviderName()).
		Msg("Analyzing logs...")
	analysis, stats, err := llmClient.Analyze(ctx, systemPrompt, userPrompt)
	if err != nil {
		return fmt.Errorf("LLM analysis failed: %w", err)
	}

	log.Info().
		Str("status", analysis.SystemStatus).
		Int("critical_issues", len(analysis.CriticalIssues)).
		Int("warnings", len(analysis.Warnings)).
		Int("recommendations", len(analysis.Recommendations)).
		Float64("cost_usd", stats.CostUSD).
		Float64("duration_s", stats.DurationSeconds).
		Msg("Analysis completed")

	// Log token usage
	log.Debug().
		Int("input_tokens", stats.InputTokens).
		Int("output_tokens", stats.OutputTokens).
		Int("cache_creation_tokens", stats.CacheCreationTokens).
		Int("cache_read_tokens", stats.CacheReadTokens).
		Msg("Token usage details")

	// Save to database (if enabled)
	if store != nil {
		log.Info().Msg("Saving analysis to database...")
		summary := &storage.Summary{
			Timestamp:       time.Now(),
			LogSourceType:   cfg.LogSourceType,
			SiteName:        cfg.DrupalSiteName, // Empty for logwatch
			SystemStatus:    analysis.SystemStatus,
			Summary:         analysis.Summary,
			CriticalIssues:  analysis.CriticalIssues,
			Warnings:        analysis.Warnings,
			Recommendations: analysis.Recommendations,
			Metrics:         analysis.Metrics,
			InputTokens:     stats.InputTokens,
			OutputTokens:    stats.OutputTokens,
			CostUSD:         stats.CostUSD,
		}

		if err := store.SaveSummary(summary); err != nil {
			log.Warn().Err(err).Msg("Failed to save summary to database")
		} else {
			log.Info().Int64("id", summary.ID).Msg("Summary saved to database")
		}

		// Cleanup old summaries (>90 days)
		log.Info().Msg("Cleaning up old summaries...")
		deleted, err := store.CleanupOldSummaries(90)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to cleanup old summaries")
		} else if deleted > 0 {
			log.Info().Int64("deleted", deleted).Msg("Old summaries cleaned up")
		}
	}

	// Send Telegram notifications
	log.Info().Msg("Sending Telegram notifications...")
	if err := telegramClient.SendAnalysisReport(analysis, stats, cfg.LogSourceType, cfg.DrupalSiteName); err != nil {
		return fmt.Errorf("failed to send Telegram notification: %w", err)
	}

	if cfg.HasAlertsChannel() && ai.ShouldTriggerAlert(analysis.SystemStatus) {
		log.Info().Msg("Alert notification sent (status warrants attention)")
	}

	// Final summary
	totalDuration := time.Since(startTime)
	log.Info().
		Float64("total_duration_s", totalDuration.Seconds()).
		Msg("All operations completed successfully")

	return nil
}

// createLLMClient creates the appropriate LLM client based on configuration
func createLLMClient(ctx context.Context, cfg *config.Config, log *logging.SecureLogger) (ai.Provider, error) {
	switch cfg.LLMProvider {
	case "anthropic":
		proxyURL := cfg.GetProxyURL(true) // HTTPS proxy for API calls
		client, err := ai.NewClient(cfg.AnthropicAPIKey, cfg.ClaudeModel, proxyURL, cfg.AITimeoutSeconds, cfg.AIMaxTokens)
		if err != nil {
			return nil, fmt.Errorf("failed to create Anthropic client: %w", err)
		}
		return client, nil

	case "ollama":
		client, err := ai.NewOllamaClient(ai.OllamaConfig{
			BaseURL:        cfg.OllamaBaseURL,
			Model:          cfg.OllamaModel,
			TimeoutSeconds: cfg.AITimeoutSeconds,
			MaxTokens:      cfg.AIMaxTokens,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create Ollama client: %w", err)
		}

		// Check connection and model availability
		log.Info().
			Str("base_url", cfg.OllamaBaseURL).
			Str("model", cfg.OllamaModel).
			Msg("Checking Ollama connection...")

		if err := client.CheckConnection(ctx); err != nil {
			return nil, fmt.Errorf("Ollama connection check failed: %w", err)
		}

		return client, nil

	case "lmstudio":
		client, err := ai.NewLMStudioClient(ai.LMStudioConfig{
			BaseURL:        cfg.LMStudioBaseURL,
			Model:          cfg.LMStudioModel,
			TimeoutSeconds: cfg.AITimeoutSeconds,
			MaxTokens:      cfg.AIMaxTokens,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create LM Studio client: %w", err)
		}

		// Check connection and model availability
		log.Info().
			Str("base_url", cfg.LMStudioBaseURL).
			Str("model", cfg.LMStudioModel).
			Msg("Checking LM Studio connection...")

		if err := client.CheckConnection(ctx); err != nil {
			return nil, fmt.Errorf("LM Studio connection check failed: %w", err)
		}

		return client, nil

	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.LLMProvider)
	}
}

// createLogSource creates the appropriate log source based on configuration
func createLogSource(cfg *config.Config) (*analyzer.LogSource, error) {
	switch cfg.LogSourceType {
	case "logwatch":
		return &analyzer.LogSource{
			Type: analyzer.LogSourceLogwatch,
			Reader: logwatch.NewReader(
				cfg.MaxLogSizeMB,
				cfg.EnablePreprocessing,
				cfg.MaxPreprocessingTokens,
			),
			Preprocessor:  logwatch.NewPreprocessor(cfg.MaxPreprocessingTokens),
			PromptBuilder: logwatch.NewPromptBuilder(),
		}, nil

	case "drupal_watchdog":
		promptBuilder := drupal.NewPromptBuilder()
		if cfg.DrupalSiteName != "" {
			promptBuilder.SetSiteName(cfg.DrupalSiteName)
		}
		return &analyzer.LogSource{
			Type: analyzer.LogSourceDrupalWatchdog,
			Reader: drupal.NewReader(
				cfg.MaxLogSizeMB,
				cfg.EnablePreprocessing,
				cfg.MaxPreprocessingTokens,
				drupal.InputFormat(cfg.DrupalWatchdogFormat),
			),
			Preprocessor:  drupal.NewPreprocessor(cfg.MaxPreprocessingTokens),
			PromptBuilder: promptBuilder,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported log source type: %s", cfg.LogSourceType)
	}
}

// handleListDrupalSites lists available Drupal sites from drupal-sites.json
func handleListDrupalSites(cli *config.CLIOptions) int {
	sitesConfig, configPath, err := config.LoadDrupalSitesConfig(cli.DrupalSitesConfig)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitFailure
	}

	if sitesConfig == nil {
		_, _ = fmt.Fprintf(os.Stderr, "No drupal-sites.json configuration file found.\n")
		_, _ = fmt.Fprintf(os.Stderr, "\nSearch locations:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  - ./drupal-sites.json\n")
		_, _ = fmt.Fprintf(os.Stderr, "  - ./configs/drupal-sites.json\n")
		_, _ = fmt.Fprintf(os.Stderr, "  - /opt/logwatch-ai/drupal-sites.json\n")
		_, _ = fmt.Fprintf(os.Stderr, "  - ~/.config/logwatch-ai/drupal-sites.json\n")
		_, _ = fmt.Fprintf(os.Stderr, "\nUse -drupal-sites-config to specify a custom path.\n")
		return exitFailure
	}

	fmt.Printf("Drupal sites configuration: %s\n", configPath)
	fmt.Printf("Version: %s\n\n", sitesConfig.Version)
	fmt.Printf("Available sites:\n")

	for _, siteID := range sitesConfig.ListSites() {
		site := sitesConfig.Sites[siteID]
		defaultMarker := ""
		if siteID == sitesConfig.DefaultSite {
			defaultMarker = " (default)"
		}

		displayName := site.Name
		if displayName == "" {
			displayName = siteID
		}

		fmt.Printf("  %-20s %s%s\n", siteID, displayName, defaultMarker)
		fmt.Printf("    Drupal root:    %s\n", site.DrupalRoot)
		fmt.Printf("    Watchdog path:  %s\n", site.WatchdogPath)
		fmt.Printf("    Format:         %s\n", getFormatOrDefault(site.WatchdogFormat))
		fmt.Printf("    Min severity:   %d\n", site.MinSeverity)
		fmt.Println()
	}

	return exitSuccess
}

// getFormatOrDefault returns the format or "json" if empty
func getFormatOrDefault(format string) string {
	if format == "" {
		return "json"
	}
	return format
}
