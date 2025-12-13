package analyzer

import (
	"testing"
)

// mockReader implements LogReader for testing
type mockReader struct{}

func (m *mockReader) Read(sourcePath string) (string, error) {
	return "test content", nil
}

func (m *mockReader) Validate(content string) error {
	return nil
}

func (m *mockReader) GetSourceInfo(sourcePath string) (map[string]interface{}, error) {
	return map[string]interface{}{"size_bytes": int64(100)}, nil
}

// mockPreprocessor implements Preprocessor for testing
type mockPreprocessor struct{}

func (m *mockPreprocessor) EstimateTokens(content string) int {
	return len(content) / 4
}

func (m *mockPreprocessor) Process(content string) (string, error) {
	return content, nil
}

func (m *mockPreprocessor) ShouldProcess(content string, maxTokens int) bool {
	return m.EstimateTokens(content) > maxTokens
}

// mockPromptBuilder implements PromptBuilder for testing
type mockPromptBuilder struct {
	logType string
}

func (m *mockPromptBuilder) GetSystemPrompt() string {
	return "test system prompt"
}

func (m *mockPromptBuilder) GetUserPrompt(logContent, historicalContext string) string {
	return "test user prompt: " + logContent
}

func (m *mockPromptBuilder) GetLogType() string {
	return m.logType
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.sources == nil {
		t.Fatal("Registry sources map is nil")
	}
}

func TestRegistry_Register(t *testing.T) {
	tests := []struct {
		name    string
		source  *LogSource
		wantErr bool
	}{
		{
			name: "valid source",
			source: &LogSource{
				Type:          LogSourceLogwatch,
				Reader:        &mockReader{},
				Preprocessor:  &mockPreprocessor{},
				PromptBuilder: &mockPromptBuilder{logType: "logwatch"},
			},
			wantErr: false,
		},
		{
			name:    "nil source",
			source:  nil,
			wantErr: true,
		},
		{
			name: "empty type",
			source: &LogSource{
				Type:          "",
				Reader:        &mockReader{},
				Preprocessor:  &mockPreprocessor{},
				PromptBuilder: &mockPromptBuilder{logType: "test"},
			},
			wantErr: true,
		},
		{
			name: "nil reader",
			source: &LogSource{
				Type:          LogSourceLogwatch,
				Reader:        nil,
				Preprocessor:  &mockPreprocessor{},
				PromptBuilder: &mockPromptBuilder{logType: "test"},
			},
			wantErr: true,
		},
		{
			name: "nil preprocessor",
			source: &LogSource{
				Type:          LogSourceLogwatch,
				Reader:        &mockReader{},
				Preprocessor:  nil,
				PromptBuilder: &mockPromptBuilder{logType: "test"},
			},
			wantErr: true,
		},
		{
			name: "nil prompt builder",
			source: &LogSource{
				Type:          LogSourceLogwatch,
				Reader:        &mockReader{},
				Preprocessor:  &mockPreprocessor{},
				PromptBuilder: nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			err := r.Register(tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("Register() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()

	// Register a source
	source := &LogSource{
		Type:          LogSourceLogwatch,
		Reader:        &mockReader{},
		Preprocessor:  &mockPreprocessor{},
		PromptBuilder: &mockPromptBuilder{logType: "logwatch"},
	}
	if err := r.Register(source); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Test getting existing source
	got, ok := r.Get(LogSourceLogwatch)
	if !ok {
		t.Error("Get() returned false for registered source")
	}
	if got != source {
		t.Error("Get() returned different source than registered")
	}

	// Test getting non-existing source
	_, ok = r.Get(LogSourceDrupalWatchdog)
	if ok {
		t.Error("Get() returned true for non-registered source")
	}
}

func TestRegistry_MustGet(t *testing.T) {
	r := NewRegistry()

	// Register a source
	source := &LogSource{
		Type:          LogSourceLogwatch,
		Reader:        &mockReader{},
		Preprocessor:  &mockPreprocessor{},
		PromptBuilder: &mockPromptBuilder{logType: "logwatch"},
	}
	if err := r.Register(source); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Test MustGet with existing source
	got := r.MustGet(LogSourceLogwatch)
	if got != source {
		t.Error("MustGet() returned different source than registered")
	}

	// Test MustGet with non-existing source (should panic)
	defer func() {
		if recover() == nil {
			t.Error("MustGet() did not panic for non-registered source")
		}
	}()
	r.MustGet(LogSourceDrupalWatchdog)
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()

	// Empty registry
	if len(r.List()) != 0 {
		t.Error("List() should return empty slice for empty registry")
	}

	// Register sources
	source1 := &LogSource{
		Type:          LogSourceLogwatch,
		Reader:        &mockReader{},
		Preprocessor:  &mockPreprocessor{},
		PromptBuilder: &mockPromptBuilder{logType: "logwatch"},
	}
	source2 := &LogSource{
		Type:          LogSourceDrupalWatchdog,
		Reader:        &mockReader{},
		Preprocessor:  &mockPreprocessor{},
		PromptBuilder: &mockPromptBuilder{logType: "drupal_watchdog"},
	}

	_ = r.Register(source1)
	_ = r.Register(source2)

	list := r.List()
	if len(list) != 2 {
		t.Errorf("List() returned %d items, want 2", len(list))
	}
}

func TestRegistry_Has(t *testing.T) {
	r := NewRegistry()

	source := &LogSource{
		Type:          LogSourceLogwatch,
		Reader:        &mockReader{},
		Preprocessor:  &mockPreprocessor{},
		PromptBuilder: &mockPromptBuilder{logType: "logwatch"},
	}
	_ = r.Register(source)

	if !r.Has(LogSourceLogwatch) {
		t.Error("Has() returned false for registered source")
	}
	if r.Has(LogSourceDrupalWatchdog) {
		t.Error("Has() returned true for non-registered source")
	}
}

func TestValidSourceTypes(t *testing.T) {
	types := ValidSourceTypes()
	if len(types) != 2 {
		t.Errorf("ValidSourceTypes() returned %d items, want 2", len(types))
	}

	expected := map[string]bool{
		"logwatch":        true,
		"drupal_watchdog": true,
	}

	for _, typ := range types {
		if !expected[typ] {
			t.Errorf("ValidSourceTypes() contains unexpected type: %s", typ)
		}
	}
}

func TestParseSourceType(t *testing.T) {
	tests := []struct {
		input   string
		want    LogSourceType
		wantErr bool
	}{
		{"logwatch", LogSourceLogwatch, false},
		{"drupal_watchdog", LogSourceDrupalWatchdog, false},
		{"invalid", "", true},
		{"", "", true},
		{"LOGWATCH", "", true}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSourceType(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSourceType(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseSourceType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
