// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

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
	"github.com/olegiv/logwatch-ai-go/internal/ocms"
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
		fmt.Println("Copyright (C) 2025-2026 Oleg Ivanchenko")
		fmt.Println("License GPLv3+: GNU GPL version 3 or later <https://gnu.org/licenses/gpl.html>")
		fmt.Println("This is free software: you are free to change and redistribute it.")
		fmt.Println("There is NO WARRANTY, to the extent permitted by law.")
		return exitSuccess
	}

	// Handle -list-drupal-sites flag
	if cli.ListDrupalSites {
		return handleListDrupalSites(cli)
	}
	if cli.ListOCMSSites {
		return handleListOCMSSites(cli)
	}

	// Setup signal handling for graceful shutdown
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	go func() {
		<-ctx.Done()
		// Restore default signal handling after the first signal so a second
		// SIGTERM/SIGINT terminates immediately instead of being swallowed.
		stopSignals()
	}()

	// Load configuration with CLI overrides
	cfg, err := config.LoadWithCLI(cli)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		return exitFailure
	}
	if cli.CheckRuntime {
		if err := validateRuntime(cfg); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Runtime validation error: %v\n", err)
			return exitFailure
		}
		fmt.Println("Runtime configuration and paths are valid")
		return exitSuccess
	}

	// Initialize logger with credential sanitization (M-02 fix)
	if err := logging.ValidateLogDestination(cfg.LogDir, "analyzer.log"); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Logging configuration error: %v\n", err)
		return exitFailure
	}
	baseLog := logger.New(logger.Config{
		Level:      cfg.LogLevel,
		LogDir:     cfg.LogDir,
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
	if cfg.SelectedSiteID() != "" {
		logEvent = logEvent.Str("site_id", cfg.SelectedSiteID())
	}
	if cfg.SelectedSiteName() != "" && cfg.SelectedSiteName() != cfg.SelectedSiteID() {
		logEvent = logEvent.Str("site_name", cfg.SelectedSiteName())
	}
	logEvent.Msg("Starting Log AI Analyzer")
	log.Info().
		Str("provider", cfg.LLMProvider).
		Str("model", cfg.GetLLMModel()).
		Msg("Configured LLM")
	if cfg.Exclusions != nil {
		log.Info().
			Str("path", cfg.ExclusionsConfigPath).
			Int("global_patterns", len(cfg.Exclusions.Global)).
			Int("sites", len(cfg.Exclusions.Sites)).
			Msg("Loaded finding exclusions")
	}

	// Run the analyzer
	if err := runAnalyzer(ctx, cfg, log); err != nil {
		log.Error().Err(err).Msg("Analysis failed")
		return exitFailure
	}

	log.Info().Msg("Analysis completed successfully")
	return exitSuccess
}

func validateRuntime(cfg *config.Config) error {
	if err := logging.ValidateLogDestinationReadOnly(cfg.LogDir, "analyzer.log"); err != nil {
		return fmt.Errorf("logging: %w", err)
	}
	if cfg.EnableDatabase {
		if err := storage.ValidatePath(cfg.DatabasePath); err != nil {
			return fmt.Errorf("database: %w", err)
		}
	}
	return nil
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
		if siteID := cfg.SelectedSiteID(); siteID != "" {
			siteNames := append([]string{cfg.SelectedSiteName()}, cfg.SelectedSiteLegacyNames()...)
			reconciled, reconcileErr := store.ReconcileSiteIdentity(
				cfg.LogSourceType,
				siteID,
				siteNames...,
			)
			if reconcileErr != nil {
				return fmt.Errorf("failed to reconcile stored site identity: %w", reconcileErr)
			}
			if reconciled > 0 {
				log.Info().Int64("rows", reconciled).Msg("Reconciled legacy site history")
			}
		}
		log.Info().Str("path", cfg.DatabasePath).Msg("Database initialized")
	}

	// 2. Initialize Telegram client
	telegramClient, err := notification.NewTelegramClient(
		ctx,
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

	// 3. Initialize log source before the LLM. A valid no-entry report must
	// not depend on local model availability or consume an Anthropic request.
	logSource, err := createLogSource(cfg)
	if err != nil {
		return fmt.Errorf("failed to create log source: %w", err)
	}

	// Get source path
	sourcePath := cfg.GetLogSourcePath()

	// Read log content
	var logContent string
	if cfg.IsOCMS() && len(cfg.GetOCMSLogPaths()) > 1 {
		ocmsReader, ok := logSource.Reader.(*ocms.Reader)
		if !ok {
			return fmt.Errorf("OCMS multi-log read requires OCMS reader")
		}

		ocmsPaths := cfg.GetOCMSLogPaths()
		files := make([]ocms.LogFile, 0, len(ocmsPaths))
		log.Info().
			Int("files", len(ocmsPaths)).
			Str("type", cfg.LogSourceType).
			Msg("Reading OCMS log content...")
		for _, logPath := range ocmsPaths {
			files = append(files, ocms.LogFile{Kind: logPath.Kind, Path: logPath.Path})
			log.Info().
				Str("path", logPath.Path).
				Str("log_kind", logPath.Kind).
				Msg("Reading OCMS log file...")
		}

		logContent, err = ocmsReader.ReadFiles(files)
		if err != nil {
			return fmt.Errorf("failed to read log content: %w", err)
		}

		for _, logPath := range ocmsPaths {
			sourceInfo, infoErr := ocmsReader.GetSourceInfo(logPath.Path)
			if infoErr != nil {
				log.Warn().Err(infoErr).Str("path", logPath.Path).Msg("Could not get source file info")
				continue
			}
			log.Info().
				Str("path", logPath.Path).
				Str("log_kind", logPath.Kind).
				Float64("size_mb", sourceInfo["size_mb"].(float64)).
				Float64("age_hours", sourceInfo["age_hours"].(float64)).
				Msg("Log file read successfully")
		}
	} else {
		log.Info().
			Str("path", sourcePath).
			Str("type", cfg.LogSourceType).
			Msg("Reading log content...")

		logContent, err = logSource.Reader.Read(sourcePath)
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
	}

	// Check for no entries (Drupal watchdog specific)
	// When there are no log entries for the time period, skip AI analysis
	// and send an informational notification instead
	if cfg.LogSourceType == "drupal_watchdog" && drupal.IsNoEntriesContent(logContent) {
		log.Info().Msg("No watchdog entries found for the time period - skipping AI analysis")

		// Send informational Telegram notification
		if err := telegramClient.SendNoEntriesReport(cfg.LogSourceType, cfg.SelectedSiteName()); err != nil {
			return fmt.Errorf("failed to send no-entries notification: %w", err)
		}

		log.Info().Msg("No-entries notification sent to Telegram")
		return nil
	}

	// 4. Initialize LLM only after confirming there is content to analyze.
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

	// Get historical context (if database enabled)
	// Filter by source type and site to get relevant historical data only
	var historicalContext string
	sourceFilter := &storage.SourceFilter{
		LogSourceType: cfg.LogSourceType,
		SiteID:        cfg.SelectedSiteID(),
		SiteName:      cfg.SelectedSiteName(),
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

	// Resolve operator-defined exclusions (optional feature). Patterns are
	// injected into the prompts below so the LLM avoids matching findings
	// and their influence on systemStatus, summary, and metrics. Pattern
	// text is deliberately not logged; only counts are reported.
	var globalExclusions, contextualExclusions []string
	if cfg.Exclusions != nil {
		globalExclusions = cfg.Exclusions.GlobalPatterns()
		logType, err := analyzer.ParseSourceType(logSource.PromptBuilder.GetLogType())
		if err == nil {
			contextualExclusions = cfg.Exclusions.ContextualPatterns(logType, cfg.SelectedSiteID())
		}
		if len(globalExclusions)+len(contextualExclusions) > 0 {
			log.Info().
				Int("patterns_global", len(globalExclusions)).
				Int("patterns_contextual", len(contextualExclusions)).
				Msg("Injecting operator-defined exclusion patterns into prompt")
		}
	}

	// Build prompts using the log source's prompt builder
	systemPrompt := logSource.PromptBuilder.GetSystemPrompt(globalExclusions)

	promptResult, err := preparePromptForAnalysis(
		ctx,
		cfg,
		llmClient,
		logSource,
		systemPrompt,
		logContent,
		historicalContext,
		contextualExclusions,
		log,
	)
	if err != nil {
		return err
	}
	userPrompt := promptResult.UserPrompt

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
			SiteID:          cfg.SelectedSiteID(),
			SiteName:        cfg.SelectedSiteName(),
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
	deliveryErr := telegramClient.SendAnalysisReport(analysis, stats, cfg.LogSourceType, cfg.SelectedSiteName())
	if deliveryErr != nil {
		if notification.IsPartialDelivery(deliveryErr) {
			log.Error().
				Err(deliveryErr).
				Msg("Telegram delivery was incomplete after some messages were published; analysis will not be rerun")
		} else {
			return fmt.Errorf("failed to send Telegram notification: %w", deliveryErr)
		}
	}

	if deliveryErr == nil && cfg.HasAlertsChannel() && ai.ShouldTriggerAlert(analysis.SystemStatus) {
		log.Info().Msg("Alert notification sent (status warrants attention)")
	}

	// Final summary
	totalDuration := time.Since(startTime)
	completionLog := log.Info().Float64("total_duration_s", totalDuration.Seconds())
	if deliveryErr != nil {
		completionLog.Msg("Analysis completed with incomplete Telegram delivery")
	} else {
		completionLog.Msg("All operations completed successfully")
	}

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
			ContextTokens:  cfg.OllamaContextTokens,
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
			return nil, fmt.Errorf("ollama connection check failed: %w", err)
		}

		return client, nil

	case "lmstudio":
		client, err := ai.NewLMStudioClient(ai.LMStudioConfig{
			BaseURL:        cfg.LMStudioBaseURL,
			Model:          cfg.LMStudioModel,
			TimeoutSeconds: cfg.AITimeoutSeconds,
			MaxTokens:      cfg.AIMaxTokens,
			ContextTokens:  cfg.LMStudioContextTokens,
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
				false, // Reader preprocessing disabled — handled by preparePromptForAnalysis
				cfg.MaxPreprocessingTokens,
			),
			Preprocessor:  logwatch.NewPreprocessor(cfg.MaxPreprocessingTokens),
			PromptBuilder: logwatch.NewPromptBuilder(),
		}, nil

	case "drupal_watchdog":
		promptBuilder := drupal.NewPromptBuilder()
		if cfg.SelectedSiteName() != "" {
			promptBuilder.SetSiteName(cfg.SelectedSiteName())
		}
		return &analyzer.LogSource{
			Type: analyzer.LogSourceDrupalWatchdog,
			Reader: drupal.NewReader(
				cfg.MaxLogSizeMB,
				false, // Reader preprocessing disabled — handled by preparePromptForAnalysis
				cfg.MaxPreprocessingTokens,
				drupal.InputFormat(cfg.DrupalWatchdogFormat),
			),
			Preprocessor:  drupal.NewPreprocessor(cfg.MaxPreprocessingTokens),
			PromptBuilder: promptBuilder,
		}, nil
	case "ocms":
		promptBuilder := ocms.NewPromptBuilder()
		if cfg.SelectedSiteName() != "" {
			promptBuilder.SetSiteName(cfg.SelectedSiteName())
		}
		maxAge := time.Duration(0)
		if cfg.OCMSLogRange == config.OCMSLogRangeYesterday {
			maxAge = ocms.MaxYesterdayLogAge
		}
		return &analyzer.LogSource{
			Type: analyzer.LogSourceOCMS,
			Reader: ocms.NewReaderWithMaxAge(
				cfg.MaxLogSizeMB,
				false, // Reader preprocessing disabled — handled by preparePromptForAnalysis
				cfg.MaxPreprocessingTokens,
				maxAge,
			),
			Preprocessor:  ocms.NewPreprocessor(cfg.MaxPreprocessingTokens),
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

// handleListOCMSSites lists available OCMS sites from ocms-sites.json.
func handleListOCMSSites(cli *config.CLIOptions) int {
	sitesConfig, configPath, err := config.LoadOCMSSitesConfig(cli.OCMSSitesConfig)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitFailure
	}

	if sitesConfig == nil {
		_, _ = fmt.Fprintf(os.Stderr, "No ocms-sites.json configuration file found.\n")
		_, _ = fmt.Fprintf(os.Stderr, "\nSearch locations:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  - ./ocms-sites.json\n")
		_, _ = fmt.Fprintf(os.Stderr, "  - ./configs/ocms-sites.json\n")
		_, _ = fmt.Fprintf(os.Stderr, "  - /opt/logwatch-ai/ocms-sites.json\n")
		_, _ = fmt.Fprintf(os.Stderr, "  - ~/.config/logwatch-ai/ocms-sites.json\n")
		_, _ = fmt.Fprintf(os.Stderr, "\nUse -ocms-sites-config to specify a custom path.\n")
		return exitFailure
	}

	registryPath := sitesConfig.RegistryPath
	if cli.OCMSSitesRegistry != "" {
		registryPath = cli.OCMSSitesRegistry
	}
	registry, registryFoundPath, err := config.LoadOCMSSitesRegistry(registryPath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitFailure
	}
	if registry == nil {
		_, _ = fmt.Fprintf(os.Stderr, "No OCMS sites registry found.\n")
		_, _ = fmt.Fprintf(os.Stderr, "\nDefault location:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  - %s\n", config.DefaultOCMSSitesRegistryPath)
		_, _ = fmt.Fprintf(os.Stderr, "\nUse registry_path in ocms-sites.json or -ocms-sites-registry to specify a custom path.\n")
		return exitFailure
	}

	fmt.Printf("OCMS sites configuration: %s\n", configPath)
	fmt.Printf("Version: %s\n", sitesConfig.Version)
	fmt.Printf("Default site: %s\n", sitesConfig.DefaultSite)
	fmt.Printf("Default log kind: %s\n", getOCMSLogKindOrDefault(sitesConfig.DefaultLogKind))
	fmt.Printf("OCMS sites registry: %s\n", registryFoundPath)

	logRange, err := config.NormalizeOCMSLogRange(cli.OCMSLogRange)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitFailure
	}
	fmt.Printf("Log range: %s\n\n", logRange)
	fmt.Printf("Available sites:\n")

	for _, siteID := range sitesConfig.ListSites() {
		siteConfig := sitesConfig.Sites[siteID]
		registrySite, err := registry.GetSite(siteID)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitFailure
		}
		logKind, err := sitesConfig.EffectiveLogKind(&siteConfig)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitFailure
		}
		if cli.OCMSLogKind != "" {
			logKind, err = config.NormalizeOCMSLogKind(cli.OCMSLogKind)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return exitFailure
			}
		}
		mainLog, err := registrySite.LogPath(config.OCMSLogKindMain, logRange)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitFailure
		}
		errorLog, err := registrySite.LogPath(config.OCMSLogKindError, logRange)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitFailure
		}
		selectedLogs, err := registrySite.LogPaths(logKind, logRange)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitFailure
		}

		displayName := siteConfig.Name
		if displayName == "" {
			displayName = siteID
		}
		defaultMarker := ""
		if siteID == sitesConfig.DefaultSite {
			defaultMarker = " (default)"
		}

		fmt.Printf("  %-20s %s%s\n", siteID, displayName, defaultMarker)
		fmt.Printf("    Instance dir:  %s\n", registrySite.InstanceDir)
		fmt.Printf("    System user:   %s\n", registrySite.SystemUser)
		fmt.Printf("    Port:          %d\n", registrySite.Port)
		fmt.Printf("    Log kind:      %s\n", logKind)
		if len(selectedLogs) == 1 {
			fmt.Printf("    Selected log:  %s\n", selectedLogs[0].Path)
		} else {
			fmt.Printf("    Selected logs:\n")
			for _, selectedLog := range selectedLogs {
				fmt.Printf("      %-5s %s\n", selectedLog.Kind, selectedLog.Path)
			}
		}
		fmt.Printf("    Main log:      %s\n", mainLog)
		fmt.Printf("    Error log:     %s\n", errorLog)
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

func getOCMSLogKindOrDefault(logKind string) string {
	if logKind == "" {
		return config.OCMSLogKindMain
	}
	return logKind
}
