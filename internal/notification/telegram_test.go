package notification

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/olegiv/logwatch-ai-go/internal/ai"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type telegramHTTPClientFunc func(*http.Request) (*http.Response, error)

func (f telegramHTTPClientFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func telegramJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func telegramRateLimitError(retryAfter int) error {
	err := &tgbotapi.Error{Code: 429}
	err.RetryAfter = retryAfter

	return err
}

func TestNewTelegramClientSanitizesTransportURL(t *testing.T) {
	t.Parallel()

	const token = "123456:ABC-def_GHI"
	baseClient := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network unavailable")
		}),
	}
	client, err := newTelegramClient(context.Background(), token, -1001, 0, baseClient)
	if client != nil {
		_ = client.Close()
		t.Fatal("newTelegramClient() returned a client after transport failure")
	}
	if err == nil {
		t.Fatal("newTelegramClient() error = nil")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "ABC-def_GHI") {
		t.Fatalf("newTelegramClient() leaked token in error: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("newTelegramClient() error was not visibly redacted: %v", err)
	}
}

func TestSendAnalysisReportRecordsArchiveSideEffectWhenAlertsFail(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	archiveRequests := 0
	alertRequests := 0
	httpClient := telegramHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/getMe") {
			return telegramJSONResponse(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"test","username":"test_bot"}}`), nil
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		switch values.Get("chat_id") {
		case "-1001":
			archiveRequests++
			return telegramJSONResponse(`{"ok":true,"result":{"message_id":1}}`), nil
		case "-1002":
			alertRequests++
			cancel()
			return nil, errors.New("alerts unavailable")
		default:
			return nil, fmt.Errorf("unexpected chat_id %q", values.Get("chat_id"))
		}
	})
	bot, err := tgbotapi.NewBotAPIWithClient("123456:ABC-def_GHI", tgbotapi.APIEndpoint, httpClient)
	if err != nil {
		t.Fatalf("NewBotAPIWithClient() error = %v", err)
	}
	client := &TelegramClient{
		bot:            bot,
		archiveChannel: -1001,
		alertsChannel:  -1002,
		hostname:       "test-host",
		ctx:            ctx,
	}
	analysis := &ai.Analysis{SystemStatus: "Bad", Summary: "incident"}
	stats := &ai.Stats{Provider: "test", Model: "test"}

	err = client.SendAnalysisReport(analysis, stats, "logwatch", "")
	if err == nil {
		t.Fatal("SendAnalysisReport() error = nil, want failed alert delivery")
	}
	if !IsPartialDelivery(err) {
		t.Fatalf("SendAnalysisReport() error = %v, want durable archive side effect", err)
	}
	var partial *PartialDeliveryError
	if !errors.As(err, &partial) || partial.DeliveredParts < 1 || partial.DeliveredParts >= partial.TotalParts {
		t.Fatalf("partial delivery metadata = %+v, want archive delivered and alerts missing", partial)
	}
	if !strings.Contains(err.Error(), "alerts channel") {
		t.Fatalf("SendAnalysisReport() error = %v, want alerts-channel context", err)
	}
	if archiveRequests != 1 || alertRequests != 1 {
		t.Fatalf("delivery requests archive=%d alerts=%d, want 1 each", archiveRequests, alertRequests)
	}
}

func TestSendAnalysisReportAlertsAfterPartialArchiveDelivery(t *testing.T) {
	t.Parallel()

	archiveDelivered := false
	alertRequests := 0
	httpClient := telegramHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/getMe") {
			return telegramJSONResponse(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"test","username":"test_bot"}}`), nil
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		switch values.Get("chat_id") {
		case "-1001":
			if !archiveDelivered {
				archiveDelivered = true
				return telegramJSONResponse(`{"ok":true,"result":{"message_id":1}}`), nil
			}
			// A one-second retry_after keeps the exhausted retry loop short.
			return telegramJSONResponse(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`), nil
		case "-1002":
			alertRequests++
			return telegramJSONResponse(`{"ok":true,"result":{"message_id":2}}`), nil
		default:
			return nil, fmt.Errorf("unexpected chat_id %q", values.Get("chat_id"))
		}
	})
	bot, err := tgbotapi.NewBotAPIWithClient("123456:ABC-def_GHI", tgbotapi.APIEndpoint, httpClient)
	if err != nil {
		t.Fatalf("NewBotAPIWithClient() error = %v", err)
	}
	client := &TelegramClient{
		bot:            bot,
		archiveChannel: -1001,
		alertsChannel:  -1002,
		hostname:       "test-host",
		ctx:            context.Background(),
	}
	analysis := &ai.Analysis{SystemStatus: "Bad", Summary: strings.Repeat("incident evidence line\n", 220)}
	stats := &ai.Stats{Provider: "test", Model: "test"}
	parts := len(client.splitMessage(client.formatMessage(analysis, stats, "logwatch", "")))
	if parts < 2 {
		t.Fatalf("report splits into %d parts, want at least 2", parts)
	}

	err = client.SendAnalysisReport(analysis, stats, "logwatch", "")
	var partial *PartialDeliveryError
	if !errors.As(err, &partial) {
		t.Fatalf("SendAnalysisReport() error = %v, want partial delivery", err)
	}
	if partial.DeliveredParts != 1+parts || partial.TotalParts != 2*parts {
		t.Fatalf("partial delivery metadata = %+v, want %d/%d parts", partial, 1+parts, 2*parts)
	}
	if alertRequests != parts {
		t.Fatalf("alert requests = %d, want all %d parts sent after the partial archive", alertRequests, parts)
	}
	if !strings.Contains(err.Error(), "archive partially delivered") || !strings.Contains(err.Error(), "alerts delivered") {
		t.Fatalf("SendAnalysisReport() error = %v, want archive and alerts outcomes", err)
	}
}

func TestSendToChannelRecordsPartsDeliveredBeforeRateLimitCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	requests := 0
	httpClient := telegramHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/getMe") {
			return telegramJSONResponse(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"test","username":"test_bot"}}`), nil
		}
		requests++
		cancel()
		return telegramJSONResponse(`{"ok":true,"result":{"message_id":1}}`), nil
	})
	bot, err := tgbotapi.NewBotAPIWithClient("123456:ABC-def_GHI", tgbotapi.APIEndpoint, httpClient)
	if err != nil {
		t.Fatalf("NewBotAPIWithClient() error = %v", err)
	}
	client := &TelegramClient{bot: bot, ctx: ctx}
	message := strings.Repeat("security evidence line\n", 500)

	err = client.sendToChannel(-1001, message)
	var partial *PartialDeliveryError
	if !errors.As(err, &partial) {
		t.Fatalf("sendToChannel() error = %v, want partial delivery", err)
	}
	if partial.DeliveredParts != 1 || partial.TotalParts < 2 {
		t.Fatalf("partial delivery metadata = %+v, want first part recorded", partial)
	}
	if requests != 1 {
		t.Fatalf("send requests = %d, want one delivered part before cancellation", requests)
	}
}

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
		Metrics: map[string]any{
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
		Metrics:         map[string]any{},
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
		Metrics:         map[string]any{},
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
		Metrics:         map[string]any{},
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
				Metrics:         map[string]any{},
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
		Metrics: map[string]any{
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
			name: "structured Telegram rate limit",
			err:  telegramRateLimitError(17),
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
			name:           "ocms source",
			logSourceType:  "ocms",
			expectedResult: "OCMS",
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
		Metrics:         map[string]any{},
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
		Metrics:         map[string]any{},
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
		Metrics:         map[string]any{},
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
			want: int(maxRetryAfter / time.Second),
		},
		{
			name: "retry after 5",
			err:  fmt.Errorf("error: retry after 5"),
			want: 5,
		},
		{
			name: "structured retry after",
			err:  telegramRateLimitError(17),
			want: 17,
		},
		{
			name: "structured retry after is capped",
			err:  telegramRateLimitError(86400),
			want: int(maxRetryAfter / time.Second),
		},
		{
			name: "text retry after is capped",
			err:  fmt.Errorf("too many requests: retry after 86400"),
			want: int(maxRetryAfter / time.Second),
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
			err := client.waitForRateLimit()
			elapsed := time.Since(start)
			if err != nil {
				t.Fatalf("waitForRateLimit() error = %v", err)
			}

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

func TestWaitForRateLimitHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &TelegramClient{
		ctx:             ctx,
		lastMessageTime: time.Now(),
	}
	start := time.Now()
	err := client.waitForRateLimit()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForRateLimit() error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("cancelled wait took %v", elapsed)
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
		Metrics:         map[string]any{},
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
			model:          "claude-haiku-4-5-20251001",
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

func TestSplitMessage_EscapePairBoundary(t *testing.T) {
	client := &TelegramClient{
		hostname: "test-server",
	}

	tests := []struct {
		name string
		raw  string
	}{
		{
			// "a" + 3000 escaped dots -> "a" + 6000 bytes of `\.` pairs; the
			// 4096-byte boundary lands between a backslash and its escaped char.
			name: "boundary splits backslash-dot pair",
			raw:  "a" + strings.Repeat(".", 3000),
		},
		{
			// Raw `\.` escapes to `\\\.`; the boundary lands inside an odd run
			// of three backslashes — the case a naive suffix check misses.
			name: "boundary splits odd backslash run",
			raw:  "a" + strings.Repeat(`\.`, 1500),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			escaped := escapeMarkdown(tt.raw)
			parts := client.splitMessage(escaped)

			if len(parts) < 2 {
				t.Fatalf("expected at least 2 parts, got %d", len(parts))
			}
			for i, part := range parts {
				if len(part) > maxMessageLength {
					t.Errorf("part %d exceeds max length: %d > %d", i, len(part), maxMessageLength)
				}
				if part == "" {
					t.Errorf("part %d is empty", i)
				}
				trimmed := strings.TrimRight(part, `\`)
				if (len(part)-len(trimmed))%2 == 1 {
					t.Errorf("part %d ends with an unpaired trailing backslash", i)
				}
			}
			if joined := strings.Join(parts, ""); joined != escaped {
				t.Error("joined chunks do not round-trip to the escaped input")
			}
		})
	}
}

func TestSplitMessage_ExactLimitLineInsideLongMessage(t *testing.T) {
	t.Parallel()

	client := &TelegramClient{}
	message := strings.Repeat("x", maxMessageLength) + "\ntail"
	parts := client.splitMessage(message)
	if len(parts) < 2 {
		t.Fatalf("splitMessage() returned %d part(s), want at least 2", len(parts))
	}
	for i, part := range parts {
		if len(part) > maxMessageLength {
			t.Fatalf("part %d has %d bytes, exceeds %d", i, len(part), maxMessageLength)
		}
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
			switch tt.logSourceType {
			case "logwatch":
				expectedDisplay = "Logwatch"
			case "drupal_watchdog":
				expectedDisplay = "Drupal Watchdog"
			case "ocms":
				expectedDisplay = "OCMS"
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
