package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaClient wraps the Ollama REST API
type OllamaClient struct {
	baseURL    string
	model      string
	maxTokens  int
	httpClient *http.Client
}

// OllamaConfig holds Ollama-specific configuration
type OllamaConfig struct {
	BaseURL        string // e.g., "http://localhost:11434"
	Model          string // e.g., "llama3.3:latest"
	TimeoutSeconds int    // Request timeout
	MaxTokens      int    // Max tokens in response
}

// ollamaGenerateRequest is the request body for Ollama's /api/generate endpoint
type ollamaGenerateRequest struct {
	Model   string        `json:"model"`
	Prompt  string        `json:"prompt"`
	System  string        `json:"system,omitempty"`
	Stream  bool          `json:"stream"`
	Options ollamaOptions `json:"options,omitempty"`
	Format  string        `json:"format,omitempty"`
}

// ollamaOptions contains model parameters
type ollamaOptions struct {
	NumPredict  int     `json:"num_predict,omitempty"` // Max tokens to generate
	Temperature float64 `json:"temperature,omitempty"`
	TopP        float64 `json:"top_p,omitempty"`
	TopK        int     `json:"top_k,omitempty"`
}

// ollamaGenerateResponse is the response from Ollama's /api/generate endpoint
type ollamaGenerateResponse struct {
	Model              string    `json:"model"`
	CreatedAt          time.Time `json:"created_at"`
	Response           string    `json:"response"`
	Done               bool      `json:"done"`
	Context            []int     `json:"context,omitempty"`
	TotalDuration      int64     `json:"total_duration,omitempty"`
	LoadDuration       int64     `json:"load_duration,omitempty"`
	PromptEvalCount    int       `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64     `json:"prompt_eval_duration,omitempty"`
	EvalCount          int       `json:"eval_count,omitempty"`
	EvalDuration       int64     `json:"eval_duration,omitempty"`
}

// ollamaChatRequest is the request body for Ollama's /api/chat endpoint
type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  ollamaOptions   `json:"options,omitempty"`
	Format   string          `json:"format,omitempty"`
}

// ollamaMessage represents a chat message
type ollamaMessage struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

// ollamaChatResponse is the response from Ollama's /api/chat endpoint
type ollamaChatResponse struct {
	Model              string        `json:"model"`
	CreatedAt          time.Time     `json:"created_at"`
	Message            ollamaMessage `json:"message"`
	Done               bool          `json:"done"`
	TotalDuration      int64         `json:"total_duration,omitempty"`
	LoadDuration       int64         `json:"load_duration,omitempty"`
	PromptEvalCount    int           `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64         `json:"prompt_eval_duration,omitempty"`
	EvalCount          int           `json:"eval_count,omitempty"`
	EvalDuration       int64         `json:"eval_duration,omitempty"`
}

// NewOllamaClient creates a new Ollama client
func NewOllamaClient(cfg OllamaConfig) (*OllamaClient, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}

	// Remove trailing slash from base URL
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")

	if cfg.Model == "" {
		return nil, fmt.Errorf("ollama model is required")
	}

	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 300 // Default 5 minutes for large models
	}

	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 8000
	}

	return &OllamaClient{
		baseURL:   cfg.BaseURL,
		model:     cfg.Model,
		maxTokens: cfg.MaxTokens,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
		},
	}, nil
}

// Analyze performs log analysis using Ollama
func (c *OllamaClient) Analyze(ctx context.Context, systemPrompt, userPrompt string) (*Analysis, *Stats, error) {
	startTime := time.Now()

	// Create request with retry logic
	response, err := retryWithBackoff(defaultMaxRetries, func() (*ollamaChatResponse, error) {
		return c.callAPI(ctx, systemPrompt, userPrompt)
	})
	if err != nil {
		return nil, nil, err
	}

	// Extract response content
	responseText := response.Message.Content
	if responseText == "" {
		return nil, nil, fmt.Errorf("empty response from Ollama")
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

// callAPI makes the actual API call to Ollama using the chat endpoint
func (c *OllamaClient) callAPI(ctx context.Context, systemPrompt, userPrompt string) (*ollamaChatResponse, error) {
	request := ollamaChatRequest{
		Model: c.model,
		Messages: []ollamaMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Stream: false,
		Options: ollamaOptions{
			NumPredict:  c.maxTokens,
			Temperature: 0.1, // Low temperature for consistent, factual output
			TopP:        0.9,
		},
		Format: "json", // Request JSON output format
	}

	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.baseURL + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API call failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response ollamaChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !response.Done {
		return nil, fmt.Errorf("incomplete response from Ollama")
	}

	return &response, nil
}

// calculateStats calculates statistics from Ollama response
func (c *OllamaClient) calculateStats(response *ollamaChatResponse, durationSeconds float64) *Stats {
	// Ollama provides token counts
	inputTokens := response.PromptEvalCount
	outputTokens := response.EvalCount

	// Local inference has no monetary cost
	// But we track tokens for comparison purposes
	return &Stats{
		Provider:            "Ollama",
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
func (c *OllamaClient) GetModelInfo() map[string]interface{} {
	return map[string]interface{}{
		"model":         c.model,
		"provider":      "Ollama",
		"max_tokens":    c.maxTokens,
		"base_url":      c.baseURL,
		"context_limit": 128000, // Varies by model, using common default
	}
}

// GetProviderName returns the name of the provider
func (c *OllamaClient) GetProviderName() string {
	return "Ollama"
}

// CheckConnection verifies that Ollama is running and the model is available
func (c *OllamaClient) CheckConnection(ctx context.Context) error {
	// Check if Ollama is running
	url := c.baseURL + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama is not running at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	// Parse response to check if model is available
	var tagsResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if err := json.Unmarshal(body, &tagsResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if the configured model is available
	modelFound := false
	for _, m := range tagsResp.Models {
		// Match model name (e.g., "llama3.3:latest" matches "llama3.3")
		if m.Name == c.model || strings.HasPrefix(m.Name, strings.Split(c.model, ":")[0]) {
			modelFound = true
			break
		}
	}

	if !modelFound {
		availableModels := make([]string, len(tagsResp.Models))
		for i, m := range tagsResp.Models {
			availableModels[i] = m.Name
		}
		return fmt.Errorf("model '%s' not found in Ollama. Available models: %v. Run 'ollama pull %s' to download it",
			c.model, availableModels, c.model)
	}

	return nil
}

// Ensure OllamaClient implements Provider interface
var _ Provider = (*OllamaClient)(nil)
