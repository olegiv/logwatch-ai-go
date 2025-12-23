package notification

import (
	"fmt"
	"strings"
	"testing"
	"time"

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
	message := client.formatMessage(analysis, stats, "logwatch", "")

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
			name:     "All special chars including backslash",
			input:    "\\_*[]()~`>#+-=|{}.!:",
			expected: "\\\\\\_\\*\\[\\]\\(\\)\\~\\`\\>\\#\\+\\-\\=\\|\\{\\}\\.\\!\\:",
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

	message := client.formatMessage(analysis, stats, "logwatch", "")

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

	message := client.formatMessage(analysis, stats, "logwatch", "")

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

	message := client.formatMessage(analysis, stats, "logwatch", "")

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

			message := client.formatMessage(analysis, stats, "logwatch", "")

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

	message := client.formatMessage(analysis, stats, "logwatch", "")

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

func TestIsRateLimitError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "429 error",
			err:  fmt.Errorf("telegram: 429 too many requests"),
			want: true,
		},
		{
			name: "too many requests error",
			err:  fmt.Errorf("too many requests: retry after 30"),
			want: true,
		},
		{
			name: "other error",
			err:  fmt.Errorf("connection timeout"),
			want: false,
		},
		{
			name: "network error",
			err:  fmt.Errorf("failed to connect to api.telegram.org"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRateLimitError(tt.err)
			if got != tt.want {
				t.Errorf("isRateLimitError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetLogSourceDisplayName(t *testing.T) {
	tests := []struct {
		name           string
		logSourceType  string
		expectedResult string
	}{
		{
			name:           "logwatch source",
			logSourceType:  "logwatch",
			expectedResult: "Logwatch",
		},
		{
			name:           "drupal_watchdog source",
			logSourceType:  "drupal_watchdog",
			expectedResult: "Drupal Watchdog",
		},
		{
			name:           "unknown source",
			logSourceType:  "unknown",
			expectedResult: "Log",
		},
		{
			name:           "empty source",
			logSourceType:  "",
			expectedResult: "Log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getLogSourceDisplayName(tt.logSourceType)
			if result != tt.expectedResult {
				t.Errorf("Expected '%s', got '%s'", tt.expectedResult, result)
			}
		})
	}
}

func TestFormatMessage_DrupalWatchdogHeader(t *testing.T) {
	client := &TelegramClient{
		hostname: "test-server",
	}

	analysis := &ai.Analysis{
		SystemStatus:    "Good",
		Summary:         "Drupal site running well",
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

	message := client.formatMessage(analysis, stats, "drupal_watchdog", "")

	// Should contain Drupal Watchdog in header
	if !strings.Contains(message, "Drupal Watchdog Report") {
		t.Error("Message should contain 'Drupal Watchdog Report' in header")
	}
}

func TestFormatMessage_WithSiteName(t *testing.T) {
	client := &TelegramClient{
		hostname: "test-server",
	}

	analysis := &ai.Analysis{
		SystemStatus:    "Good",
		Summary:         "Drupal site running well",
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

	// Test with site name
	message := client.formatMessage(analysis, stats, "drupal_watchdog", "Production Site")

	// Should contain site name in header
	if !strings.Contains(message, "Production Site") {
		t.Error("Message should contain site name in header")
	}

	// Should contain Drupal Watchdog in header
	if !strings.Contains(message, "Drupal Watchdog Report") {
		t.Error("Message should contain 'Drupal Watchdog Report' in header")
	}
}

func TestFormatMessage_WithoutSiteName(t *testing.T) {
	client := &TelegramClient{
		hostname: "test-server",
	}

	analysis := &ai.Analysis{
		SystemStatus:    "Good",
		Summary:         "Log analysis completed",
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

	// Test without site name (empty string)
	message := client.formatMessage(analysis, stats, "logwatch", "")

	// Should contain Logwatch in header but no separator for site name
	if !strings.Contains(message, "Logwatch Report") {
		t.Error("Message should contain 'Logwatch Report' in header")
	}

	// Should not contain the site name separator
	if strings.Contains(message, "\\-") && strings.Contains(message, "Logwatch Report\\*") {
		// This is checking we don't have "Logwatch Report - " with an empty site name
		t.Error("Message should not contain site name separator when site name is empty")
	}
}

func TestExtractRetryAfter(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "nil error",
			err:  nil,
			want: 0,
		},
		{
			name: "retry after 30",
			err:  fmt.Errorf("too Many Requests: retry after 30"),
			want: 30,
		},
		{
			name: "retry after 60",
			err:  fmt.Errorf("telegram: 429 Too Many Requests: retry after 60 seconds"),
			want: 60,
		},
		{
			name: "retry after 5",
			err:  fmt.Errorf("error: retry after 5"),
			want: 5,
		},
		{
			name: "no retry after value - defaults to 30",
			err:  fmt.Errorf("too Many Requests"),
			want: 30,
		},
		{
			name: "other error - defaults to 30",
			err:  fmt.Errorf("connection timeout"),
			want: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRetryAfter(tt.err)
			if got != tt.want {
				t.Errorf("extractRetryAfter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEscapeMarkdown_Backslashes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Backslash before dot - from Claude output",
			input:    `path\.config`,
			expected: `path\\\.config`, // \ -> \\, . -> \. = 3 backslashes before dot
		},
		{
			name:     "Backslash before pipe",
			input:    `data\|value`,
			expected: `data\\\|value`, // \ -> \\, | -> \| = 3 backslashes before pipe
		},
		{
			name:     "Plain backslash followed by letter",
			input:    `path\file`,
			expected: `path\\file`, // \ -> \\ = 2 backslashes
		},
		{
			name:     "Windows path with colons and backslashes",
			input:    `C:\Users\test`,
			expected: `C\:\\Users\\test`, // : -> \:, \ -> \\
		},
		{
			name:     "No backslash - dot only",
			input:    "test.value",
			expected: `test\.value`,
		},
		{
			name:     "No backslash - pipe only",
			input:    "data|pipe",
			expected: `data\|pipe`,
		},
		{
			name:     "SQL-like content with pipe",
			input:    "SELECT * FROM users|archived WHERE id > 5",
			expected: `SELECT \* FROM users\|archived WHERE id \> 5`,
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Double backslash",
			input:    `test\\value`,
			expected: `test\\\\value`, // \\ -> \\\\ = 4 backslashes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeMarkdown(tt.input)
			if result != tt.expected {
				t.Errorf("escapeMarkdown(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestWaitForRateLimit(t *testing.T) {
	tests := []struct {
		name            string
		lastMessageTime time.Time
		expectWait      bool
	}{
		{
			name:            "Zero time - no wait",
			lastMessageTime: time.Time{},
			expectWait:      false,
		},
		{
			name:            "Recent message - should wait",
			lastMessageTime: time.Now(),
			expectWait:      true,
		},
		{
			name:            "Old message - no wait",
			lastMessageTime: time.Now().Add(-2 * time.Second),
			expectWait:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &TelegramClient{
				hostname:        "test-host",
				lastMessageTime: tt.lastMessageTime,
			}

			start := time.Now()
			client.waitForRateLimit()
			elapsed := time.Since(start)

			if tt.expectWait {
				// Should have waited some time (at least a few hundred ms)
				if elapsed < 100*time.Millisecond {
					// Only fail if lastMessageTime is very recent
					if time.Since(tt.lastMessageTime) < 500*time.Millisecond {
						t.Errorf("Expected to wait for rate limit, but returned in %v", elapsed)
					}
				}
			} else {
				// Should return quickly
				if elapsed > 100*time.Millisecond {
					t.Errorf("Expected no wait, but waited %v", elapsed)
				}
			}
		})
	}
}

func TestTelegramClient_ChannelFields(t *testing.T) {
	// Test that channel and hostname fields are accessible
	// Note: We can't test GetBotInfo fully without a real bot connection
	// because it accesses bot.Self.UserName which requires a real bot
	client := &TelegramClient{
		archiveChannel: -1001234567890,
		alertsChannel:  -1009876543210,
		hostname:       "test-server",
	}

	if client.archiveChannel != -1001234567890 {
		t.Errorf("Expected archive_channel -1001234567890, got %v", client.archiveChannel)
	}
	if client.alertsChannel != -1009876543210 {
		t.Errorf("Expected alerts_channel -1009876543210, got %v", client.alertsChannel)
	}
	if client.hostname != "test-server" {
		t.Errorf("Expected hostname test-server, got %v", client.hostname)
	}
}

func TestFormatMessage_Provider(t *testing.T) {
	client := &TelegramClient{
		hostname: "test-server",
	}

	analysis := &ai.Analysis{
		SystemStatus:    "Good",
		Summary:         "Test summary",
		CriticalIssues:  []string{},
		Warnings:        []string{},
		Recommendations: []string{},
		Metrics:         map[string]interface{}{},
	}

	tests := []struct {
		name           string
		provider       string
		model          string
		expectContains string
	}{
		{
			name:           "Anthropic provider",
			provider:       "Anthropic",
			model:          "claude-sonnet-4-5-20250929",
			expectContains: "Anthropic",
		},
		{
			name:           "Ollama provider",
			provider:       "Ollama",
			model:          "llama3.3:latest",
			expectContains: "Ollama",
		},
		{
			name:           "LM Studio provider",
			provider:       "LM Studio",
			model:          "local-model",
			expectContains: "LM Studio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &ai.Stats{
				Model:           tt.model,
				Provider:        tt.provider,
				InputTokens:     1000,
				OutputTokens:    500,
				CostUSD:         0.01,
				DurationSeconds: 5.0,
			}

			message := client.formatMessage(analysis, stats, "logwatch", "")

			if !strings.Contains(message, escapeMarkdown(tt.provider)) {
				t.Errorf("Message should contain provider '%s'", tt.provider)
			}
		})
	}
}

func TestSplitMessage_EdgeCases(t *testing.T) {
	client := &TelegramClient{
		hostname: "test-server",
	}

	tests := []struct {
		name          string
		message       string
		minParts      int
		maxPartLength int
	}{
		{
			name:          "Message exactly at limit",
			message:       strings.Repeat("a", maxMessageLength),
			minParts:      1,
			maxPartLength: maxMessageLength,
		},
		{
			name:          "Message one char over limit",
			message:       strings.Repeat("a", maxMessageLength+1),
			minParts:      2,
			maxPartLength: maxMessageLength,
		},
		{
			name:          "Message with newlines near limit",
			message:       strings.Repeat("short\n", maxMessageLength/6),
			minParts:      1,
			maxPartLength: maxMessageLength,
		},
		{
			name:          "Very long single line",
			message:       strings.Repeat("x", maxMessageLength*2+100),
			minParts:      3,
			maxPartLength: maxMessageLength,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := client.splitMessage(tt.message)

			if len(parts) < tt.minParts {
				t.Errorf("Expected at least %d parts, got %d", tt.minParts, len(parts))
			}

			for i, part := range parts {
				if len(part) > tt.maxPartLength {
					t.Errorf("Part %d exceeds max length: %d > %d", i, len(part), tt.maxPartLength)
				}
			}
		})
	}
}

func TestFormatMessage_NoEntriesReport(t *testing.T) {
	tests := []struct {
		name           string
		logSourceType  string
		siteName       string
		expectContains []string
	}{
		{
			name:          "Logwatch without site name",
			logSourceType: "logwatch",
			siteName:      "",
			expectContains: []string{
				"Logwatch Report",
				"No Entries Found",
				"test-server",
			},
		},
		{
			name:          "Drupal with site name",
			logSourceType: "drupal_watchdog",
			siteName:      "Production",
			expectContains: []string{
				"Drupal Watchdog Report",
				"Production",
				"No Entries Found",
			},
		},
	}

	// Note: We can't actually call SendNoEntriesReport without a real bot,
	// but we can test the message formatting logic by examining the internal functions
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test getLogSourceDisplayName which is used in the report
			displayName := getLogSourceDisplayName(tt.logSourceType)
			expectedDisplay := ""
			if tt.logSourceType == "logwatch" {
				expectedDisplay = "Logwatch"
			} else if tt.logSourceType == "drupal_watchdog" {
				expectedDisplay = "Drupal Watchdog"
			}
			if displayName != expectedDisplay {
				t.Errorf("Expected display name %q, got %q", expectedDisplay, displayName)
			}
		})
	}
}

func TestConstants(t *testing.T) {
	// Verify constants have sensible values
	if maxMessageLength <= 0 {
		t.Error("maxMessageLength should be positive")
	}
	if maxMessageLength > 10000 {
		t.Error("maxMessageLength seems too large for Telegram")
	}

	if minMessageInterval <= 0 {
		t.Error("minMessageInterval should be positive")
	}
	if minMessageInterval > 5*time.Second {
		t.Error("minMessageInterval seems too long")
	}

	if maxRetries <= 0 {
		t.Error("maxRetries should be positive")
	}
	if maxRetries > 10 {
		t.Error("maxRetries seems too high")
	}

	if baseRetryDelay <= 0 {
		t.Error("baseRetryDelay should be positive")
	}
}
