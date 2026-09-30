// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/liushuangls/go-anthropic/v2"
)

func TestRetryWithBackoffHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	start := time.Now()
	_, err := retryWithBackoff(ctx, 3, func() (int, error) {
		attempts++
		cancel()
		return 0, fmt.Errorf("retryable")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retryWithBackoff() error = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("cancelled retry took %v", elapsed)
	}
}

func TestRetryWithBackoffStopsOnPermanentError(t *testing.T) {
	attempts := 0
	want := &HTTPStatusError{StatusCode: 400, Body: "invalid request"}
	_, err := retryWithBackoff(context.Background(), 3, func() (int, error) {
		attempts++
		return 0, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("retryWithBackoff() error = %v, want %v", err, want)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestOverloadDetectionDoesNotMatchPort5030(t *testing.T) {
	if isOverloadedError(errors.New("dial http://localhost:5030 failed")) {
		t.Fatal("port 5030 must not be classified as HTTP 503 overload")
	}
}

func TestShouldRetryAnthropicRequestError(t *testing.T) {
	t.Parallel()

	if !shouldRetry(&anthropic.RequestError{StatusCode: http.StatusTooManyRequests, Err: errors.New("limited")}) {
		t.Fatal("shouldRetry() rejected Anthropic HTTP 429")
	}
	if !shouldRetry(&anthropic.RequestError{StatusCode: http.StatusServiceUnavailable, Err: errors.New("unavailable")}) {
		t.Fatal("shouldRetry() rejected Anthropic HTTP 503")
	}
	if shouldRetry(&anthropic.RequestError{StatusCode: http.StatusBadRequest, Err: errors.New("invalid")}) {
		t.Fatal("shouldRetry() accepted Anthropic HTTP 400")
	}
}
