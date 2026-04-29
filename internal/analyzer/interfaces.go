// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

// Package analyzer provides common interfaces for log analysis.
// This abstraction layer enables support for multiple log source types
// (logwatch, drupal_watchdog, ocms) through a unified interface.
package analyzer

import "strings"

// EstimateTokens estimates the number of tokens in the content.
// Uses the algorithm: max(chars/4, words/0.75)
// This is a shared utility function used by all Preprocessor implementations.
func EstimateTokens(content string) int {
	chars := len(content)
	words := len(strings.Fields(content))

	charsEstimate := chars / 4
	wordsEstimate := int(float64(words) / 0.75)

	if charsEstimate > wordsEstimate {
		return charsEstimate
	}
	return wordsEstimate
}

// LogReader reads and validates log content from a source.
// Implementations handle format-specific parsing and validation.
type LogReader interface {
	// Read reads log content from the specified source path.
	// Returns the processed content ready for analysis.
	Read(sourcePath string) (string, error)

	// Validate checks if the content is valid for this log type.
	// Called internally by Read, but exposed for testing.
	Validate(content string) error

	// GetSourceInfo returns metadata about the log source.
	// Common keys: size_bytes, size_mb, modified, age_hours
	GetSourceInfo(sourcePath string) (map[string]any, error)
}

// Preprocessor handles content preprocessing for large logs.
// Reduces token count while preserving critical information.
type Preprocessor interface {
	// EstimateTokens estimates the number of tokens in the content.
	// Uses the algorithm: max(chars/4, words/0.75)
	EstimateTokens(content string) int

	// Process preprocesses content to reduce size while preserving critical info.
	// Returns processed content or original if no processing needed.
	Process(content string) (string, error)

	// ShouldProcess determines if preprocessing is needed based on token count.
	ShouldProcess(content string, maxTokens int) bool
}

// BudgetPreprocessor extends Preprocessor with support for dynamic token budgets.
// Implementations should return content that fits within maxTokens whenever possible.
type BudgetPreprocessor interface {
	ProcessWithBudget(content string, maxTokens int) (string, error)
}

// PromptBuilder constructs prompts for Claude AI analysis.
// Each log type has its own prompt builder with specialized instructions.
type PromptBuilder interface {
	// GetSystemPrompt returns the system prompt for this log type.
	// This prompt defines Claude's role and analysis framework.
	//
	// globalExclusions, if non-empty, are rendered as an operator-scoped
	// instruction block telling the LLM to ignore matching findings and
	// their influence on systemStatus, summary, and metrics. Pass nil for
	// no exclusions; the resulting prompt is byte-identical to the
	// pre-exclusion output, which preserves Anthropic prompt-cache hits.
	GetSystemPrompt(globalExclusions []string) string

	// GetUserPrompt constructs the user prompt with log content and history.
	// The logContent should already be sanitized before passing.
	//
	// contextualExclusions, if non-empty, are rendered as a run-scoped
	// instruction block (source-wide and/or site-specific patterns). Pass
	// nil for no exclusions.
	GetUserPrompt(logContent, historicalContext string, contextualExclusions []string) string

	// GetLogType returns the type identifier (e.g., "logwatch", "drupal_watchdog", "ocms").
	GetLogType() string
}
