// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ocms

import (
	"fmt"
	"strings"
	"testing"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

var (
	_ analyzer.Preprocessor       = (*Preprocessor)(nil)
	_ analyzer.BudgetPreprocessor = (*Preprocessor)(nil)
)

func TestPreprocessor_Basic(t *testing.T) {
	t.Parallel()

	p := NewPreprocessor(1000)
	if p == nil {
		t.Fatal("NewPreprocessor returned nil")
	}

	content := strings.Repeat("INFO request completed in 15ms\n", 200)
	if p.EstimateTokens(content) <= 0 {
		t.Fatal("EstimateTokens should be > 0")
	}

	processed, err := p.Process(content)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if processed == "" {
		t.Fatal("Process() should not return empty content")
	}
}

func TestPreprocessorPreservesDistinctSecurityOrigins(t *testing.T) {
	t.Parallel()

	p := NewPreprocessor(1000)
	var content strings.Builder
	content.WriteString("################### Authentication Failures ###################\n")
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&content, "Unauthorized login failed from 203.0.113.%d for account %d\n", i, i)
	}

	processed, err := p.ProcessWithBudget(content.String(), 100)
	if err != nil {
		t.Fatalf("ProcessWithBudget() error = %v", err)
	}
	for _, origin := range []string{"203.0.113.1", "203.0.113.2"} {
		if !strings.Contains(processed, origin) {
			t.Fatalf("OCMS preprocessing merged distinct security origin %s: %q", origin, processed)
		}
	}
	if strings.Contains(processed, "occurred 12 times") {
		t.Fatalf("OCMS preprocessing collapsed distinct attacker evidence: %q", processed)
	}
}
