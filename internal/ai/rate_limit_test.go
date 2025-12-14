package ai

import (
	"errors"
	"testing"
	"time"

	"github.com/liushuangls/go-anthropic/v2"
)

func TestIsRateLimitError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "Anthropic API rate limit error",
			err:  &anthropic.APIError{Type: anthropic.ErrTypeRateLimit, Message: "Rate limit exceeded"},
			want: true,
		},
		{
			name: "Anthropic API authentication error",
			err:  &anthropic.APIError{Type: anthropic.ErrTypeAuthentication, Message: "Invalid API key"},
			want: false,
		},
		{
			name: "Anthropic API invalid request error",
			err:  &anthropic.APIError{Type: anthropic.ErrTypeInvalidRequest, Message: "Invalid request"},
			want: false,
		},
		{
			name: "String contains rate_limit_error",
			err:  errors.New("rate_limit_error: This request would exceed the rate limit"),
			want: true,
		},
		{
			name: "String contains 429",
			err:  errors.New("API returned status 429"),
			want: true,
		},
		{
			name: "String contains rate limit phrase",
			err:  errors.New("rate limit exceeded for organization"),
			want: true,
		},
		{
			name: "String contains too many requests",
			err:  errors.New("too many requests, please try again later"),
			want: true,
		},
		{
			name: "Generic connection error",
			err:  errors.New("connection timeout"),
			want: false,
		},
		{
			name: "Generic API error",
			err:  errors.New("internal server error"),
			want: false,
		},
		{
			name: "Wrapped rate limit error",
			err:  errors.New("API call failed: rate_limit_error: exceeded"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRateLimitError(tt.err)
			if got != tt.want {
				t.Errorf("isRateLimitError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsOverloadedError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "Anthropic API overloaded error",
			err:  &anthropic.APIError{Type: anthropic.ErrTypeOverloaded, Message: "API is overloaded"},
			want: true,
		},
		{
			name: "Anthropic API rate limit error - not overloaded",
			err:  &anthropic.APIError{Type: anthropic.ErrTypeRateLimit, Message: "Rate limit exceeded"},
			want: false,
		},
		{
			name: "String contains overloaded",
			err:  errors.New("API is currently overloaded"),
			want: true,
		},
		{
			name: "String contains 503",
			err:  errors.New("API returned status 503"),
			want: true,
		},
		{
			name: "Generic connection error",
			err:  errors.New("connection timeout"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOverloadedError(tt.err)
			if got != tt.want {
				t.Errorf("isOverloadedError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetBackoffDuration(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		attempt int
		wantMin time.Duration
		wantMax time.Duration
	}{
		{
			name:    "Rate limit error attempt 1",
			err:     &anthropic.APIError{Type: anthropic.ErrTypeRateLimit},
			attempt: 1,
			wantMin: 60 * time.Second,
			wantMax: 60 * time.Second,
		},
		{
			name:    "Rate limit error attempt 2",
			err:     &anthropic.APIError{Type: anthropic.ErrTypeRateLimit},
			attempt: 2,
			wantMin: 120 * time.Second,
			wantMax: 120 * time.Second,
		},
		{
			name:    "Rate limit error attempt 3 - capped at max",
			err:     &anthropic.APIError{Type: anthropic.ErrTypeRateLimit},
			attempt: 3,
			wantMin: 120 * time.Second, // Capped at max
			wantMax: 120 * time.Second,
		},
		{
			name:    "Overloaded error attempt 1",
			err:     &anthropic.APIError{Type: anthropic.ErrTypeOverloaded},
			attempt: 1,
			wantMin: 60 * time.Second,
			wantMax: 60 * time.Second,
		},
		{
			name:    "String rate limit error",
			err:     errors.New("rate_limit_error: exceeded"),
			attempt: 1,
			wantMin: 60 * time.Second,
			wantMax: 60 * time.Second,
		},
		{
			name:    "Normal error attempt 1",
			err:     errors.New("connection timeout"),
			attempt: 1,
			wantMin: 2 * time.Second,
			wantMax: 2 * time.Second,
		},
		{
			name:    "Normal error attempt 2",
			err:     errors.New("connection timeout"),
			attempt: 2,
			wantMin: 4 * time.Second,
			wantMax: 4 * time.Second,
		},
		{
			name:    "Normal error attempt 3",
			err:     errors.New("connection timeout"),
			attempt: 3,
			wantMin: 8 * time.Second,
			wantMax: 8 * time.Second,
		},
		{
			name:    "Nil error (fallback to standard backoff)",
			err:     nil,
			attempt: 1,
			wantMin: 2 * time.Second,
			wantMax: 2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getBackoffDuration(tt.err, tt.attempt)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("getBackoffDuration() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestGetBackoffDuration_RateLimitVsNormal(t *testing.T) {
	// Verify that rate limit errors get significantly longer backoff
	rateLimitErr := errors.New("rate_limit_error: exceeded")
	normalErr := errors.New("connection timeout")

	rateLimitBackoff := getBackoffDuration(rateLimitErr, 1)
	normalBackoff := getBackoffDuration(normalErr, 1)

	if rateLimitBackoff <= normalBackoff {
		t.Errorf("Rate limit backoff (%v) should be greater than normal backoff (%v)",
			rateLimitBackoff, normalBackoff)
	}

	// Rate limit should be at least 30x longer than normal
	if rateLimitBackoff < 30*normalBackoff {
		t.Errorf("Rate limit backoff (%v) should be at least 30x normal backoff (%v)",
			rateLimitBackoff, normalBackoff)
	}
}
