package drupal

import (
	"strings"
	"testing"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

// Compile-time interface check
var _ analyzer.Preprocessor = (*Preprocessor)(nil)

func TestNewPreprocessor(t *testing.T) {
	p := NewPreprocessor(150000)

	if p == nil {
		t.Fatal("NewPreprocessor returned nil")
	}
	if p.maxTokens != 150000 {
		t.Errorf("maxTokens = %d, want 150000", p.maxTokens)
	}
}

func TestPreprocessor_EstimateTokens(t *testing.T) {
	p := NewPreprocessor(150000)

	tests := []struct {
		name    string
		content string
		wantMin int
		wantMax int
	}{
		{
			name:    "empty",
			content: "",
			wantMin: 0,
			wantMax: 0,
		},
		{
			name:    "short text",
			content: "hello world",
			wantMin: 2,
			wantMax: 15,
		},
		{
			name:    "longer text",
			content: strings.Repeat("word ", 100),
			wantMin: 100,
			wantMax: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.EstimateTokens(tt.content)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("EstimateTokens() = %d, want between %d and %d", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestPreprocessor_ShouldProcess(t *testing.T) {
	p := NewPreprocessor(100)

	tests := []struct {
		name      string
		content   string
		maxTokens int
		want      bool
	}{
		{
			name:      "below limit",
			content:   "small content",
			maxTokens: 100,
			want:      false,
		},
		{
			name:      "above limit",
			content:   strings.Repeat("word ", 200),
			maxTokens: 100,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.ShouldProcess(tt.content, tt.maxTokens)
			if got != tt.want {
				t.Errorf("ShouldProcess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPreprocessor_Process(t *testing.T) {
	p := NewPreprocessor(1000)

	content := `## Critical/Error Entries (Full Detail)
[2024-11-13 10:00:00] ERROR | php | 192.168.1.1 | /admin
  Message: PDOException: SQLSTATE[HY000]

## Warning Entries
- [5x] php: Deprecated function call
- [3x] cron: Slow query detected

## Recent Notice/Info Entries (Sample)
- [notice] system: Cache cleared
- [info] user: User login`

	result, err := p.Process(content)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Should preserve critical sections
	if !strings.Contains(result, "ERROR") {
		t.Error("Process() should preserve ERROR entries")
	}

	// Should contain section headers
	if !strings.Contains(result, "##") {
		t.Error("Process() should preserve section headers")
	}
}

func TestPreprocessor_Process_Empty(t *testing.T) {
	p := NewPreprocessor(1000)

	result, err := p.Process("")
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if result != "" {
		t.Errorf("Process() = %q, want empty string", result)
	}
}

func TestPreprocessor_determinePriority(t *testing.T) {
	p := NewPreprocessor(1000)

	tests := []struct {
		name     string
		section  string
		content  string
		wantPrio int
	}{
		{
			name:     "security keyword in name",
			section:  "Security Events",
			content:  "some content",
			wantPrio: priorityHigh,
		},
		{
			name:     "error in content",
			section:  "General",
			content:  "PDOException: database error occurred",
			wantPrio: priorityHigh,
		},
		{
			name:     "warning section",
			section:  "Warning Entries",
			content:  "some warnings",
			wantPrio: priorityMedium,
		},
		{
			name:     "cron section",
			section:  "Cron Status",
			content:  "cron completed",
			wantPrio: priorityMedium,
		},
		{
			name:     "low priority",
			section:  "Debug Info",
			content:  "debug messages here",
			wantPrio: priorityLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.determinePriority(tt.section, tt.content)
			if got != tt.wantPrio {
				t.Errorf("determinePriority() = %d, want %d", got, tt.wantPrio)
			}
		})
	}
}

func TestPreprocessor_normalizeLine(t *testing.T) {
	p := NewPreprocessor(1000)

	tests := []struct {
		input string
		want  string
	}{
		{
			input: "Error from 192.168.1.100",
			want:  "Error from IP",
		},
		{
			input: "2024-11-13 10:30:45 Event occurred",
			want:  "TIMESTAMP Event occurred",
		},
		{
			input: "Request 12345 completed in 500ms",
			want:  "Request N completed in Nms",
		},
		{
			input: "UUID: a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			want:  "UUID: UUID",
		},
		{
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := p.normalizeLine(tt.input)
			if got != tt.want {
				t.Errorf("normalizeLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPreprocessor_deduplicateLines(t *testing.T) {
	p := NewPreprocessor(1000)

	lines := []string{
		"Error from 192.168.1.1",
		"Error from 192.168.1.2",
		"Error from 192.168.1.3",
		"Different error message",
		"Error from 192.168.1.4",
		"Error from 192.168.1.5",
	}

	result := p.deduplicateLines(lines)

	// Should have fewer lines due to deduplication
	if len(result) >= len(lines) {
		t.Errorf("deduplicateLines() returned %d lines, expected fewer than %d", len(result), len(lines))
	}

	// Should contain count indicator
	found := false
	for _, line := range result {
		if strings.Contains(line, "(x") {
			found = true
			break
		}
	}
	if !found {
		t.Error("deduplicateLines() should add count indicator for duplicates")
	}
}

func TestPreprocessor_deduplicateLines_Small(t *testing.T) {
	p := NewPreprocessor(1000)

	lines := []string{"line1", "line2", "line3"}

	result := p.deduplicateLines(lines)

	// Small lists should not be modified
	if len(result) != len(lines) {
		t.Errorf("deduplicateLines() modified small list: got %d lines, want %d", len(result), len(lines))
	}
}

func TestPreprocessor_aggressiveCompress(t *testing.T) {
	p := NewPreprocessor(100) // Very small limit

	content := `## Section Header
This is a notice line
This line has an error keyword
Another notice line
This is critical issue
Normal debug info
Failed to connect`

	result := p.aggressiveCompress(content)

	// Should keep section headers
	if !strings.Contains(result, "## Section Header") {
		t.Error("aggressiveCompress() should keep section headers")
	}

	// Should keep lines with critical keywords
	if !strings.Contains(result, "error") || !strings.Contains(result, "critical") || !strings.Contains(result, "Failed") {
		t.Error("aggressiveCompress() should keep lines with critical keywords")
	}
}
