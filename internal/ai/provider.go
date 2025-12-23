package ai

import "context"

// Provider defines the interface for LLM providers (Anthropic, Ollama, etc.)
type Provider interface {
	// Analyze performs log analysis using provided prompts
	Analyze(ctx context.Context, systemPrompt, userPrompt string) (*Analysis, *Stats, error)

	// GetModelInfo returns information about the configured model
	GetModelInfo() map[string]interface{}

	// GetProviderName returns the name of the provider (e.g., "Anthropic", "Ollama")
	GetProviderName() string
}
