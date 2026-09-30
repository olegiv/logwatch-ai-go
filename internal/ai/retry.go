// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/liushuangls/go-anthropic/v2"
)

const (
	// defaultMaxRetries is the default number of retry attempts for normal errors
	defaultMaxRetries = 3
)

// retryWithBackoff executes fn with error-aware exponential backoff retry logic.
// Rate limit and overload errors get longer backoff times (60-120 seconds),
// while other errors use standard exponential backoff (2^n seconds).
// Returns the result of the first successful call or the last error after maxAttempts.
func retryWithBackoff[T any](ctx context.Context, maxAttempts int, fn func() (T, error)) (T, error) {
	var result T
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var err error
		result, err = fn()
		if err == nil {
			return result, nil
		}

		lastErr = err
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		if !shouldRetry(err) {
			return result, err
		}
		if attempt < maxAttempts {
			// Use error-aware backoff duration
			backoff := getBackoffDuration(err, attempt)
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return result, ctx.Err()
			case <-timer.C:
			}
		}
	}

	return result, fmt.Errorf("all retry attempts failed: %w", lastErr)
}

func shouldRetry(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	if statusErr, ok := errors.AsType[*HTTPStatusError](err); ok {
		return statusErr.StatusCode == http.StatusTooManyRequests || statusErr.StatusCode >= 500
	}
	if anthropicRequestErr, ok := errors.AsType[*anthropic.RequestError](err); ok {
		return anthropicRequestErr.StatusCode == http.StatusTooManyRequests || anthropicRequestErr.StatusCode >= 500
	}
	if anthropicAPIErr, ok := errors.AsType[*anthropic.APIError](err); ok {
		return anthropicAPIErr.IsRateLimitErr() || anthropicAPIErr.IsOverloadedErr() || anthropicAPIErr.IsApiErr()
	}
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return true
	}
	if networkErr, ok := errors.AsType[net.Error](err); ok {
		return networkErr.Timeout()
	}

	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}
