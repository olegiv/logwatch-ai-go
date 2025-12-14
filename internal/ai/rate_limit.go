package ai

import (
	"errors"
	"strings"
	"time"

	"github.com/liushuangls/go-anthropic/v2"
)

const (
	// rateLimitBaseBackoff is the initial wait time for rate limit errors (60 seconds)
	// This is appropriate for Anthropic's token-based rate limits which reset per minute
	rateLimitBaseBackoff = 60 * time.Second

	// rateLimitMaxBackoff is the maximum wait time for rate limit errors (2 minutes)
	rateLimitMaxBackoff = 120 * time.Second
)

// isRateLimitError detects if an error is a rate limit error from any LLM provider.
// It checks both the Anthropic SDK error type and error message patterns.
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}

	// Check Anthropic SDK error type
	var apiErr *anthropic.APIError
	if errors.As(err, &apiErr) {
		return apiErr.IsRateLimitErr()
	}

	// Fallback: check error message for rate limit indicators
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "rate_limit_error") ||
		strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "too many requests")
}

// isOverloadedError detects if an error indicates API overload.
// Overloaded errors should be treated similarly to rate limits.
func isOverloadedError(err error) bool {
	if err == nil {
		return false
	}

	// Check Anthropic SDK error type
	var apiErr *anthropic.APIError
	if errors.As(err, &apiErr) {
		return apiErr.IsOverloadedErr()
	}

	// Fallback: check error message
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "overloaded") ||
		strings.Contains(errStr, "503")
}

// getBackoffDuration returns the appropriate backoff duration based on error type.
// Rate limit and overload errors get longer backoff times (60-120 seconds),
// while other errors use standard exponential backoff (2^n seconds).
func getBackoffDuration(err error, attempt int) time.Duration {
	if isRateLimitError(err) || isOverloadedError(err) {
		// Rate limit/overload: use longer backoff to wait for token window reset
		backoff := rateLimitBaseBackoff * time.Duration(attempt)
		if backoff > rateLimitMaxBackoff {
			return rateLimitMaxBackoff
		}
		return backoff
	}

	// Standard exponential backoff: 2^n seconds (2s, 4s, 8s, ...)
	return time.Duration(1<<attempt) * time.Second
}
