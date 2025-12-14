package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewLMStudioClient(t *testing.T) {
	tests := []struct {
		name    string
		cfg     LMStudioConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: LMStudioConfig{
				BaseURL:        "http://localhost:1234",
				Model:          "local-model",
				TimeoutSeconds: 120,
				MaxTokens:      8000,
			},
			wantErr: false,
		},
		{
			name: "empty model uses default",
			cfg: LMStudioConfig{
				BaseURL:        "http://localhost:1234",
				Model:          "",
				TimeoutSeconds: 120,
				MaxTokens:      8000,
			},
			wantErr: false, // LM Studio allows empty model (defaults to "local-model")
		},
		{
			name: "default base URL",
			cfg: LMStudioConfig{
				Model: "local-model",
			},
			wantErr: false,
		},
		{
			name: "trailing slash in base URL",
			cfg: LMStudioConfig{
				BaseURL: "http://localhost:1234/",
				Model:   "local-model",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewLMStudioClient(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLMStudioClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && client == nil {
				t.Error("NewLMStudioClient() returned nil client without error")
			}
		})
	}
}

func TestLMStudioClient_GetModelInfo(t *testing.T) {
	client, err := NewLMStudioClient(LMStudioConfig{
		BaseURL:   "http://localhost:1234",
		Model:     "my-model",
		MaxTokens: 8000,
	})
	if err != nil {
		t.Fatalf("NewLMStudioClient() error = %v", err)
	}

	info := client.GetModelInfo()

	if info["model"] != "my-model" {
		t.Errorf("GetModelInfo() model = %v, want my-model", info["model"])
	}
	if info["provider"] != "LMStudio" {
		t.Errorf("GetModelInfo() provider = %v, want LMStudio", info["provider"])
	}
	if info["max_tokens"] != 8000 {
		t.Errorf("GetModelInfo() max_tokens = %v, want 8000", info["max_tokens"])
	}
	if info["base_url"] != "http://localhost:1234" {
		t.Errorf("GetModelInfo() base_url = %v, want http://localhost:1234", info["base_url"])
	}
}

func TestLMStudioClient_GetProviderName(t *testing.T) {
	client, err := NewLMStudioClient(LMStudioConfig{
		Model: "local-model",
	})
	if err != nil {
		t.Fatalf("NewLMStudioClient() error = %v", err)
	}

	if got := client.GetProviderName(); got != "LMStudio" {
		t.Errorf("GetProviderName() = %v, want LMStudio", got)
	}
}

func TestLMStudioClient_CheckConnection(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		response   interface{}
		statusCode int
		wantErr    bool
	}{
		{
			name:  "model loaded with local-model",
			model: "local-model",
			response: map[string]interface{}{
				"object": "list",
				"data": []map[string]interface{}{
					{"id": "some-loaded-model", "object": "model"},
				},
			},
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:  "specific model found",
			model: "llama-2-7b",
			response: map[string]interface{}{
				"object": "list",
				"data": []map[string]interface{}{
					{"id": "llama-2-7b", "object": "model"},
					{"id": "mistral-7b", "object": "model"},
				},
			},
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:  "specific model not found",
			model: "nonexistent-model",
			response: map[string]interface{}{
				"object": "list",
				"data": []map[string]interface{}{
					{"id": "llama-2-7b", "object": "model"},
				},
			},
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:  "no models loaded",
			model: "local-model",
			response: map[string]interface{}{
				"object": "list",
				"data":   []map[string]interface{}{},
			},
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:       "server error",
			model:      "local-model",
			response:   "Internal Server Error",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/models" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				if str, ok := tt.response.(string); ok {
					w.Write([]byte(str))
				} else {
					json.NewEncoder(w).Encode(tt.response)
				}
			}))
			defer server.Close()

			client, err := NewLMStudioClient(LMStudioConfig{
				BaseURL: server.URL,
				Model:   tt.model,
			})
			if err != nil {
				t.Fatalf("NewLMStudioClient() error = %v", err)
			}

			err = client.CheckConnection(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckConnection() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLMStudioClient_Analyze(t *testing.T) {
	// Create a mock LM Studio server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		// Parse request to verify it's well-formed
		var req openAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Verify request structure
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

		// Return a valid analysis response in OpenAI format
		response := openAIChatResponse{
			ID:      "chatcmpl-123",
			Object:  "chat.completion",
			Created: 1699000000,
			Model:   req.Model,
			Choices: []struct {
				Index        int           `json:"index"`
				Message      openAIMessage `json:"message"`
				FinishReason string        `json:"finish_reason"`
			}{
				{
					Index: 0,
					Message: openAIMessage{
						Role: "assistant",
						Content: `{
							"systemStatus": "Good",
							"summary": "System is operating normally with no critical issues.",
							"criticalIssues": [],
							"warnings": ["Minor disk usage increase"],
							"recommendations": ["Monitor disk usage"],
							"metrics": {"diskUsage": "75%", "errorCount": 0}
						}`,
					},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			}{
				PromptTokens:     1500,
				CompletionTokens: 250,
				TotalTokens:      1750,
			},
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client, err := NewLMStudioClient(LMStudioConfig{
		BaseURL:        server.URL,
		Model:          "local-model",
		TimeoutSeconds: 30,
		MaxTokens:      4000,
	})
	if err != nil {
		t.Fatalf("NewLMStudioClient() error = %v", err)
	}

	analysis, stats, err := client.Analyze(context.Background(), "System prompt", "User prompt")
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	// Verify analysis
	if analysis.SystemStatus != "Good" {
		t.Errorf("SystemStatus = %v, want Good", analysis.SystemStatus)
	}
	if len(analysis.Warnings) != 1 {
		t.Errorf("len(Warnings) = %v, want 1", len(analysis.Warnings))
	}
	if len(analysis.Recommendations) != 1 {
		t.Errorf("len(Recommendations) = %v, want 1", len(analysis.Recommendations))
	}

	// Verify stats
	if stats.InputTokens != 1500 {
		t.Errorf("InputTokens = %v, want 1500", stats.InputTokens)
	}
	if stats.OutputTokens != 250 {
		t.Errorf("OutputTokens = %v, want 250", stats.OutputTokens)
	}
	if stats.CostUSD != 0 {
		t.Errorf("CostUSD = %v, want 0 (local inference)", stats.CostUSD)
	}
	if stats.Provider != "LMStudio" {
		t.Errorf("Provider = %v, want LMStudio", stats.Provider)
	}
}

func TestLMStudioClient_Analyze_Error(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
	}{
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			response:   "Internal Server Error",
		},
		{
			name:       "empty choices",
			statusCode: http.StatusOK,
			response:   `{"choices": []}`,
		},
		{
			name:       "empty content",
			statusCode: http.StatusOK,
			response:   `{"choices": [{"message": {"role": "assistant", "content": ""}}]}`,
		},
		{
			name:       "invalid JSON in content",
			statusCode: http.StatusOK,
			response:   `{"choices": [{"message": {"role": "assistant", "content": "not valid json"}}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client, err := NewLMStudioClient(LMStudioConfig{
				BaseURL: server.URL,
				Model:   "local-model",
			})
			if err != nil {
				t.Fatalf("NewLMStudioClient() error = %v", err)
			}

			_, _, err = client.Analyze(context.Background(), "System prompt", "User prompt")
			if err == nil {
				t.Error("Analyze() expected error, got nil")
			}
		})
	}
}

func TestLMStudioClient_ImplementsProvider(t *testing.T) {
	var _ Provider = (*LMStudioClient)(nil)
}
