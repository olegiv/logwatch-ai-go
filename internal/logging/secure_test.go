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

// TestSecureEventMsgfWithMultipleTypes tests Msgf with various argument types.
func TestSecureEventMsgfWithMultipleTypes(t *testing.T) {
	tests := []struct {
		name   string
		format string
		args   []interface{}
	}{
		{
			name:   "mixed types without credentials",
			format: "Count: %d, Rate: %.2f, Name: %s",
			args:   []interface{}{42, 0.95, "test"},
		},
		{
			name:   "string with credential",
			format: "API Key: %s",
			args:   []interface{}{"sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890"},
		},
		{
			name:   "error with credential",
			format: "Error: %v",
			args:   []interface{}{errors.New("failed with sk-ant-api03-secret123456789")},
		},
		{
			name:   "multiple credentials",
			format: "Key: %s, Token: %s",
			args:   []interface{}{"sk-ant-api03-secret123456789", "1234567890:ABCdefGHI_jklMNOpqrSTUvwxYZ-12345678"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			zl := zerolog.New(&buf)
			event := &SecureEvent{event: zl.Info()}

			event.Msgf(tt.format, tt.args...)
			output := buf.String()

			// Verify no credential patterns appear
			if strings.Contains(output, "sk-ant-api03") {
				t.Errorf("output contains unsanitized API key: %s", output)
			}
			if strings.Contains(output, "ABCdefGHI_jkl") {
				t.Errorf("output contains unsanitized token: %s", output)
			}
		})
	}
}

// TestSecureEventIntFields tests integer field methods.
func TestSecureEventIntFields(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	event := &SecureEvent{event: zl.Info()}

	event.Int("count", 42).Msg("test")
	output := buf.String()

	if !strings.Contains(output, `"count":42`) {
		t.Errorf("output should contain int field: %s", output)
	}
}

// TestSecureEventInt64Fields tests int64 field methods.
func TestSecureEventInt64Fields(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	event := &SecureEvent{event: zl.Info()}

	event.Int64("tokens", 1234567890123).Msg("test")
	output := buf.String()

	if !strings.Contains(output, `"tokens":1234567890123`) {
		t.Errorf("output should contain int64 field: %s", output)
	}
}

// TestSecureEventFloat64Fields tests float64 field methods.
func TestSecureEventFloat64Fields(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	event := &SecureEvent{event: zl.Info()}

	event.Float64("cost", 0.0086).Msg("test")
	output := buf.String()

	if !strings.Contains(output, "cost") {
		t.Errorf("output should contain float64 field: %s", output)
	}
}

// TestSecureEventBoolFields tests bool field methods.
func TestSecureEventBoolFields(t *testing.T) {
	tests := []struct {
		name     string
		value    bool
		expected string
	}{
		{"true value", true, `"enabled":true`},
		{"false value", false, `"enabled":false`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			zl := zerolog.New(&buf)
			event := &SecureEvent{event: zl.Info()}

			event.Bool("enabled", tt.value).Msg("test")
			output := buf.String()

			if !strings.Contains(output, tt.expected) {
				t.Errorf("output should contain bool field: %s", output)
			}
		})
	}
}

// TestSecureEventInterfaceNonString tests Interface with non-string types.
func TestSecureEventInterfaceNonString(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value interface{}
	}{
		{"int value", "count", 42},
		{"float value", "rate", 0.95},
		{"bool value", "active", true},
		{"slice value", "items", []int{1, 2, 3}},
		{"map value", "config", map[string]int{"a": 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			zl := zerolog.New(&buf)
			event := &SecureEvent{event: zl.Info()}

			event.Interface(tt.key, tt.value).Msg("test")
			output := buf.String()

			// Should contain the key
			if !strings.Contains(output, tt.key) {
				t.Errorf("output should contain key %s: %s", tt.key, output)
			}
		})
	}
}

// TestSecureEventEmptyStrings tests handling of empty strings.
func TestSecureEventEmptyStrings(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	event := &SecureEvent{event: zl.Info()}

	event.Str("empty", "").Msg("")
	output := buf.String()

	// Should still produce valid JSON output
	if !strings.Contains(output, `"empty":""`) {
		t.Errorf("output should contain empty string field: %s", output)
	}
}
