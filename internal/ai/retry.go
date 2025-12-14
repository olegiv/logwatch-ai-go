package ai

import (
	"fmt"
	"math"
	"time"
)

const (
	// defaultMaxRetries is the default number of retry attempts
	defaultMaxRetries = 3
)

// retryWithBackoff executes fn with exponential backoff retry logic.
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
			// Exponential backoff: 2^n * 1000ms
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			time.Sleep(backoff)
		}
	}

	return result, fmt.Errorf("all retry attempts failed: %w", lastErr)
}
