package notification

import (
	"fmt"
	"strings"
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

func TestEscapeMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Special characters",
			input:    "Test: value = 100%",
			expected: "Test\\: value \\= 100%",
		},
		{
			name:     "Dots and exclamation",
			input:    "Hello! This is a test.",
			expected: "Hello\\! This is a test\\.",
		},
		{
			name:     "All special chars",
			input:    "_*[]()~`>#+-=|{}.!:",
			expected: "\\_\\*\\[\\]\\(\\)\\~\\`\\>\\#\\+\\-\\=\\|\\{\\}\\.\\!\\:",
		},
		{
			name:     "No special chars",
			input:    "Plain text",
			expected: "Plain text",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeMarkdown(tt.input)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestSplitMessage(t *testing.T) {
	client := &TelegramClient{
		hostname: "test-server",
	}

	tests := []struct {
		name           string
		message        string
		expectedParts  int
		checkFirstPart func(string) bool
	}{
		{
			name:          "Short message",
			message:       "This is a short message",
			expectedParts: 1,
			checkFirstPart: func(s string) bool {
				return s == "This is a short message"
			},
		},
		{
			name:          "Long message",
			message:       strings.Repeat("Line\n", 1000),
			expectedParts: 2, // Should be split into multiple parts
			checkFirstPart: func(s string) bool {
				return len(s) <= maxMessageLength
			},
		},
		{
			name:          "Empty message",
			message:       "",
			expectedParts: 1,
		},
		{
			name:          "Single very long line",
			message:       strings.Repeat("a", maxMessageLength+100),
			expectedParts: 2,
			checkFirstPart: func(s string) bool {
				return len(s) == maxMessageLength
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.splitMessage(tt.message)

			if len(result) < tt.expectedParts {
				t.Errorf("Expected at least %d parts, got %d", tt.expectedParts, len(result))
			}

			// Verify each part is within limits
			for i, part := range result {
				if len(part) > maxMessageLength {
					t.Errorf("Part %d exceeds max length: %d > %d", i, len(part), maxMessageLength)
				}
			}

			if tt.checkFirstPart != nil && len(result) > 0 {
				if !tt.checkFirstPart(result[0]) {
					t.Error("First part check failed")
				}
			}
		})
	}
}

func TestFormatMessage_EmptyFields(t *testing.T) {
	client := &TelegramClient{
		hostname: "test-server",
	}

	analysis := &ai.Analysis{
		SystemStatus:    "Excellent",
		Summary:         "All good",
		CriticalIssues:  []string{},
		Warnings:        []string{},
		Recommendations: []string{},
		Metrics:         map[string]interface{}{},
	}

	stats := &ai.Stats{
		InputTokens:         100,
		OutputTokens:        50,
		CacheCreationTokens: 0,
		CacheReadTokens:     0,
		CostUSD:             0.001,
		DurationSeconds:     1.5,
	}

	message := client.formatMessage(analysis, stats)

	if message == "" {
		t.Error("Message should not be empty")
	}

	// Should contain status
	if !strings.Contains(message, "Excellent") {
		t.Error("Message should contain status")
	}

	// Should not contain empty sections
	if strings.Contains(message, "Critical Issues (0)") {
		t.Error("Should not show empty critical issues section")
	}
}

func TestFormatMessage_WithCacheTokens(t *testing.T) {
	client := &TelegramClient{
		hostname: "test-server",
	}

	analysis := &ai.Analysis{
		SystemStatus:    "Good",
		Summary:         "Test",
		CriticalIssues:  []string{},
		Warnings:        []string{},
		Recommendations: []string{},
		Metrics:         map[string]interface{}{},
	}

	stats := &ai.Stats{
		InputTokens:         1000,
		OutputTokens:        500,
		CacheCreationTokens: 200,
		CacheReadTokens:     100,
		CostUSD:             0.01,
		DurationSeconds:     5.0,
	}

	message := client.formatMessage(analysis, stats)

	// Should contain cache read info when cache is used
	if !strings.Contains(message, "Cache Read") {
		t.Error("Message should contain cache read info when cache tokens > 0")
	}
}

func TestFormatMessage_WithoutCacheTokens(t *testing.T) {
	client := &TelegramClient{
		hostname: "test-server",
	}

	analysis := &ai.Analysis{
		SystemStatus:    "Good",
		Summary:         "Test",
		CriticalIssues:  []string{},
		Warnings:        []string{},
		Recommendations: []string{},
		Metrics:         map[string]interface{}{},
	}

	stats := &ai.Stats{
		InputTokens:         1000,
		OutputTokens:        500,
		CacheCreationTokens: 0,
		CacheReadTokens:     0,
		CostUSD:             0.01,
		DurationSeconds:     5.0,
	}

	message := client.formatMessage(analysis, stats)

	// Should not contain cache info when no cache is used
	if strings.Contains(message, "Cache Read") {
		t.Error("Message should not contain cache info when no cache tokens")
	}
}

func TestFormatMessage_AllStatuses(t *testing.T) {
	client := &TelegramClient{
		hostname: "test-server",
	}

	statuses := []string{"Excellent", "Good", "Satisfactory", "Bad", "Awful"}

	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			analysis := &ai.Analysis{
				SystemStatus:    status,
				Summary:         "Test summary",
				CriticalIssues:  []string{},
				Warnings:        []string{},
				Recommendations: []string{},
				Metrics:         map[string]interface{}{},
			}

			stats := &ai.Stats{
				InputTokens:     1000,
				OutputTokens:    500,
				CostUSD:         0.01,
				DurationSeconds: 5.0,
			}

			message := client.formatMessage(analysis, stats)

			if !strings.Contains(message, status) {
				t.Errorf("Message should contain status '%s'", status)
			}

			// Check that emoji is present
			emoji := ai.GetStatusEmoji(status)
			if !strings.Contains(message, emoji) {
				t.Errorf("Message should contain emoji for status '%s'", status)
			}
		})
	}
}

func TestTelegramClient_Structure(t *testing.T) {
	client := &TelegramClient{
		archiveChannel: -1001234567890,
		alertsChannel:  -1009876543210,
		hostname:       "test-host",
	}

	if client.archiveChannel != -1001234567890 {
		t.Error("Archive channel not set correctly")
	}

	if client.alertsChannel != -1009876543210 {
		t.Error("Alerts channel not set correctly")
	}

	if client.hostname != "test-host" {
		t.Error("Hostname not set correctly")
	}
}

func TestFormatMessage_MultipleIssues(t *testing.T) {
	client := &TelegramClient{
		hostname: "test-server",
	}

	analysis := &ai.Analysis{
		SystemStatus: "Bad",
		Summary:      "Multiple issues detected",
		CriticalIssues: []string{
			"Critical issue 1",
			"Critical issue 2",
			"Critical issue 3",
		},
		Warnings: []string{
			"Warning 1",
			"Warning 2",
		},
		Recommendations: []string{
			"Fix issue 1",
			"Fix issue 2",
			"Fix issue 3",
		},
		Metrics: map[string]interface{}{
			"failedLogins": float64(10),
			"errorCount":   float64(5),
			"diskUsage":    "95%",
		},
	}

	stats := &ai.Stats{
		InputTokens:     2000,
		OutputTokens:    1000,
		CostUSD:         0.02,
		DurationSeconds: 8.5,
	}

	message := client.formatMessage(analysis, stats)

	// Verify all critical issues are present
	for i, issue := range analysis.CriticalIssues {
		if !strings.Contains(message, escapeMarkdown(issue)) {
			t.Errorf("Critical issue %d not found in message", i)
		}
	}

	// Verify all warnings are present
	for i, warning := range analysis.Warnings {
		if !strings.Contains(message, escapeMarkdown(warning)) {
			t.Errorf("Warning %d not found in message", i)
		}
	}

	// Verify all recommendations are present
	for i, rec := range analysis.Recommendations {
		if !strings.Contains(message, escapeMarkdown(rec)) {
			t.Errorf("Recommendation %d not found in message", i)
		}
	}

	// Verify metrics are present
	for key := range analysis.Metrics {
		if !strings.Contains(message, escapeMarkdown(key)) {
			t.Errorf("Metric key '%s' not found in message", key)
		}
	}
}
