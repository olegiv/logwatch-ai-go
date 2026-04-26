// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ocms

import (
	"strings"
	"testing"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

var _ analyzer.PromptBuilder = (*PromptBuilder)(nil)

func TestPromptBuilder_GetLogType(t *testing.T) {
	t.Parallel()

	pb := NewPromptBuilder()
	if got := pb.GetLogType(); got != "ocms" {
		t.Fatalf("GetLogType() = %q, want %q", got, "ocms")
	}
}

func TestPromptBuilder_GetSystemPrompt(t *testing.T) {
	t.Parallel()

	pb := NewPromptBuilder()
	prompt := pb.GetSystemPrompt([]string{"ignore known false positive"})
	if !strings.Contains(prompt, "OCMS") {
		t.Fatal("system prompt should mention OCMS")
	}
	if !strings.Contains(prompt, "ignore known false positive") {
		t.Fatal("system prompt should include global exclusions block")
	}
}

func TestPromptBuilder_GetUserPrompt(t *testing.T) {
	t.Parallel()

	pb := NewPromptBuilder()
	prompt := pb.GetUserPrompt("line1", "history1", []string{"foo"})
	if !strings.Contains(prompt, "OCMS LOG OUTPUT") {
		t.Fatal("user prompt should include OCMS header")
	}
	if !strings.Contains(prompt, "HISTORICAL CONTEXT") {
		t.Fatal("user prompt should include historical context")
	}
	if !strings.Contains(prompt, "foo") {
		t.Fatal("user prompt should include contextual exclusions")
	}
}
