package ai

import (
	"encoding/json"
	"net/http"
	"testing"
)

// verifyOpenAIChatRequest validates an OpenAI-style chat completion request.
// It decodes the request body and verifies the structure is well-formed.
func verifyOpenAIChatRequest(t *testing.T, r *http.Request, w http.ResponseWriter) *openAIChatRequest {
	t.Helper()

	var req openAIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Errorf("failed to decode request: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}

	if req.Model == "" {
		t.Error("model is empty")
	}
	if len(req.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("first message should be system, got %s", req.Messages[0].Role)
	}
	if req.Messages[1].Role != "user" {
		t.Errorf("second message should be user, got %s", req.Messages[1].Role)
	}

	return &req
}

// verifyOllamaChatRequest validates an Ollama chat request.
// It decodes the request body and verifies the structure is well-formed.
func verifyOllamaChatRequest(t *testing.T, r *http.Request, w http.ResponseWriter) *ollamaChatRequest {
	t.Helper()

	var req ollamaChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Errorf("failed to decode request: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}

	if req.Model == "" {
		t.Error("model is empty")
	}
	if len(req.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("first message should be system, got %s", req.Messages[0].Role)
	}
	if req.Messages[1].Role != "user" {
		t.Errorf("second message should be user, got %s", req.Messages[1].Role)
	}

	return &req
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
