// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/olegiv/logwatch-ai-go/internal/ai"
	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
	"github.com/olegiv/logwatch-ai-go/internal/config"
)

type testPromptBuilder struct{}

func (p *testPromptBuilder) GetSystemPrompt() string {
	return "system"
}

func (p *testPromptBuilder) GetUserPrompt(logContent, historicalContext string) string {
	return "LOG|" + logContent + "|HIST|" + historicalContext
}

func (p *testPromptBuilder) GetLogType() string {
	return "logwatch"
}

type mockProvider struct {
	providerName string
	modelInfo    map[string]interface{}
}

func (m *mockProvider) Analyze(_ context.Context, _, _ string) (*ai.Analysis, *ai.Stats, error) {
	return nil, nil, fmt.Errorf("not implemented")
}

func (m *mockProvider) GetModelInfo() map[string]interface{} {
	return m.modelInfo
}

func (m *mockProvider) GetProviderName() string {
	return m.providerName
}

type mockPromptTokenCounter struct {
	*mockProvider
	countFunc func(systemPrompt, userPrompt string) (int, error)
}

func (m *mockPromptTokenCounter) CountPromptTokens(_ context.Context, systemPrompt, userPrompt string) (int, error) {
	return m.countFunc(systemPrompt, userPrompt)
}

type scalingBudgetPreprocessor struct {
	multiplier   float64
	processCalls int
}

func (p *scalingBudgetPreprocessor) EstimateTokens(content string) int {
	return int(math.Ceil(float64(len(content)) / p.multiplier))
}

func (p *scalingBudgetPreprocessor) Process(content string) (string, error) {
	return p.ProcessWithBudget(content, p.EstimateTokens(content))
}

func (p *scalingBudgetPreprocessor) ProcessWithBudget(content string, maxTokens int) (string, error) {
	p.processCalls++
	actualSize := int(math.Ceil(float64(maxTokens) * p.multiplier))
	if actualSize > len(content) {
		actualSize = len(content)
	}
	if actualSize < 0 {
		actualSize = 0
	}
	return content[:actualSize], nil
}

func (p *scalingBudgetPreprocessor) ShouldProcess(content string, maxTokens int) bool {
	return p.EstimateTokens(content) > maxTokens
}

type stubbornBudgetPreprocessor struct {
	processCalls int
}

func (p *stubbornBudgetPreprocessor) EstimateTokens(content string) int {
	return len(content) / 10
}

func (p *stubbornBudgetPreprocessor) Process(content string) (string, error) {
	return p.ProcessWithBudget(content, len(content))
}

func (p *stubbornBudgetPreprocessor) ProcessWithBudget(content string, _ int) (string, error) {
	p.processCalls++
	return content, nil
}

func (p *stubbornBudgetPreprocessor) ShouldProcess(_ string, _ int) bool {
	return true
}

func TestPreparePromptForAnalysisAnthropicAlreadyFits(t *testing.T) {
	cfg := &config.Config{
		EnablePreprocessing: true,
		AIMaxTokens:         100,
	}
	provider := &mockPromptTokenCounter{
		mockProvider: &mockProvider{
			providerName: "Anthropic",
			modelInfo: map[string]interface{}{
				"context_limit": 4200,
			},
		},
		countFunc: func(systemPrompt, userPrompt string) (int, error) {
			return len(systemPrompt) + len(userPrompt), nil
		},
	}
	preprocessor := &scalingBudgetPreprocessor{multiplier: 1.9}
	logSource := &analyzer.LogSource{
		Preprocessor:  preprocessor,
		PromptBuilder: &testPromptBuilder{},
	}

	result, err := preparePromptForAnalysis(
		context.Background(),
		cfg,
		provider,
		logSource,
		"system",
		strings.Repeat("x", 200),
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("preparePromptForAnalysis() error = %v", err)
	}

	if result.LogContent != strings.Repeat("x", 200) {
		t.Fatalf("expected raw log content to pass through unchanged")
	}

	if preprocessor.processCalls != 0 {
		t.Fatalf("expected no preprocessing, got %d calls", preprocessor.processCalls)
	}
}

func TestPreparePromptForAnalysisAnthropicFitsAfterOneRecompression(t *testing.T) {
	cfg := &config.Config{
		EnablePreprocessing: true,
		AIMaxTokens:         100,
	}
	provider := &mockPromptTokenCounter{
		mockProvider: &mockProvider{
			providerName: "Anthropic",
			modelInfo: map[string]interface{}{
				"context_limit": 4200,
			},
		},
		countFunc: func(systemPrompt, userPrompt string) (int, error) {
			return len(systemPrompt) + len(userPrompt), nil
		},
	}
	preprocessor := &scalingBudgetPreprocessor{multiplier: 1.75}
	rawLogContent := strings.Repeat("x", 3500)
	logSource := &analyzer.LogSource{
		Preprocessor:  preprocessor,
		PromptBuilder: &testPromptBuilder{},
	}

	result, err := preparePromptForAnalysis(
		context.Background(),
		cfg,
		provider,
		logSource,
		"system",
		rawLogContent,
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("preparePromptForAnalysis() error = %v", err)
	}

	if preprocessor.processCalls != 1 {
		t.Fatalf("expected 1 recompression, got %d", preprocessor.processCalls)
	}

	if len(result.LogContent) >= len(rawLogContent) {
		t.Fatalf("expected compressed log content, got %d >= %d", len(result.LogContent), len(rawLogContent))
	}
}

func TestPreparePromptForAnalysisAnthropicNeedsMultipleRecompressions(t *testing.T) {
	cfg := &config.Config{
		EnablePreprocessing: true,
		AIMaxTokens:         100,
	}
	provider := &mockPromptTokenCounter{
		mockProvider: &mockProvider{
			providerName: "Anthropic",
			modelInfo: map[string]interface{}{
				"context_limit": 4700,
			},
		},
		countFunc: func(systemPrompt, userPrompt string) (int, error) {
			return len(systemPrompt) + len(userPrompt), nil
		},
	}
	preprocessor := &scalingBudgetPreprocessor{multiplier: 2.1}
	logSource := &analyzer.LogSource{
		Preprocessor:  preprocessor,
		PromptBuilder: &testPromptBuilder{},
	}

	result, err := preparePromptForAnalysis(
		context.Background(),
		cfg,
		provider,
		logSource,
		"system",
		strings.Repeat("x", 5000),
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("preparePromptForAnalysis() error = %v", err)
	}

	if preprocessor.processCalls < 2 {
		t.Fatalf("expected multiple recompressions, got %d", preprocessor.processCalls)
	}

	if result.UserPrompt == "" {
		t.Fatal("expected non-empty user prompt")
	}
}

func TestPreparePromptForAnalysisAnthropicCountFailure(t *testing.T) {
	cfg := &config.Config{
		EnablePreprocessing: true,
		AIMaxTokens:         100,
	}
	provider := &mockPromptTokenCounter{
		mockProvider: &mockProvider{
			providerName: "Anthropic",
			modelInfo: map[string]interface{}{
				"context_limit": 4200,
			},
		},
		countFunc: func(systemPrompt, userPrompt string) (int, error) {
			return 0, fmt.Errorf("count failed")
		},
	}
	logSource := &analyzer.LogSource{
		Preprocessor:  &scalingBudgetPreprocessor{multiplier: 2},
		PromptBuilder: &testPromptBuilder{},
	}

	_, err := preparePromptForAnalysis(
		context.Background(),
		cfg,
		provider,
		logSource,
		"system",
		strings.Repeat("x", 1000),
		"",
		nil,
	)
	if err == nil {
		t.Fatal("preparePromptForAnalysis() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to count Anthropic prompt tokens") {
		t.Fatalf("expected count failure error, got %v", err)
	}
}

func TestPreparePromptForAnalysisAnthropicStillTooLargeAfterRetries(t *testing.T) {
	cfg := &config.Config{
		EnablePreprocessing: true,
		AIMaxTokens:         100,
	}
	provider := &mockPromptTokenCounter{
		mockProvider: &mockProvider{
			providerName: "Anthropic",
			modelInfo: map[string]interface{}{
				"context_limit": 4200,
			},
		},
		countFunc: func(systemPrompt, userPrompt string) (int, error) {
			return len(systemPrompt) + len(userPrompt), nil
		},
	}
	preprocessor := &stubbornBudgetPreprocessor{}
	logSource := &analyzer.LogSource{
		Preprocessor:  preprocessor,
		PromptBuilder: &testPromptBuilder{},
	}

	_, err := preparePromptForAnalysis(
		context.Background(),
		cfg,
		provider,
		logSource,
		"system",
		strings.Repeat("x", 4000),
		"",
		nil,
	)
	if err == nil {
		t.Fatal("preparePromptForAnalysis() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "Anthropic prompt still exceeds context window") {
		t.Fatalf("expected oversized prompt error, got %v", err)
	}

	if preprocessor.processCalls != maxAnthropicPromptFitAttempts {
		t.Fatalf("expected %d compression attempts, got %d", maxAnthropicPromptFitAttempts, preprocessor.processCalls)
	}
}

func TestPreparePromptForAnalysisNonAnthropicUsesHeuristicPath(t *testing.T) {
	cfg := &config.Config{
		EnablePreprocessing: true,
		AIMaxTokens:         1000,
	}
	provider := &mockProvider{
		providerName: "Ollama",
		modelInfo: map[string]interface{}{
			"context_limit": 4000,
		},
	}
	preprocessor := &scalingBudgetPreprocessor{multiplier: 1.0}
	rawLogContent := strings.Repeat("x", 3000)
	logSource := &analyzer.LogSource{
		Preprocessor:  preprocessor,
		PromptBuilder: &testPromptBuilder{},
	}

	result, err := preparePromptForAnalysis(
		context.Background(),
		cfg,
		provider,
		logSource,
		"system",
		rawLogContent,
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("preparePromptForAnalysis() error = %v", err)
	}

	if preprocessor.processCalls == 0 {
		t.Fatal("expected heuristic preprocessing for non-Anthropic provider")
	}

	if len(result.LogContent) >= len(rawLogContent) {
		t.Fatalf("expected compressed content for heuristic path, got %d >= %d", len(result.LogContent), len(rawLogContent))
	}
}
