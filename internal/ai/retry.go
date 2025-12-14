package ai

import (
	"fmt"
	"time"
)

const (
	// defaultMaxRetries is the default number of retry attempts for normal errors
	defaultMaxRetries = 3
)

// retryWithBackoff executes fn with error-aware exponential backoff retry logic.
// Rate limit and overload errors get longer backoff times (60-120 seconds),
// while other errors use standard exponential backoff (2^n seconds).
// Returns the result of the first successful call or the last error after maxAttempts.
func retryWithBackoff[T any](maxAttempts int, fn func() (T, error)) (T, error) {
	var result T
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var err error
		result, err = fn()
		if err == nil {
			return result, nil
		}

		lastErr = err
		if attempt < maxAttempts {
			// Use error-aware backoff duration
			backoff := getBackoffDuration(err, attempt)
			time.Sleep(backoff)
		}
	}

	return result, fmt.Errorf("all retry attempts failed: %w", lastErr)
}
