// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCoerceStringArray(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "empty raw message",
			raw:  "",
			want: []string{},
		},
		{
			name: "explicit null",
			raw:  "null",
			want: []string{},
		},
		{
			name: "empty array",
			raw:  "[]",
			want: []string{},
		},
		{
			name: "array of plain strings",
			raw:  `["first", "second"]`,
			want: []string{"first", "second"},
		},
		{
			name: "array with object using description key",
			raw:  `[{"description": "Configure trust"}]`,
			want: []string{"Configure trust"},
		},
		{
			name: "array with mixed string and object",
			raw:  `["plain", {"description": "from object"}]`,
			want: []string{"plain", "from object"},
		},
		{
			name: "object with message key",
			raw:  `[{"message": "from message"}]`,
			want: []string{"from message"},
		},
		{
			name: "object with unknown keys is skipped",
			raw:  `[{"alpha": "first", "beta": "second"}]`,
			want: []string{},
		},
		{
			name: "object with only nested objects is skipped",
			raw:  `[{"nested": {"a": "b"}}]`,
			want: []string{},
		},
		{
			name: "numbers and nulls are skipped",
			raw:  `[42, null, "keep"]`,
			want: []string{"keep"},
		},
		{
			name: "empty string items are skipped",
			raw:  `["", "kept"]`,
			want: []string{"kept"},
		},
		{
			name: "scalar string instead of array",
			raw:  `"restart nginx"`,
			want: []string{"restart nginx"},
		},
		{
			name: "scalar object instead of array",
			raw:  `{"description": "single"}`,
			want: []string{"single"},
		},
		{
			name: "description wins over message",
			raw:  `[{"description": "desc", "message": "msg"}]`,
			want: []string{"desc"},
		},
		{
			name: "empty description falls through to next key",
			raw:  `[{"description": "", "message": "msg"}]`,
			want: []string{"msg"},
		},
		{
			name: "malformed array JSON",
			raw:  `[unterminated`,
			want: []string{},
		},
		{
			name: "scalar number returns empty",
			raw:  `42`,
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := coerceStringArray(json.RawMessage(tt.raw))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("coerceStringArray(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCoerceStringItem(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		want   string
		wantOk bool
	}{
		{name: "plain string", raw: `"hello"`, want: "hello", wantOk: true},
		{name: "empty string skipped", raw: `""`, want: "", wantOk: false},
		{name: "null skipped", raw: `null`, want: "", wantOk: false},
		{name: "number skipped", raw: `42`, want: "", wantOk: false},
		{name: "bool skipped", raw: `true`, want: "", wantOk: false},
		{name: "object with description", raw: `{"description": "x"}`, want: "x", wantOk: true},
		{name: "object with only nested", raw: `{"nested": {"a": "b"}}`, want: "", wantOk: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := coerceStringItem(json.RawMessage(tt.raw))
			if ok != tt.wantOk {
				t.Errorf("coerceStringItem(%q) ok = %v, want %v", tt.raw, ok, tt.wantOk)
			}
			if got != tt.want {
				t.Errorf("coerceStringItem(%q) value = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestExtractDescriptiveField(t *testing.T) {
	tests := []struct {
		name   string
		obj    map[string]any
		want   string
		wantOk bool
	}{
		{
			name:   "description preferred",
			obj:    map[string]any{"description": "d", "message": "m"},
			want:   "d",
			wantOk: true,
		},
		{
			name:   "message used when description absent",
			obj:    map[string]any{"message": "m"},
			want:   "m",
			wantOk: true,
		},
		{
			name:   "non-string descriptive field ignored",
			obj:    map[string]any{"description": 42, "message": "m"},
			want:   "m",
			wantOk: true,
		},
		{
			name:   "object with only unknown keys is skipped",
			obj:    map[string]any{"zulu": "z", "alpha": "a"},
			want:   "",
			wantOk: false,
		},
		{
			name:   "no string values under known keys returns false",
			obj:    map[string]any{"num": 1, "nested": map[string]any{"a": "b"}},
			want:   "",
			wantOk: false,
		},
		{
			name:   "empty description with no other known keys is skipped",
			obj:    map[string]any{"description": "", "beta": "b"},
			want:   "",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractDescriptiveField(tt.obj)
			if ok != tt.wantOk {
				t.Errorf("extractDescriptiveField ok = %v, want %v", ok, tt.wantOk)
			}
			if got != tt.want {
				t.Errorf("extractDescriptiveField value = %q, want %q", got, tt.want)
			}
		})
	}
}
