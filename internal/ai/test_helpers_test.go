package ai

import (
	"encoding/json"
	"net/http"
	"testing"
)

// chatMessage represents a message with a Role field for validation.
type chatMessage interface {
	GetRole() string
}

// Implement GetRole for both message types
func (m openAIMessage) GetRole() string  { return m.Role }
func (m ollamaMessage) GetRole() string  { return m.Role }

// chatRequest is a constraint for chat request types that can be validated.
type chatRequest interface {
	openAIChatRequest | ollamaChatRequest
}

// verifyChatRequest is a generic helper that decodes and validates chat requests.
func verifyChatRequest[T chatRequest](t *testing.T, r *http.Request, w http.ResponseWriter) *T {
	t.Helper()

	var req T
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Errorf("failed to decode request: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}

	// Use type switch to access fields since Go generics don't support field access
	switch v := any(&req).(type) {
	case *openAIChatRequest:
		verifyRequestFields(t, v.Model, v.Messages[0].Role, v.Messages[1].Role, len(v.Messages))
	case *ollamaChatRequest:
		verifyRequestFields(t, v.Model, v.Messages[0].Role, v.Messages[1].Role, len(v.Messages))
	}

	return &req
}

// verifyRequestFields validates common chat request fields.
func verifyRequestFields(t *testing.T, model, firstRole, secondRole string, msgCount int) {
	t.Helper()

	if model == "" {
		t.Error("model is empty")
	}
	if msgCount != 2 {
		t.Errorf("expected 2 messages, got %d", msgCount)
	}
	if firstRole != "system" {
		t.Errorf("first message should be system, got %s", firstRole)
	}
	if secondRole != "user" {
		t.Errorf("second message should be user, got %s", secondRole)
	}
}

// verifyOpenAIChatRequest validates an OpenAI-style chat completion request.
func verifyOpenAIChatRequest(t *testing.T, r *http.Request, w http.ResponseWriter) *openAIChatRequest {
	return verifyChatRequest[openAIChatRequest](t, r, w)
}

// verifyOllamaChatRequest validates an Ollama chat request.
func verifyOllamaChatRequest(t *testing.T, r *http.Request, w http.ResponseWriter) *ollamaChatRequest {
	return verifyChatRequest[ollamaChatRequest](t, r, w)
}

// verifyAnalysisResult checks that an analysis result has the expected values
// for a standard "Good" status response with 1 warning and 1 recommendation.
func verifyAnalysisResult(t *testing.T, analysis *Analysis) {
	t.Helper()

	if analysis.SystemStatus != "Good" {
		t.Errorf("SystemStatus = %v, want Good", analysis.SystemStatus)
	}
	if len(analysis.Warnings) != 1 {
		t.Errorf("len(Warnings) = %v, want 1", len(analysis.Warnings))
	}
	if len(analysis.Recommendations) != 1 {
		t.Errorf("len(Recommendations) = %v, want 1", len(analysis.Recommendations))
	}
}

// verifyLocalProviderStats checks stats from local LLM providers (Ollama, LM Studio).
// Local providers have zero cost and expected token counts.
func verifyLocalProviderStats(t *testing.T, stats *Stats, provider string) {
	t.Helper()

	if stats.InputTokens != 1500 {
		t.Errorf("InputTokens = %v, want 1500", stats.InputTokens)
	}
	if stats.OutputTokens != 250 {
		t.Errorf("OutputTokens = %v, want 250", stats.OutputTokens)
	}
	if stats.CostUSD != 0 {
		t.Errorf("CostUSD = %v, want 0 (local inference)", stats.CostUSD)
	}
	if provider != "" && stats.Provider != provider {
		t.Errorf("Provider = %v, want %s", stats.Provider, provider)
	}
}
