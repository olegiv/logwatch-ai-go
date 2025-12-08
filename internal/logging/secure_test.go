package logging

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// Note: We test SecureEvent methods directly with zerolog since
// we can't easily create a go-logger without file setup in tests.

// TestSecureEventStr tests that Str sanitizes credentials.
func TestSecureEventStr(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		wantSafe bool // should not contain credential patterns
	}{
		{
			name:     "normal string",
			key:      "model",
			value:    "claude-sonnet-4-5",
			wantSafe: true,
		},
		{
			name:     "anthropic API key",
			key:      "key",
			value:    "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890",
			wantSafe: true,
		},
		{
			name:     "telegram bot token",
			key:      "token",
			value:    "1234567890:ABCdefGHI_jklMNOpqrSTUvwxYZ-12345678",
			wantSafe: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			zl := zerolog.New(&buf)
			event := &SecureEvent{event: zl.Info()}

			event.Str(tt.key, tt.value).Msg("test")
			output := buf.String()

			// Check that the output doesn't contain known credential patterns
			if strings.Contains(output, "sk-ant-api03") {
				t.Errorf("output contains unsanitized API key prefix")
			}
			if strings.Contains(output, "ABCdefGHI_jkl") {
				t.Errorf("output contains unsanitized token")
			}
		})
	}
}

// TestSecureEventErr tests that Err sanitizes error messages.
func TestSecureEventErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "error with API key",
			err:  errors.New("failed with key sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890"),
		},
		{
			name: "error with bot token",
			err:  errors.New("telegram error: 1234567890:ABCdefGHI_jklMNOpqrSTUvwxYZ-12345678"),
		},
		{
			name: "nil error",
			err:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			zl := zerolog.New(&buf)
			event := &SecureEvent{event: zl.Error()}

			event.Err(tt.err).Msg("test")
			output := buf.String()

			// Check that the output doesn't contain known credential patterns
			if strings.Contains(output, "sk-ant-api03") {
				t.Errorf("output contains unsanitized API key prefix")
			}
			if strings.Contains(output, "ABCdefGHI_jkl") {
				t.Errorf("output contains unsanitized token")
			}
		})
	}
}

// TestSecureEventMsg tests that Msg sanitizes messages.
func TestSecureEventMsg(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "normal message",
			message: "Starting application",
		},
		{
			name:    "message with API key",
			message: "Using key sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			zl := zerolog.New(&buf)
			event := &SecureEvent{event: zl.Info()}

			event.Msg(tt.message)
			output := buf.String()

			// Check that the output doesn't contain known credential patterns
			if strings.Contains(output, "sk-ant-api03") {
				t.Errorf("output contains unsanitized API key prefix")
			}
		})
	}
}

// TestSecureEventMsgf tests that Msgf sanitizes format arguments.
func TestSecureEventMsgf(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	event := &SecureEvent{event: zl.Info()}

	apiKey := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890"
	event.Msgf("Key: %s, Count: %d", apiKey, 42)
	output := buf.String()

	if strings.Contains(output, "sk-ant-api03") {
		t.Errorf("output contains unsanitized API key: %s", output)
	}
	if !strings.Contains(output, "42") {
		t.Errorf("output should contain non-string argument 42")
	}
}

// TestSecureEventInterface tests that Interface sanitizes string values.
func TestSecureEventInterface(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value interface{}
	}{
		{
			name:  "string with credential",
			key:   "data",
			value: "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890",
		},
		{
			name:  "int value",
			key:   "count",
			value: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			zl := zerolog.New(&buf)
			event := &SecureEvent{event: zl.Info()}

			event.Interface(tt.key, tt.value).Msg("test")
			output := buf.String()

			// Check that the output doesn't contain known credential patterns
			if strings.Contains(output, "sk-ant-api03") {
				t.Errorf("output contains unsanitized API key: %s", output)
			}
		})
	}
}

// TestSecureEventChaining tests that method chaining works correctly.
func TestSecureEventChaining(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	event := &SecureEvent{event: zl.Info()}

	event.
		Str("key", "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890").
		Int("count", 10).
		Int64("total", 100).
		Float64("rate", 0.95).
		Bool("enabled", true).
		Msg("test")

	output := buf.String()

	if strings.Contains(output, "sk-ant-api03") {
		t.Errorf("output contains unsanitized API key: %s", output)
	}
	if !strings.Contains(output, "10") {
		t.Errorf("output should contain int value")
	}
	if !strings.Contains(output, "100") {
		t.Errorf("output should contain int64 value")
	}
	if !strings.Contains(output, "0.95") {
		t.Errorf("output should contain float64 value")
	}
	if !strings.Contains(output, "true") {
		t.Errorf("output should contain bool value")
	}
}

// TestSecureLoggerLevels tests that all log levels create SecureEvents.
func TestSecureLoggerLevels(t *testing.T) {
	// Note: We can't easily test SecureLogger without mocking go-logger,
	// but we can verify the SecureEvent works at different levels
	levelNames := []string{"info", "debug", "warn", "error"}

	for _, levelName := range levelNames {
		t.Run(levelName, func(t *testing.T) {
			var buf bytes.Buffer
			zl := zerolog.New(&buf)
			var event *zerolog.Event

			switch levelName {
			case "info":
				event = zl.Info()
			case "debug":
				event = zl.Debug()
			case "warn":
				event = zl.Warn()
			case "error":
				event = zl.Error()
			}

			secureEvent := &SecureEvent{event: event}
			secureEvent.Str("key", "sk-ant-api03-test1234567890abcdefghij").Msg("test")
			output := buf.String()

			if strings.Contains(output, "sk-ant-api03") {
				t.Errorf("level %s: output contains unsanitized API key", levelName)
			}
		})
	}
}
