package ai

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/liushuangls/go-anthropic/v2"
	internalerrors "github.com/olegiv/logwatch-ai-go/internal/errors"
)

// Client wraps the Anthropic API client
type Client struct {
	client    *anthropic.Client
	model     string
	maxTokens int // L-02 fix: configurable max tokens
}

// Stats holds statistics about the API call
type Stats struct {
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

	return &Client{
		client:    client,
		model:     model,
		maxTokens: maxTokens,
	}, nil
}

// Analyze performs log analysis using provided prompts.
// This is the generic analysis method that accepts custom system and user prompts.
func (c *Client) Analyze(ctx context.Context, systemPrompt, userPrompt string) (*Analysis, *Stats, error) {
	startTime := time.Now()

	// Create request with retry logic
	var response anthropic.MessagesResponse
	var lastErr error

	for attempt := 1; attempt <= 3; attempt++ {
		var err error
		response, err = c.callAPI(ctx, systemPrompt, userPrompt)
		if err == nil {
			break
		}

		lastErr = err
		if attempt < 3 {
			// Exponential backoff: 2^n * 1000ms
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			time.Sleep(backoff)
		}
	}

	if lastErr != nil {
		return nil, nil, fmt.Errorf("all retry attempts failed: %w", lastErr)
	}

	// Extract response content
	if len(response.Content) == 0 {
		return nil, nil, fmt.Errorf("empty response from Claude")
	}

	responseText := ""
	for _, content := range response.Content {
		if content.Type == "text" && content.Text != nil {
			responseText += *content.Text
		}
	}

	// Parse analysis
	analysis, err := ParseAnalysis(responseText)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse analysis: %w", err)
	}

	// Calculate statistics
	stats := c.calculateStats(response, time.Since(startTime).Seconds())

	return analysis, stats, nil
}

// AnalyzeLogwatch analyzes logwatch content using Claude.
// Deprecated: Use Analyze() with a PromptBuilder instead. This method is kept for backward compatibility.
func (c *Client) AnalyzeLogwatch(ctx context.Context, logwatchContent, historicalContext string) (*Analysis, *Stats, error) {
	systemPrompt := GetSystemPrompt()
	userPrompt := GetUserPrompt(logwatchContent, historicalContext)
	return c.Analyze(ctx, systemPrompt, userPrompt)
}

// callAPI makes the actual API call to Claude
func (c *Client) callAPI(ctx context.Context, systemPrompt, userPrompt string) (anthropic.MessagesResponse, error) {
	request := anthropic.MessagesRequest{
		Model: anthropic.Model(c.model),
		Messages: []anthropic.Message{
			{
				Role: anthropic.RoleUser,
				Content: []anthropic.MessageContent{
					anthropic.NewTextMessageContent(userPrompt),
				},
			},
		},
		System:    systemPrompt,
		MaxTokens: c.maxTokens, // L-02 fix: use configurable value
	}

	response, err := c.client.CreateMessages(ctx, request)
	if err != nil {
		// Sanitize error to prevent credentials from appearing in error messages (M-01 fix)
		return anthropic.MessagesResponse{}, internalerrors.Wrapf(err, "API call failed")
	}

	return response, nil
}

// calculateStats calculates cost and token statistics
func (c *Client) calculateStats(response anthropic.MessagesResponse, durationSeconds float64) *Stats {
	inputTokens := response.Usage.InputTokens
	outputTokens := response.Usage.OutputTokens

	// Cache tokens (may be 0 if not using cache)
	cacheCreationTokens := response.Usage.CacheCreationInputTokens
	cacheReadTokens := response.Usage.CacheReadInputTokens

	// Calculate costs (Claude Sonnet 4.5 pricing)
	// Input: $3/MTok, Output: $15/MTok
	// Cache write: $3.75/MTok, Cache read: $0.30/MTok
	inputCost := float64(inputTokens) / 1000000 * 3.0
	outputCost := float64(outputTokens) / 1000000 * 15.0
	cacheWriteCost := float64(cacheCreationTokens) / 1000000 * 3.75
	cacheReadCost := float64(cacheReadTokens) / 1000000 * 0.30

	totalCost := inputCost + outputCost + cacheWriteCost + cacheReadCost

	return &Stats{
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		CacheCreationTokens: cacheCreationTokens,
		CacheReadTokens:     cacheReadTokens,
		CostUSD:             totalCost,
		DurationSeconds:     durationSeconds,
	}
}

// GetModelInfo returns information about the configured model
func (c *Client) GetModelInfo() map[string]interface{} {
	return map[string]interface{}{
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
var _ Provider = (*Client)(nil)
