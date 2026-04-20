// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import "testing"

// TestResolvePricing covers the longest-prefix lookup, dated-ID resolution,
// and the unknown-model fallback. This is the invariant that keeps
// cost_usd correct when new dated snapshots of supported families ship.
func TestResolvePricing(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		wantKnown  bool
		wantInput  float64
		wantOutput float64
	}{
		{"Haiku 4.5 dated", "claude-haiku-4-5-20251001", true, 1.0, 5.0},
		{"Haiku 4.5 alias", "claude-haiku-4-5", true, 1.0, 5.0},
		{"Sonnet 4.6", "claude-sonnet-4-6", true, 3.0, 15.0},
		{"Sonnet 4.5 dated", "claude-sonnet-4-5-20250929", true, 3.0, 15.0},
		{"Opus 4.7", "claude-opus-4-7", true, 5.0, 25.0},
		{"Opus 4.6", "claude-opus-4-6", true, 5.0, 25.0},
		{"Unknown model falls back to Sonnet tier", "claude-imaginary-9-0", false, 3.0, 15.0},
		{"Empty model falls back", "", false, 3.0, 15.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := ResolvePricing(tt.model)
			if ok != tt.wantKnown {
				t.Errorf("ResolvePricing(%q) known = %v, want %v", tt.model, ok, tt.wantKnown)
			}
			if p.Input != tt.wantInput || p.Output != tt.wantOutput {
				t.Errorf("ResolvePricing(%q) = {Input: %.2f, Output: %.2f}, want {Input: %.2f, Output: %.2f}",
					tt.model, p.Input, p.Output, tt.wantInput, tt.wantOutput)
			}
		})
	}
}

// TestModelPricing_Cost exercises the Cost formula against each supported
// model family. Verifying 1M-token round numbers catches any formula
// regression without depending on floating-point tolerance.
func TestModelPricing_Cost(t *testing.T) {
	tests := []struct {
		name             string
		model            string
		cacheWriteTokens int
		cacheReadTokens  int
		wantCost         float64 // for 1M input + 1M output + cache amounts below
	}{
		{"Haiku 4.5 no cache", "claude-haiku-4-5", 0, 0, 6.0},                                       // 1 + 5
		{"Sonnet 4.6 no cache", "claude-sonnet-4-6", 0, 0, 18.0},                                    // 3 + 15
		{"Opus 4.7 no cache", "claude-opus-4-7", 0, 0, 30.0},                                        // 5 + 25
		{"Haiku 4.5 with cache", "claude-haiku-4-5", 1_000_000, 1_000_000, 7_350_000 / 1_000_000.0}, // 6 + 1.25 + 0.10
	}

	const in, out = 1_000_000, 1_000_000
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := ResolvePricing(tt.model)
			got := p.Cost(in, out, tt.cacheWriteTokens, tt.cacheReadTokens)
			const tolerance = 0.0001
			if got < tt.wantCost-tolerance || got > tt.wantCost+tolerance {
				t.Errorf("%s: Cost(%d, %d, %d, %d) = %.4f, want %.4f",
					tt.model, in, out, tt.cacheWriteTokens, tt.cacheReadTokens, got, tt.wantCost)
			}
		})
	}
}

// TestModelPricing_Cost_HaikuVsSonnet pins the Haiku-vs-Sonnet ratio, which
// is the main reason for making pricing model-aware in the first place.
// If someone accidentally wires Haiku through Sonnet rates, cost is 3× wrong.
func TestModelPricing_Cost_HaikuVsSonnet(t *testing.T) {
	haiku, _ := ResolvePricing("claude-haiku-4-5-20251001")
	sonnet, _ := ResolvePricing("claude-sonnet-4-6")

	const in, out = 1_000_000, 1_000_000
	haikuCost := haiku.Cost(in, out, 0, 0)
	sonnetCost := sonnet.Cost(in, out, 0, 0)

	if haikuCost != 6.0 {
		t.Errorf("Haiku Cost(1M,1M) = %.2f, want 6.00", haikuCost)
	}
	if sonnetCost != 18.0 {
		t.Errorf("Sonnet Cost(1M,1M) = %.2f, want 18.00", sonnetCost)
	}
	if ratio := sonnetCost / haikuCost; ratio < 2.99 || ratio > 3.01 {
		t.Errorf("Sonnet/Haiku cost ratio = %.2f, want ~3.00", ratio)
	}
}
