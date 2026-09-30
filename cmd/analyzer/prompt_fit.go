// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/olegiv/logwatch-ai-go/internal/ai"
	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
	"github.com/olegiv/logwatch-ai-go/internal/config"
	internalerrors "github.com/olegiv/logwatch-ai-go/internal/errors"
	"github.com/olegiv/logwatch-ai-go/internal/logging"
)

const (
	maxAnthropicPromptFitAttempts = 4
	promptFitAdjustmentFactor     = 0.95
	minPromptFitLogBudget         = 1
)

type promptPreparationResult struct {
	LogContent string
	UserPrompt string
}

func preparePromptForAnalysis(
	ctx context.Context,
	cfg *config.Config,
	llmClient ai.Provider,
	logSource *analyzer.LogSource,
	systemPrompt, rawLogContent, historicalContext string,
	contextualExclusions []string,
	log *logging.SecureLogger,
) (*promptPreparationResult, error) {
	// Prompt builders sanitize log content before it reaches an LLM. Perform
	// that same idempotent transformation before budgeting so NFKC expansion
	// or injection-marker replacement cannot make the final prompt larger than
	// the content the preprocessor measured.
	promptLogContent := ai.SanitizeLogContent(rawLogContent)

	if llmClient.GetProviderName() == "Anthropic" {
		counter, ok := llmClient.(ai.PromptTokenCounter)
		if !ok {
			return nil, fmt.Errorf("failed to count Anthropic prompt tokens: provider does not support prompt token counting")
		}

		return prepareAnthropicPromptForAnalysis(
			ctx,
			cfg,
			llmClient,
			counter,
			logSource,
			systemPrompt,
			promptLogContent,
			historicalContext,
			contextualExclusions,
			log,
		)
	}

	return prepareHeuristicPromptForAnalysis(
		cfg,
		llmClient,
		logSource,
		systemPrompt,
		promptLogContent,
		historicalContext,
		contextualExclusions,
		log,
	)
}

func prepareAnthropicPromptForAnalysis(
	ctx context.Context,
	cfg *config.Config,
	llmClient ai.Provider,
	counter ai.PromptTokenCounter,
	logSource *analyzer.LogSource,
	systemPrompt, rawLogContent, historicalContext string,
	contextualExclusions []string,
	log *logging.SecureLogger,
) (*promptPreparationResult, error) {
	contextLimit := analyzer.ContextLimitFromModelInfo(llmClient.GetModelInfo())
	targetInputTokens := contextLimit - cfg.AIMaxTokens - analyzer.PromptSafetyMarginTokens(contextLimit)
	if targetInputTokens < minPromptFitLogBudget {
		return nil, fmt.Errorf(
			"anthropic context window has no safe input capacity after reserving output and safety margin: %d tokens",
			contextLimit,
		)
	}

	baseUserPrompt := logSource.PromptBuilder.GetUserPrompt("", historicalContext, contextualExclusions)
	exactBasePromptTokens, err := counter.CountPromptTokens(ctx, systemPrompt, baseUserPrompt)
	if err != nil {
		if terminalErr := promptTokenCountTerminalError(ctx, err); terminalErr != nil {
			return nil, terminalErr
		}
		if log != nil {
			log.Warn().Err(err).Msg("Anthropic token counting failed, falling back to heuristic sizing")
		}

		return prepareHeuristicPromptForAnalysis(cfg, llmClient, logSource, systemPrompt, rawLogContent, historicalContext, contextualExclusions, log)
	}

	if exactBasePromptTokens >= targetInputTokens {
		return nil, fmt.Errorf(
			"fixed Anthropic prompt is too large for the context window: %d tokens >= target %d",
			exactBasePromptTokens,
			targetInputTokens,
		)
	}
	targetLogTokensExact := targetInputTokens - exactBasePromptTokens

	if log != nil {
		log.Info().
			Int("exact_base_prompt_tokens", exactBasePromptTokens).
			Int("target_input_tokens", targetInputTokens).
			Int("target_log_tokens", targetLogTokensExact).
			Msg("Calculated Anthropic exact prompt budget")
	}

	currentBudget := targetLogTokensExact
	compressionAttempts := 0
	var exactPromptTokens int
	var logContent string
	var userPrompt string

	for attempt := 1; attempt <= maxAnthropicPromptFitAttempts; attempt++ {
		logContent, compressionAttempts, err = preprocessLogContent(
			logSource.Preprocessor,
			rawLogContent,
			currentBudget,
			cfg.EnablePreprocessing,
			compressionAttempts,
		)
		if err != nil {
			return nil, internalerrors.Wrapf(err, "preprocessing failed")
		}

		userPrompt = logSource.PromptBuilder.GetUserPrompt(logContent, historicalContext, contextualExclusions)
		exactPromptTokens, err = counter.CountPromptTokens(ctx, systemPrompt, userPrompt)
		if err != nil {
			if terminalErr := promptTokenCountTerminalError(ctx, err); terminalErr != nil {
				return nil, terminalErr
			}
			if log != nil {
				log.Warn().Err(err).Msg("Anthropic token counting failed during fitting, using heuristic result")
			}
			return validateHeuristicPromptFit(cfg, llmClient, systemPrompt, logContent, userPrompt)
		}

		if log != nil {
			log.Info().
				Int("attempt", attempt).
				Int("heuristic_budget", currentBudget).
				Int("exact_prompt_tokens", exactPromptTokens).
				Int("target_input_tokens", targetInputTokens).
				Int("compression_attempts", compressionAttempts).
				Msg("Anthropic prompt sizing attempt")
		}

		if exactPromptTokens <= targetInputTokens {
			return &promptPreparationResult{
				LogContent: logContent,
				UserPrompt: userPrompt,
			}, nil
		}

		if !cfg.EnablePreprocessing {
			return nil, fmt.Errorf(
				"prompt is too long for Anthropic and preprocessing is disabled: %d tokens > target %d",
				exactPromptTokens,
				targetInputTokens,
			)
		}

		if attempt == maxAnthropicPromptFitAttempts {
			break
		}

		actualLogTokens := max(exactPromptTokens-exactBasePromptTokens, 1)

		nextBudget := int(math.Floor(
			float64(currentBudget) *
				(float64(targetLogTokensExact) / float64(actualLogTokens)) *
				promptFitAdjustmentFactor,
		))
		nextBudget = clampPromptFitBudget(nextBudget)
		if nextBudget >= currentBudget && currentBudget > minPromptFitLogBudget {
			nextBudget = currentBudget - 1
		}
		currentBudget = clampPromptFitBudget(nextBudget)
	}

	return nil, fmt.Errorf(
		"prompt still exceeds Anthropic context window after %d compression attempts: %d tokens > target %d",
		compressionAttempts,
		exactPromptTokens,
		targetInputTokens,
	)
}

func promptTokenCountTerminalError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func prepareHeuristicPromptForAnalysis(
	cfg *config.Config,
	llmClient ai.Provider,
	logSource *analyzer.LogSource,
	systemPrompt, rawLogContent, historicalContext string,
	contextualExclusions []string,
	log *logging.SecureLogger,
) (*promptPreparationResult, error) {
	logContent := rawLogContent
	modelInfo := llmClient.GetModelInfo()
	contextLimit := analyzer.ContextLimitFromModelInfo(modelInfo)

	if cfg.EnablePreprocessing {
		systemPromptTokens := analyzer.EstimateTokens(systemPrompt)
		userPromptOverheadTokens := analyzer.EstimateTokens(
			logSource.PromptBuilder.GetUserPrompt("", historicalContext, contextualExclusions),
		)
		logTokenBudget := analyzer.CalculateLogTokenBudget(
			contextLimit,
			cfg.AIMaxTokens,
			systemPromptTokens,
			userPromptOverheadTokens,
		)
		if logTokenBudget == 0 {
			return nil, fmt.Errorf(
				"fixed prompt leaves no safe capacity for log content in the %d-token context window",
				contextLimit,
			)
		}

		if log != nil {
			log.Info().
				Int("context_limit", contextLimit).
				Int("response_reserve_tokens", cfg.AIMaxTokens).
				Int("system_prompt_tokens", systemPromptTokens).
				Int("prompt_overhead_tokens", userPromptOverheadTokens).
				Int("log_token_budget", logTokenBudget).
				Msg("Calculated prompt token budget")
		}

		originalTokens := logSource.Preprocessor.EstimateTokens(rawLogContent)
		processedLogContent, _, err := preprocessLogContent(
			logSource.Preprocessor,
			rawLogContent,
			logTokenBudget,
			true,
			0,
		)
		if err != nil {
			return nil, internalerrors.Wrapf(err, "preprocessing failed")
		}

		logContent = processedLogContent
		if logContent != rawLogContent && log != nil {
			log.Info().
				Int("original_tokens", originalTokens).
				Int("processed_tokens", logSource.Preprocessor.EstimateTokens(logContent)).
				Msg("Preprocessed log content to fit prompt budget")
		}
	}

	userPrompt := logSource.PromptBuilder.GetUserPrompt(logContent, historicalContext, contextualExclusions)
	return validateHeuristicPromptFit(cfg, llmClient, systemPrompt, logContent, userPrompt)
}

func validateHeuristicPromptFit(
	cfg *config.Config,
	llmClient ai.Provider,
	systemPrompt, logContent, userPrompt string,
) (*promptPreparationResult, error) {
	contextLimit := analyzer.ContextLimitFromModelInfo(llmClient.GetModelInfo())
	targetInputTokens := contextLimit - cfg.AIMaxTokens - analyzer.PromptSafetyMarginTokens(contextLimit)
	estimatedPromptTokens := analyzer.EstimateTokens(systemPrompt) + analyzer.EstimateTokens(userPrompt)
	if targetInputTokens < minPromptFitLogBudget || estimatedPromptTokens > targetInputTokens {
		return nil, fmt.Errorf(
			"prompt is too large for the configured context window: estimated %d input tokens > safe target %d",
			estimatedPromptTokens,
			max(targetInputTokens, 0),
		)
	}
	return &promptPreparationResult{LogContent: logContent, UserPrompt: userPrompt}, nil
}

func preprocessLogContent(
	preprocessor analyzer.Preprocessor,
	rawLogContent string,
	budget int,
	enablePreprocessing bool,
	compressionAttempts int,
) (string, int, error) {
	if !enablePreprocessing || preprocessor == nil {
		return rawLogContent, compressionAttempts, nil
	}

	if !preprocessor.ShouldProcess(rawLogContent, budget) {
		return rawLogContent, compressionAttempts, nil
	}

	if budgetPreprocessor, ok := preprocessor.(analyzer.BudgetPreprocessor); ok {
		processed, err := budgetPreprocessor.ProcessWithBudget(rawLogContent, budget)
		if err != nil {
			return "", compressionAttempts, err
		}
		return processed, compressionAttempts + 1, nil
	}

	processed, err := preprocessor.Process(rawLogContent)
	if err != nil {
		return "", compressionAttempts, err
	}

	return processed, compressionAttempts + 1, nil
}

func clampPromptFitBudget(budget int) int {
	if budget < minPromptFitLogBudget {
		return minPromptFitLogBudget
	}
	return budget
}
