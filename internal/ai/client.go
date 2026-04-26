// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/liushuangls/go-anthropic/v2"
	internalerrors "github.com/olegiv/logwatch-ai-go/internal/errors"
)

// Client wraps the Anthropic API client
type Client struct {
	client         *anthropic.Client
	countingClient *anthropic.Client
	model          string
	maxTokens      int // L-02 fix: configurable max tokens
	pricing        ModelPricing
}

// Stats holds statistics about the API call
type Stats struct {
	Provider            string
	Model               string
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	CostUSD             float64
	DurationSeconds     float64
}

// NewClient creates a new Claude AI client
// timeoutSeconds and maxTokens are configurable (L-02 fix)
func NewClient(apiKey, model, proxyURL string, timeoutSeconds, maxTokens int) (*Client, error) {
	var httpClient *http.Client
	timeout := time.Duration(timeoutSeconds) * time.Second

	// Configure proxy if provided
	if proxyURL != "" {
		proxyURLParsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}

		// Validate proxy URL scheme for security
		if proxyURLParsed.Scheme != "http" && proxyURLParsed.Scheme != "https" {
			return nil, fmt.Errorf("proxy URL must use http or https scheme, got: %s", proxyURLParsed.Scheme)
		}

		httpClient = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(proxyURLParsed),
			},
			Timeout: timeout,
		}
	} else {
		httpClient = &http.Client{
			Timeout: timeout,
		}
	}

	client := anthropic.NewClient(
		apiKey,
		anthropic.WithHTTPClient(httpClient),
	)
	countingClient := anthropic.NewClient(
		apiKey,
		anthropic.WithHTTPClient(httpClient),
		anthropic.WithBetaVersion(anthropic.BetaTokenCounting20241101),
	)

	pricing, known := ResolvePricing(model)
	if !known {
		// NewClient runs before the SecureLogger is wired to the anthropic
		// Client struct, so emit to stderr instead of the stdlib log package.
		// model has already passed the CLAUDE_MODEL regex in config.Validate.
		fmt.Fprintf(os.Stderr, "ai: unknown model %q - cost will be estimated using fallback pricing (%.2f/%.2f per MTok input/output); update modelPricingTable in internal/ai/pricing.go\n",
			model, pricing.Input, pricing.Output)
	}

	return &Client{
		client:         client,
		countingClient: countingClient,
		model:          model,
		maxTokens:      maxTokens,
		pricing:        pricing,
	}, nil
}

// Analyze performs log analysis using provided prompts.
// This is the generic analysis method that accepts custom system and user prompts.
func (c *Client) Analyze(ctx context.Context, systemPrompt, userPrompt string) (*Analysis, *Stats, error) {
	startTime := time.Now()

	// Create request with retry logic
	response, err := retryWithBackoff(defaultMaxRetries, func() (anthropic.MessagesResponse, error) {
		return c.callAPI(ctx, systemPrompt, userPrompt)
	})
	if err != nil {
		return nil, nil, err
	}

	// Extract response content
	if len(response.Content) == 0 {
		return nil, nil, fmt.Errorf("empty response from Claude")
	}

	var responseText strings.Builder
	for _, content := range response.Content {
		if content.Type == "text" && content.Text != nil {
			responseText.WriteString(*content.Text)
		}
	}

	// Parse analysis
	analysis, err := ParseAnalysis(responseText.String())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse analysis: %w", err)
	}

	// Calculate statistics
	stats := c.calculateStats(response, time.Since(startTime).Seconds())

	return analysis, stats, nil
}

// callAPI makes the actual API call to Claude
func (c *Client) callAPI(ctx context.Context, systemPrompt, userPrompt string) (anthropic.MessagesResponse, error) {
	request := c.buildMessagesRequest(systemPrompt, userPrompt)
	request.MaxTokens = c.maxTokens // L-02 fix: use configurable value

	response, err := c.client.CreateMessages(ctx, request)
	if err != nil {
		// Sanitize error to prevent credentials from appearing in error messages (M-01 fix)
		return anthropic.MessagesResponse{}, internalerrors.Wrapf(err, "API call failed")
	}

	return response, nil
}

// CountPromptTokens counts prompt tokens exactly using Anthropic's token counting API.
func (c *Client) CountPromptTokens(ctx context.Context, systemPrompt, userPrompt string) (int, error) {
	if c.countingClient == nil {
		return 0, fmt.Errorf("token counting client is not configured")
	}

	request := c.buildMessagesRequest(systemPrompt, userPrompt)
	response, err := retryWithBackoff(defaultMaxRetries, func() (anthropic.CountTokensResponse, error) {
		resp, retryErr := c.countingClient.CountTokens(ctx, request)
		if retryErr != nil {
			return resp, internalerrors.Wrapf(retryErr, "API call failed")
		}
		return resp, nil
	})
	if err != nil {
		return 0, err
	}

	return response.InputTokens, nil
}

func (c *Client) buildMessagesRequest(systemPrompt, userPrompt string) anthropic.MessagesRequest {
	return anthropic.MessagesRequest{
		Model: anthropic.Model(c.model),
		Messages: []anthropic.Message{
			{
				Role: anthropic.RoleUser,
				Content: []anthropic.MessageContent{
					anthropic.NewTextMessageContent(userPrompt),
				},
			},
		},
		System: systemPrompt,
	}
}

// calculateStats calculates cost and token statistics using the model-aware
// pricing resolved at client construction time (see ResolvePricing).
func (c *Client) calculateStats(response anthropic.MessagesResponse, durationSeconds float64) *Stats {
	inputTokens := response.Usage.InputTokens
	outputTokens := response.Usage.OutputTokens
	cacheCreationTokens := response.Usage.CacheCreationInputTokens
	cacheReadTokens := response.Usage.CacheReadInputTokens

	return &Stats{
		Provider:            "Anthropic",
		Model:               c.model,
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		CacheCreationTokens: cacheCreationTokens,
		CacheReadTokens:     cacheReadTokens,
		CostUSD:             c.pricing.Cost(inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens),
		DurationSeconds:     durationSeconds,
	}
}

// GetModelInfo returns information about the configured model
func (c *Client) GetModelInfo() map[string]any {
	return map[string]any{
		"model":         c.model,
		"provider":      "Anthropic",
		"max_tokens":    c.maxTokens, // L-02 fix: use configurable value
		"context_limit": 200000,
	}
}

// GetProviderName returns the name of the provider
func (c *Client) GetProviderName() string {
	return "Anthropic"
}

// Ensure Client implements Provider interface
var (
	_ Provider           = (*Client)(nil)
	_ PromptTokenCounter = (*Client)(nil)
)
