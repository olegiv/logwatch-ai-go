// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package analyzer

import "strings"

// TrimToTokenBudget trims content line-by-line until it fits within maxTokens,
// verifying each candidate with EstimateTokens via a binary search over the
// number of retained lines. A truncation notice is appended when lines are
// dropped. maxTokens <= 0 disables trimming and returns content unchanged.
// If not even one line fits, the notice alone is returned, or "" when the
// notice itself exceeds the budget.
//
// Shared terminal fallback for preprocessors: unlike line-count heuristics,
// the returned content is guaranteed to fit the budget (for maxTokens > 0).
func TrimToTokenBudget(content string, maxTokens int) string {
	if maxTokens <= 0 || EstimateTokens(content) <= maxTokens {
		return content
	}

	lines := strings.Split(content, "\n")
	truncationNotice := "[... truncated to fit token budget ...]"

	low := 0
	high := len(lines)

	for low < high {
		mid := (low + high + 1) / 2
		candidate := strings.Join(lines[:mid], "\n")
		if mid < len(lines) {
			candidate += "\n" + truncationNotice
		}

		if EstimateTokens(candidate) <= maxTokens {
			low = mid
		} else {
			high = mid - 1
		}
	}

	if low == 0 {
		if EstimateTokens(truncationNotice) <= maxTokens {
			return truncationNotice
		}
		return ""
	}

	result := strings.Join(lines[:low], "\n")
	if low < len(lines) {
		candidate := result + "\n" + truncationNotice
		if EstimateTokens(candidate) <= maxTokens {
			return candidate
		}
	}

	return result
}
