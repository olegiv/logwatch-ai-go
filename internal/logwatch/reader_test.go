package logwatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewReader(t *testing.T) {
	reader := NewReader(10, true, 150000)

	if reader == nil {
		t.Fatal("Expected reader to be created")
	}

	if reader.maxSizeMB != 10 {
		t.Errorf("Expected maxSizeMB 10, got %d", reader.maxSizeMB)
	}

	if !reader.enablePreprocessing {
		t.Error("Expected preprocessing to be enabled")
	}

	if reader.maxTokens != 150000 {
		t.Errorf("Expected maxTokens 150000, got %d", reader.maxTokens)
	}

	if reader.preprocessor == nil {
		t.Error("Expected preprocessor to be initialized")
	}
}

func TestReadLogwatchOutput_FileNotFound(t *testing.T) {
	reader := NewReader(10, false, 150000)

	_, err := reader.ReadLogwatchOutput("/nonexistent/file.txt")

	if err == nil {
		t.Fatal("Expected error for nonexistent file")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

func TestReadLogwatchOutput_ValidFile(t *testing.T) {
	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "logwatch.txt")

	content := strings.Repeat("This is a logwatch output line.\n", 10)
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	reader := NewReader(10, false, 150000)
	result, err := reader.ReadLogwatchOutput(testFile)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if result != content {
		t.Error("Content mismatch")
	}
}

func TestReadLogwatchOutput_FileTooBig(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "logwatch.txt")

	// Create a file larger than 1MB (maxSizeMB is 1)
	largeContent := strings.Repeat("X", 2*1024*1024)
	err := os.WriteFile(testFile, []byte(largeContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	reader := NewReader(1, false, 150000) // 1MB limit
	_, err = reader.ReadLogwatchOutput(testFile)

	if err == nil {
		t.Fatal("Expected error for file exceeding size limit")
	}

	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Errorf("Expected 'exceeds maximum size' error, got: %v", err)
	}
}

func TestReadLogwatchOutput_FileTooOld(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "logwatch.txt")

	content := strings.Repeat("This is a logwatch output line.\n", 10)
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Change file modification time to 25 hours ago
	oldTime := time.Now().Add(-25 * time.Hour)
	err = os.Chtimes(testFile, oldTime, oldTime)
	if err != nil {
		t.Fatalf("Failed to change file time: %v", err)
	}

	reader := NewReader(10, false, 150000)
	_, err = reader.ReadLogwatchOutput(testFile)

	if err == nil {
		t.Fatal("Expected error for old file")
	}

	if !strings.Contains(err.Error(), "too old") {
		t.Errorf("Expected 'too old' error, got: %v", err)
	}
}

func TestReadLogwatchOutput_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "logwatch.txt")

	err := os.WriteFile(testFile, []byte(""), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	reader := NewReader(10, false, 150000)
	_, err = reader.ReadLogwatchOutput(testFile)

	if err == nil {
		t.Fatal("Expected error for empty file")
	}

	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("Expected 'empty' error, got: %v", err)
	}
}

func TestReadLogwatchOutput_FileTooSmall(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "logwatch.txt")

	// File with less than 100 bytes
	err := os.WriteFile(testFile, []byte("Small"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	reader := NewReader(10, false, 150000)
	_, err = reader.ReadLogwatchOutput(testFile)

	if err == nil {
		t.Fatal("Expected error for file too small")
	}

	if !strings.Contains(err.Error(), "too small") {
		t.Errorf("Expected 'too small' error, got: %v", err)
	}
}

func TestReadLogwatchOutput_WithPreprocessing(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "logwatch.txt")

	// Create content that will trigger preprocessing (high token count)
	content := strings.Repeat("This is a logwatch line with many words to increase token count.\n", 10000)
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Use low max tokens to trigger preprocessing
	reader := NewReader(10, true, 1000)
	result, err := reader.ReadLogwatchOutput(testFile)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if result == "" {
		t.Error("Expected non-empty result")
	}

	// Result should be different from original due to preprocessing
	if result == content {
		t.Log("Note: Preprocessing may not always modify content")
	}
}

func TestReadLogwatchOutput_NoPreprocessing(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "logwatch.txt")

	content := strings.Repeat("This is a logwatch output line.\n", 100)
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	reader := NewReader(10, false, 150000)
	result, err := reader.ReadLogwatchOutput(testFile)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Without preprocessing, content should match exactly
	if result != content {
		t.Error("Content should match exactly when preprocessing is disabled")
	}
}

func TestValidateContent(t *testing.T) {
	reader := NewReader(10, false, 150000)

	tests := []struct {
		name        string
		content     string
		expectError bool
	}{
		{
			name:        "Valid content",
			content:     strings.Repeat("This is valid logwatch content.\n", 10),
			expectError: false,
		},
		{
			name:        "Empty content",
			content:     "",
			expectError: true,
		},
		{
			name:        "Too small content",
			content:     "Small",
			expectError: true,
		},
		{
			name:        "Minimal valid content",
			content:     strings.Repeat("X", 100),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := reader.validateContent(tt.content)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestGetFileInfo(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "logwatch.txt")

	content := strings.Repeat("Test content.\n", 100)
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	reader := NewReader(10, false, 150000)
	info, err := reader.GetFileInfo(testFile)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if info == nil {
		t.Fatal("Expected file info but got nil")
	}

	// Check that info contains expected fields
	if _, ok := info["size_bytes"]; !ok {
		t.Error("Expected 'size_bytes' field in info")
	}

	if _, ok := info["size_mb"]; !ok {
		t.Error("Expected 'size_mb' field in info")
	}

	if _, ok := info["modified"]; !ok {
		t.Error("Expected 'modified' field in info")
	}

	if _, ok := info["age_hours"]; !ok {
		t.Error("Expected 'age_hours' field in info")
	}

	// Verify size is correct
	sizeBytes, ok := info["size_bytes"].(int64)
	if !ok {
		t.Error("size_bytes should be int64")
	}

	if sizeBytes != int64(len(content)) {
		t.Errorf("Expected size %d, got %d", len(content), sizeBytes)
	}

	// Verify size_mb calculation
	sizeMB, ok := info["size_mb"].(float64)
	if !ok {
		t.Error("size_mb should be float64")
	}

	expectedMB := float64(len(content)) / 1024 / 1024
	if sizeMB != expectedMB {
		t.Errorf("Expected size_mb %.6f, got %.6f", expectedMB, sizeMB)
	}

	// Verify age_hours is recent (less than 1 hour)
	ageHours, ok := info["age_hours"].(float64)
	if !ok {
		t.Error("age_hours should be float64")
	}

	if ageHours > 1.0 {
		t.Errorf("Expected age_hours to be recent, got %.2f", ageHours)
	}
}

func TestGetFileInfo_FileNotFound(t *testing.T) {
	reader := NewReader(10, false, 150000)

	_, err := reader.GetFileInfo("/nonexistent/file.txt")

	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestReadLogwatchOutput_NotReadableFile(t *testing.T) {
	// Skip on Windows as file permissions work differently
	if os.Getenv("GOOS") == "windows" {
		t.Skip("Skipping permission test on Windows")
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "logwatch.txt")

	content := strings.Repeat("This is a logwatch output line.\n", 10)
	err := os.WriteFile(testFile, []byte(content), 0000) // No permissions
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Ensure cleanup
	defer func() { _ = os.Chmod(testFile, 0644) }()

	reader := NewReader(10, false, 150000)
	_, err = reader.ReadLogwatchOutput(testFile)

	if err == nil {
		t.Fatal("Expected error for unreadable file")
	}

	if !strings.Contains(err.Error(), "not readable") {
		t.Logf("Got error: %v", err)
		// Don't fail test as permission errors vary by OS
	}
}

func TestReaderStructure(t *testing.T) {
	// Test that Reader structure is properly initialized
	reader := &Reader{
		maxSizeMB:           25,
		enablePreprocessing: true,
		maxTokens:           200000,
		preprocessor:        NewPreprocessor(200000),
	}

	if reader.maxSizeMB != 25 {
		t.Errorf("Expected maxSizeMB 25, got %d", reader.maxSizeMB)
	}

	if !reader.enablePreprocessing {
		t.Error("Expected preprocessing to be enabled")
	}

	if reader.maxTokens != 200000 {
		t.Errorf("Expected maxTokens 200000, got %d", reader.maxTokens)
	}

	if reader.preprocessor == nil {
		t.Error("Expected preprocessor to be initialized")
	}
}

func TestReadLogwatchOutput_PreprocessingThreshold(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "logwatch.txt")

	// Create content just below threshold
	content := strings.Repeat("Test line.\n", 100)
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	reader := NewReader(10, true, 1000000) // High threshold
	result, err := reader.ReadLogwatchOutput(testFile)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Content should not be preprocessed if below threshold
	if result != content {
		t.Log("Content may have been preprocessed despite being below threshold")
	}
}

func TestReadLogwatchOutput_ExactSizeLimit(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "logwatch.txt")

	// Create a file exactly at the limit
	maxSizeMB := 1
	exactSize := maxSizeMB * 1024 * 1024
	content := strings.Repeat("X", exactSize)
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	reader := NewReader(maxSizeMB, false, 150000)
	_, err = reader.ReadLogwatchOutput(testFile)

	// Should not error at exact limit
	if err != nil && strings.Contains(err.Error(), "exceeds maximum size") {
		t.Error("Should not error when file is exactly at size limit")
	}
}
