package notification

import (
	"fmt"
	"testing"

	"github.com/olegiv/logwatch-ai-go/internal/ai"
)

func TestFormatMessage(t *testing.T) {
	// Create a mock telegram client
	client := &TelegramClient{
		hostname: "test-server",
	}

	// Create test analysis
	analysis := &ai.Analysis{
		SystemStatus: "Good",
		Summary:      "System is running well. No major issues detected.",
		CriticalIssues: []string{
			"Critical issue 1 with dots...",
		},
		Warnings: []string{
			"Warning with special chars: test-warning",
		},
		Recommendations: []string{
			"Run command: apt-get update",
			"Check disk space at 85.5%",
		},
		Metrics: map[string]interface{}{
			"failedLogins": 5,
			"diskUsage":    "85.5% on /var",
			"errorCount":   0,
		},
	}

	stats := &ai.Stats{
		InputTokens:         1000,
		OutputTokens:        500,
		CacheCreationTokens: 200,
		CacheReadTokens:     100,
		CostUSD:             0.008604,
		DurationSeconds:     9.967695458,
	}

	// Format message
	message := client.formatMessage(analysis, stats)

	// Print the message to see what it looks like
	fmt.Println("=== FORMATTED MESSAGE ===")
	fmt.Println(message)
	fmt.Println("=== END MESSAGE ===")

	// Check that special characters are escaped
	// In MarkdownV2, these need to be escaped: _*[]()~`>#+-=|{}.!
	// Verify some key escaping
	if !containsEscaped(message, ":") {
		t.Error("Colons should be escaped with \\:")
	}
}

func containsEscaped(s, char string) bool {
	escaped := "\\" + char
	for i := 0; i < len(s)-1; i++ {
		if s[i:i+len(escaped)] == escaped {
			return true
		}
	}
	return false
}
