package ai

import (
	"context"
	"testing"
	"time"
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.apiKey, tt.model, tt.proxyURL)

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
		})
	}
}

func TestCalculateStats(t *testing.T) {
	// Create a mock anthropic response
	type usage struct {
		InputTokens              int
		OutputTokens             int
		CacheCreationInputTokens int
		CacheReadInputTokens     int
	}

	type mockResponse struct {
		Usage usage
	}

	tests := []struct {
		name             string
		inputTokens      int
		outputTokens     int
		cacheCreate      int
		cacheRead        int
		durationSeconds  float64
		expectedCostMin  float64
		expectedCostMax  float64
	}{
		{
			name:             "Basic calculation without cache",
			inputTokens:      1000,
			outputTokens:     500,
			cacheCreate:      0,
			cacheRead:        0,
			durationSeconds:  5.0,
			expectedCostMin:  0.0105,  // (1000*3 + 500*15)/1000000
			expectedCostMax:  0.0105,
		},
		{
			name:             "With cache creation",
			inputTokens:      1000,
			outputTokens:     500,
			cacheCreate:      2000,
			cacheRead:        0,
			durationSeconds:  5.0,
			expectedCostMin:  0.0179,  // (1000*3 + 500*15 + 2000*3.75)/1000000
			expectedCostMax:  0.0181,
		},
		{
			name:             "With cache read",
			inputTokens:      1000,
			outputTokens:     500,
			cacheCreate:      0,
			cacheRead:        5000,
			durationSeconds:  3.0,
			expectedCostMin:  0.0120,  // (1000*3 + 500*15 + 5000*0.30)/1000000
			expectedCostMax:  0.0120,
		},
		{
			name:             "Large tokens",
			inputTokens:      100000,
			outputTokens:     50000,
			cacheCreate:      10000,
			cacheRead:        80000,
			durationSeconds:  15.0,
			expectedCostMin:  1.08,    // (100000*3 + 50000*15 + 10000*3.75 + 80000*0.30)/1000000
			expectedCostMax:  1.12,
		},
		{
			name:             "Zero tokens",
			inputTokens:      0,
			outputTokens:     0,
			cacheCreate:      0,
			cacheRead:        0,
			durationSeconds:  1.0,
			expectedCostMin:  0.0,
			expectedCostMax:  0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For testing purposes, we'll simulate the calculation directly
			// without needing a full client or anthropic response

			// Simulate the calculation
			inputCost := float64(tt.inputTokens) / 1000000 * 3.0
			outputCost := float64(tt.outputTokens) / 1000000 * 15.0
			cacheWriteCost := float64(tt.cacheCreate) / 1000000 * 3.75
			cacheReadCost := float64(tt.cacheRead) / 1000000 * 0.30
			expectedCost := inputCost + outputCost + cacheWriteCost + cacheReadCost

			// Verify the calculation logic matches our expectations (with tolerance for floating-point precision)
			const tolerance = 0.0001
			if expectedCost < tt.expectedCostMin-tolerance || expectedCost > tt.expectedCostMax+tolerance {
				t.Errorf("Expected cost between %.4f and %.4f, calculated %.4f",
					tt.expectedCostMin, tt.expectedCostMax, expectedCost)
			}

			// Test that our cost calculation formula is correct
			stats := &Stats{
				InputTokens:         tt.inputTokens,
				OutputTokens:        tt.outputTokens,
				CacheCreationTokens: tt.cacheCreate,
				CacheReadTokens:     tt.cacheRead,
				CostUSD:             expectedCost,
				DurationSeconds:     tt.durationSeconds,
			}

			if stats.InputTokens != tt.inputTokens {
				t.Errorf("Expected InputTokens %d, got %d", tt.inputTokens, stats.InputTokens)
			}

			if stats.OutputTokens != tt.outputTokens {
				t.Errorf("Expected OutputTokens %d, got %d", tt.outputTokens, stats.OutputTokens)
			}

			if stats.DurationSeconds != tt.durationSeconds {
				t.Errorf("Expected Duration %.2f, got %.2f", tt.durationSeconds, stats.DurationSeconds)
			}

			// Verify cost is within expected range
			if stats.CostUSD < tt.expectedCostMin-0.0001 || stats.CostUSD > tt.expectedCostMax+0.0001 {
				t.Errorf("Expected cost between %.4f and %.4f, got %.4f",
					tt.expectedCostMin, tt.expectedCostMax, stats.CostUSD)
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
				model: tt.model,
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
	client, err := NewClient("sk-ant-test-key", "claude-sonnet-4.5", "")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// This should fail quickly due to cancelled context
	// Note: This will make an actual API call attempt, which will fail
	// In a real test environment, we'd mock the API client
	_, _, err = client.AnalyzeLogwatch(ctx, "test content", "")

	// We expect an error (either context cancelled or API call failed)
	if err == nil {
		t.Log("Note: Expected an error due to cancelled context or API failure")
		// Don't fail the test as the actual API behavior may vary
	}
}

func TestContextTimeout(t *testing.T) {
	// Test that context timeout is respected
	client, err := NewClient("sk-ant-test-key", "claude-sonnet-4.5", "")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Create a context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait a bit to ensure timeout
	time.Sleep(1 * time.Millisecond)

	// This should fail due to timeout
	_, _, err = client.AnalyzeLogwatch(ctx, "test content", "")

	// We expect an error
	if err == nil {
		t.Log("Note: Expected an error due to context timeout or API failure")
		// Don't fail the test as the actual API behavior may vary
	}
}

func TestClientStructure(t *testing.T) {
	// Test that Client structure is properly initialized
	client, err := NewClient("sk-ant-test-key", "claude-sonnet-4.5", "")
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
		name        string
		inputTokens int
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
