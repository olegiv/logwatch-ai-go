// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ocms reads and preprocesses OCMS logs for LLM analysis.
package ocms

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

// LogFile identifies one OCMS log file to read.
type LogFile struct {
	Kind string
	Path string
}

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
	contentStr, err := r.readRaw(sourcePath)
	if err != nil {
		return "", err
	}

	return r.preprocessIfNeeded(contentStr)
}

func (r *Reader) readRaw(sourcePath string) (string, error) {
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

	return contentStr, nil
}

func (r *Reader) preprocessIfNeeded(content string) (string, error) {
	if r.enablePreprocessing {
		tokens := r.preprocessor.EstimateTokens(content)
		if tokens > r.maxTokens {
			processedContent, perr := r.preprocessor.Process(content)
			if perr != nil {
				return "", fmt.Errorf("preprocessing failed: %w", perr)
			}
			return processedContent, nil
		}
	}

	return content, nil
}

// ReadFiles reads one or more OCMS log files. Multiple files are combined with
// labels so the LLM can distinguish main and error log sections. Missing
// files are tolerated in multi-file mode — a site with no errors won't have
// error.log (or its rotated .1) and that's a normal case. Fails only if
// every requested file is missing.
func (r *Reader) ReadFiles(files []LogFile) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("no OCMS log files specified")
	}
	if len(files) == 1 {
		return r.Read(files[0].Path)
	}

	var combined strings.Builder
	var skipped []string
	written := 0
	for _, file := range files {
		content, err := r.readRaw(file.Path)
		if err != nil {
			// Tolerate missing rotated files in multi-file mode without a
			// separate os.Stat call — going through readRaw alone keeps the
			// existence check and the read in one path lookup, removing a
			// TOCTOU window.
			if errors.Is(err, fs.ErrNotExist) {
				skipped = append(skipped, fmt.Sprintf("%s (%s)", file.Kind, file.Path))
				continue
			}
			return "", fmt.Errorf("failed to read OCMS %s log %s: %w", file.Kind, file.Path, err)
		}

		if written > 0 {
			combined.WriteString("\n\n")
		}
		combined.WriteString("### OCMS ")
		combined.WriteString(strings.ToUpper(file.Kind))
		combined.WriteString(" LOG\n")
		combined.WriteString("Path: ")
		combined.WriteString(file.Path)
		combined.WriteString("\n\n")
		combined.WriteString(content)
		written++
	}

	if written == 0 {
		return "", fmt.Errorf("no readable OCMS log files (all missing): %s", strings.Join(skipped, ", "))
	}

	return r.preprocessIfNeeded(combined.String())
}

// Validate validates OCMS log content.
func (r *Reader) Validate(content string) error {
	return r.validateContent(content)
}

func (r *Reader) validateContent(content string) error {
	if len(content) == 0 {
		return fmt.Errorf("ocms log file is empty")
	}

	return nil
}

// GetSourceInfo returns metadata about the OCMS source file.
func (r *Reader) GetSourceInfo(sourcePath string) (map[string]any, error) {
	return analyzer.GetSourceFileInfo(sourcePath)
}
