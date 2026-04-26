// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ocms

import (
	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
	"github.com/olegiv/logwatch-ai-go/internal/logwatch"
)

// Preprocessor wraps logwatch preprocessing for OCMS log content.
type Preprocessor struct {
	inner *logwatch.Preprocessor
}

var _ analyzer.Preprocessor = (*Preprocessor)(nil)
var _ analyzer.BudgetPreprocessor = (*Preprocessor)(nil)

// NewPreprocessor creates a new OCMS preprocessor.
func NewPreprocessor(maxTokens int) *Preprocessor {
	return &Preprocessor{
		inner: logwatch.NewPreprocessor(maxTokens),
	}
}

// EstimateTokens estimates token count for content.
func (p *Preprocessor) EstimateTokens(content string) int {
	return p.inner.EstimateTokens(content)
}

// Process preprocesses content when needed.
func (p *Preprocessor) Process(content string) (string, error) {
	return p.inner.Process(content)
}

// ProcessWithBudget preprocesses content against a dynamic token budget.
func (p *Preprocessor) ProcessWithBudget(content string, maxTokens int) (string, error) {
	return p.inner.ProcessWithBudget(content, maxTokens)
}

// ShouldProcess reports whether preprocessing is needed.
func (p *Preprocessor) ShouldProcess(content string, maxTokens int) bool {
	return p.inner.ShouldProcess(content, maxTokens)
}
