package errors

import (
	"errors"
	"testing"
)

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no credentials",
			input:    "simple error message",
			expected: "simple error message",
		},
		{
			name:     "anthropic API key",
			input:    "failed to call API with key sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890",
			expected: "failed to call API with key [REDACTED]",
		},
		{
			name:     "short anthropic API key",
			input:    "invalid key: sk-ant-abc123xyz789def456",
			expected: "invalid key: [REDACTED]",
		},
		{
			name:     "telegram bot token",
			input:    "bot token 1234567890:ABCdefGHI_jklMNOpqrSTUvwxYZ-12345678",
			expected: "bot token [REDACTED]",
		},
		{
			name:     "bearer token",
			input:    "Token: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			expected: "Token: [REDACTED]",
		},
		{
			name:     "authorization header",
			input:    "request failed with authorization: sk-test-key",
			expected: "request failed with [REDACTED]",
		},
		{
			name:     "api key in url",
			input:    "https://api.example.com?api_key=secret123456",
			expected: "https://api.example.com?[REDACTED]",
		},
		{
			name:     "x-api-key header",
			input:    "x-api-key: my-secret-key-12345",
			expected: "[REDACTED]",
		},
		{
			name:     "multiple credentials",
			input:    "key1: sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890, bot: 1234567890:ABCdefGHI_jklMNOpqrSTUvwxYZ-12345678",
			expected: "key1: [REDACTED], bot: [REDACTED]",
		},
		{
			name:     "credential in json error",
			input:    `{"error":"invalid_api_key","key":"sk-ant-api03-test1234567890abcdefghij"}`,
			expected: `{"error":"invalid_api_key","key":"[REDACTED]"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeString(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeString() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSanitizeError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantNil     bool
		wantMessage string
	}{
		{
			name:    "nil error",
			err:     nil,
			wantNil: true,
		},
		{
			name:        "no credentials",
			err:         errors.New("connection timeout"),
			wantMessage: "connection timeout",
		},
		{
			name:        "error with API key",
			err:         errors.New("failed with key sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890"),
			wantMessage: "failed with key [REDACTED]",
		},
		{
			name:        "error with bot token",
			err:         errors.New("telegram error: 1234567890:ABCdefGHI_jklMNOpqrSTUvwxYZ-12345678"),
			wantMessage: "telegram error: [REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeError(tt.err)

			if tt.wantNil {
				if result != nil {
					t.Errorf("SanitizeError() = %v, want nil", result)
				}
				return
			}

			if result == nil {
				t.Fatal("SanitizeError() = nil, want non-nil")
			}

			if result.Error() != tt.wantMessage {
				t.Errorf("SanitizeError().Error() = %q, want %q", result.Error(), tt.wantMessage)
			}
		})
	}
}

func TestWrapf(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		format      string
		args        []interface{}
		wantNil     bool
		wantMessage string
	}{
		{
			name:    "nil error",
			err:     nil,
			format:  "wrapped",
			wantNil: true,
		},
		{
			name:        "wrap clean error",
			err:         errors.New("connection failed"),
			format:      "API call failed",
			wantMessage: "API call failed: connection failed",
		},
		{
			name:        "wrap error with credential",
			err:         errors.New("invalid key sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890"),
			format:      "authentication failed",
			wantMessage: "authentication failed: invalid key [REDACTED]",
		},
		{
			name:        "wrap with format args",
			err:         errors.New("error"),
			format:      "operation %s failed with code %d",
			args:        []interface{}{"upload", 500},
			wantMessage: "operation upload failed with code 500: error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Wrapf(tt.err, tt.format, tt.args...)

			if tt.wantNil {
				if result != nil {
					t.Errorf("Wrapf() = %v, want nil", result)
				}
				return
			}

			if result == nil {
				t.Fatal("Wrapf() = nil, want non-nil")
			}

			if result.Error() != tt.wantMessage {
				t.Errorf("Wrapf().Error() = %q, want %q", result.Error(), tt.wantMessage)
			}
		})
	}
}

func TestContainsCredentials(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "no credentials",
			input: "regular error message",
			want:  false,
		},
		{
			name:  "anthropic key",
			input: "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890",
			want:  true,
		},
		{
			name:  "telegram token",
			input: "1234567890:ABCdefGHI_jklMNOpqrSTUvwxYZ-12345678",
			want:  true,
		},
		{
			name:  "bearer token",
			input: "Bearer some-jwt-token",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainsCredentials(tt.input); got != tt.want {
				t.Errorf("ContainsCredentials() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMaskCredential(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "short string",
			input: "abc",
			want:  "***",
		},
		{
			name:  "anthropic key",
			input: "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890",
			want:  "sk-ant-***...",
		},
		{
			name:  "telegram token",
			input: "1234567890:ABCdefGHI_jklMNOpqrSTUvwxYZ-12345678",
			want:  "1234567890:***...",
		},
		{
			name:  "generic string",
			input: "some-random-long-string-here",
			want:  "some***...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskCredential(tt.input); got != tt.want {
				t.Errorf("MaskCredential() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizedErrorUnwrap(t *testing.T) {
	originalErr := errors.New("original: sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890")
	sanitizedErr := SanitizeError(originalErr)

	// errors.Is should find the original error in the chain
	if !errors.Is(sanitizedErr, originalErr) {
		t.Error("errors.Is() should find original error in sanitized error chain")
	}

	// Error message should be sanitized
	if sanitizedErr.Error() == originalErr.Error() {
		t.Error("sanitized error message should differ from original")
	}
}
