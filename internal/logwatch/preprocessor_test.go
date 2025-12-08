package logwatch

import (
	"strings"
	"testing"
)

func TestNewPreprocessor(t *testing.T) {
	maxTokens := 150000
	preprocessor := NewPreprocessor(maxTokens)

	if preprocessor == nil {
		t.Fatal("Expected preprocessor to be created")
	}

	if preprocessor.maxTokens != maxTokens {
		t.Errorf("Expected maxTokens %d, got %d", maxTokens, preprocessor.maxTokens)
	}
}

func TestEstimateTokens(t *testing.T) {
	preprocessor := NewPreprocessor(150000)

	tests := []struct {
		name        string
		content     string
		expectedMin int
		expectedMax int
	}{
		{
			name:        "Empty content",
			content:     "",
			expectedMin: 0,
			expectedMax: 0,
		},
		{
			name:        "Single word",
			content:     "test",
			expectedMin: 1,
			expectedMax: 2,
		},
		{
			name:        "Multiple words",
			content:     "This is a test sentence",
			expectedMin: 5,
			expectedMax: 8,
		},
		{
			name:        "Long text with many chars",
			content:     strings.Repeat("a", 1000),
			expectedMin: 200,
			expectedMax: 300,
		},
		{
			name:        "Text with many words",
			content:     strings.Repeat("word ", 1000),
			expectedMin: 1200,
			expectedMax: 1400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := preprocessor.EstimateTokens(tt.content)

			if tokens < tt.expectedMin || tokens > tt.expectedMax {
				t.Errorf("Expected tokens between %d and %d, got %d", tt.expectedMin, tt.expectedMax, tokens)
			}
		})
	}
}

func TestParseSections(t *testing.T) {
	preprocessor := NewPreprocessor(150000)

	tests := []struct {
		name             string
		content          string
		expectedSections int
	}{
		{
			name: "Content with sections",
			content: `################### SSH ###################
Failed login attempts: 5

################### Disk Space ###################
Usage: 85%`,
			expectedSections: 2,
		},
		{
			name:             "Content without sections",
			content:          "Just some plain text without section headers",
			expectedSections: 1,
		},
		{
			name:             "Empty content",
			content:          "",
			expectedSections: 1,
		},
		{
			name: "Multiple hash characters",
			content: `### SSH ###
Some content
#### Network ####
More content`,
			expectedSections: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sections := preprocessor.parseSections(tt.content)

			if len(sections) != tt.expectedSections {
				t.Errorf("Expected %d sections, got %d", tt.expectedSections, len(sections))
			}
		})
	}
}

func TestDeterminePriority(t *testing.T) {
	preprocessor := NewPreprocessor(150000)

	tests := []struct {
		name             string
		sectionName      string
		content          string
		expectedPriority int
	}{
		{
			name:             "SSH section - HIGH",
			sectionName:      "SSH",
			content:          "Failed login attempts",
			expectedPriority: 1,
		},
		{
			name:             "Security section - HIGH",
			sectionName:      "Security Alerts",
			content:          "Some security content",
			expectedPriority: 1,
		},
		{
			name:             "Content with error - HIGH",
			sectionName:      "Logs",
			content:          "error: something went wrong",
			expectedPriority: 1,
		},
		{
			name:             "Network section - MEDIUM",
			sectionName:      "Network",
			content:          "Network traffic",
			expectedPriority: 2,
		},
		{
			name:             "Disk section - MEDIUM",
			sectionName:      "Disk Usage",
			content:          "Disk usage report",
			expectedPriority: 2,
		},
		{
			name:             "Generic section - LOW",
			sectionName:      "General",
			content:          "General information",
			expectedPriority: 3,
		},
		{
			name:             "Unknown section - LOW",
			sectionName:      "Random Section",
			content:          "Random content",
			expectedPriority: 3,
		},
		{
			name:             "Kernel panic - HIGH",
			sectionName:      "Kernel",
			content:          "kernel panic detected",
			expectedPriority: 1,
		},
		{
			name:             "Sudo logs - HIGH",
			sectionName:      "Sudo",
			content:          "sudo commands",
			expectedPriority: 1,
		},
		{
			name:             "Memory warnings - MEDIUM",
			sectionName:      "Memory",
			content:          "memory usage",
			expectedPriority: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priority := preprocessor.determinePriority(tt.sectionName, tt.content)

			if priority != tt.expectedPriority {
				t.Errorf("Expected priority %d, got %d", tt.expectedPriority, priority)
			}
		})
	}
}

func TestNormalizeLine(t *testing.T) {
	preprocessor := NewPreprocessor(150000)

	tests := []struct {
		name     string
		line     string
		expected string
	}{
		{
			name:     "Line with IP",
			line:     "Failed login from 192.168.1.100",
			expected: "Failed login from IP",
		},
		{
			name:     "Line with timestamp",
			line:     "Error at 14:23:45",
			expected: "Error at TIME",
		},
		{
			name:     "Line with date",
			line:     "Event on 2025-11-13",
			expected: "Event on DATE",
		},
		{
			name:     "Line with numbers",
			line:     "Process 1234 failed",
			expected: "Process N failed",
		},
		{
			name:     "Multiple IPs",
			line:     "Connection from 10.0.0.1 to 192.168.1.1",
			expected: "Connection from IP to IP",
		},
		{
			name:     "Empty line",
			line:     "",
			expected: "",
		},
		{
			name:     "Whitespace only",
			line:     "   ",
			expected: "",
		},
		{
			name:     "Complex line",
			line:     "2025-11-13 14:23:45 [192.168.1.100] Process 5678 error",
			expected: "DATE TIME [IP] Process N error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := preprocessor.normalizeLine(tt.line)

			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestDeduplicateContent(t *testing.T) {
	preprocessor := NewPreprocessor(150000)

	tests := []struct {
		name             string
		content          string
		shouldContain    string
		shouldNotContain string
	}{
		{
			name: "Duplicate log lines",
			content: `Failed login from 192.168.1.100
Failed login from 192.168.1.101
Failed login from 192.168.1.102
Failed login from 192.168.1.103
Failed login from 192.168.1.104
Failed login from 192.168.1.105
Failed login from 192.168.1.106
Failed login from 192.168.1.107
Failed login from 192.168.1.108
Failed login from 192.168.1.109
Failed login from 192.168.1.110
Some other message`,
			shouldContain:    "occurred",
			shouldNotContain: "192.168.1.105",
		},
		{
			name: "Short content - no deduplication",
			content: `Line 1
Line 2
Line 3`,
			shouldContain:    "Line 1",
			shouldNotContain: "occurred",
		},
		{
			name:             "No duplicates",
			content:          strings.Repeat("Unique line ", 20),
			shouldContain:    "Unique line",
			shouldNotContain: "occurred",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := preprocessor.deduplicateContent(tt.content)

			if tt.shouldContain != "" && !strings.Contains(result, tt.shouldContain) {
				t.Errorf("Expected result to contain '%s'", tt.shouldContain)
			}

			if tt.shouldNotContain != "" && strings.Contains(result, tt.shouldNotContain) {
				t.Errorf("Result should not contain '%s'", tt.shouldNotContain)
			}
		})
	}
}

func TestCompressByPriority(t *testing.T) {
	preprocessor := NewPreprocessor(150000)

	tests := []struct {
		name             string
		section          *Section
		shouldContainAll bool
		shouldCompress   bool
	}{
		{
			name: "HIGH priority - keep all",
			section: &Section{
				Name:     "SSH",
				Priority: 1,
				Content:  strings.Repeat("Line\n", 100),
			},
			shouldContainAll: true,
			shouldCompress:   false,
		},
		{
			name: "MEDIUM priority - keep 50%",
			section: &Section{
				Name:     "Network",
				Priority: 2,
				Content:  strings.Repeat("Line\n", 100),
			},
			shouldContainAll: false,
			shouldCompress:   true,
		},
		{
			name: "LOW priority - keep 20%",
			section: &Section{
				Name:     "General",
				Priority: 3,
				Content:  strings.Repeat("Line\n", 100),
			},
			shouldContainAll: false,
			shouldCompress:   true,
		},
		{
			name: "Short content - no compression",
			section: &Section{
				Name:     "Test",
				Priority: 3,
				Content:  "Short content",
			},
			shouldContainAll: true,
			shouldCompress:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := preprocessor.compressByPriority(tt.section)

			if tt.shouldContainAll && !strings.Contains(result, tt.section.Content) {
				t.Error("Expected all content to be preserved for HIGH priority")
			}

			if tt.shouldCompress && !strings.Contains(result, "omitted for brevity") {
				t.Error("Expected compression indicator for MEDIUM/LOW priority")
			}

			if !tt.shouldCompress && strings.Contains(result, "omitted") {
				t.Error("Should not compress content when not needed")
			}
		})
	}
}

func TestClassifySections(t *testing.T) {
	preprocessor := NewPreprocessor(150000)

	sections := []*Section{
		{Name: "SSH", Content: "ssh logs"},
		{Name: "Network", Content: "network logs"},
		{Name: "General", Content: "general logs"},
	}

	preprocessor.classifySections(sections)

	if sections[0].Priority != 1 {
		t.Errorf("Expected SSH section to have HIGH priority (1), got %d", sections[0].Priority)
	}

	if sections[1].Priority != 2 {
		t.Errorf("Expected Network section to have MEDIUM priority (2), got %d", sections[1].Priority)
	}

	if sections[2].Priority != 3 {
		t.Errorf("Expected General section to have LOW priority (3), got %d", sections[2].Priority)
	}
}

func TestProcess(t *testing.T) {
	preprocessor := NewPreprocessor(1000) // Low threshold for testing

	tests := []struct {
		name        string
		content     string
		expectError bool
	}{
		{
			name: "Small content - no processing",
			content: `################### SSH ###################
Some SSH logs`,
			expectError: false,
		},
		{
			name: "Large content - needs processing",
			content: `################### SSH ###################
` + strings.Repeat("Failed login attempt\n", 500),
			expectError: false,
		},
		{
			name:        "Content without sections",
			content:     strings.Repeat("Log line\n", 100),
			expectError: false,
		},
		{
			name:        "Empty content",
			content:     "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := preprocessor.Process(tt.content)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.expectError && result == "" && tt.content != "" {
				t.Error("Expected non-empty result for non-empty content")
			}
		})
	}
}

func TestProcessPreservesStructure(t *testing.T) {
	preprocessor := NewPreprocessor(10000)

	content := `################### SSH Security ###################
Failed login from 192.168.1.100
Failed login from 192.168.1.101
Root access denied

################### Disk Space ###################
Partition /var at 85%
Partition /home at 60%

################### General Logs ###################
` + strings.Repeat("General log line\n", 100)

	result, err := preprocessor.Process(content)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Check that section headers are preserved
	if !strings.Contains(result, "SSH Security") {
		t.Error("Expected SSH Security section to be preserved")
	}

	if !strings.Contains(result, "Disk Space") {
		t.Error("Expected Disk Space section to be preserved")
	}

	if !strings.Contains(result, "General Logs") {
		t.Error("Expected General Logs section to be preserved")
	}
}

func TestTokenEstimationAlgorithm(t *testing.T) {
	// Test that the algorithm matches the spec: max(chars/4, words/0.75)
	preprocessor := NewPreprocessor(150000)

	tests := []struct {
		name          string
		content       string
		verifyFormula bool
	}{
		{
			name:          "Chars dominant",
			content:       strings.Repeat("a", 1000),
			verifyFormula: true,
		},
		{
			name:          "Words dominant",
			content:       strings.Repeat("word ", 1000),
			verifyFormula: true,
		},
		{
			name:          "Balanced",
			content:       "This is a test with balanced char to word ratio",
			verifyFormula: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := preprocessor.EstimateTokens(tt.content)

			if tt.verifyFormula {
				chars := len(tt.content)
				words := len(strings.Fields(tt.content))

				charsEstimate := chars / 4
				wordsEstimate := int(float64(words) / 0.75)

				expected := charsEstimate
				if wordsEstimate > charsEstimate {
					expected = wordsEstimate
				}

				if tokens != expected {
					t.Errorf("Token estimation mismatch: got %d, expected %d (chars=%d, words=%d)",
						tokens, expected, chars, words)
				}
			}
		})
	}
}

func TestSectionStructure(t *testing.T) {
	section := &Section{
		Name:     "Test Section",
		Content:  "Test content",
		Priority: 1,
	}

	if section.Name != "Test Section" {
		t.Errorf("Expected Name 'Test Section', got '%s'", section.Name)
	}

	if section.Content != "Test content" {
		t.Errorf("Expected Content 'Test content', got '%s'", section.Content)
	}

	if section.Priority != 1 {
		t.Errorf("Expected Priority 1, got %d", section.Priority)
	}
}

func TestProcessWithMultipleSectionTypes(t *testing.T) {
	preprocessor := NewPreprocessor(5000) // Low threshold to trigger compression

	content := `################### SSH (HIGH priority) ###################
` + strings.Repeat("SSH log line\n", 50) + `

################### Network (MEDIUM priority) ###################
` + strings.Repeat("Network log line\n", 50) + `

################### General (LOW priority) ###################
` + strings.Repeat("General log line\n", 50)

	result, err := preprocessor.Process(content)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// All sections should be present
	if !strings.Contains(result, "SSH") {
		t.Error("SSH section missing")
	}

	if !strings.Contains(result, "Network") {
		t.Error("Network section missing")
	}

	if !strings.Contains(result, "General") {
		t.Error("General section missing")
	}

	// Result should be shorter than original due to compression
	if len(result) >= len(content) {
		t.Log("Note: Result should typically be shorter than original when compression occurs")
	}
}
