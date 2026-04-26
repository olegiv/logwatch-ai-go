// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

// Package logwatch reads and preprocesses Linux logwatch reports for LLM analysis.
package logwatch

import (
	"fmt"
	"time"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

// Compile-time interface check
var _ analyzer.LogReader = (*Reader)(nil)

// Reader handles reading and validating logwatch output files.
// Implements analyzer.LogReader interface.
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

// Read implements analyzer.LogReader.Read.
// Reads and processes the logwatch output file.
func (r *Reader) Read(sourcePath string) (string, error) {
	contentStr, err := analyzer.ReadSourceFileWithGuards(
		sourcePath,
		analyzer.FileReadOptions{
			SourceLabel: "logwatch",
			MaxSizeMB:   r.maxSizeMB,
			MaxAge:      24 * time.Hour,
		},
		r.validateContent,
	)
	if err != nil {
		return "", err
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

// ReadLogwatchOutput reads and processes the logwatch output file.
//
// Deprecated: Use Read() instead. This method is kept for backward compatibility.
func (r *Reader) ReadLogwatchOutput(filePath string) (string, error) {
	return r.Read(filePath)
}

// Validate implements analyzer.LogReader.Validate.
// Performs basic validation on logwatch content.
func (r *Reader) Validate(content string) error {
	return r.validateContent(content)
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

// GetSourceInfo implements analyzer.LogReader.GetSourceInfo.
// Returns metadata about the logwatch file.
func (r *Reader) GetSourceInfo(sourcePath string) (map[string]any, error) {
	return analyzer.GetSourceFileInfo(sourcePath)
}

// GetFileInfo returns information about the logwatch file.
//
// Deprecated: Use GetSourceInfo() instead. This method is kept for backward compatibility.
func (r *Reader) GetFileInfo(filePath string) (map[string]any, error) {
	return r.GetSourceInfo(filePath)
}
