// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"encoding/json"
	"strings"
)

// rawAnalysis mirrors Analysis but defers array parsing so we can coerce
// non-string items (e.g. {"description": "..."}) the LLM occasionally emits
// despite prompt instructions. Each array field is captured as a raw JSON
// message and normalized by coerceStringArray.
type rawAnalysis struct {
	SystemStatus    string          `json:"systemStatus"`
	Summary         string          `json:"summary"`
	CriticalIssues  json.RawMessage `json:"criticalIssues"`
	Warnings        json.RawMessage `json:"warnings"`
	Recommendations json.RawMessage `json:"recommendations"`
	Metrics         map[string]any  `json:"metrics"`
}

// descriptiveFieldKeys lists the JSON keys (in priority order) consulted when
// coercing an object into a string. The first non-empty value wins.
var descriptiveFieldKeys = []string{
	"description",
	"message",
	"text",
	"issue",
	"recommendation",
	"warning",
	"summary",
	"detail",
	"title",
	"name",
}

// coerceStringArray normalizes a JSON value into a []string. It tolerates:
//   - a missing field or explicit null (returns empty slice)
//   - an empty array (returns empty slice)
//   - a well-formed string array (pass-through)
//   - mixed arrays containing objects, numbers, or nulls (objects are extracted
//     via descriptive-field lookup; non-coercible items are skipped)
//   - a scalar string or object where an array was expected (wrapped into a
//     single-item slice)
func coerceStringArray(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return []string{}
	}

	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []string{}
	}

	if strings.HasPrefix(trimmed, "[") {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return []string{}
		}
		result := make([]string, 0, len(items))
		for _, item := range items {
			if v, ok := coerceStringItem(item); ok {
				result = append(result, v)
			}
		}
		return result
	}

	// Scalar fallback: string or object where array was expected.
	if v, ok := coerceStringItem(raw); ok {
		return []string{v}
	}
	return []string{}
}

// coerceStringItem returns the string form of a single JSON value. A return of
// ok=false means the value should be skipped (null, number, bool, or an object
// with no extractable descriptive string).
func coerceStringItem(item json.RawMessage) (string, bool) {
	trimmed := strings.TrimSpace(string(item))
	if trimmed == "" || trimmed == "null" {
		return "", false
	}

	// Plain string.
	var s string
	if err := json.Unmarshal(item, &s); err == nil {
		if s == "" {
			return "", false
		}
		return s, true
	}

	// Object: extract via descriptive-field lookup.
	if strings.HasPrefix(trimmed, "{") {
		var obj map[string]any
		if err := json.Unmarshal(item, &obj); err == nil {
			return extractDescriptiveField(obj)
		}
	}

	// Numbers, booleans, nested arrays - skip.
	return "", false
}

// extractDescriptiveField looks for a string under common descriptive keys
// (descriptiveFieldKeys). Returns ok=false if the object contains no match
// under one of those keys. Previously this fell back to joining all
// top-level string values - that was removed because a synthesized string
// that never appeared in the LLM output can silently defeat operator-
// configured exclusion substring patterns in internal/exclusions.
func extractDescriptiveField(m map[string]any) (string, bool) {
	for _, key := range descriptiveFieldKeys {
		if v, present := m[key]; present {
			if s, ok := v.(string); ok && s != "" {
				return s, true
			}
		}
	}
	return "", false
}
