// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ocms

import (
	"strings"
	"testing"
)

func TestPreprocessor_ShouldProcess(t *testing.T) {
	p := NewPreprocessor(10)
	if !p.ShouldProcess(strings.Repeat("word ", 100), 10) {
		t.Error("expected ShouldProcess=true for content over budget")
	}
	if p.ShouldProcess("small", 10) {
		t.Error("expected ShouldProcess=false for content under budget")
	}
}

func TestPreprocessor_Process_UnderBudget(t *testing.T) {
	p := NewPreprocessor(1_000_000)
	got, err := p.Process(sampleSlog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != sampleSlog {
		t.Error("content under budget should be returned unchanged")
	}
}

func TestPreprocessor_Process_Empty(t *testing.T) {
	p := NewPreprocessor(100)
	got, err := p.Process("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
}

func TestPreprocessor_Process_Deduplicates(t *testing.T) {
	line := "time=2026-04-23T10:16:20Z level=info msg=\"Login failed\" category=auth ip_address=192.168.1.5\n"
	content := strings.Repeat(line, 100)
	p := NewPreprocessor(50) // well below content size to force processing
	got, err := p.Process(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "occurred") {
		t.Errorf("expected deduplicated output to mention 'occurred', got: %s", got)
	}
	if strings.Count(got, "Login failed") > 2 {
		t.Errorf("expected dedup to collapse duplicates, got %d mentions", strings.Count(got, "Login failed"))
	}
}

func TestPreprocessor_Process_BucketedByLevel(t *testing.T) {
	var lines []string
	lines = append(lines, "time=T level=error msg=\"db fail\" q=1")
	lines = append(lines, "time=T level=warn msg=\"cache miss\" d=2")
	for i := 0; i < 50; i++ {
		lines = append(lines, "time=T level=info msg=\"req\" path=/a")
	}
	for i := 0; i < 50; i++ {
		lines = append(lines, "time=T level=debug msg=\"trace\" step=x")
	}
	content := strings.Join(lines, "\n") + "\n"

	p := NewPreprocessor(30) // tight budget; should drop debug/info, keep error+warn
	got, err := p.Process(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "db fail") {
		t.Error("error-level line must be preserved")
	}
	if !strings.Contains(got, "ERROR") {
		t.Error("ERROR section header should appear")
	}
}

func TestPreprocessor_ProcessWithBudget_Trims(t *testing.T) {
	content := strings.Repeat("time=T level=info msg=\"line\" i=1\n", 5000)
	p := NewPreprocessor(10_000)
	got, err := p.ProcessWithBudget(content, 40)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.EstimateTokens(got) > 40 {
		t.Errorf("output exceeds budget: estimated=%d budget=40", p.EstimateTokens(got))
	}
}

func TestPreprocessor_EstimateTokens(t *testing.T) {
	p := NewPreprocessor(100)
	if p.EstimateTokens("") != 0 {
		t.Error("empty content should yield 0 tokens")
	}
	if p.EstimateTokens("hello world") <= 0 {
		t.Error("non-empty content should yield >0 tokens")
	}
}

func TestPriorityForLevel(t *testing.T) {
	tests := map[string]int{
		"error":   priorityError,
		"ERROR":   priorityError,
		"fatal":   priorityError,
		"warn":    priorityWarn,
		"warning": priorityWarn,
		"info":    priorityInfo,
		"debug":   priorityDebug,
		"trace":   priorityDebug,
		"custom":  priorityUnknown,
	}
	for in, want := range tests {
		if got := priorityForLevel(in); got != want {
			t.Errorf("priorityForLevel(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestNormalizeLine_GroupsByShape(t *testing.T) {
	a := normalizeLine("time=2026-04-23T10:15:42Z level=info msg=\"req\" ip=192.168.1.1 n=42")
	b := normalizeLine("time=2026-04-24T11:00:00Z level=info msg=\"req\" ip=10.0.0.5 n=99")
	if a != b {
		t.Errorf("lines differing only in time/ip/numbers should normalize equal:\n a=%q\n b=%q", a, b)
	}
}
