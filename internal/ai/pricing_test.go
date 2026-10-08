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
		{"Haiku 5.5", "claude-haiku-5-5", true, 0.10, 0.50},
		{"Sonnet 4.6", "claude-sonnet-4-6", true, 3.0, 15.0},
		{"Sonnet 4.5 dated", "claude-sonnet-4-5-20250929", true, 3.0, 15.0},
		{"Opus 4.7", "claude-opus-4-7", true, 5.0, 25.0},
		{"Opus 4.6", "claude-opus-4-6", true, 5.0, 25.0},
		{"Fable 5", "claude-fable-5", true, 10.0, 50.0},
		{"Opus 5", "claude-opus-5", true, 5.0, 25.0},
		{"Opus 5 dated", "claude-opus-5-20260101", true, 5.0, 25.0},
		{"Opus 4.8", "claude-opus-4-8", true, 5.0, 25.0},
		{"Sonnet 5", "claude-sonnet-5", true, 2.0, 10.0},
		// Sonnet 5 must not be captured by the older Sonnet prefixes, which
		// would silently over-report it at 3/15 instead of 2/10.
		{"Sonnet 5 dated", "claude-sonnet-5-20260101", true, 2.0, 10.0},
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

// Haiku 5.5 applies one rate tier to the entire request, selected by total
// prompt tokens including cache writes and reads, but excluding output.
func TestModelPricing_Cost_Haiku55(t *testing.T) {
	p, _ := ResolvePricing("claude-haiku-5-5")
	tests := []struct {
		name                                 string
		input, output, cacheWrite, cacheRead int
		want                                 float64
	}{
		{"below threshold", 99_999, 1000, 0, 0, 0.0104999},
		{"at threshold", 100_000, 1000, 0, 0, 0.0105},
		{"above threshold", 100_001, 1000, 0, 0, 0.0525005},
		{"output does not select tier", 1000, 100_000, 0, 0, 0.0501},
		{"cached prompt at threshold", 1000, 1000, 49_000, 50_000, 0.007225},
		{"cache write crosses threshold", 1000, 1000, 49_001, 50_000, 0.036125625},
		{"cache read crosses threshold", 1000, 1000, 49_000, 50_001, 0.03612505},
		{"negative counts clamp before tier selection", -1000, 1000, 100_001, -1000, 0.065000625},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.Cost(tt.input, tt.output, tt.cacheWrite, tt.cacheRead)
			if delta := got - tt.want; delta < -1e-10 || delta > 1e-10 {
				t.Errorf("Cost() = %.12f, want %.12f", got, tt.want)
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
		inputTokens      int
		outputTokens     int
		cacheWriteTokens int
		cacheReadTokens  int
		wantCost         float64
	}{
		{"Haiku 4.5 no cache", "claude-haiku-4-5", 1_000_000, 1_000_000, 0, 0, 6.0},                                       // 1 + 5
		{"Sonnet 4.6 no cache", "claude-sonnet-4-6", 1_000_000, 1_000_000, 0, 0, 18.0},                                    // 3 + 15
		{"Opus 4.7 no cache", "claude-opus-4-7", 1_000_000, 1_000_000, 0, 0, 30.0},                                        // 5 + 25
		{"Opus 5 no cache", "claude-opus-5", 1_000_000, 1_000_000, 0, 0, 30.0},                                            // 5 + 25
		{"Sonnet 5 no cache", "claude-sonnet-5", 1_000_000, 1_000_000, 0, 0, 12.0},                                        // 2 + 10
		{"Fable 5 no cache", "claude-fable-5", 1_000_000, 1_000_000, 0, 0, 60.0},                                          // 10 + 50
		{"Fable 5 with cache", "claude-fable-5", 1_000_000, 1_000_000, 1_000_000, 1_000_000, 73.5},                        // 60 + 12.50 + 1.00
		{"Haiku 4.5 with cache", "claude-haiku-4-5", 1_000_000, 1_000_000, 1_000_000, 1_000_000, 7_350_000 / 1_000_000.0}, // 6 + 1.25 + 0.10
		// Negative provider-supplied counts are clamped to zero.
		{"all negative counts clamp to zero", "claude-sonnet-4-6", -1_000_000, -2_000_000, -1, -1, 0.0},
		{"negative input clamps, output still costed", "claude-sonnet-4-6", -500_000, 1_000_000, 0, 0, 15.0},
		{"negative cache counts clamp", "claude-haiku-4-5", 1_000_000, 1_000_000, -1_000_000, -42, 6.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := ResolvePricing(tt.model)
			got := p.Cost(tt.inputTokens, tt.outputTokens, tt.cacheWriteTokens, tt.cacheReadTokens)
			const tolerance = 0.0001
			if got < tt.wantCost-tolerance || got > tt.wantCost+tolerance {
				t.Errorf("%s: Cost(%d, %d, %d, %d) = %.4f, want %.4f",
					tt.model, tt.inputTokens, tt.outputTokens, tt.cacheWriteTokens, tt.cacheReadTokens, got, tt.wantCost)
			}
			if got < 0 {
				t.Errorf("Cost() = %.4f, must never be negative", got)
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
