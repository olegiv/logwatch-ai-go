// Package errors provides utilities for sanitizing errors to prevent credential leakage.
package errors

import (
	"fmt"
	"regexp"
	"strings"
)

// Credential patterns to redact from error messages
var credentialPatterns = []*regexp.Regexp{
	// Anthropic API key: sk-ant-api03-... or sk-ant-... (variable length, min 10 chars after prefix)
	regexp.MustCompile(`sk-ant-[a-zA-Z0-9_-]{10,}`),
	// Generic OpenAI-style API key patterns
	regexp.MustCompile(`sk-[a-zA-Z0-9_-]{32,}`),
	// Telegram bot token: 123456789:ABC-DEF... (token part is typically 35-36 chars)
	regexp.MustCompile(`\d{8,12}:[a-zA-Z0-9_-]{30,}`),
	// Bearer tokens in headers
	regexp.MustCompile(`Bearer\s+[a-zA-Z0-9_.-]+`),
	// Authorization headers (matches "authorization: value" or "authorization value")
	regexp.MustCompile(`(?i)authorization[:\s]+[^\s]+`),
	// API key in URLs
	regexp.MustCompile(`(?i)api[_-]?key[=:][^\s&"']+`),
	// X-API-Key headers
	regexp.MustCompile(`(?i)x-api-key[:\s]+[^\s]+`),
}

const redactedPlaceholder = "[REDACTED]"

// SanitizeError wraps an error, redacting any credentials that may appear in the error message.
// This prevents sensitive information from being logged or exposed in error responses.
func SanitizeError(err error) error {
	if err == nil {
		return nil
	}

	sanitized := SanitizeString(err.Error())
	if sanitized == err.Error() {
		// No changes needed, return original error to preserve error chain
		return err
	}

	return &sanitizedError{
		original:  err,
		sanitized: sanitized,
	}
}

// SanitizeString redacts credential patterns from a string.
func SanitizeString(s string) string {
	result := s
	for _, pattern := range credentialPatterns {
		result = pattern.ReplaceAllString(result, redactedPlaceholder)
	}
	return result
}

// Wrapf wraps an error with a formatted message, sanitizing any credentials in the underlying error.
// This is a replacement for fmt.Errorf("...: %w", err) when the error may contain credentials.
func Wrapf(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}

	msg := fmt.Sprintf(format, args...)
	sanitizedErr := SanitizeError(err)

	return fmt.Errorf("%s: %w", msg, sanitizedErr)
}

// sanitizedError wraps an error with a sanitized message.
type sanitizedError struct {
	original  error
	sanitized string
}

func (e *sanitizedError) Error() string {
	return e.sanitized
}

func (e *sanitizedError) Unwrap() error {
	return e.original
}

// ContainsCredentials checks if a string appears to contain credentials.
// This can be used for defensive checks before logging.
func ContainsCredentials(s string) bool {
	for _, pattern := range credentialPatterns {
		if pattern.MatchString(s) {
			return true
		}
	}
	return false
}

// MaskCredential partially masks a credential string for safe logging.
// Example: "sk-ant-api03-abc123..." -> "sk-ant-***..."
func MaskCredential(s string) string {
	if len(s) < 10 {
		return strings.Repeat("*", len(s))
	}

	// Check for Anthropic API key format
	if strings.HasPrefix(s, "sk-ant-") {
		return "sk-ant-***..."
	}

	// Check for Telegram bot token format (number:token)
	if idx := strings.Index(s, ":"); idx > 0 && idx < 15 {
		parts := strings.SplitN(s, ":", 2)
		if len(parts) == 2 && len(parts[0]) <= 12 {
			return parts[0] + ":***..."
		}
	}

	// Generic masking: show first 4 chars + "***..."
	return s[:4] + "***..."
}
