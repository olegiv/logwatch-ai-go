package logger

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger wraps zerolog.Logger with additional functionality
type Logger struct {
	zerolog.Logger
}

// Config holds logger configuration
type Config struct {
	Level      string // debug, info, warn, error
	LogDir     string
	MaxSizeMB  int
	MaxBackups int
	Console    bool // Enable console output
}

// New creates a new logger instance
func New(cfg Config) *Logger {
	// Set defaults
	if cfg.LogDir == "" {
		cfg.LogDir = "./logs"
	}
	if cfg.MaxSizeMB == 0 {
		cfg.MaxSizeMB = 10
	}
	if cfg.MaxBackups == 0 {
		cfg.MaxBackups = 5
	}

	// Create log directory if it doesn't exist
	if err := os.MkdirAll(cfg.LogDir, 0755); err != nil {
		// Fallback to stderr if directory creation fails
		return &Logger{
			Logger: zerolog.New(os.Stderr).With().Timestamp().Logger(),
		}
	}

	// Set log level
	level := parseLogLevel(cfg.Level)
	zerolog.SetGlobalLevel(level)

	// Configure file rotation
	fileWriter := &lumberjack.Logger{
		Filename:   filepath.Join(cfg.LogDir, "logwatch-analyzer.log"),
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     30, // days
		Compress:   false,
	}

	// Create multi-writer (file + console if enabled)
	var writers []io.Writer
	writers = append(writers, fileWriter)

	if cfg.Console {
		consoleWriter := zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "2006-01-02 15:04:05",
			NoColor:    false,
		}
		writers = append(writers, consoleWriter)
	}

	multiWriter := io.MultiWriter(writers...)

	// Create logger
	logger := zerolog.New(multiWriter).
		With().
		Timestamp().
		Caller().
		Logger()

	return &Logger{Logger: logger}
}

// parseLogLevel converts string log level to zerolog level
func parseLogLevel(level string) zerolog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// Close closes the logger (flushes any buffered logs)
func (l *Logger) Close() error {
	// Zerolog doesn't require explicit closing, but we can sync here if needed
	return nil
}

// WithField adds a field to the logger
func (l *Logger) WithField(key string, value interface{}) *Logger {
	newLogger := l.Logger.With().Interface(key, value).Logger()
	return &Logger{Logger: newLogger}
}

// WithFields adds multiple fields to the logger
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	ctx := l.Logger.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	newLogger := ctx.Logger()
	return &Logger{Logger: newLogger}
}

// WithError adds an error to the logger context
func (l *Logger) WithError(err error) *Logger {
	newLogger := l.Logger.With().Err(err).Logger()
	return &Logger{Logger: newLogger}
}
