// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package analyzer

const (
	// DefaultContextLimit is used when the provider does not report a context window.
	DefaultContextLimit = 128000

	minPromptSafetyMarginTokens = 2000
	promptSafetyMarginDivisor   = 20
	minLogTokenBudget           = 1000
)

// ContextLimitFromModelInfo extracts a model context limit from provider metadata.
// Falls back to DefaultContextLimit when the value is missing or invalid.
func ContextLimitFromModelInfo(modelInfo map[string]any) int {
	if modelInfo == nil {
		return DefaultContextLimit
	}

	switch v := modelInfo["context_limit"].(type) {
	case int:
		if v > 0 {
			return v
		}
	case int32:
		if v > 0 {
			return int(v)
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	}

	return DefaultContextLimit
}

// CalculateLogTokenBudget computes how many tokens can be safely allocated to log content.
// It reserves space for the response, the fixed prompt overhead, and an additional safety margin.
func CalculateLogTokenBudget(contextLimit, responseReserve, systemPromptTokens, userPromptOverheadTokens int) int {
	if contextLimit <= 0 {
		contextLimit = DefaultContextLimit
	}
	if responseReserve < 0 {
		responseReserve = 0
	}
	if systemPromptTokens < 0 {
		systemPromptTokens = 0
	}
	if userPromptOverheadTokens < 0 {
		userPromptOverheadTokens = 0
	}

	safetyMargin := max(contextLimit/promptSafetyMarginDivisor, minPromptSafetyMarginTokens)

	budget := contextLimit - responseReserve - systemPromptTokens - userPromptOverheadTokens - safetyMargin
	if budget < minLogTokenBudget {
		return minLogTokenBudget
	}

	return budget
}
