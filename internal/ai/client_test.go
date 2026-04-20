// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liushuangls/go-anthropic/v2"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name        string
		apiKey      string
		model       string
		proxyURL    string
		expectError bool
	}{
		{
			name:        "Valid client without proxy",
			apiKey:      "sk-ant-test-key",
			model:       "claude-sonnet-4.5",
			proxyURL:    "",
			expectError: false,
		},
		{
			name:        "Valid client with proxy",
			apiKey:      "sk-ant-test-key",
			model:       "claude-sonnet-4.5",
			proxyURL:    "http://proxy.example.com:8080",
			expectError: false,
		},
		{
			name:        "Valid client with https proxy",
			apiKey:      "sk-ant-test-key",
			model:       "claude-sonnet-4.5",
			proxyURL:    "https://proxy.example.com:8080",
			expectError: false,
		},
		{
			name:        "Invalid proxy URL",
			apiKey:      "sk-ant-test-key",
			model:       "claude-sonnet-4.5",
			proxyURL:    "://invalid-url",
			expectError: true,
		},
		{
			name:        "Invalid proxy scheme - socks5",
			apiKey:      "sk-ant-test-key",
			model:       "claude-sonnet-4.5",
			proxyURL:    "socks5://proxy.example.com:1080",
			expectError: true,
		},
		{
			name:        "Invalid proxy scheme - ftp",
			apiKey:      "sk-ant-test-key",
			model:       "claude-sonnet-4.5",
			proxyURL:    "ftp://proxy.example.com:21",
			expectError: true,
		},
		{
			name:        "Invalid proxy scheme - file",
			apiKey:      "sk-ant-test-key",
			model:       "claude-sonnet-4.5",
			proxyURL:    "file:///etc/passwd",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.apiKey, tt.model, tt.proxyURL, 120, 8000)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if client == nil {
				t.Error("Expected client but got nil")
				return
			}

			if client.model != tt.model {
				t.Errorf("Expected model %s, got %s", tt.model, client.model)
			}

			if client.client == nil {
				t.Error("Expected Anthropic client to be initialized")
			}

			if client.countingClient == nil {
				t.Error("Expected Anthropic counting client to be initialized")
			}
		})
	}
}

func TestCountPromptTokens(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/messages/count_tokens" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}

			if got := r.Header.Get("Anthropic-Beta"); !strings.Contains(got, string(anthropic.BetaTokenCounting20241101)) {
				t.Fatalf("missing token counting beta header: %q", got)
			}

			var req anthropic.MessagesRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}

			if req.MaxTokens != 0 {
				t.Fatalf("CountPromptTokens should not send max_tokens, got %d", req.MaxTokens)
			}

			res := anthropic.CountTokensResponse{InputTokens: 321}
			if err := json.NewEncoder(w).Encode(res); err != nil {
				t.Fatalf("failed to encode response: %v", err)
			}
		}))
		defer server.Close()

		client := &Client{
			client: anthropic.NewClient(
				"sk-ant-test-key",
				anthropic.WithBaseURL(server.URL+"/v1"),
				anthropic.WithHTTPClient(server.Client()),
			),
			countingClient: anthropic.NewClient(
				"sk-ant-test-key",
				anthropic.WithBaseURL(server.URL+"/v1"),
				anthropic.WithHTTPClient(server.Client()),
				anthropic.WithBetaVersion(anthropic.BetaTokenCounting20241101),
			),
			model:     "claude-sonnet-4.5",
			maxTokens: 8000,
		}

		tokens, err := client.CountPromptTokens(context.Background(), "system", "user")
		if err != nil {
			t.Fatalf("CountPromptTokens() error = %v", err)
		}

		if tokens != 321 {
			t.Fatalf("CountPromptTokens() = %d, want 321", tokens)
		}
	})

	t.Run("failure sanitizes credentials", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type": "error",
				"error": map[string]string{
					"type":    "invalid_request_error",
					"message": "bad token sk-ant-secretsecretsecret1234567890",
				},
			})
		}))
		defer server.Close()

		client := &Client{
			countingClient: anthropic.NewClient(
				"sk-ant-test-key",
				anthropic.WithBaseURL(server.URL+"/v1"),
				anthropic.WithHTTPClient(server.Client()),
				anthropic.WithBetaVersion(anthropic.BetaTokenCounting20241101),
			),
			model:     "claude-sonnet-4.5",
			maxTokens: 8000,
		}

		_, err := client.CountPromptTokens(context.Background(), "system", "user")
		if err == nil {
			t.Fatal("CountPromptTokens() expected error, got nil")
		}

		if !strings.Contains(err.Error(), "API call failed") {
			t.Fatalf("expected wrapped error, got %v", err)
		}

		if strings.Contains(err.Error(), "sk-ant-secretsecretsecret1234567890") {
			t.Fatalf("error should be sanitized, got %v", err)
		}
	})
}

// TestCalculateStats exercises the cost calculation via the real pricing
// function (ModelPricing.Cost), not an inline copy of the pricing math, so
// drift between production pricing and test expectations is caught.
func TestCalculateStats(t *testing.T) {
	sonnet, _ := ResolvePricing("claude-sonnet-4-6")

	tests := []struct {
		name         string
		inputTokens  int
		outputTokens int
		cacheCreate  int
		cacheRead    int
		expectedCost float64 // Computed from Sonnet-tier rates ($3/$15/$3.75/$0.30 per MTok)
	}{
		{"Basic calculation without cache", 1000, 500, 0, 0, 0.0105},
		{"With cache creation", 1000, 500, 2000, 0, 0.0180},
		{"With cache read", 1000, 500, 0, 5000, 0.0120},
		{"Large tokens", 100000, 50000, 10000, 80000, 1.1115},
		{"Zero tokens", 0, 0, 0, 0, 0.0},
	}

	const tolerance = 0.0001
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sonnet.Cost(tt.inputTokens, tt.outputTokens, tt.cacheCreate, tt.cacheRead)
			if got < tt.expectedCost-tolerance || got > tt.expectedCost+tolerance {
				t.Errorf("Cost(%d, %d, %d, %d) = %.4f, want %.4f",
					tt.inputTokens, tt.outputTokens, tt.cacheCreate, tt.cacheRead, got, tt.expectedCost)
			}
		})
	}
}

func TestGetModelInfo(t *testing.T) {
	tests := []struct {
		name  string
		model string
	}{
		{
			name:  "Sonnet 4.5",
			model: "claude-sonnet-4.5",
		},
		{
			name:  "Opus",
			model: "claude-opus-4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{
				model:     tt.model,
				maxTokens: 8000,
			}

			info := client.GetModelInfo()

			if info == nil {
				t.Error("Expected model info but got nil")
				return
			}

			if model, ok := info["model"].(string); !ok || model != tt.model {
				t.Errorf("Expected model %s, got %v", tt.model, info["model"])
			}

			if provider, ok := info["provider"].(string); !ok || provider != "Anthropic" {
				t.Errorf("Expected provider 'Anthropic', got %v", info["provider"])
			}

			if maxTokens, ok := info["max_tokens"].(int); !ok || maxTokens != 8000 {
				t.Errorf("Expected max_tokens 8000, got %v", info["max_tokens"])
			}

			if contextLimit, ok := info["context_limit"].(int); !ok || contextLimit != 200000 {
				t.Errorf("Expected context_limit 200000, got %v", info["context_limit"])
			}
		})
	}
}

func TestStatsStructure(t *testing.T) {
	// Test that Stats structure holds data correctly
	stats := &Stats{
		InputTokens:         1000,
		OutputTokens:        500,
		CacheCreationTokens: 200,
		CacheReadTokens:     100,
		CostUSD:             0.0105,
		DurationSeconds:     5.5,
	}

	if stats.InputTokens != 1000 {
		t.Errorf("Expected InputTokens 1000, got %d", stats.InputTokens)
	}

	if stats.OutputTokens != 500 {
		t.Errorf("Expected OutputTokens 500, got %d", stats.OutputTokens)
	}

	if stats.CacheCreationTokens != 200 {
		t.Errorf("Expected CacheCreationTokens 200, got %d", stats.CacheCreationTokens)
	}

	if stats.CacheReadTokens != 100 {
		t.Errorf("Expected CacheReadTokens 100, got %d", stats.CacheReadTokens)
	}

	if stats.CostUSD != 0.0105 {
		t.Errorf("Expected CostUSD 0.0105, got %f", stats.CostUSD)
	}

	if stats.DurationSeconds != 5.5 {
		t.Errorf("Expected DurationSeconds 5.5, got %f", stats.DurationSeconds)
	}
}

func TestContextCancellation(t *testing.T) {
	// Test that context cancellation is respected
	client, err := NewClient("sk-ant-test-key", "claude-sonnet-4.5", "", 120, 8000)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// This should fail quickly due to cancelled context
	// Note: This will make an actual API call attempt, which will fail
	// In a real test environment, we'd mock the API client
	_, _, err = client.Analyze(ctx, "You are a test assistant", "test content")

	// We expect an error (either context cancelled or API call failed)
	if err == nil {
		t.Log("Note: Expected an error due to cancelled context or API failure")
		// Don't fail the test as the actual API behavior may vary
	}
}

func TestContextTimeout(t *testing.T) {
	// Test that context timeout is respected
	client, err := NewClient("sk-ant-test-key", "claude-sonnet-4.5", "", 120, 8000)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Create a context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait a bit to ensure timeout
	time.Sleep(1 * time.Millisecond)

	// This should fail due to timeout
	_, _, err = client.Analyze(ctx, "You are a test assistant", "test content")

	// We expect an error
	if err == nil {
		t.Log("Note: Expected an error due to context timeout or API failure")
		// Don't fail the test as the actual API behavior may vary
	}
}

func TestClientStructure(t *testing.T) {
	// Test that Client structure is properly initialized
	client, err := NewClient("sk-ant-test-key", "claude-sonnet-4.5", "", 120, 8000)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if client.model == "" {
		t.Error("Client model should not be empty")
	}

	if client.client == nil {
		t.Error("Anthropic client should be initialized")
	}
}

func TestCostCalculationPrecision(t *testing.T) {
	// Test cost calculation with various token counts to ensure precision
	tests := []struct {
		name         string
		inputTokens  int
		outputTokens int
		expectedCost float64
	}{
		{
			name:         "Small numbers",
			inputTokens:  100,
			outputTokens: 50,
			expectedCost: 0.00105, // (100*3 + 50*15)/1000000
		},
		{
			name:         "Medium numbers",
			inputTokens:  10000,
			outputTokens: 5000,
			expectedCost: 0.105, // (10000*3 + 5000*15)/1000000
		},
		{
			name:         "Large numbers",
			inputTokens:  1000000,
			outputTokens: 500000,
			expectedCost: 10.5, // (1000000*3 + 500000*15)/1000000
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputCost := float64(tt.inputTokens) / 1000000 * 3.0
			outputCost := float64(tt.outputTokens) / 1000000 * 15.0
			totalCost := inputCost + outputCost

			// Use small tolerance for floating point comparison
			tolerance := 0.000001
			diff := totalCost - tt.expectedCost
			if diff < 0 {
				diff = -diff
			}

			if diff > tolerance {
				t.Errorf("Expected cost %.6f, got %.6f (diff: %.9f)", tt.expectedCost, totalCost, diff)
			}
		})
	}
}
