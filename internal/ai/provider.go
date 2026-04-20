// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import "context"

// Provider defines the interface for LLM providers (Anthropic, Ollama, etc.)
type Provider interface {
	// Analyze performs log analysis using provided prompts
	Analyze(ctx context.Context, systemPrompt, userPrompt string) (*Analysis, *Stats, error)

	// GetModelInfo returns information about the configured model
	GetModelInfo() map[string]any

	// GetProviderName returns the name of the provider (e.g., "Anthropic", "Ollama")
	GetProviderName() string
}

// PromptTokenCounter is an optional capability for providers that can count
// prompt tokens exactly before sending an analysis request.
type PromptTokenCounter interface {
	CountPromptTokens(ctx context.Context, systemPrompt, userPrompt string) (int, error)
}
