package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LMStudioClient wraps the LM Studio OpenAI-compatible REST API.
//
// Recommended models for log analysis (download from LM Studio's model browser):
//   - Llama-3.3-70B-Instruct: Best quality (~40GB VRAM)
//   - Qwen2.5-32B-Instruct: Excellent reasoning (~20GB VRAM)
//   - Mistral-Small-24B-Instruct: Good balance (~15GB VRAM)
//   - Phi-4-14B: Fast, good quality (~9GB VRAM)
//   - Llama-3.2-8B-Instruct: Lightweight (~5GB VRAM)
//
// Use GGUF quantized versions (Q4_K_M, Q5_K_M) for better VRAM efficiency.
type LMStudioClient struct {
	baseURL    string
	model      string
	maxTokens  int
	httpClient *http.Client
}

// LMStudioConfig holds LM Studio-specific configuration
type LMStudioConfig struct {
	BaseURL        string // e.g., "http://localhost:1234"
	Model          string // e.g., "local-model" (LM Studio model identifier)
	TimeoutSeconds int    // Request timeout
	MaxTokens      int    // Max tokens in response
}

// openAIChatRequest is the request body for OpenAI-compatible /v1/chat/completions endpoint
type openAIChatRequest struct {
	Model          string          `json:"model"`
	Messages       []openAIMessage `json:"messages"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Temperature    float64         `json:"temperature,omitempty"`
	TopP           float64         `json:"top_p,omitempty"`
	Stream         bool            `json:"stream"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

// responseFormat specifies the output format
type responseFormat struct {
	Type string `json:"type"` // "json_object" for JSON mode
}

// openAIMessage represents a chat message in OpenAI format
type openAIMessage struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

// openAIChatResponse is the response from OpenAI-compatible /v1/chat/completions endpoint
type openAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int           `json:"index"`
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// openAIModelsResponse is the response from /v1/models endpoint
type openAIModelsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

// NewLMStudioClient creates a new LM Studio client
func NewLMStudioClient(cfg LMStudioConfig) (*LMStudioClient, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:1234"
	}

	// Remove trailing slash from base URL
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")

	if cfg.Model == "" {
		// LM Studio uses "local-model" or the loaded model's name
		cfg.Model = "local-model"
	}

	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 300 // Default 5 minutes for large models
	}

	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 8000
	}

	return &LMStudioClient{
		baseURL:   cfg.BaseURL,
		model:     cfg.Model,
		maxTokens: cfg.MaxTokens,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
		},
	}, nil
}

// Analyze performs log analysis using LM Studio
func (c *LMStudioClient) Analyze(ctx context.Context, systemPrompt, userPrompt string) (*Analysis, *Stats, error) {
	startTime := time.Now()

	// Create request with retry logic
	response, err := retryWithBackoff(defaultMaxRetries, func() (*openAIChatResponse, error) {
		return c.callAPI(ctx, systemPrompt, userPrompt)
	})
	if err != nil {
		return nil, nil, err
	}

	// Extract response content
	if len(response.Choices) == 0 {
		return nil, nil, fmt.Errorf("empty response from LM Studio (no choices)")
	}

	responseText := response.Choices[0].Message.Content
	if responseText == "" {
		return nil, nil, fmt.Errorf("empty response from LM Studio")
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

// callAPI makes the actual API call to LM Studio using the OpenAI-compatible endpoint
func (c *LMStudioClient) callAPI(ctx context.Context, systemPrompt, userPrompt string) (*openAIChatResponse, error) {
	// Note: LM Studio doesn't support "json_object" response_format like OpenAI.
	// It only accepts "json_schema" (requires full schema) or "text".
	// We rely on the system prompt to request JSON output instead.
	request := openAIChatRequest{
		Model: c.model,
		Messages: []openAIMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		MaxTokens:   c.maxTokens,
		Temperature: 0.1, // Low temperature for consistent, factual output
		TopP:        0.9,
		Stream:      false,
		// ResponseFormat omitted - not all LM Studio models support json_object mode
	}

	url := c.baseURL + "/v1/chat/completions"
	return doJSONPost[openAIChatResponse](ctx, c.httpClient, url, request)
}

// calculateStats calculates statistics from LM Studio response
func (c *LMStudioClient) calculateStats(response *openAIChatResponse, durationSeconds float64) *Stats {
	// LM Studio provides token counts in OpenAI format
	inputTokens := response.Usage.PromptTokens
	outputTokens := response.Usage.CompletionTokens

	// Local inference has no monetary cost
	return &Stats{
		Provider:            "LMStudio",
		Model:               c.model,
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		CacheCreationTokens: 0,
		CacheReadTokens:     0,
		CostUSD:             0.0, // Local inference is free
		DurationSeconds:     durationSeconds,
	}
}

// GetModelInfo returns information about the configured model
func (c *LMStudioClient) GetModelInfo() map[string]interface{} {
	return map[string]interface{}{
		"model":         c.model,
		"provider":      "LMStudio",
		"max_tokens":    c.maxTokens,
		"base_url":      c.baseURL,
		"context_limit": 128000, // Varies by model, using common default
	}
}

// GetProviderName returns the name of the provider
func (c *LMStudioClient) GetProviderName() string {
	return "LMStudio"
}

// CheckConnection verifies that LM Studio is running and a model is loaded
func (c *LMStudioClient) CheckConnection(ctx context.Context) error {
	// Check if LM Studio is running by querying the models endpoint
	url := c.baseURL + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("LM Studio is not running at %s: %w", c.baseURL, err)
	}
	if resp == nil {
		return fmt.Errorf("LM Studio returned nil response")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("LM Studio returned status %d", resp.StatusCode)
	}

	// Parse response to check if any model is loaded
	var modelsResp openAIModelsResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if err := json.Unmarshal(body, &modelsResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if any models are available
	if len(modelsResp.Data) == 0 {
		return fmt.Errorf("no models loaded in LM Studio. Please load a model in LM Studio first")
	}

	// If a specific model is configured, check if it's available
	// LM Studio often uses "local-model" as a generic identifier
	if c.model != "local-model" {
		modelFound := false
		for _, m := range modelsResp.Data {
			if m.ID == c.model || strings.Contains(m.ID, c.model) {
				modelFound = true
				break
			}
		}

		if !modelFound {
			availableModels := make([]string, len(modelsResp.Data))
			for i, m := range modelsResp.Data {
				availableModels[i] = m.ID
			}
			return fmt.Errorf("model '%s' not found in LM Studio. Available models: %v. You can use 'local-model' to use the currently loaded model",
				c.model, availableModels)
		}
	}

	return nil
}

// Ensure LMStudioClient implements Provider interface
var _ Provider = (*LMStudioClient)(nil)
