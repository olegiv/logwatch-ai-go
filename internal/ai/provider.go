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

// ProviderType represents the type of LLM provider
type ProviderType string

const (
	ProviderAnthropic ProviderType = "anthropic"
	ProviderOllama    ProviderType = "ollama"
)

// ValidProviderTypes returns a list of valid provider types
func ValidProviderTypes() []ProviderType {
	return []ProviderType{ProviderAnthropic, ProviderOllama}
}

// IsValidProviderType checks if the given provider type is valid
func IsValidProviderType(pt string) bool {
	for _, valid := range ValidProviderTypes() {
		if string(valid) == pt {
			return true
		}
	}
	return false
}
