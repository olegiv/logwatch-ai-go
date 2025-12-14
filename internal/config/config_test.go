package config

import (
	"os"
	"strings"
	"testing"
)

// checkError is a helper to verify error expectations in tests
func checkError(t *testing.T, err error, expectError bool, errorContains string) {
	t.Helper()
	if expectError {
		if err == nil {
			t.Error("Expected an error but got none")
			return
		}
		if errorContains != "" && !strings.Contains(err.Error(), errorContains) {
			t.Errorf("Expected error to contain '%s', got '%s'", errorContains, err.Error())
		}
	} else {
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name          string
		config        *Config
		expectError   bool
		errorContains string
	}{
		{
			name: "Valid config",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
				EnablePreprocessing:    true,
				MaxPreprocessingTokens: 150000,
				AITimeoutSeconds:       120,
				AIMaxTokens:            8000,
			},
			expectError: false,
		},
		{
			name: "Missing Anthropic API Key",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
			},
			expectError:   true,
			errorContains: "ANTHROPIC_API_KEY is required",
		},
		{
			name: "Invalid Anthropic API Key format",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "invalid-key",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
			},
			expectError:   true,
			errorContains: "must start with 'sk-ant-'",
		},
		{
			name: "Missing Telegram Bot Token",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
			},
			expectError:   true,
			errorContains: "TELEGRAM_BOT_TOKEN is required",
		},
		{
			name: "Invalid Telegram Bot Token format",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "invalid-token",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
			},
			expectError:   true,
			errorContains: "invalid format",
		},
		{
			name: "Missing Telegram Archive Channel",
			config: &Config{
				LLMProvider:        "anthropic",
				ClaudeModel:        "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:    "sk-ant-test-key-1234567890",
				TelegramBotToken:   "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				LogSourceType:      "logwatch",
				LogwatchOutputPath: "/tmp/logwatch.txt",
				MaxLogSizeMB:       10,
				LogLevel:           "info",
			},
			expectError:   true,
			errorContains: "TELEGRAM_CHANNEL_ARCHIVE_ID is required",
		},
		{
			name: "Invalid Telegram Archive Channel ID",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -99,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
			},
			expectError:   true,
			errorContains: "must be a supergroup/channel ID",
		},
		{
			name: "Invalid Telegram Alerts Channel ID",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				TelegramAlertsChannel:  -99,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
			},
			expectError:   true,
			errorContains: "TELEGRAM_CHANNEL_ALERTS_ID must be a supergroup/channel ID",
		},
		{
			name: "Missing logwatch output path",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
			},
			expectError:   true,
			errorContains: "LOGWATCH_OUTPUT_PATH is required when LOG_SOURCE_TYPE=logwatch",
		},
		{
			name: "MaxLogSizeMB too small",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           0,
				LogLevel:               "info",
			},
			expectError:   true,
			errorContains: "must be between 1 and 100",
		},
		{
			name: "MaxLogSizeMB too large",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           101,
				LogLevel:               "info",
			},
			expectError:   true,
			errorContains: "must be between 1 and 100",
		},
		{
			name: "Invalid log level",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "invalid",
			},
			expectError:   true,
			errorContains: "must be one of: debug, info, warn, error",
		},
		{
			name: "Valid log level - debug",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "debug",
				AITimeoutSeconds:       120,
				AIMaxTokens:            8000,
			},
			expectError: false,
		},
		{
			name: "Valid log level - warn",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "warn",
				AITimeoutSeconds:       120,
				AIMaxTokens:            8000,
			},
			expectError: false,
		},
		{
			name: "Valid log level - error",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "error",
				AITimeoutSeconds:       120,
				AIMaxTokens:            8000,
			},
			expectError: false,
		},
		{
			name: "MaxPreprocessingTokens too small",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
				EnablePreprocessing:    true,
				MaxPreprocessingTokens: 5000,
			},
			expectError:   true,
			errorContains: "must be at least 10000",
		},
		{
			name: "Preprocessing disabled with small tokens - valid",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
				EnablePreprocessing:    false,
				MaxPreprocessingTokens: 5000,
				AITimeoutSeconds:       120,
				AIMaxTokens:            8000,
			},
			expectError: false,
		},
		{
			name: "With valid alerts channel",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				TelegramAlertsChannel:  -1009876543210,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
				AITimeoutSeconds:       120,
				AIMaxTokens:            8000,
			},
			expectError: false,
		},
		{
			name: "AI timeout too small",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
				AITimeoutSeconds:       10,
				AIMaxTokens:            8000,
			},
			expectError:   true,
			errorContains: "AI_TIMEOUT_SECONDS must be between 30 and 600",
		},
		{
			name: "AI timeout too large",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
				AITimeoutSeconds:       700,
				AIMaxTokens:            8000,
			},
			expectError:   true,
			errorContains: "AI_TIMEOUT_SECONDS must be between 30 and 600",
		},
		{
			name: "AI max tokens too small",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
				AITimeoutSeconds:       120,
				AIMaxTokens:            500,
			},
			expectError:   true,
			errorContains: "AI_MAX_TOKENS must be between 1000 and 16000",
		},
		{
			name: "AI max tokens too large",
			config: &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
				AITimeoutSeconds:       120,
				AIMaxTokens:            20000,
			},
			expectError:   true,
			errorContains: "AI_MAX_TOKENS must be between 1000 and 16000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			checkError(t, err, tt.expectError, tt.errorContains)
		})
	}
}

func TestHasAlertsChannel(t *testing.T) {
	tests := []struct {
		name              string
		alertsChannelID   int64
		expectedHasAlerts bool
	}{
		{
			name:              "Has alerts channel",
			alertsChannelID:   -1001234567890,
			expectedHasAlerts: true,
		},
		{
			name:              "No alerts channel",
			alertsChannelID:   0,
			expectedHasAlerts: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				TelegramAlertsChannel: tt.alertsChannelID,
			}

			result := config.HasAlertsChannel()
			if result != tt.expectedHasAlerts {
				t.Errorf("Expected HasAlertsChannel() to be %v, got %v", tt.expectedHasAlerts, result)
			}
		})
	}
}

func TestGetProxyURL(t *testing.T) {
	tests := []struct {
		name        string
		httpProxy   string
		httpsProxy  string
		isHTTPS     bool
		expectedURL string
	}{
		{
			name:        "HTTPS request with HTTPS proxy",
			httpProxy:   "http://proxy.example.com:8080",
			httpsProxy:  "https://secure-proxy.example.com:8443",
			isHTTPS:     true,
			expectedURL: "https://secure-proxy.example.com:8443",
		},
		{
			name:        "HTTPS request with HTTP proxy fallback",
			httpProxy:   "http://proxy.example.com:8080",
			httpsProxy:  "",
			isHTTPS:     true,
			expectedURL: "http://proxy.example.com:8080",
		},
		{
			name:        "HTTP request with HTTP proxy",
			httpProxy:   "http://proxy.example.com:8080",
			httpsProxy:  "https://secure-proxy.example.com:8443",
			isHTTPS:     false,
			expectedURL: "http://proxy.example.com:8080",
		},
		{
			name:        "No proxy configured",
			httpProxy:   "",
			httpsProxy:  "",
			isHTTPS:     true,
			expectedURL: "",
		},
		{
			name:        "Only HTTP proxy for HTTPS request",
			httpProxy:   "http://proxy.example.com:8080",
			httpsProxy:  "",
			isHTTPS:     true,
			expectedURL: "http://proxy.example.com:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				HTTPProxy:  tt.httpProxy,
				HTTPSProxy: tt.httpsProxy,
			}

			result := config.GetProxyURL(tt.isHTTPS)
			if result != tt.expectedURL {
				t.Errorf("Expected proxy URL '%s', got '%s'", tt.expectedURL, result)
			}
		})
	}
}

func TestSetDefaults(t *testing.T) {
	// Clear any existing environment variables
	os.Clearenv()

	// Call setDefaults
	setDefaults()

	// We can't directly test viper defaults, but we can verify the function runs without error
	// The actual defaults are tested through the Load function
}

func TestConfigStructure(t *testing.T) {
	// Test that Config structure holds all fields correctly
	config := &Config{
		AnthropicAPIKey:        "sk-ant-test-key",
		ClaudeModel:            "claude-sonnet-4.5",
		TelegramBotToken:       "123:ABC",
		TelegramArchiveChannel: -100123,
		TelegramAlertsChannel:  -100456,
		LogwatchOutputPath:     "/tmp/test.txt",
		MaxLogSizeMB:           50,
		LogLevel:               "debug",
		EnableDatabase:         true,
		DatabasePath:           "./test.db",
		EnablePreprocessing:    true,
		MaxPreprocessingTokens: 200000,
		HTTPProxy:              "http://proxy:8080",
		HTTPSProxy:             "https://proxy:8443",
	}

	// Verify all fields are set correctly
	if config.AnthropicAPIKey != "sk-ant-test-key" {
		t.Errorf("AnthropicAPIKey not set correctly")
	}
	if config.ClaudeModel != "claude-sonnet-4.5" {
		t.Errorf("ClaudeModel not set correctly")
	}
	if config.TelegramBotToken != "123:ABC" {
		t.Errorf("TelegramBotToken not set correctly")
	}
	if config.TelegramArchiveChannel != -100123 {
		t.Errorf("TelegramArchiveChannel not set correctly")
	}
	if config.TelegramAlertsChannel != -100456 {
		t.Errorf("TelegramAlertsChannel not set correctly")
	}
	if config.LogwatchOutputPath != "/tmp/test.txt" {
		t.Errorf("LogwatchOutputPath not set correctly")
	}
	if config.MaxLogSizeMB != 50 {
		t.Errorf("MaxLogSizeMB not set correctly")
	}
	if config.LogLevel != "debug" {
		t.Errorf("LogLevel not set correctly")
	}
	if !config.EnableDatabase {
		t.Errorf("EnableDatabase not set correctly")
	}
	if config.DatabasePath != "./test.db" {
		t.Errorf("DatabasePath not set correctly")
	}
	if !config.EnablePreprocessing {
		t.Errorf("EnablePreprocessing not set correctly")
	}
	if config.MaxPreprocessingTokens != 200000 {
		t.Errorf("MaxPreprocessingTokens not set correctly")
	}
	if config.HTTPProxy != "http://proxy:8080" {
		t.Errorf("HTTPProxy not set correctly")
	}
	if config.HTTPSProxy != "https://proxy:8443" {
		t.Errorf("HTTPSProxy not set correctly")
	}
}

func TestTelegramTokenRegex(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		shouldMatch bool
	}{
		{"Valid token", "123456789:ABCdefGHIjklMNOpqrsTUVwxyz", true},
		{"Valid with dashes", "123456789:ABC-def_GHI", true},
		{"Valid with underscores", "123456789:ABC_def_GHI", true},
		{"Invalid - no colon", "123456789ABCdef", false},
		{"Invalid - no number", "ABCdef:123456789", false},
		{"Invalid - special chars", "123:ABC@def", false},
		{"Invalid - empty", "", false},
		{"Invalid - only number", "123456789:", false},
		{"Invalid - only token", ":ABCdef", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       tt.token,
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               "info",
				AITimeoutSeconds:       120,
				AIMaxTokens:            8000,
			}

			err := config.Validate()
			// Check for any error related to telegram token (either "required" or "invalid format")
			hasError := err != nil && (strings.Contains(err.Error(), "invalid format") || strings.Contains(err.Error(), "TELEGRAM_BOT_TOKEN"))

			if tt.shouldMatch && hasError {
				t.Errorf("Expected token '%s' to be valid, but got error: %v", tt.token, err)
			}

			if !tt.shouldMatch && !hasError {
				t.Errorf("Expected token '%s' to be invalid, but validation passed", tt.token)
			}
		})
	}
}

func TestLogLevelCaseInsensitive(t *testing.T) {
	tests := []string{"DEBUG", "Info", "WARN", "Error", "DeBuG"}

	for _, level := range tests {
		t.Run(level, func(t *testing.T) {
			config := &Config{
				LLMProvider:            "anthropic",
				ClaudeModel:            "claude-sonnet-4-5-20250929",
				AnthropicAPIKey:        "sk-ant-test-key-1234567890",
				TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
				TelegramArchiveChannel: -1001234567890,
				LogSourceType:          "logwatch",
				LogwatchOutputPath:     "/tmp/logwatch.txt",
				MaxLogSizeMB:           10,
				LogLevel:               level,
				AITimeoutSeconds:       120,
				AIMaxTokens:            8000,
			}

			err := config.Validate()
			if err != nil {
				t.Errorf("Expected log level '%s' to be valid, got error: %v", level, err)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	// Set environment variables for the test (t.Setenv automatically cleans up)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key-1234567890")
	t.Setenv("TELEGRAM_BOT_TOKEN", "123456789:ABCdefGHIjklMNOpqrsTUVwxyz")
	t.Setenv("TELEGRAM_CHANNEL_ARCHIVE_ID", "-1001234567890")

	config, err := Load()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if config == nil {
		t.Fatal("Expected config to be loaded")
	}

	// Verify defaults are set
	if config.ClaudeModel == "" {
		t.Error("Expected ClaudeModel to have a default value")
	}

	// Verify environment variables were loaded
	if config.AnthropicAPIKey != "sk-ant-test-key-1234567890" {
		t.Error("AnthropicAPIKey not loaded from environment")
	}
}

func TestLoad_ValidationFails(t *testing.T) {
	// Clear environment to trigger validation errors
	os.Clearenv()

	_, err := Load()
	if err == nil {
		t.Error("Expected Load to fail when required env vars are missing")
	}
}

func TestConstantTimePrefixMatch(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		prefix string
		want   bool
	}{
		{
			name:   "exact prefix match",
			s:      "sk-ant-api03-test1234567890",
			prefix: "sk-ant-",
			want:   true,
		},
		{
			name:   "prefix match with longer string",
			s:      "sk-ant-very-long-api-key-here",
			prefix: "sk-ant-",
			want:   true,
		},
		{
			name:   "exact match",
			s:      "sk-ant-",
			prefix: "sk-ant-",
			want:   true,
		},
		{
			name:   "no match - different prefix",
			s:      "invalid-key-here",
			prefix: "sk-ant-",
			want:   false,
		},
		{
			name:   "no match - string too short",
			s:      "sk-a",
			prefix: "sk-ant-",
			want:   false,
		},
		{
			name:   "no match - empty string",
			s:      "",
			prefix: "sk-ant-",
			want:   false,
		},
		{
			name:   "match - empty prefix",
			s:      "anything",
			prefix: "",
			want:   true,
		},
		{
			name:   "no match - partial prefix",
			s:      "sk-ant",
			prefix: "sk-ant-",
			want:   false,
		},
		{
			name:   "no match - similar but different",
			s:      "sk-ANT-key",
			prefix: "sk-ant-",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := constantTimePrefixMatch(tt.s, tt.prefix)
			if got != tt.want {
				t.Errorf("constantTimePrefixMatch(%q, %q) = %v, want %v", tt.s, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestValidateLogSource(t *testing.T) {
	baseConfig := func() *Config {
		return &Config{
			LLMProvider:            "anthropic",
			ClaudeModel:            "claude-sonnet-4-5-20250929",
			AnthropicAPIKey:        "sk-ant-test-key-1234567890",
			TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
			TelegramArchiveChannel: -1001234567890,
			MaxLogSizeMB:           10,
			LogLevel:               "info",
			AITimeoutSeconds:       120,
			AIMaxTokens:            8000,
		}
	}

	tests := []struct {
		name          string
		setup         func(*Config)
		expectError   bool
		errorContains string
	}{
		{
			name: "Valid logwatch config",
			setup: func(c *Config) {
				c.LogSourceType = "logwatch"
				c.LogwatchOutputPath = "/tmp/logwatch.txt"
			},
			expectError: false,
		},
		{
			name: "Valid drupal_watchdog config with json format",
			setup: func(c *Config) {
				c.LogSourceType = "drupal_watchdog"
				c.DrupalWatchdogPath = "/var/log/drupal-watchdog.json"
				c.DrupalWatchdogFormat = "json"
			},
			expectError: false,
		},
		{
			name: "Valid drupal_watchdog config with drush format",
			setup: func(c *Config) {
				c.LogSourceType = "drupal_watchdog"
				c.DrupalWatchdogPath = "/var/log/drupal-watchdog.txt"
				c.DrupalWatchdogFormat = "drush"
			},
			expectError: false,
		},
		{
			name: "Invalid log source type",
			setup: func(c *Config) {
				c.LogSourceType = "invalid"
				c.LogwatchOutputPath = "/tmp/logwatch.txt"
			},
			expectError:   true,
			errorContains: "LOG_SOURCE_TYPE must be 'logwatch' or 'drupal_watchdog'",
		},
		{
			name: "Missing logwatch path when logwatch selected",
			setup: func(c *Config) {
				c.LogSourceType = "logwatch"
				c.LogwatchOutputPath = ""
			},
			expectError:   true,
			errorContains: "LOGWATCH_OUTPUT_PATH is required when LOG_SOURCE_TYPE=logwatch",
		},
		{
			name: "Missing drupal path when drupal_watchdog selected",
			setup: func(c *Config) {
				c.LogSourceType = "drupal_watchdog"
				c.DrupalWatchdogPath = ""
				c.DrupalWatchdogFormat = "json"
			},
			expectError:   true,
			errorContains: "watchdog_path is required in drupal-sites.json",
		},
		{
			name: "Invalid drupal watchdog format",
			setup: func(c *Config) {
				c.LogSourceType = "drupal_watchdog"
				c.DrupalWatchdogPath = "/var/log/watchdog.json"
				c.DrupalWatchdogFormat = "invalid"
			},
			expectError:   true,
			errorContains: "watchdog_format must be 'json' or 'drush' in drupal-sites.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig()
			tt.setup(cfg)

			err := cfg.Validate()
			checkError(t, err, tt.expectError, tt.errorContains)
		})
	}
}

func TestGetLogSourcePath(t *testing.T) {
	tests := []struct {
		name           string
		logSourceType  string
		logwatchPath   string
		drupalPath     string
		expectedResult string
	}{
		{
			name:           "Logwatch source type",
			logSourceType:  "logwatch",
			logwatchPath:   "/tmp/logwatch.txt",
			drupalPath:     "/var/log/drupal.json",
			expectedResult: "/tmp/logwatch.txt",
		},
		{
			name:           "Drupal watchdog source type",
			logSourceType:  "drupal_watchdog",
			logwatchPath:   "/tmp/logwatch.txt",
			drupalPath:     "/var/log/drupal.json",
			expectedResult: "/var/log/drupal.json",
		},
		{
			name:           "Unknown source type defaults to logwatch",
			logSourceType:  "unknown",
			logwatchPath:   "/tmp/logwatch.txt",
			drupalPath:     "/var/log/drupal.json",
			expectedResult: "/tmp/logwatch.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				LogSourceType:      tt.logSourceType,
				LogwatchOutputPath: tt.logwatchPath,
				DrupalWatchdogPath: tt.drupalPath,
			}

			result := cfg.GetLogSourcePath()
			if result != tt.expectedResult {
				t.Errorf("GetLogSourcePath() = %q, want %q", result, tt.expectedResult)
			}
		})
	}
}

func TestIsDrupalWatchdog(t *testing.T) {
	tests := []struct {
		name          string
		logSourceType string
		expected      bool
	}{
		{"Drupal watchdog", "drupal_watchdog", true},
		{"Logwatch", "logwatch", false},
		{"Unknown", "unknown", false},
		{"Empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{LogSourceType: tt.logSourceType}
			if got := cfg.IsDrupalWatchdog(); got != tt.expected {
				t.Errorf("IsDrupalWatchdog() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsLogwatch(t *testing.T) {
	tests := []struct {
		name          string
		logSourceType string
		expected      bool
	}{
		{"Logwatch", "logwatch", true},
		{"Drupal watchdog", "drupal_watchdog", false},
		{"Unknown", "unknown", false},
		{"Empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{LogSourceType: tt.logSourceType}
			if got := cfg.IsLogwatch(); got != tt.expected {
				t.Errorf("IsLogwatch() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConfigStructure_WithDrupalFields(t *testing.T) {
	config := &Config{
		LogSourceType:        "drupal_watchdog",
		DrupalWatchdogPath:   "/var/log/drupal-watchdog.json",
		DrupalWatchdogFormat: "json",
		DrupalSiteName:       "production",
	}

	if config.LogSourceType != "drupal_watchdog" {
		t.Errorf("LogSourceType not set correctly")
	}
	if config.DrupalWatchdogPath != "/var/log/drupal-watchdog.json" {
		t.Errorf("DrupalWatchdogPath not set correctly")
	}
	if config.DrupalWatchdogFormat != "json" {
		t.Errorf("DrupalWatchdogFormat not set correctly")
	}
	if config.DrupalSiteName != "production" {
		t.Errorf("DrupalSiteName not set correctly")
	}
}

func TestCLIOptionsStructure(t *testing.T) {
	// Test that CLIOptions structure holds all fields correctly
	opts := &CLIOptions{
		SourceType:        "drupal_watchdog",
		SourcePath:        "/tmp/watchdog.json",
		DrupalSite:        "production",
		DrupalSitesConfig: "/etc/drupal-sites.json",
		ListDrupalSites:   true,
		ShowHelp:          true,
		ShowVersion:       true,
	}

	if opts.SourceType != "drupal_watchdog" {
		t.Errorf("SourceType not set correctly")
	}
	if opts.SourcePath != "/tmp/watchdog.json" {
		t.Errorf("SourcePath not set correctly")
	}
	if opts.DrupalSite != "production" {
		t.Errorf("DrupalSite not set correctly")
	}
	if opts.DrupalSitesConfig != "/etc/drupal-sites.json" {
		t.Errorf("DrupalSitesConfig not set correctly")
	}
	if !opts.ListDrupalSites {
		t.Errorf("ListDrupalSites not set correctly")
	}
	if !opts.ShowHelp {
		t.Errorf("ShowHelp not set correctly")
	}
	if !opts.ShowVersion {
		t.Errorf("ShowVersion not set correctly")
	}
}

func TestCLIOptionsDefaults(t *testing.T) {
	// Test that a zero-value CLIOptions has the expected defaults
	opts := &CLIOptions{}

	if opts.SourceType != "" {
		t.Errorf("Expected empty SourceType by default, got %q", opts.SourceType)
	}
	if opts.SourcePath != "" {
		t.Errorf("Expected empty SourcePath by default, got %q", opts.SourcePath)
	}
	if opts.DrupalSite != "" {
		t.Errorf("Expected empty DrupalSite by default, got %q", opts.DrupalSite)
	}
	if opts.DrupalSitesConfig != "" {
		t.Errorf("Expected empty DrupalSitesConfig by default, got %q", opts.DrupalSitesConfig)
	}
	if opts.ListDrupalSites {
		t.Errorf("Expected ListDrupalSites to be false by default")
	}
	if opts.ShowHelp {
		t.Errorf("Expected ShowHelp to be false by default")
	}
	if opts.ShowVersion {
		t.Errorf("Expected ShowVersion to be false by default")
	}
}
