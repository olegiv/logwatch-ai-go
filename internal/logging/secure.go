// Package logging provides secure logging utilities with credential sanitization.
package logging

import (
	"github.com/olegiv/go-logger"
	internalerrors "github.com/olegiv/logwatch-ai-go/internal/errors"
	"github.com/rs/zerolog"
)

// SecureLogger wraps a logger.Logger and sanitizes all string values
// to prevent accidental credential exposure in logs (M-02 fix).
type SecureLogger struct {
	log *logger.Logger
}

// NewSecure creates a new SecureLogger wrapper around the provided logger.
func NewSecure(log *logger.Logger) *SecureLogger {
	return &SecureLogger{log: log}
}

// SecureEvent wraps a zerolog Event to provide secure string methods.
type SecureEvent struct {
	event *zerolog.Event
}

// Info starts a new info-level log event with credential sanitization.
func (s *SecureLogger) Info() *SecureEvent {
	return &SecureEvent{event: s.log.Info()}
}

// Debug starts a new debug-level log event with credential sanitization.
func (s *SecureLogger) Debug() *SecureEvent {
	return &SecureEvent{event: s.log.Debug()}
}

// Warn starts a new warn-level log event with credential sanitization.
func (s *SecureLogger) Warn() *SecureEvent {
	return &SecureEvent{event: s.log.Warn()}
}

// Error starts a new error-level log event with credential sanitization.
func (s *SecureLogger) Error() *SecureEvent {
	return &SecureEvent{event: s.log.Error()}
}

// Close closes the underlying logger.
func (s *SecureLogger) Close() error {
	return s.log.Close()
}

// Str adds a sanitized string field to the log event.
// Credentials are automatically redacted.
func (e *SecureEvent) Str(key, val string) *SecureEvent {
	e.event.Str(key, internalerrors.SanitizeString(val))
	return e
}

// Int adds an integer field to the log event.
func (e *SecureEvent) Int(key string, val int) *SecureEvent {
	e.event.Int(key, val)
	return e
}

// Int64 adds an int64 field to the log event.
func (e *SecureEvent) Int64(key string, val int64) *SecureEvent {
	e.event.Int64(key, val)
	return e
}

// Float64 adds a float64 field to the log event.
func (e *SecureEvent) Float64(key string, val float64) *SecureEvent {
	e.event.Float64(key, val)
	return e
}

// Bool adds a boolean field to the log event.
func (e *SecureEvent) Bool(key string, val bool) *SecureEvent {
	e.event.Bool(key, val)
	return e
}

// Err adds a sanitized error field to the log event.
// Credentials in error messages are automatically redacted.
func (e *SecureEvent) Err(err error) *SecureEvent {
	if err != nil {
		e.event.Err(internalerrors.SanitizeError(err))
	}
	return e
}

// Msg sends the log event with a sanitized message.
func (e *SecureEvent) Msg(msg string) {
	e.event.Msg(internalerrors.SanitizeString(msg))
}

// Msgf sends a formatted log event with sanitized format arguments.
// Note: Only string arguments are sanitized; other types pass through unchanged.
func (e *SecureEvent) Msgf(format string, v ...interface{}) {
	// Sanitize string arguments
	sanitizedArgs := make([]interface{}, len(v))
	for i, arg := range v {
		if s, ok := arg.(string); ok {
			sanitizedArgs[i] = internalerrors.SanitizeString(s)
		} else if err, ok := arg.(error); ok {
			sanitizedArgs[i] = internalerrors.SanitizeError(err)
		} else {
			sanitizedArgs[i] = arg
		}
	}
	e.event.Msgf(format, sanitizedArgs...)
}

// Interface adds an interface field to the log event.
// Warning: This does not sanitize the interface value.
// Use Str() for string values that may contain credentials.
func (e *SecureEvent) Interface(key string, val interface{}) *SecureEvent {
	// Try to sanitize string values
	if s, ok := val.(string); ok {
		e.event.Str(key, internalerrors.SanitizeString(s))
	} else {
		e.event.Interface(key, val)
	}
	return e
}
