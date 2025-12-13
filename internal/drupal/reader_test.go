package drupal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

// Compile-time interface check
var _ analyzer.LogReader = (*Reader)(nil)

func TestNewReader(t *testing.T) {
	r := NewReader(10, true, 150000, FormatJSON)

	if r == nil {
		t.Fatal("NewReader returned nil")
	}
	if r.maxSizeMB != 10 {
		t.Errorf("maxSizeMB = %d, want 10", r.maxSizeMB)
	}
	if r.format != FormatJSON {
		t.Errorf("format = %s, want %s", r.format, FormatJSON)
	}
}

func TestIsNoEntriesContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "NoEntriesContent constant",
			content: NoEntriesContent,
			want:    true,
		},
		{
			name:    "content starting with marker",
			content: "=== NO WATCHDOG ENTRIES ===\n\nCustom message",
			want:    true,
		},
		{
			name:    "regular watchdog content",
			content: "=== DRUPAL WATCHDOG LOG ANALYSIS ===\n\n## Summary",
			want:    false,
		},
		{
			name:    "empty content",
			content: "",
			want:    false,
		},
		{
			name:    "random content",
			content: "Some random log content",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsNoEntriesContent(tt.content)
			if got != tt.want {
				t.Errorf("IsNoEntriesContent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReader_Validate(t *testing.T) {
	r := NewReader(10, false, 150000, FormatJSON)

	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "valid content",
			content: strings.Repeat("x", 100),
			wantErr: false,
		},
		{
			name:    "empty content",
			content: "",
			wantErr: true,
		},
		{
			name:    "too small",
			content: "small",
			wantErr: true,
		},
		{
			name:    "no entries content",
			content: NoEntriesContent,
			wantErr: false, // NoEntriesContent is a valid state
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.Validate(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReader_parseJSON(t *testing.T) {
	r := NewReader(10, false, 150000, FormatJSON)

	tests := []struct {
		name      string
		content   string
		wantCount int
		wantErr   bool
	}{
		{
			name: "array of entries",
			content: `[
				{"wid": 1, "type": "php", "message": "Error 1", "severity": 3, "timestamp": 1699900800},
				{"wid": 2, "type": "access", "message": "Access denied", "severity": 4, "timestamp": 1699900801}
			]`,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "single entry",
			content:   `{"wid": 1, "type": "php", "message": "Error", "severity": 3, "timestamp": 1699900800}`,
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "newline-delimited JSON",
			content: `{"wid": 1, "type": "php", "message": "Error 1", "severity": 3, "timestamp": 1699900800}
{"wid": 2, "type": "access", "message": "Access denied", "severity": 4, "timestamp": 1699900801}`,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "invalid JSON",
			content:   `not json at all`,
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := r.parseJSON(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(entries) != tt.wantCount {
				t.Errorf("parseJSON() returned %d entries, want %d", len(entries), tt.wantCount)
			}
		})
	}
}

func TestReader_parseDrush(t *testing.T) {
	r := NewReader(10, false, 150000, FormatDrush)

	content := `ID      Date                 Type     Severity  Message
------- -------------------- -------- --------- ----------------------------------------
12345   2024-11-13 10:00:00  php      error     PDOException: SQLSTATE[HY000]
12344   2024-11-13 09:55:00  access   notice    Access check for admin
`

	entries, err := r.parseDrush(content)
	if err != nil {
		t.Fatalf("parseDrush() error = %v", err)
	}

	if len(entries) != 2 {
		t.Errorf("parseDrush() returned %d entries, want 2", len(entries))
	}

	if entries[0].WID != 12345 {
		t.Errorf("entries[0].WID = %d, want 12345", entries[0].WID)
	}

	if entries[0].Type != "php" {
		t.Errorf("entries[0].Type = %s, want php", entries[0].Type)
	}

	if entries[0].Severity != SeverityError {
		t.Errorf("entries[0].Severity = %d, want %d", entries[0].Severity, SeverityError)
	}
}

func TestReader_parseDrush_Empty(t *testing.T) {
	r := NewReader(10, false, 150000, FormatDrush)

	content := `ID      Date                 Type     Severity  Message
------- -------------------- -------- --------- ----------------------------------------
`

	_, err := r.parseDrush(content)
	if err == nil {
		t.Error("parseDrush() should return error for empty entries")
	}
}

func TestReader_Read(t *testing.T) {
	// Create temp file with JSON content
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "watchdog.json")

	content := `[
		{"wid": 1, "type": "php", "message": "Test error", "severity": 3, "timestamp": 1699900800, "hostname": "127.0.0.1", "location": "/admin"},
		{"wid": 2, "type": "access", "message": "Access denied for user", "severity": 4, "timestamp": 1699900801, "hostname": "192.168.1.1", "location": "/admin/config"}
	]`

	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	r := NewReader(10, false, 150000, FormatJSON)

	result, err := r.Read(tmpFile)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	// Check that the result contains expected content
	if !strings.Contains(result, "DRUPAL WATCHDOG LOG ANALYSIS") {
		t.Error("Read() result missing header")
	}

	if !strings.Contains(result, "php") {
		t.Error("Read() result missing entry type")
	}
}

func TestReader_Read_FileNotFound(t *testing.T) {
	r := NewReader(10, false, 150000, FormatJSON)

	_, err := r.Read("/nonexistent/file.json")
	if err == nil {
		t.Error("Read() should return error for missing file")
	}
}

func TestReader_GetSourceInfo(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "watchdog.json")

	if err := os.WriteFile(tmpFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}

	r := NewReader(10, false, 150000, FormatJSON)

	info, err := r.GetSourceInfo(tmpFile)
	if err != nil {
		t.Fatalf("GetSourceInfo() error = %v", err)
	}

	if info["format"] != "json" {
		t.Errorf("format = %v, want json", info["format"])
	}

	if _, ok := info["size_bytes"]; !ok {
		t.Error("GetSourceInfo() missing size_bytes")
	}

	if _, ok := info["modified"]; !ok {
		t.Error("GetSourceInfo() missing modified")
	}
}

func TestReader_formatEntriesForAnalysis(t *testing.T) {
	r := NewReader(10, false, 150000, FormatJSON)

	entries := []WatchdogEntry{
		{WID: 1, Type: "php", Message: "Error message", Severity: SeverityError, Timestamp: 1699900800},
		{WID: 2, Type: "access", Message: "Access denied", Severity: SeverityWarning, Timestamp: 1699900801},
		{WID: 3, Type: "cron", Message: "Cron completed", Severity: SeverityNotice, Timestamp: 1699900802},
	}

	result := r.formatEntriesForAnalysis(entries)

	// Check for expected sections
	expectedSections := []string{
		"DRUPAL WATCHDOG LOG ANALYSIS",
		"Summary Statistics",
		"Severity Breakdown",
		"Entry Types",
	}

	for _, section := range expectedSections {
		if !strings.Contains(result, section) {
			t.Errorf("formatEntriesForAnalysis() missing section: %s", section)
		}
	}

	// Check for severity entries
	if !strings.Contains(result, "ERROR") {
		t.Error("formatEntriesForAnalysis() missing ERROR severity")
	}
}

func TestReader_formatEntriesForAnalysis_Empty(t *testing.T) {
	r := NewReader(10, false, 150000, FormatJSON)

	result := r.formatEntriesForAnalysis(nil)

	// Should return NoEntriesContent constant
	if result != NoEntriesContent {
		t.Errorf("formatEntriesForAnalysis() = %q, want NoEntriesContent", result)
	}

	// Verify IsNoEntriesContent detects it correctly
	if !IsNoEntriesContent(result) {
		t.Error("IsNoEntriesContent() should return true for empty entries result")
	}
}

func TestReader_normalizeMessage(t *testing.T) {
	r := NewReader(10, false, 150000, FormatJSON)

	tests := []struct {
		input string
		want  string
	}{
		{
			input: "Error from 192.168.1.100 at 10:30:45",
			want:  "Error from [IP] at [N]:[N]:[N]",
		},
		{
			input: "Request 12345 failed",
			want:  "Request [N] failed",
		},
		{
			input: "UUID: a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			want:  "UUID: [UUID]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := r.normalizeMessage(tt.input)
			if got != tt.want {
				t.Errorf("normalizeMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReader_truncateMessage(t *testing.T) {
	r := NewReader(10, false, 150000, FormatJSON)

	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"this is a long message", 10, "this is..."},
		{"exactly10!", 10, "exactly10!"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := r.truncateMessage(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
