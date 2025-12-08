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
	"github.com/olegiv/logwatch-ai-go/internal/config"
	"github.com/olegiv/logwatch-ai-go/internal/logwatch"
	"github.com/olegiv/logwatch-ai-go/internal/notification"
	"github.com/olegiv/logwatch-ai-go/internal/storage"
)

const (
	exitSuccess = 0
	exitFailure = 1
)

func main() {
	os.Exit(run())
}

func run() int {
	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		return exitFailure
	}

	// Initialize logger
	log := logger.New(logger.Config{
		Level:      cfg.LogLevel,
		LogDir:     "./logs",
		Filename:   "analyzer.log",
		MaxSizeMB:  10,
		MaxBackups: 5,
		Console:    true,
	})

	log.Info().Msg("Starting Logwatch AI Analyzer")
	log.Info().Str("model", cfg.ClaudeModel).Msg("Configured AI model")

	// Run the analyzer
	if err := runAnalyzer(ctx, cfg, log); err != nil {
		log.Error().Err(err).Msg("Analysis failed")
		return exitFailure
	}

	log.Info().Msg("Analysis completed successfully")
	return exitSuccess
}

func runAnalyzer(ctx context.Context, cfg *config.Config, log *logger.Logger) error {
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

	// 3. Initialize Claude AI client
	proxyURL := cfg.GetProxyURL(true) // HTTPS proxy for API calls
	claudeClient, err := ai.NewClient(cfg.AnthropicAPIKey, cfg.ClaudeModel, proxyURL)
	if err != nil {
		return fmt.Errorf("failed to initialize Claude client: %w", err)
	}

	modelInfo := claudeClient.GetModelInfo()
	log.Info().
		Str("model", modelInfo["model"].(string)).
		Int("max_tokens", modelInfo["max_tokens"].(int)).
		Msg("Claude client initialized")

	// 4. Initialize logwatch reader
	reader := logwatch.NewReader(
		cfg.MaxLogSizeMB,
		cfg.EnablePreprocessing,
		cfg.MaxPreprocessingTokens,
	)

	// Read logwatch output
	log.Info().Str("path", cfg.LogwatchOutputPath).Msg("Reading logwatch output...")
	logwatchContent, err := reader.ReadLogwatchOutput(cfg.LogwatchOutputPath)
	if err != nil {
		return fmt.Errorf("failed to read logwatch output: %w", err)
	}

	fileInfo, _ := reader.GetFileInfo(cfg.LogwatchOutputPath)
	log.Info().
		Float64("size_mb", fileInfo["size_mb"].(float64)).
		Float64("age_hours", fileInfo["age_hours"].(float64)).
		Msg("Logwatch file read successfully")

	// Get historical context (if database enabled)
	var historicalContext string
	if store != nil {
		log.Info().Msg("Retrieving historical context...")
		historicalContext, err = store.GetHistoricalContext(7) // Last 7 days
		if err != nil {
			log.Warn().Err(err).Msg("Failed to get historical context, continuing without it")
		} else if historicalContext != "" {
			log.Info().Msg("Historical context retrieved")
		}
	}

	// Analyze with Claude
	log.Info().Msg("Analyzing with Claude AI...")
	analysis, stats, err := claudeClient.AnalyzeLogwatch(ctx, logwatchContent, historicalContext)
	if err != nil {
		return fmt.Errorf("claude analysis failed: %w", err)
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
	if err := telegramClient.SendAnalysisReport(analysis, stats); err != nil {
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
