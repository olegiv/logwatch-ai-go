// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package analyzer

import "testing"

func TestContextLimitFromModelInfo(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  int
	}{
		{
			name:  "nil metadata uses default",
			input: nil,
			want:  DefaultContextLimit,
		},
		{
			name:  "int value",
			input: map[string]any{"context_limit": 200000},
			want:  200000,
		},
		{
			name:  "float value",
			input: map[string]any{"context_limit": 128000.0},
			want:  128000,
		},
		{
			name:  "missing value uses default",
			input: map[string]any{"model": "test"},
			want:  DefaultContextLimit,
		},
		{
			name:  "invalid value uses default",
			input: map[string]any{"context_limit": -1},
			want:  DefaultContextLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContextLimitFromModelInfo(tt.input)
			if got != tt.want {
				t.Errorf("ContextLimitFromModelInfo() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCalculateLogTokenBudget(t *testing.T) {
	t.Run("reserves output overhead and safety margin", func(t *testing.T) {
		got := CalculateLogTokenBudget(200000, 8000, 1500, 2500)
		want := 178000 // 200000 - 8000 - 1500 - 2500 - 10000 safety margin
		if got != want {
			t.Errorf("CalculateLogTokenBudget() = %d, want %d", got, want)
		}
	})

	t.Run("enforces minimum budget", func(t *testing.T) {
		got := CalculateLogTokenBudget(5000, 4000, 800, 700)
		if got != minLogTokenBudget {
			t.Errorf("CalculateLogTokenBudget() = %d, want minimum %d", got, minLogTokenBudget)
		}
	})
}
