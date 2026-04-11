// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"strings"
	"testing"
)

func TestReadResponseBodyLimited_Success(t *testing.T) {
	t.Parallel()

	body := strings.NewReader(`{"ok":true}`)
	data, err := readResponseBodyLimited(body, 1024)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if string(data) != `{"ok":true}` {
		t.Fatalf("unexpected body: %q", string(data))
	}
}

func TestReadResponseBodyLimited_TooLarge(t *testing.T) {
	t.Parallel()

	body := strings.NewReader(strings.Repeat("a", int(maxAPIResponseBodyBytes)+1))
	_, err := readResponseBodyLimited(body, maxAPIResponseBodyBytes)
	if err == nil {
		t.Fatal("expected error for oversized response body")
	}

	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size limit error, got %v", err)
	}
}
