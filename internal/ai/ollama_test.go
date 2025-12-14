package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewOllamaClient(t *testing.T) {
	tests := []struct {
		name    string
		cfg     OllamaConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: OllamaConfig{
				BaseURL:        "http://localhost:11434",
				Model:          "llama3.3:latest",
				TimeoutSeconds: 120,
				MaxTokens:      8000,
			},
			wantErr: false,
		},
		{
			name: "missing model",
			cfg: OllamaConfig{
				BaseURL:        "http://localhost:11434",
				Model:          "",
				TimeoutSeconds: 120,
				MaxTokens:      8000,
			},
			wantErr: true,
		},
		{
			name: "default base URL",
			cfg: OllamaConfig{
				Model: "llama3.3:latest",
			},
			wantErr: false,
		},
		{
			name: "trailing slash in base URL",
			cfg: OllamaConfig{
				BaseURL: "http://localhost:11434/",
				Model:   "llama3.3:latest",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewOllamaClient(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewOllamaClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && client == nil {
				t.Error("NewOllamaClient() returned nil client without error")
			}
		})
	}
}

func TestOllamaClient_GetModelInfo(t *testing.T) {
	client, err := NewOllamaClient(OllamaConfig{
		BaseURL:   "http://localhost:11434",
		Model:     "llama3.3:latest",
		MaxTokens: 8000,
	})
	if err != nil {
		t.Fatalf("NewOllamaClient() error = %v", err)
	}

	info := client.GetModelInfo()

	if info["model"] != "llama3.3:latest" {
		t.Errorf("GetModelInfo() model = %v, want llama3.3:latest", info["model"])
	}
	if info["provider"] != "Ollama" {
		t.Errorf("GetModelInfo() provider = %v, want Ollama", info["provider"])
	}
	if info["max_tokens"] != 8000 {
		t.Errorf("GetModelInfo() max_tokens = %v, want 8000", info["max_tokens"])
	}
	if info["base_url"] != "http://localhost:11434" {
		t.Errorf("GetModelInfo() base_url = %v, want http://localhost:11434", info["base_url"])
	}
}

func TestOllamaClient_GetProviderName(t *testing.T) {
	client, err := NewOllamaClient(OllamaConfig{
		Model: "llama3.3:latest",
	})
	if err != nil {
		t.Fatalf("NewOllamaClient() error = %v", err)
	}

	if got := client.GetProviderName(); got != "Ollama" {
		t.Errorf("GetProviderName() = %v, want Ollama", got)
	}
}

func TestOllamaClient_CheckConnection(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		response   interface{}
		statusCode int
		wantErr    bool
	}{
		{
			name:  "model found",
			model: "llama3.3:latest",
			response: map[string]interface{}{
				"models": []map[string]interface{}{
					{"name": "llama3.3:latest"},
					{"name": "mistral:latest"},
				},
			},
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:  "model not found",
			model: "nonexistent:model",
			response: map[string]interface{}{
				"models": []map[string]interface{}{
					{"name": "llama3.3:latest"},
				},
			},
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:       "server error",
			model:      "llama3.3:latest",
			response:   "Internal Server Error",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
		{
			name:  "partial model name match",
			model: "llama3.3:latest",
			response: map[string]interface{}{
				"models": []map[string]interface{}{
					{"name": "llama3.3:latest"},
				},
			},
			statusCode: http.StatusOK,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/tags" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				w.WriteHeader(tt.statusCode)
				if str, ok := tt.response.(string); ok {
					_, _ = w.Write([]byte(str))
				} else {
					_ = json.NewEncoder(w).Encode(tt.response)
				}
			}))
			defer server.Close()

			client, err := NewOllamaClient(OllamaConfig{
				BaseURL: server.URL,
				Model:   tt.model,
			})
			if err != nil {
				t.Fatalf("NewOllamaClient() error = %v", err)
			}

			err = client.CheckConnection(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckConnection() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOllamaClient_Analyze(t *testing.T) {
	// Create a mock Ollama server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		// Parse request to verify it's well-formed
		var req ollamaChatRequest
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

		// Return a valid analysis response
		response := ollamaChatResponse{
			Model:     req.Model,
			CreatedAt: time.Now(),
			Message: ollamaMessage{
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
			Done:            true,
			PromptEvalCount: 1500,
			EvalCount:       250,
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client, err := NewOllamaClient(OllamaConfig{
		BaseURL:        server.URL,
		Model:          "llama3.3:latest",
		TimeoutSeconds: 30,
		MaxTokens:      4000,
	})
	if err != nil {
		t.Fatalf("NewOllamaClient() error = %v", err)
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
}

func TestOllamaClient_Analyze_Error(t *testing.T) {
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
			name:       "empty response",
			statusCode: http.StatusOK,
			response:   `{"done": true, "message": {"role": "assistant", "content": ""}}`,
		},
		{
			name:       "invalid JSON in content",
			statusCode: http.StatusOK,
			response:   `{"done": true, "message": {"role": "assistant", "content": "not valid json"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client, err := NewOllamaClient(OllamaConfig{
				BaseURL: server.URL,
				Model:   "llama3.3:latest",
			})
			if err != nil {
				t.Fatalf("NewOllamaClient() error = %v", err)
			}

			_, _, err = client.Analyze(context.Background(), "System prompt", "User prompt")
			if err == nil {
				t.Error("Analyze() expected error, got nil")
			}
		})
	}
}

func TestOllamaClient_ImplementsProvider(t *testing.T) {
	var _ Provider = (*OllamaClient)(nil)
}
