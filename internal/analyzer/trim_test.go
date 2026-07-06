// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package analyzer

import (
	"fmt"
	"strings"
	"testing"
)

func TestTrimToTokenBudget(t *testing.T) {
	t.Run("content already fits", func(t *testing.T) {
		content := "short content"
		if got := TrimToTokenBudget(content, 1000); got != content {
			t.Errorf("TrimToTokenBudget() = %q, want unchanged content", got)
		}
	})

	t.Run("zero budget returns content unchanged", func(t *testing.T) {
		content := "some content"
		if got := TrimToTokenBudget(content, 0); got != content {
			t.Errorf("TrimToTokenBudget() = %q, want unchanged content", got)
		}
	})

	t.Run("negative budget returns content unchanged", func(t *testing.T) {
		content := "some content"
		if got := TrimToTokenBudget(content, -5); got != content {
			t.Errorf("TrimToTokenBudget() = %q, want unchanged content", got)
		}
	})

	t.Run("trims large content within budget", func(t *testing.T) {
		var sb strings.Builder
		for i := range 500 {
			fmt.Fprintf(&sb, "log entry number %d with some additional text here\n", i)
		}

		const budget = 200
		result := TrimToTokenBudget(sb.String(), budget)

		if got := EstimateTokens(result); got > budget {
			t.Fatalf("TrimToTokenBudget() produced %d tokens, want <= %d", got, budget)
		}
		if !strings.Contains(result, "truncated to fit token budget") {
			t.Error("expected truncation notice in result")
		}
		if !strings.Contains(result, "log entry number 0") {
			t.Error("expected first lines to be retained")
		}
	})

	t.Run("single oversized line returns notice only", func(t *testing.T) {
		result := TrimToTokenBudget(strings.Repeat("x", 10000), 20)
		if result != "[... truncated to fit token budget ...]" {
			t.Errorf("TrimToTokenBudget() = %q, want truncation notice only", result)
		}
	})

	t.Run("budget below notice size returns empty", func(t *testing.T) {
		result := TrimToTokenBudget(strings.Repeat("x", 10000), 1)
		if result != "" {
			t.Errorf("TrimToTokenBudget() = %q, want empty string", result)
		}
	})
}
