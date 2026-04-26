package config

import (
	"os"
	"path/filepath"
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
	} else if err != nil {
		t.Errorf("Unexpected error: %v", err)
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:        "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
				ClaudeModel:            "claude-haiku-4-5-20251001",
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
			ClaudeModel:            "claude-haiku-4-5-20251001",
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
			name: "Valid ocms config",
			setup: func(c *Config) {
				c.LogSourceType = "ocms"
				c.OCMSLogsPath = "/var/www/vhosts/example.com/ocms/logs/ocms.log"
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
			errorContains: "LOG_SOURCE_TYPE must be 'logwatch', 'drupal_watchdog', or 'ocms'",
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
			name: "Missing ocms path when ocms selected",
			setup: func(c *Config) {
				c.LogSourceType = "ocms"
				c.OCMSLogsPath = ""
			},
			expectError:   true,
			errorContains: "OCMS_LOGS_PATH is required when LOG_SOURCE_TYPE=ocms",
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
		ocmsPath       string
		expectedResult string
	}{
		{
			name:           "Logwatch source type",
			logSourceType:  "logwatch",
			logwatchPath:   "/tmp/logwatch.txt",
			drupalPath:     "/var/log/drupal.json",
			ocmsPath:       "/var/www/vhosts/example.com/ocms/logs/ocms.log",
			expectedResult: "/tmp/logwatch.txt",
		},
		{
			name:           "Drupal watchdog source type",
			logSourceType:  "drupal_watchdog",
			logwatchPath:   "/tmp/logwatch.txt",
			drupalPath:     "/var/log/drupal.json",
			ocmsPath:       "/var/www/vhosts/example.com/ocms/logs/ocms.log",
			expectedResult: "/var/log/drupal.json",
		},
		{
			name:           "OCMS source type",
			logSourceType:  "ocms",
			logwatchPath:   "/tmp/logwatch.txt",
			drupalPath:     "/var/log/drupal.json",
			ocmsPath:       "/var/www/vhosts/example.com/ocms/logs/ocms.log",
			expectedResult: "/var/www/vhosts/example.com/ocms/logs/ocms.log",
		},
		{
			name:           "Unknown source type defaults to logwatch",
			logSourceType:  "unknown",
			logwatchPath:   "/tmp/logwatch.txt",
			drupalPath:     "/var/log/drupal.json",
			ocmsPath:       "/var/www/vhosts/example.com/ocms/logs/ocms.log",
			expectedResult: "/tmp/logwatch.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				LogSourceType:      tt.logSourceType,
				LogwatchOutputPath: tt.logwatchPath,
				DrupalWatchdogPath: tt.drupalPath,
				OCMSLogsPath:       tt.ocmsPath,
			}

			result := cfg.GetLogSourcePath()
			if result != tt.expectedResult {
				t.Errorf("GetLogSourcePath() = %q, want %q", result, tt.expectedResult)
			}
		})
	}
}

// ocmsMultiSiteFixtures writes a registry and ocms-sites.json into tmpDir,
// returning the paths for use by tests.
func ocmsMultiSiteFixtures(t *testing.T) (registryPath, configPath, tmpDir string) {
	t.Helper()
	tmpDir = t.TempDir()

	registryPath = filepath.Join(tmpDir, "sites.conf")
	registryContent := `example_com /var/www/vhosts/example.com/ocms example_com 8081
app_example_com /var/www/vhosts/example.com/ocms/app hosting 8082
all_example_com /var/www/vhosts/all.example.com/ocms all_example_com 8083
`
	if err := os.WriteFile(registryPath, []byte(registryContent), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	configPath = filepath.Join(tmpDir, "ocms-sites.json")
	configContent := `{
  "version": "1.0",
  "default_site": "example_com",
  "registry_path": "` + registryPath + `",
  "default_log_kind": "main",
  "sites": {
    "example_com": {
      "name": "Example Site"
    },
    "app_example_com": {
      "name": "Example App",
      "log_kind": "error"
    },
    "all_example_com": {
      "name": "All Example",
      "log_kind": "all"
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatalf("write ocms config: %v", err)
	}

	return registryPath, configPath, tmpDir
}

func TestApplyOCMSMultiSiteConfig_DerivesMainLogPath(t *testing.T) {
	_, configPath, _ := ocmsMultiSiteFixtures(t)
	cfg := &Config{LogSourceType: "ocms"}
	err := cfg.applyOCMSMultiSiteConfig(&CLIOptions{
		OCMSSite:        "example_com",
		OCMSSitesConfig: configPath,
	})
	if err != nil {
		t.Fatalf("applyOCMSMultiSiteConfig() error = %v", err)
	}
	if cfg.OCMSLogsPath != "/var/www/vhosts/example.com/ocms/logs/ocms.log" {
		t.Fatalf("OCMSLogsPath = %q", cfg.OCMSLogsPath)
	}
	if cfg.SelectedSiteID() != "example_com" {
		t.Fatalf("SelectedSiteID() = %q", cfg.SelectedSiteID())
	}
	if cfg.SelectedSiteName() != "Example Site" {
		t.Fatalf("SelectedSiteName() = %q", cfg.SelectedSiteName())
	}
	if cfg.OCMSSitesConfigPath != configPath {
		t.Fatalf("OCMSSitesConfigPath = %q", cfg.OCMSSitesConfigPath)
	}
}

func TestApplyOCMSMultiSiteConfig_DerivesErrorLogFromJSON(t *testing.T) {
	_, configPath, _ := ocmsMultiSiteFixtures(t)
	cfg := &Config{LogSourceType: "ocms"}
	err := cfg.applyOCMSMultiSiteConfig(&CLIOptions{
		OCMSSite:        "app_example_com",
		OCMSSitesConfig: configPath,
	})
	if err != nil {
		t.Fatalf("applyOCMSMultiSiteConfig() error = %v", err)
	}
	if cfg.OCMSLogsPath != "/var/www/vhosts/example.com/ocms/app/logs/error.log" {
		t.Fatalf("OCMSLogsPath = %q", cfg.OCMSLogsPath)
	}
	if cfg.OCMSLogKind != OCMSLogKindError {
		t.Fatalf("OCMSLogKind = %q", cfg.OCMSLogKind)
	}
}

func TestApplyOCMSMultiSiteConfig_CLILogKindOverridesJSON(t *testing.T) {
	_, configPath, _ := ocmsMultiSiteFixtures(t)
	cfg := &Config{LogSourceType: "ocms"}
	err := cfg.applyOCMSMultiSiteConfig(&CLIOptions{
		OCMSSite:        "app_example_com",
		OCMSSitesConfig: configPath,
		OCMSLogKind:     OCMSLogKindMain,
	})
	if err != nil {
		t.Fatalf("applyOCMSMultiSiteConfig() error = %v", err)
	}
	if cfg.OCMSLogsPath != "/var/www/vhosts/example.com/ocms/app/logs/ocms.log" {
		t.Fatalf("OCMSLogsPath = %q", cfg.OCMSLogsPath)
	}
}

func TestApplyOCMSMultiSiteConfig_DerivesAllLogPaths(t *testing.T) {
	_, configPath, _ := ocmsMultiSiteFixtures(t)
	cfg := &Config{LogSourceType: "ocms"}
	err := cfg.applyOCMSMultiSiteConfig(&CLIOptions{
		OCMSSite:        "all_example_com",
		OCMSSitesConfig: configPath,
	})
	if err != nil {
		t.Fatalf("applyOCMSMultiSiteConfig() error = %v", err)
	}
	if cfg.OCMSLogKind != OCMSLogKindAll {
		t.Fatalf("OCMSLogKind = %q", cfg.OCMSLogKind)
	}
	paths := cfg.GetOCMSLogPaths()
	if len(paths) != 2 {
		t.Fatalf("len(GetOCMSLogPaths()) = %d, want 2", len(paths))
	}
	if paths[0].Kind != OCMSLogKindMain || paths[0].Path != "/var/www/vhosts/all.example.com/ocms/logs/ocms.log" {
		t.Fatalf("main path = %+v", paths[0])
	}
	if paths[1].Kind != OCMSLogKindError || paths[1].Path != "/var/www/vhosts/all.example.com/ocms/logs/error.log" {
		t.Fatalf("error path = %+v", paths[1])
	}
	if cfg.OCMSLogsPath != paths[0].Path {
		t.Fatalf("OCMSLogsPath = %q, want first all path", cfg.OCMSLogsPath)
	}
}

func TestApplyOCMSMultiSiteConfig_CLIAllLogKindOverridesJSON(t *testing.T) {
	_, configPath, _ := ocmsMultiSiteFixtures(t)
	cfg := &Config{LogSourceType: "ocms"}
	err := cfg.applyOCMSMultiSiteConfig(&CLIOptions{
		OCMSSite:        "app_example_com",
		OCMSSitesConfig: configPath,
		OCMSLogKind:     OCMSLogKindAll,
	})
	if err != nil {
		t.Fatalf("applyOCMSMultiSiteConfig() error = %v", err)
	}
	paths := cfg.GetOCMSLogPaths()
	if len(paths) != 2 {
		t.Fatalf("len(GetOCMSLogPaths()) = %d, want 2", len(paths))
	}
	if paths[0].Path != "/var/www/vhosts/example.com/ocms/app/logs/ocms.log" {
		t.Fatalf("main path = %+v", paths[0])
	}
	if paths[1].Path != "/var/www/vhosts/example.com/ocms/app/logs/error.log" {
		t.Fatalf("error path = %+v", paths[1])
	}
}

func TestApplyOCMSMultiSiteConfig_UsesDefaultSite(t *testing.T) {
	_, configPath, _ := ocmsMultiSiteFixtures(t)
	cfg := &Config{LogSourceType: "ocms"}
	err := cfg.applyOCMSMultiSiteConfig(&CLIOptions{OCMSSitesConfig: configPath})
	if err != nil {
		t.Fatalf("applyOCMSMultiSiteConfig() error = %v", err)
	}
	if cfg.SelectedSiteID() != "example_com" {
		t.Fatalf("SelectedSiteID() = %q", cfg.SelectedSiteID())
	}
}

func TestApplyOCMSMultiSiteConfig_SourcePathOverridesRegistry(t *testing.T) {
	_, configPath, _ := ocmsMultiSiteFixtures(t)
	cfg := &Config{
		LogSourceType: "ocms",
		OCMSLogsPath:  "/tmp/manual.log",
		OCMSLogKind:   OCMSLogKindAll,
	}
	err := cfg.applyOCMSMultiSiteConfig(&CLIOptions{
		SourcePath:      "/tmp/manual.log",
		OCMSSite:        "all_example_com",
		OCMSSitesConfig: configPath,
		OCMSLogKind:     OCMSLogKindAll,
	})
	if err != nil {
		t.Fatalf("applyOCMSMultiSiteConfig() error = %v", err)
	}
	if cfg.OCMSLogsPath != "/tmp/manual.log" {
		t.Fatalf("OCMSLogsPath = %q", cfg.OCMSLogsPath)
	}
	paths := cfg.GetOCMSLogPaths()
	if len(paths) != 1 || paths[0].Path != "/tmp/manual.log" {
		t.Fatalf("GetOCMSLogPaths() = %+v, want manual source path only", paths)
	}
}

func TestApplyOCMSMultiSiteConfig_MissingSiteFails(t *testing.T) {
	_, configPath, _ := ocmsMultiSiteFixtures(t)
	cfg := &Config{LogSourceType: "ocms", OCMSLogKind: OCMSLogKindMain}
	err := cfg.applyOCMSMultiSiteConfig(&CLIOptions{
		OCMSSite:        "missing",
		OCMSSitesConfig: configPath,
	})
	if err == nil {
		t.Fatal("applyOCMSMultiSiteConfig() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get OCMS site") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyOCMSMultiSiteConfig_SiteMissingFromRegistryFails(t *testing.T) {
	registryPath, _, tmpDir := ocmsMultiSiteFixtures(t)
	missingRegistrySiteConfigPath := filepath.Join(tmpDir, "ocms-sites-missing-registry.json")
	missingRegistrySiteContent := `{
  "version": "1.0",
  "default_site": "not_in_registry",
  "registry_path": "` + registryPath + `",
  "sites": {
    "not_in_registry": {
      "name": "Missing Registry Site"
    }
  }
}`
	if err := os.WriteFile(missingRegistrySiteConfigPath, []byte(missingRegistrySiteContent), 0o600); err != nil {
		t.Fatalf("write ocms config: %v", err)
	}

	cfg := &Config{LogSourceType: "ocms"}
	err := cfg.applyOCMSMultiSiteConfig(&CLIOptions{
		OCMSSitesConfig: missingRegistrySiteConfigPath,
	})
	if err == nil {
		t.Fatal("applyOCMSMultiSiteConfig() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not_in_registry") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyOCMSMultiSiteConfig_SingleSiteModeUnchanged(t *testing.T) {
	cfg := &Config{
		LogSourceType: "ocms",
		OCMSLogsPath:  "/tmp/ocms.log",
		OCMSLogKind:   OCMSLogKindMain,
	}
	err := cfg.applyOCMSMultiSiteConfig(&CLIOptions{})
	if err != nil {
		t.Fatalf("applyOCMSMultiSiteConfig() error = %v", err)
	}
	if cfg.OCMSLogsPath != "/tmp/ocms.log" {
		t.Fatalf("OCMSLogsPath = %q", cfg.OCMSLogsPath)
	}
	if cfg.SelectedSiteID() != "" {
		t.Fatalf("SelectedSiteID() = %q", cfg.SelectedSiteID())
	}
}

func TestSelectedSiteID(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"empty config", Config{}, ""},
		{"explicit SiteID wins", Config{SiteID: "site-x", DrupalSiteID: "drupal-y", OCMSSiteID: "ocms-z"}, "site-x"},
		{"falls back to DrupalSiteID", Config{DrupalSiteID: "drupal-y", OCMSSiteID: "ocms-z"}, "drupal-y"},
		{"falls back to OCMSSiteID", Config{OCMSSiteID: "ocms-z"}, "ocms-z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.SelectedSiteID(); got != tt.want {
				t.Errorf("SelectedSiteID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelectedSiteName(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"empty config", Config{}, ""},
		{"explicit SiteName wins", Config{SiteName: "Site X", DrupalSiteName: "Drupal Y", OCMSSiteName: "OCMS Z"}, "Site X"},
		{"falls back to DrupalSiteName", Config{DrupalSiteName: "Drupal Y", OCMSSiteName: "OCMS Z"}, "Drupal Y"},
		{"falls back to OCMSSiteName", Config{OCMSSiteName: "OCMS Z"}, "OCMS Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.SelectedSiteName(); got != tt.want {
				t.Errorf("SelectedSiteName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetOCMSLogPaths(t *testing.T) {
	t.Run("returns copy of OCMSLogPaths when set", func(t *testing.T) {
		original := []OCMSLogPath{
			{Kind: OCMSLogKindMain, Path: "/a/ocms.log"},
			{Kind: OCMSLogKindError, Path: "/a/error.log"},
		}
		cfg := &Config{OCMSLogPaths: original}

		got := cfg.GetOCMSLogPaths()
		if len(got) != 2 {
			t.Fatalf("len(GetOCMSLogPaths()) = %d, want 2", len(got))
		}
		got[0].Path = "/mutated"
		if cfg.OCMSLogPaths[0].Path != "/a/ocms.log" {
			t.Errorf("returned slice should not alias internal storage")
		}
	})

	t.Run("falls back to single OCMSLogsPath", func(t *testing.T) {
		cfg := &Config{OCMSLogsPath: "/tmp/ocms.log", OCMSLogKind: OCMSLogKindMain}
		got := cfg.GetOCMSLogPaths()
		if len(got) != 1 || got[0].Path != "/tmp/ocms.log" || got[0].Kind != OCMSLogKindMain {
			t.Fatalf("GetOCMSLogPaths() = %+v", got)
		}
	})

	t.Run("returns nil when no path configured", func(t *testing.T) {
		cfg := &Config{}
		if got := cfg.GetOCMSLogPaths(); got != nil {
			t.Fatalf("GetOCMSLogPaths() = %+v, want nil", got)
		}
	})
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
		{"OCMS", "ocms", false},
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

func TestIsOCMS(t *testing.T) {
	tests := []struct {
		name          string
		logSourceType string
		expected      bool
	}{
		{"OCMS", "ocms", true},
		{"Logwatch", "logwatch", false},
		{"Drupal watchdog", "drupal_watchdog", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{LogSourceType: tt.logSourceType}
			if got := cfg.IsOCMS(); got != tt.expected {
				t.Errorf("IsOCMS() = %v, want %v", got, tt.expected)
			}
		})
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
		OCMSSite:          "example_com",
		OCMSSitesConfig:   "/etc/ocms-sites.json",
		OCMSSitesRegistry: "/etc/ocms/sites.conf",
		OCMSLogKind:       "error",
		ListOCMSSites:     true,
		ExclusionsConfig:  "/etc/exclusions.json",
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
	if opts.ExclusionsConfig != "/etc/exclusions.json" {
		t.Errorf("ExclusionsConfig not set correctly")
	}
	if !opts.ListDrupalSites {
		t.Errorf("ListDrupalSites not set correctly")
	}
	if opts.OCMSSite != "example_com" {
		t.Errorf("OCMSSite not set correctly")
	}
	if opts.OCMSSitesConfig != "/etc/ocms-sites.json" {
		t.Errorf("OCMSSitesConfig not set correctly")
	}
	if opts.OCMSSitesRegistry != "/etc/ocms/sites.conf" {
		t.Errorf("OCMSSitesRegistry not set correctly")
	}
	if opts.OCMSLogKind != "error" {
		t.Errorf("OCMSLogKind not set correctly")
	}
	if !opts.ListOCMSSites {
		t.Errorf("ListOCMSSites not set correctly")
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
	if opts.ExclusionsConfig != "" {
		t.Errorf("Expected empty ExclusionsConfig by default, got %q", opts.ExclusionsConfig)
	}
	if opts.ListDrupalSites {
		t.Errorf("Expected ListDrupalSites to be false by default")
	}
	if opts.OCMSSite != "" {
		t.Errorf("Expected empty OCMSSite by default, got %q", opts.OCMSSite)
	}
	if opts.OCMSSitesConfig != "" {
		t.Errorf("Expected empty OCMSSitesConfig by default, got %q", opts.OCMSSitesConfig)
	}
	if opts.OCMSSitesRegistry != "" {
		t.Errorf("Expected empty OCMSSitesRegistry by default, got %q", opts.OCMSSitesRegistry)
	}
	if opts.OCMSLogKind != "" {
		t.Errorf("Expected empty OCMSLogKind by default, got %q", opts.OCMSLogKind)
	}
	if opts.ListOCMSSites {
		t.Errorf("Expected ListOCMSSites to be false by default")
	}
	if opts.ShowHelp {
		t.Errorf("Expected ShowHelp to be false by default")
	}
	if opts.ShowVersion {
		t.Errorf("Expected ShowVersion to be false by default")
	}
}

func TestValidateOllamaProvider(t *testing.T) {
	baseConfig := func() *Config {
		return &Config{
			LLMProvider:            "ollama",
			OllamaBaseURL:          "http://localhost:11434",
			OllamaModel:            "llama3.3:latest",
			TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
			TelegramArchiveChannel: -1001234567890,
			LogSourceType:          "logwatch",
			LogwatchOutputPath:     "/tmp/logwatch.txt",
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
			name:        "Valid Ollama config",
			setup:       func(c *Config) {},
			expectError: false,
		},
		{
			name: "Missing Ollama model",
			setup: func(c *Config) {
				c.OllamaModel = ""
			},
			expectError:   true,
			errorContains: "OLLAMA_MODEL is required",
		},
		{
			name: "Missing Ollama base URL",
			setup: func(c *Config) {
				c.OllamaBaseURL = ""
			},
			expectError:   true,
			errorContains: "OLLAMA_BASE_URL is required",
		},
		{
			name: "Invalid Ollama base URL - no protocol",
			setup: func(c *Config) {
				c.OllamaBaseURL = "localhost:11434"
			},
			expectError:   true,
			errorContains: "must use http:// or https:// scheme",
		},
		{
			name: "Valid Ollama with HTTPS",
			setup: func(c *Config) {
				c.OllamaBaseURL = "https://ollama.example.com"
			},
			expectError: false,
		},
		{
			name: "Private IP rejected without ALLOW_LOCAL_LLM",
			setup: func(c *Config) {
				c.OllamaBaseURL = "http://10.0.0.5:11434"
			},
			expectError:   true,
			errorContains: "private/link-local address",
		},
		{
			name: "Link-local metadata endpoint rejected",
			setup: func(c *Config) {
				c.OllamaBaseURL = "http://169.254.169.254/latest/meta-data/"
			},
			expectError:   true,
			errorContains: "private/link-local address",
		},
		{
			name: "Scoped IPv6 link-local rejected (zone identifier stripped)",
			setup: func(c *Config) {
				c.OllamaBaseURL = "http://[fe80::1%25eth0]:11434"
			},
			expectError:   true,
			errorContains: "private/link-local address",
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

func TestValidateLMStudioProvider(t *testing.T) {
	baseConfig := func() *Config {
		return &Config{
			LLMProvider:            "lmstudio",
			LMStudioBaseURL:        "http://localhost:1234",
			LMStudioModel:          "local-model",
			TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
			TelegramArchiveChannel: -1001234567890,
			LogSourceType:          "logwatch",
			LogwatchOutputPath:     "/tmp/logwatch.txt",
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
			name:        "Valid LM Studio config",
			setup:       func(c *Config) {},
			expectError: false,
		},
		{
			name: "Missing LM Studio base URL",
			setup: func(c *Config) {
				c.LMStudioBaseURL = ""
			},
			expectError:   true,
			errorContains: "LMSTUDIO_BASE_URL is required",
		},
		{
			name: "Invalid LM Studio base URL - no protocol",
			setup: func(c *Config) {
				c.LMStudioBaseURL = "localhost:1234"
			},
			expectError:   true,
			errorContains: "must use http:// or https:// scheme",
		},
		{
			name: "LM Studio model optional - empty is valid",
			setup: func(c *Config) {
				c.LMStudioModel = ""
			},
			expectError: false,
		},
		{
			name: "Valid LM Studio with HTTPS",
			setup: func(c *Config) {
				c.LMStudioBaseURL = "https://lmstudio.example.com"
			},
			expectError: false,
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

func TestInvalidLLMProvider(t *testing.T) {
	cfg := &Config{
		LLMProvider:            "invalid_provider",
		TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
		TelegramArchiveChannel: -1001234567890,
		LogSourceType:          "logwatch",
		LogwatchOutputPath:     "/tmp/logwatch.txt",
		MaxLogSizeMB:           10,
		LogLevel:               "info",
		AITimeoutSeconds:       120,
		AIMaxTokens:            8000,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected error for invalid LLM provider")
	}
	if !strings.Contains(err.Error(), "LLM_PROVIDER must be") {
		t.Errorf("Expected LLM provider error, got: %v", err)
	}
}

func TestIsOllama(t *testing.T) {
	tests := []struct {
		name        string
		llmProvider string
		expected    bool
	}{
		{"Ollama provider", "ollama", true},
		{"Anthropic provider", "anthropic", false},
		{"LMStudio provider", "lmstudio", false},
		{"Empty provider", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{LLMProvider: tt.llmProvider}
			if got := cfg.IsOllama(); got != tt.expected {
				t.Errorf("IsOllama() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsAnthropic(t *testing.T) {
	tests := []struct {
		name        string
		llmProvider string
		expected    bool
	}{
		{"Anthropic provider", "anthropic", true},
		{"Ollama provider", "ollama", false},
		{"LMStudio provider", "lmstudio", false},
		{"Empty provider", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{LLMProvider: tt.llmProvider}
			if got := cfg.IsAnthropic(); got != tt.expected {
				t.Errorf("IsAnthropic() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsLMStudio(t *testing.T) {
	tests := []struct {
		name        string
		llmProvider string
		expected    bool
	}{
		{"LMStudio provider", "lmstudio", true},
		{"Anthropic provider", "anthropic", false},
		{"Ollama provider", "ollama", false},
		{"Empty provider", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{LLMProvider: tt.llmProvider}
			if got := cfg.IsLMStudio(); got != tt.expected {
				t.Errorf("IsLMStudio() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetLLMModel(t *testing.T) {
	tests := []struct {
		name          string
		config        *Config
		expectedModel string
	}{
		{
			name: "Anthropic provider returns Claude model",
			config: &Config{
				LLMProvider: "anthropic",
				ClaudeModel: "claude-haiku-4-5-20251001",
				OllamaModel: "llama3.3:latest",
			},
			expectedModel: "claude-haiku-4-5-20251001",
		},
		{
			name: "Ollama provider returns Ollama model",
			config: &Config{
				LLMProvider: "ollama",
				ClaudeModel: "claude-haiku-4-5-20251001",
				OllamaModel: "llama3.3:latest",
			},
			expectedModel: "llama3.3:latest",
		},
		{
			name: "LMStudio provider returns LMStudio model",
			config: &Config{
				LLMProvider:   "lmstudio",
				ClaudeModel:   "claude-haiku-4-5-20251001",
				LMStudioModel: "local-model",
			},
			expectedModel: "local-model",
		},
		{
			name: "Unknown provider defaults to Claude model",
			config: &Config{
				LLMProvider: "unknown",
				ClaudeModel: "claude-haiku-4-5-20251001",
			},
			expectedModel: "claude-haiku-4-5-20251001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetLLMModel(); got != tt.expectedModel {
				t.Errorf("GetLLMModel() = %v, want %v", got, tt.expectedModel)
			}
		})
	}
}

func TestValidateAnthropicMissingModel(t *testing.T) {
	cfg := &Config{
		LLMProvider:            "anthropic",
		AnthropicAPIKey:        "sk-ant-test-key-1234567890",
		ClaudeModel:            "", // Missing model
		TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
		TelegramArchiveChannel: -1001234567890,
		LogSourceType:          "logwatch",
		LogwatchOutputPath:     "/tmp/logwatch.txt",
		MaxLogSizeMB:           10,
		LogLevel:               "info",
		AITimeoutSeconds:       120,
		AIMaxTokens:            8000,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected error for missing Claude model")
	}
	if !strings.Contains(err.Error(), "CLAUDE_MODEL is required") {
		t.Errorf("Expected CLAUDE_MODEL error, got: %v", err)
	}
}

// TestValidateAnthropicModelFormat ensures that values which are clearly not
// Claude model IDs (e.g. an API key accidentally pasted into CLAUDE_MODEL)
// are rejected before they can reach logs or the API.
func TestValidateAnthropicModelFormat(t *testing.T) {
	base := Config{
		LLMProvider:            "anthropic",
		AnthropicAPIKey:        "sk-ant-test-key-1234567890",
		TelegramBotToken:       "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
		TelegramArchiveChannel: -1001234567890,
		LogSourceType:          "logwatch",
		LogwatchOutputPath:     "/tmp/logwatch.txt",
		MaxLogSizeMB:           10,
		LogLevel:               "info",
		AITimeoutSeconds:       120,
		AIMaxTokens:            8000,
	}

	tests := []struct {
		name    string
		model   string
		wantErr bool
	}{
		{"haiku 4.5 dated", "claude-haiku-4-5-20251001", false},
		{"sonnet 4.6 alias", "claude-sonnet-4-6", false},
		{"opus 4.7 alias", "claude-opus-4-7", false},
		{"sonnet 4.5 dated", "claude-sonnet-4-5-20250929", false},
		{"API key shape rejected", "sk-ant-api03-abcdef1234567890", true},
		{"uppercase rejected", "Claude-Haiku-4-5", true},
		{"path-like rejected", "/opt/model", true},
		{"prefix-only rejected", "claude-", true},
		{"space rejected", "claude foo", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			cfg.ClaudeModel = tt.model
			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() for %q: want error, got nil", tt.model)
				}
				if !strings.Contains(err.Error(), "CLAUDE_MODEL has invalid format") {
					t.Errorf("Validate() for %q: want format error, got %v", tt.model, err)
				}
				return
			}
			if err != nil {
				t.Errorf("Validate() for %q: want nil error, got %v", tt.model, err)
			}
		})
	}
}

func TestApplyExclusionsConfig(t *testing.T) {
	tmpDir := t.TempDir()
	goodPath := filepath.Join(tmpDir, "exclusions.json")
	goodContent := `{"version":"1.0","global":["TLS cert"]}`
	if err := os.WriteFile(goodPath, []byte(goodContent), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	v11Path := filepath.Join(tmpDir, "exclusions-v11.json")
	v11Content := `{"version":"1.1","global":["TLS cert"],"logwatch":["kernel watchdog"],"drupal":["deprecated function"]}`
	if err := os.WriteFile(v11Path, []byte(v11Content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Run("loads config when CLI path provided", func(t *testing.T) {
		cfg := &Config{}
		if err := cfg.applyExclusionsConfig(&CLIOptions{ExclusionsConfig: goodPath}); err != nil {
			t.Fatalf("applyExclusionsConfig: %v", err)
		}
		if cfg.Exclusions == nil {
			t.Fatal("Exclusions = nil, want non-nil")
		}
		if cfg.ExclusionsConfigPath != goodPath {
			t.Errorf("ExclusionsConfigPath = %q, want %q", cfg.ExclusionsConfigPath, goodPath)
		}
		if len(cfg.Exclusions.Global) != 1 {
			t.Errorf("len(Global) = %d, want 1", len(cfg.Exclusions.Global))
		}
	})

	t.Run("loads v1.1 config with logwatch and drupal scopes", func(t *testing.T) {
		cfg := &Config{}
		if err := cfg.applyExclusionsConfig(&CLIOptions{ExclusionsConfig: v11Path}); err != nil {
			t.Fatalf("applyExclusionsConfig: %v", err)
		}
		if cfg.Exclusions == nil {
			t.Fatal("Exclusions = nil, want non-nil")
		}
		if cfg.Exclusions.Version != "1.1" {
			t.Errorf("Version = %q, want 1.1", cfg.Exclusions.Version)
		}
		if len(cfg.Exclusions.Logwatch) != 1 {
			t.Errorf("len(Logwatch) = %d, want 1", len(cfg.Exclusions.Logwatch))
		}
		if len(cfg.Exclusions.Drupal) != 1 {
			t.Errorf("len(Drupal) = %d, want 1", len(cfg.Exclusions.Drupal))
		}
	})

	t.Run("nil CLI does not error when no config discoverable", func(t *testing.T) {
		t.Setenv("HOME", "/nonexistent-home-for-exclusions-config-test")
		t.Chdir(t.TempDir())

		cfg := &Config{}
		if err := cfg.applyExclusionsConfig(nil); err != nil {
			t.Fatalf("applyExclusionsConfig: %v", err)
		}
		if cfg.Exclusions != nil {
			t.Errorf("Exclusions = %+v, want nil", cfg.Exclusions)
		}
	})

	t.Run("explicit missing path is hard error", func(t *testing.T) {
		cfg := &Config{}
		err := cfg.applyExclusionsConfig(&CLIOptions{ExclusionsConfig: "/no/such/file.json"})
		if err == nil {
			t.Fatal("expected error for missing explicit path")
		}
		if !strings.Contains(err.Error(), "failed to load exclusions config") {
			t.Errorf("error = %v, want wrapped 'failed to load exclusions config'", err)
		}
	})
}
