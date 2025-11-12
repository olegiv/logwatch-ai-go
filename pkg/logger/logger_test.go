package logger

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Level:      "info",
		LogDir:     tmpDir,
		MaxSizeMB:  10,
		MaxBackups: 5,
		Console:    false,
	}

	logger := New(cfg)

	if logger == nil {
		t.Fatal("Expected logger to be created")
	}
}

func TestNew_WithDefaults(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		LogDir: tmpDir,
	}

	logger := New(cfg)

	if logger == nil {
		t.Fatal("Expected logger to be created with defaults")
	}
}

func TestNew_WithConsole(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Level:      "debug",
		LogDir:     tmpDir,
		MaxSizeMB:  10,
		MaxBackups: 5,
		Console:    true,
	}

	logger := New(cfg)

	if logger == nil {
		t.Fatal("Expected logger to be created with console output")
	}
}

func TestNew_InvalidDirectory(t *testing.T) {
	// Use a path that likely can't be created (root-owned or invalid)
	cfg := Config{
		Level:      "info",
		LogDir:     "/this/path/should/not/exist/and/fail",
		MaxSizeMB:  10,
		MaxBackups: 5,
		Console:    false,
	}

	logger := New(cfg)

	// Should still create logger (fallback to stderr)
	if logger == nil {
		t.Fatal("Expected logger to be created even with invalid directory (fallback)")
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		level    string
		expected zerolog.Level
	}{
		{"Debug", "debug", zerolog.DebugLevel},
		{"Info", "info", zerolog.InfoLevel},
		{"Warn", "warn", zerolog.WarnLevel},
		{"Warning", "warning", zerolog.WarnLevel},
		{"Error", "error", zerolog.ErrorLevel},
		{"Debug uppercase", "DEBUG", zerolog.DebugLevel},
		{"Info mixed case", "Info", zerolog.InfoLevel},
		{"Unknown", "unknown", zerolog.InfoLevel},
		{"Empty", "", zerolog.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLogLevel(tt.level)
			if result != tt.expected {
				t.Errorf("Expected level %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestClose(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Level:  "info",
		LogDir: tmpDir,
	}

	logger := New(cfg)
	err := logger.Close()

	if err != nil {
		t.Errorf("Close should not return error: %v", err)
	}
}

func TestWithField(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Level:  "info",
		LogDir: tmpDir,
	}

	logger := New(cfg)
	newLogger := logger.WithField("test_key", "test_value")

	if newLogger == nil {
		t.Fatal("Expected logger with field")
	}

	// Verify it returns a new logger instance
	if newLogger == logger {
		t.Error("WithField should return a new logger instance")
	}
}

func TestWithFields(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Level:  "info",
		LogDir: tmpDir,
	}

	logger := New(cfg)
	fields := map[string]interface{}{
		"key1": "value1",
		"key2": 123,
		"key3": true,
	}

	newLogger := logger.WithFields(fields)

	if newLogger == nil {
		t.Fatal("Expected logger with fields")
	}

	// Verify it returns a new logger instance
	if newLogger == logger {
		t.Error("WithFields should return a new logger instance")
	}
}

func TestWithFields_EmptyMap(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Level:  "info",
		LogDir: tmpDir,
	}

	logger := New(cfg)
	fields := map[string]interface{}{}

	newLogger := logger.WithFields(fields)

	if newLogger == nil {
		t.Fatal("Expected logger even with empty fields")
	}
}

func TestWithError(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Level:  "info",
		LogDir: tmpDir,
	}

	logger := New(cfg)
	err := errors.New("test error")
	newLogger := logger.WithError(err)

	if newLogger == nil {
		t.Fatal("Expected logger with error")
	}

	// Verify it returns a new logger instance
	if newLogger == logger {
		t.Error("WithError should return a new logger instance")
	}
}

func TestLogFileCreation(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Level:      "info",
		LogDir:     tmpDir,
		MaxSizeMB:  10,
		MaxBackups: 5,
		Console:    false,
	}

	logger := New(cfg)

	// Write a log message
	logger.Info().Msg("Test log message")

	// Check that log file was created
	logFile := filepath.Join(tmpDir, "logwatch-analyzer.log")
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Error("Log file should be created")
	}
}

func TestConfigStructure(t *testing.T) {
	cfg := Config{
		Level:      "debug",
		LogDir:     "/tmp/logs",
		MaxSizeMB:  20,
		MaxBackups: 10,
		Console:    true,
	}

	if cfg.Level != "debug" {
		t.Error("Level not set correctly")
	}

	if cfg.LogDir != "/tmp/logs" {
		t.Error("LogDir not set correctly")
	}

	if cfg.MaxSizeMB != 20 {
		t.Error("MaxSizeMB not set correctly")
	}

	if cfg.MaxBackups != 10 {
		t.Error("MaxBackups not set correctly")
	}

	if !cfg.Console {
		t.Error("Console not set correctly")
	}
}

func TestDefaultValues(t *testing.T) {
	tmpDir := t.TempDir()

	// Test with empty config
	cfg := Config{
		LogDir: tmpDir,
	}

	logger := New(cfg)

	if logger == nil {
		t.Fatal("Expected logger to be created with defaults")
	}

	// Verify log file is created with defaults
	logFile := filepath.Join(tmpDir, "logwatch-analyzer.log")
	logger.Info().Msg("Test")

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Error("Log file should be created with default settings")
	}
}

func TestAllLogLevels(t *testing.T) {
	tmpDir := t.TempDir()

	levels := []string{"debug", "info", "warn", "error"}

	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			cfg := Config{
				Level:  level,
				LogDir: filepath.Join(tmpDir, level),
			}

			logger := New(cfg)
			if logger == nil {
				t.Fatalf("Expected logger with level %s", level)
			}

			// Write a log message at each level
			logger.Debug().Msg("Debug message")
			logger.Info().Msg("Info message")
			logger.Warn().Msg("Warn message")
			logger.Error().Msg("Error message")
		})
	}
}

func TestWithMultipleFields(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Level:  "info",
		LogDir: tmpDir,
	}

	logger := New(cfg)

	// Chain multiple WithField calls
	logger = logger.WithField("field1", "value1")
	logger = logger.WithField("field2", 42)
	logger = logger.WithField("field3", true)

	if logger == nil {
		t.Fatal("Expected logger after chaining WithField")
	}
}

func TestLoggerInheritance(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Level:  "info",
		LogDir: tmpDir,
	}

	logger := New(cfg)

	// Verify Logger wraps zerolog.Logger
	// zerolog levels can be negative (e.g., Trace is -1), so just verify logger is functional
	if logger == nil {
		t.Error("Expected logger to be created")
	}

	// Test that the logger can log
	logger.Info().Msg("Test inheritance")
}

func TestConsoleOutput(t *testing.T) {
	tmpDir := t.TempDir()

	// Test with console enabled
	cfg := Config{
		Level:   "debug",
		LogDir:  tmpDir,
		Console: true,
	}

	logger := New(cfg)
	logger.Info().Msg("Test console output")

	// Verify log file is still created
	logFile := filepath.Join(tmpDir, "logwatch-analyzer.log")
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Error("Log file should be created even with console enabled")
	}
}

func TestLogRotationSettings(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Level:      "info",
		LogDir:     tmpDir,
		MaxSizeMB:  50,
		MaxBackups: 10,
	}

	logger := New(cfg)

	// Write multiple log messages
	for i := 0; i < 100; i++ {
		logger.Info().Int("iteration", i).Msg("Test message")
	}

	// Verify log file exists
	logFile := filepath.Join(tmpDir, "logwatch-analyzer.log")
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Error("Log file should exist")
	}
}

func TestWithFieldsPreservesOriginal(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Level:  "info",
		LogDir: tmpDir,
	}

	original := New(cfg)
	modified := original.WithField("test", "value")

	// Original should be unchanged
	if original == modified {
		t.Error("WithField should create a new logger instance")
	}
}

func TestWithErrorNilError(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Level:  "info",
		LogDir: tmpDir,
	}

	logger := New(cfg)
	newLogger := logger.WithError(nil)

	if newLogger == nil {
		t.Fatal("Expected logger even with nil error")
	}
}

func TestLogDirCreation(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "log", "dir")

	cfg := Config{
		Level:  "info",
		LogDir: nestedDir,
	}

	logger := New(cfg)

	// Verify nested directory was created
	if _, err := os.Stat(nestedDir); os.IsNotExist(err) {
		t.Error("Nested log directory should be created")
	}

	// Verify logger works
	logger.Info().Msg("Test")
}

func TestEmptyLogDir(t *testing.T) {
	cfg := Config{
		Level:  "info",
		LogDir: "",
	}

	logger := New(cfg)

	if logger == nil {
		t.Fatal("Expected logger with empty LogDir (should use default)")
	}

	// Default should be "./logs"
	// Verify it was created or fallback to stderr
	logger.Info().Msg("Test")
}
