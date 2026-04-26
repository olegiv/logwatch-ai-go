// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package analyzer

import (
	"fmt"
	"os"
	"time"
)

// FileReadOptions controls common source-file read guards used by log readers.
type FileReadOptions struct {
	SourceLabel string
	MaxSizeMB   int
	MaxAge      time.Duration
}

// ReadSourceFileWithGuards reads a text source file after common safety checks.
// It standardizes not-found/readability/size/age errors and then delegates
// content validation to validateContent.
func ReadSourceFileWithGuards(
	sourcePath string,
	opts FileReadOptions,
	validateContent func(content string) error,
) (string, error) {
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%s file not found: %s", opts.SourceLabel, sourcePath)
		}
		return "", fmt.Errorf("failed to stat %s file: %w", opts.SourceLabel, err)
	}

	if fileInfo.Mode().Perm()&0400 == 0 {
		return "", fmt.Errorf("%s file is not readable: %s", opts.SourceLabel, sourcePath)
	}

	maxBytes := int64(opts.MaxSizeMB) * 1024 * 1024
	if fileInfo.Size() > maxBytes {
		return "", fmt.Errorf("%s file exceeds maximum size of %dMB (size: %.2fMB)",
			opts.SourceLabel, opts.MaxSizeMB, float64(fileInfo.Size())/1024/1024)
	}

	if opts.MaxAge > 0 {
		fileAge := time.Since(fileInfo.ModTime())
		if fileAge > opts.MaxAge {
			return "", fmt.Errorf("%s file is too old (%.1f hours), may be stale", opts.SourceLabel, fileAge.Hours())
		}
	}

	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", fmt.Errorf("failed to read %s file: %w", opts.SourceLabel, err)
	}
	contentStr := string(content)

	if err := validateContent(contentStr); err != nil {
		return "", fmt.Errorf("%s content validation failed: %w", opts.SourceLabel, err)
	}

	return contentStr, nil
}

// GetSourceFileInfo returns common file metadata used in log-source readers.
func GetSourceFileInfo(sourcePath string) (map[string]any, error) {
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"size_bytes": fileInfo.Size(),
		"size_mb":    float64(fileInfo.Size()) / 1024 / 1024,
		"modified":   fileInfo.ModTime(),
		"age_hours":  time.Since(fileInfo.ModTime()).Hours(),
	}, nil
}
