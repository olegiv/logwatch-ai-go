// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package analyzer

import (
	"fmt"
)

// LogSourceType identifies the type of log source.
type LogSourceType string

// Supported log source types.
const (
	LogSourceLogwatch       LogSourceType = "logwatch"
	LogSourceDrupalWatchdog LogSourceType = "drupal_watchdog"
	LogSourceOCMS           LogSourceType = "ocms"
)

// LogSource bundles all components needed to analyze a specific log type.
type LogSource struct {
	Type          LogSourceType
	Reader        LogReader
	Preprocessor  Preprocessor
	PromptBuilder PromptBuilder
}

// ValidSourceTypes returns a list of valid log source type strings.
// Useful for configuration validation.
func ValidSourceTypes() []string {
	return []string{
		string(LogSourceLogwatch),
		string(LogSourceDrupalWatchdog),
		string(LogSourceOCMS),
	}
}

// ParseSourceType converts a string to LogSourceType.
// Returns an error if the string is not a valid source type.
func ParseSourceType(s string) (LogSourceType, error) {
	switch s {
	case string(LogSourceLogwatch):
		return LogSourceLogwatch, nil
	case string(LogSourceDrupalWatchdog):
		return LogSourceDrupalWatchdog, nil
	case string(LogSourceOCMS):
		return LogSourceOCMS, nil
	default:
		return "", fmt.Errorf("invalid log source type: %q (valid types: %v)", s, ValidSourceTypes())
	}
}
