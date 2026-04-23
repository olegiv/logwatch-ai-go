// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ocms reads and preprocesses ocms-go slog output for LLM analysis.
package ocms

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

// Compile-time interface check
var _ analyzer.LogReader = (*Reader)(nil)

// slogShapeRegex matches a minimal slog text-handler line:
//
//	time=<value> level=<value> msg=<value>
//
// Only one such line is required to accept the file as OCMS output; other
// lines (stack traces, panics, pre-startup stderr) are tolerated.
var slogShapeRegex = regexp.MustCompile(`(?m)^time=\S+\s+level=\S+\s+msg=`)

// Reader reads and validates ocms-go slog output files.
// Implements analyzer.LogReader.
type Reader struct {
	maxSizeMB           int
	enablePreprocessing bool
	maxTokens           int
	preprocessor        *Preprocessor
}

// NewReader creates a new OCMS log reader.
func NewReader(maxSizeMB int, enablePreprocessing bool, maxTokens int) *Reader {
	return &Reader{
		maxSizeMB:           maxSizeMB,
		enablePreprocessing: enablePreprocessing,
		maxTokens:           maxTokens,
		preprocessor:        NewPreprocessor(maxTokens),
	}
}

// Read implements analyzer.LogReader.Read.
func (r *Reader) Read(sourcePath string) (string, error) {
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("ocms log file not found: %s", sourcePath)
		}
		return "", fmt.Errorf("failed to stat ocms log file: %w", err)
	}

	if fileInfo.Mode().Perm()&0400 == 0 {
		return "", fmt.Errorf("ocms log file is not readable: %s", sourcePath)
	}

	maxBytes := int64(r.maxSizeMB) * 1024 * 1024
	if fileInfo.Size() > maxBytes {
		return "", fmt.Errorf("ocms log file exceeds maximum size of %dMB (size: %.2fMB)",
			r.maxSizeMB, float64(fileInfo.Size())/1024/1024)
	}

	fileAge := time.Since(fileInfo.ModTime())
	if fileAge > 24*time.Hour {
		return "", fmt.Errorf("ocms log file is too old (%.1f hours), may be stale",
			fileAge.Hours())
	}

	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", fmt.Errorf("failed to read ocms log file: %w", err)
	}

	contentStr := string(content)

	if err := r.validateContent(contentStr); err != nil {
		return "", fmt.Errorf("ocms content validation failed: %w", err)
	}

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

// Validate implements analyzer.LogReader.Validate.
func (r *Reader) Validate(content string) error {
	return r.validateContent(content)
}

// validateContent performs basic shape validation. A file is accepted if
// it is non-empty, at least 50 bytes, and contains at least one line that
// looks like a slog text-handler record. The shape check is intentionally
// lenient so mixed content (stack traces, pre-startup stderr) is tolerated.
func (r *Reader) validateContent(content string) error {
	if len(content) == 0 {
		return fmt.Errorf("ocms log file is empty")
	}
	if len(content) < 50 {
		return fmt.Errorf("ocms log file seems too small to be valid (only %d bytes)", len(content))
	}
	if !slogShapeRegex.MatchString(content) {
		return fmt.Errorf("ocms log file does not contain any recognizable slog lines (expected 'time=... level=... msg=...')")
	}
	return nil
}

// GetSourceInfo implements analyzer.LogReader.GetSourceInfo.
func (r *Reader) GetSourceInfo(sourcePath string) (map[string]any, error) {
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return nil, err
	}

	info := map[string]any{
		"size_bytes": fileInfo.Size(),
		"size_mb":    float64(fileInfo.Size()) / 1024 / 1024,
		"modified":   fileInfo.ModTime(),
		"age_hours":  time.Since(fileInfo.ModTime()).Hours(),
	}

	return info, nil
}
