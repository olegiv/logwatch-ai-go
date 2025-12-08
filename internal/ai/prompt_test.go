package ai

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// assertAnalysisEqual compares two Analysis structs using reflect.DeepEqual
func assertAnalysisEqual(t *testing.T, got, want *Analysis) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Analysis mismatch:\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestGetSystemPrompt(t *testing.T) {
	prompt := GetSystemPrompt()

	if prompt == "" {
		t.Error("System prompt should not be empty")
	}

	// Check that prompt contains key elements
	requiredElements := []string{
		"system administrator",
		"System Status Assessment",
		"Security Analysis",
		"JSON object",
		"systemStatus",
		"summary",
		"criticalIssues",
		"warnings",
		"recommendations",
		"metrics",
	}

	for _, element := range requiredElements {
		if !strings.Contains(prompt, element) {
			t.Errorf("System prompt should contain '%s'", element)
		}
	}
}

func TestGetUserPrompt(t *testing.T) {
	tests := []struct {
		name              string
		logwatchContent   string
		historicalContext string
		shouldContainLog  bool
		shouldContainHist bool
	}{
		{
			name:              "With logwatch only",
			logwatchContent:   "Test logwatch output",
			historicalContext: "",
			shouldContainLog:  true,
			shouldContainHist: false,
		},
		{
			name:              "With both logwatch and history",
			logwatchContent:   "Test logwatch output",
			historicalContext: "Previous analysis data",
			shouldContainLog:  true,
			shouldContainHist: true,
		},
		{
			name:              "Empty logwatch",
			logwatchContent:   "",
			historicalContext: "Previous analysis data",
			shouldContainLog:  true,
			shouldContainHist: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := GetUserPrompt(tt.logwatchContent, tt.historicalContext)

			if prompt == "" {
				t.Error("User prompt should not be empty")
			}

			if tt.shouldContainLog && !strings.Contains(prompt, "LOGWATCH OUTPUT:") {
				t.Error("Prompt should contain logwatch section")
			}

			if tt.shouldContainLog && !strings.Contains(prompt, tt.logwatchContent) {
				t.Error("Prompt should contain logwatch content")
			}

			if tt.shouldContainHist && !strings.Contains(prompt, "HISTORICAL CONTEXT:") {
				t.Error("Prompt should contain historical context section")
			}

			if tt.shouldContainHist && !strings.Contains(prompt, tt.historicalContext) {
				t.Error("Prompt should contain historical context content")
			}

			if !tt.shouldContainHist && strings.Contains(prompt, "HISTORICAL CONTEXT:") {
				t.Error("Prompt should not contain historical context when not provided")
			}
		})
	}
}

func TestParseAnalysis(t *testing.T) {
	tests := []struct {
		name        string
		response    string
		expectError bool
		validate    func(*testing.T, *Analysis)
	}{
		{
			name: "Valid JSON response",
			response: `{
				"systemStatus": "Good",
				"summary": "System is operating normally",
				"criticalIssues": ["Issue 1"],
				"warnings": ["Warning 1", "Warning 2"],
				"recommendations": ["Rec 1"],
				"metrics": {"failedLogins": 5, "diskUsage": "80%"}
			}`,
			expectError: false,
			validate: func(t *testing.T, a *Analysis) {
				if a.SystemStatus != "Good" {
					t.Errorf("Expected status 'Good', got '%s'", a.SystemStatus)
				}
				if a.Summary != "System is operating normally" {
					t.Errorf("Unexpected summary: %s", a.Summary)
				}
				if len(a.CriticalIssues) != 1 {
					t.Errorf("Expected 1 critical issue, got %d", len(a.CriticalIssues))
				}
				if len(a.Warnings) != 2 {
					t.Errorf("Expected 2 warnings, got %d", len(a.Warnings))
				}
				if len(a.Recommendations) != 1 {
					t.Errorf("Expected 1 recommendation, got %d", len(a.Recommendations))
				}
			},
		},
		{
			name: "JSON with extra text before",
			response: `Here is the analysis:
{
	"systemStatus": "Excellent",
	"summary": "All good",
	"criticalIssues": [],
	"warnings": [],
	"recommendations": [],
	"metrics": {}
}`,
			expectError: false,
			validate: func(t *testing.T, a *Analysis) {
				if a.SystemStatus != "Excellent" {
					t.Errorf("Expected status 'Excellent', got '%s'", a.SystemStatus)
				}
			},
		},
		{
			name:        "No JSON in response",
			response:    "This is just plain text without JSON",
			expectError: true,
		},
		{
			name:        "Invalid JSON",
			response:    `{"systemStatus": "Good", invalid}`,
			expectError: true,
		},
		{
			name: "Missing systemStatus",
			response: `{
				"summary": "System is operating normally",
				"criticalIssues": [],
				"warnings": [],
				"recommendations": [],
				"metrics": {}
			}`,
			expectError: true,
		},
		{
			name: "Invalid systemStatus",
			response: `{
				"systemStatus": "Unknown",
				"summary": "System is operating normally",
				"criticalIssues": [],
				"warnings": [],
				"recommendations": [],
				"metrics": {}
			}`,
			expectError: true,
		},
		{
			name: "Missing summary",
			response: `{
				"systemStatus": "Good",
				"criticalIssues": [],
				"warnings": [],
				"recommendations": [],
				"metrics": {}
			}`,
			expectError: true,
		},
		{
			name: "Nil arrays get initialized",
			response: `{
				"systemStatus": "Good",
				"summary": "All good"
			}`,
			expectError: false,
			validate: func(t *testing.T, a *Analysis) {
				if a.CriticalIssues == nil {
					t.Error("CriticalIssues should be initialized to empty array, not nil")
				}
				if a.Warnings == nil {
					t.Error("Warnings should be initialized to empty array, not nil")
				}
				if a.Recommendations == nil {
					t.Error("Recommendations should be initialized to empty array, not nil")
				}
				if a.Metrics == nil {
					t.Error("Metrics should be initialized to empty map, not nil")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis, err := ParseAnalysis(tt.response)

			if tt.expectError {
				if err == nil {
					t.Error("Expected an error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if analysis == nil {
				t.Error("Expected analysis but got nil")
				return
			}

			if tt.validate != nil {
				tt.validate(t, analysis)
			}
		})
	}
}

func TestValidateAnalysis(t *testing.T) {
	tests := []struct {
		name        string
		analysis    *Analysis
		expectError bool
	}{
		{
			name: "Valid analysis with all statuses",
			analysis: &Analysis{
				SystemStatus:    "Excellent",
				Summary:         "Test summary",
				CriticalIssues:  []string{},
				Warnings:        []string{},
				Recommendations: []string{},
				Metrics:         map[string]interface{}{},
			},
			expectError: false,
		},
		{
			name: "Status Good",
			analysis: &Analysis{
				SystemStatus: "Good",
				Summary:      "Test",
			},
			expectError: false,
		},
		{
			name: "Status Satisfactory",
			analysis: &Analysis{
				SystemStatus: "Satisfactory",
				Summary:      "Test",
			},
			expectError: false,
		},
		{
			name: "Status Bad",
			analysis: &Analysis{
				SystemStatus: "Bad",
				Summary:      "Test",
			},
			expectError: false,
		},
		{
			name: "Status Awful",
			analysis: &Analysis{
				SystemStatus: "Awful",
				Summary:      "Test",
			},
			expectError: false,
		},
		{
			name: "Empty status",
			analysis: &Analysis{
				Summary: "Test",
			},
			expectError: true,
		},
		{
			name: "Invalid status",
			analysis: &Analysis{
				SystemStatus: "Unknown",
				Summary:      "Test",
			},
			expectError: true,
		},
		{
			name: "Empty summary",
			analysis: &Analysis{
				SystemStatus: "Good",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAnalysis(tt.analysis)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Check that nil arrays/maps are initialized after validation
			if !tt.expectError {
				if tt.analysis.CriticalIssues == nil {
					t.Error("CriticalIssues should be initialized")
				}
				if tt.analysis.Warnings == nil {
					t.Error("Warnings should be initialized")
				}
				if tt.analysis.Recommendations == nil {
					t.Error("Recommendations should be initialized")
				}
				if tt.analysis.Metrics == nil {
					t.Error("Metrics should be initialized")
				}
			}
		})
	}
}

func TestGetStatusEmoji(t *testing.T) {
	tests := []struct {
		status string
		emoji  string
	}{
		{"Excellent", "✅"},
		{"Good", "🟢"},
		{"Satisfactory", "🟡"},
		{"Bad", "🟠"},
		{"Awful", "🔴"},
		{"Unknown", "⚪"},
		{"", "⚪"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			emoji := GetStatusEmoji(tt.status)
			if emoji != tt.emoji {
				t.Errorf("Expected emoji '%s' for status '%s', got '%s'", tt.emoji, tt.status, emoji)
			}
		})
	}
}

func TestShouldTriggerAlert(t *testing.T) {
	tests := []struct {
		status      string
		shouldAlert bool
	}{
		{"Excellent", false},
		{"Good", false},
		{"Satisfactory", true},
		{"Bad", true},
		{"Awful", true},
		{"Unknown", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			result := ShouldTriggerAlert(tt.status)
			if result != tt.shouldAlert {
				t.Errorf("Expected ShouldTriggerAlert('%s') to be %v, got %v", tt.status, tt.shouldAlert, result)
			}
		})
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple JSON",
			input:    `{"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "JSON with text before",
			input:    `Here is the result: {"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "JSON with text after",
			input:    `{"key": "value"} That's all!`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "JSON with text before and after",
			input:    `Analysis: {"systemStatus": "Good", "summary": "All OK"} End of analysis.`,
			expected: `{"systemStatus": "Good", "summary": "All OK"}`,
		},
		{
			name:     "Nested JSON",
			input:    `{"outer": {"inner": "value"}}`,
			expected: `{"outer": {"inner": "value"}}`,
		},
		{
			name:     "JSON with braces in strings",
			input:    `{"message": "Use {brackets} carefully"}`,
			expected: `{"message": "Use {brackets} carefully"}`,
		},
		{
			name:     "JSON with escaped quotes",
			input:    `{"message": "He said \"hello\""}`,
			expected: `{"message": "He said \"hello\""}`,
		},
		{
			name:     "Multiple JSON objects - returns first",
			input:    `{"first": 1} some text {"second": 2}`,
			expected: `{"first": 1}`,
		},
		{
			name:     "No JSON",
			input:    `This is just plain text`,
			expected: ``,
		},
		{
			name:     "Empty string",
			input:    ``,
			expected: ``,
		},
		{
			name:     "Unbalanced braces",
			input:    `{"key": "value"`,
			expected: ``,
		},
		{
			name:     "Complex nested structure",
			input:    `Result: {"metrics": {"disk": {"used": 80}, "memory": {"used": 50}}, "status": "ok"}`,
			expected: `{"metrics": {"disk": {"used": 80}, "memory": {"used": 50}}, "status": "ok"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSON(tt.input)
			if result != tt.expected {
				t.Errorf("extractJSON(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseAnalysis_SizeLimit(t *testing.T) {
	// Test that extremely large JSON responses are rejected
	// Create a valid JSON structure that exceeds the size limit
	largeContent := strings.Repeat("x", maxJSONResponseSize+1000)
	largeJSON := `{"systemStatus": "Good", "summary": "` + largeContent + `"}`

	_, err := ParseAnalysis(largeJSON)
	if err == nil {
		t.Error("Expected error for oversized JSON response")
	}
	if err != nil && !strings.Contains(err.Error(), "too large") {
		t.Errorf("Expected 'too large' error, got: %v", err)
	}
}

func TestAnalysisJSONSerialization(t *testing.T) {
	// Test that Analysis can be marshaled and unmarshaled correctly
	original := &Analysis{
		SystemStatus: "Good",
		Summary:      "Test summary with special chars: <>&\"",
		CriticalIssues: []string{
			"Critical issue 1",
			"Critical issue 2",
		},
		Warnings: []string{
			"Warning 1",
		},
		Recommendations: []string{
			"Recommendation 1",
			"Recommendation 2",
			"Recommendation 3",
		},
		Metrics: map[string]interface{}{
			"failedLogins": float64(10),
			"diskUsage":    "85%",
			"errorCount":   float64(0),
		},
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal analysis: %v", err)
	}

	// Unmarshal back
	var restored Analysis
	if err := json.Unmarshal(jsonData, &restored); err != nil {
		t.Fatalf("Failed to unmarshal analysis: %v", err)
	}

	// Verify fields
	assertAnalysisEqual(t, &restored, original)
}
