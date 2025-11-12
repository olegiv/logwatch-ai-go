package logwatch

import (
	"fmt"
	"os"
	"time"
)

// Reader handles reading and validating logwatch output files
type Reader struct {
	maxSizeMB           int
	enablePreprocessing bool
	maxTokens           int
	preprocessor        *Preprocessor
}

// NewReader creates a new logwatch reader
func NewReader(maxSizeMB int, enablePreprocessing bool, maxTokens int) *Reader {
	return &Reader{
		maxSizeMB:           maxSizeMB,
		enablePreprocessing: enablePreprocessing,
		maxTokens:           maxTokens,
		preprocessor:        NewPreprocessor(maxTokens),
	}
}

// ReadLogwatchOutput reads and processes the logwatch output file
func (r *Reader) ReadLogwatchOutput(filePath string) (string, error) {
	// Check if file exists
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("logwatch output file not found: %s", filePath)
		}
		return "", fmt.Errorf("failed to stat logwatch file: %w", err)
	}

	// Check file permissions
	if fileInfo.Mode().Perm()&0400 == 0 {
		return "", fmt.Errorf("logwatch file is not readable: %s", filePath)
	}

	// Check file size
	maxBytes := int64(r.maxSizeMB) * 1024 * 1024
	if fileInfo.Size() > maxBytes {
		return "", fmt.Errorf("logwatch file exceeds maximum size of %dMB (size: %.2fMB)",
			r.maxSizeMB, float64(fileInfo.Size())/1024/1024)
	}

	// Check file age (warn if older than 24 hours)
	fileAge := time.Since(fileInfo.ModTime())
	if fileAge > 24*time.Hour {
		return "", fmt.Errorf("logwatch file is too old (%.1f hours), may be stale",
			fileAge.Hours())
	}

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read logwatch file: %w", err)
	}

	// Convert to string
	contentStr := string(content)

	// Validate content
	if err := r.validateContent(contentStr); err != nil {
		return "", fmt.Errorf("logwatch content validation failed: %w", err)
	}

	// Apply preprocessing if enabled
	if r.enablePreprocessing {
		tokens := r.preprocessor.EstimateTokens(contentStr)
		if tokens > r.maxTokens {
			processedContent, err := r.preprocessor.Process(contentStr)
			if err != nil {
				return "", fmt.Errorf("preprocessing failed: %w", err)
			}
			return processedContent, nil
		}
	}

	return contentStr, nil
}

// validateContent performs basic validation on logwatch content
func (r *Reader) validateContent(content string) error {
	if len(content) == 0 {
		return fmt.Errorf("logwatch file is empty")
	}

	// Check for minimal expected content
	// Logwatch typically includes headers and sections
	if len(content) < 100 {
		return fmt.Errorf("logwatch file seems too small to be valid (only %d bytes)", len(content))
	}

	return nil
}

// GetFileInfo returns information about the logwatch file
func (r *Reader) GetFileInfo(filePath string) (map[string]interface{}, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	info := map[string]interface{}{
		"size_bytes": fileInfo.Size(),
		"size_mb":    float64(fileInfo.Size()) / 1024 / 1024,
		"modified":   fileInfo.ModTime(),
		"age_hours":  time.Since(fileInfo.ModTime()).Hours(),
	}

	return info, nil
}
