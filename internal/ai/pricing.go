// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import "strings"

// ModelPricing defines per-model pricing in USD per million tokens.
// Values match Anthropic's published rate card (5-minute cache write tier).
type ModelPricing struct {
	Input      float64
	Output     float64
	CacheWrite float64 // 5-minute cache write
	CacheRead  float64
}

// modelPricingTable maps model family prefixes to pricing. Dated model IDs
// (e.g. "claude-haiku-4-5-20251001") resolve via longest-prefix match, so
// we don't need a new entry every time Anthropic publishes a dated snapshot.
var modelPricingTable = map[string]ModelPricing{
	"claude-fable-5":    {Input: 10.0, Output: 50.0, CacheWrite: 12.50, CacheRead: 1.00},
	"claude-opus-5":     {Input: 5.0, Output: 25.0, CacheWrite: 6.25, CacheRead: 0.50},
	"claude-opus-4-8":   {Input: 5.0, Output: 25.0, CacheWrite: 6.25, CacheRead: 0.50},
	"claude-opus-4-7":   {Input: 5.0, Output: 25.0, CacheWrite: 6.25, CacheRead: 0.50},
	"claude-opus-4-6":   {Input: 5.0, Output: 25.0, CacheWrite: 6.25, CacheRead: 0.50},
	"claude-sonnet-5":   {Input: 2.0, Output: 10.0, CacheWrite: 2.50, CacheRead: 0.20},
	"claude-sonnet-4-6": {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheRead: 0.30},
	"claude-sonnet-4-5": {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheRead: 0.30},
	"claude-haiku-4-5":  {Input: 1.0, Output: 5.0, CacheWrite: 1.25, CacheRead: 0.10},
}

// fallbackPricing is used for unknown models. Sonnet-tier rates avoid
// silently reporting $0, which would hide cost in the database entirely.
// Note the fallback is only approximate in either direction: it over-reports
// for cheaper models (Haiku) and under-reports for any unlisted Opus- or
// Fable-tier model. Add new families to modelPricingTable rather than relying
// on it -- ResolvePricing returns ok=false so a miss is logged.
var fallbackPricing = ModelPricing{Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheRead: 0.30}

// ResolvePricing returns pricing for a model ID plus a boolean indicating
// whether the lookup hit an entry in modelPricingTable. Callers should log
// a warning once when ok is false so unexpected cost values are traceable.
func ResolvePricing(model string) (ModelPricing, bool) {
	var bestKey string
	for key := range modelPricingTable {
		if strings.HasPrefix(model, key) && len(key) > len(bestKey) {
			bestKey = key
		}
	}
	if bestKey == "" {
		return fallbackPricing, false
	}
	return modelPricingTable[bestKey], true
}

// Cost computes total USD cost for a request given token counts.
// Counts are clamped to >= 0: a buggy or misbehaving endpoint could report
// negative usage, which must never produce a negative cost_usd in the
// database.
func (p ModelPricing) Cost(inputTokens, outputTokens, cacheWriteTokens, cacheReadTokens int) float64 {
	inputTokens = max(inputTokens, 0)
	outputTokens = max(outputTokens, 0)
	cacheWriteTokens = max(cacheWriteTokens, 0)
	cacheReadTokens = max(cacheReadTokens, 0)

	const perMillion = 1_000_000.0
	return float64(inputTokens)/perMillion*p.Input +
		float64(outputTokens)/perMillion*p.Output +
		float64(cacheWriteTokens)/perMillion*p.CacheWrite +
		float64(cacheReadTokens)/perMillion*p.CacheRead
}
