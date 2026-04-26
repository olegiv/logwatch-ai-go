// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ocms

import (
	"fmt"
	"time"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

// Reader handles reading and validating OCMS log files.
type Reader struct {
	maxSizeMB           int
	enablePreprocessing bool
	maxTokens           int
	preprocessor        *Preprocessor
}

var _ analyzer.LogReader = (*Reader)(nil)

// NewReader creates a new OCMS reader.
func NewReader(maxSizeMB int, enablePreprocessing bool, maxTokens int) *Reader {
	return &Reader{
		maxSizeMB:           maxSizeMB,
		enablePreprocessing: enablePreprocessing,
		maxTokens:           maxTokens,
		preprocessor:        NewPreprocessor(maxTokens),
	}
}

// Read reads and validates OCMS log content.
func (r *Reader) Read(sourcePath string) (string, error) {
	contentStr, err := analyzer.ReadSourceFileWithGuards(
		sourcePath,
		analyzer.FileReadOptions{
			SourceLabel: "ocms log",
			MaxSizeMB:   r.maxSizeMB,
			MaxAge:      24 * time.Hour,
		},
		r.validateContent,
	)
	if err != nil {
		return "", err
	}

	if r.enablePreprocessing {
		tokens := r.preprocessor.EstimateTokens(contentStr)
		if tokens > r.maxTokens {
			processedContent, perr := r.preprocessor.Process(contentStr)
			if perr != nil {
				return "", fmt.Errorf("preprocessing failed: %w", perr)
			}
			return processedContent, nil
		}
	}

	return contentStr, nil
}

// Validate validates OCMS log content.
func (r *Reader) Validate(content string) error {
	return r.validateContent(content)
}

func (r *Reader) validateContent(content string) error {
	if len(content) == 0 {
		return fmt.Errorf("ocms log file is empty")
	}

	if len(content) < 100 {
		return fmt.Errorf("ocms log file seems too small to be valid (only %d bytes)", len(content))
	}

	return nil
}

// GetSourceInfo returns metadata about the OCMS source file.
func (r *Reader) GetSourceInfo(sourcePath string) (map[string]any, error) {
	return analyzer.GetSourceFileInfo(sourcePath)
}
