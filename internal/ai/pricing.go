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
	"claude-haiku-4-5":  {Input: 1.0, Output: 5.0, CacheWrite: 1.25, CacheRead: 0.10},
	"claude-sonnet-4-6": {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheRead: 0.30},
	"claude-sonnet-4-5": {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheRead: 0.30},
	"claude-opus-4-7":   {Input: 5.0, Output: 25.0, CacheWrite: 6.25, CacheRead: 0.50},
	"claude-opus-4-6":   {Input: 5.0, Output: 25.0, CacheWrite: 6.25, CacheRead: 0.50},
}

// fallbackPricing is used for unknown models. Sonnet-tier rates are a safe
// default: they over-report for cheaper models (Haiku) rather than silently
// reporting $0, which would hide cost in the database.
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
func (p ModelPricing) Cost(inputTokens, outputTokens, cacheWriteTokens, cacheReadTokens int) float64 {
	const perMillion = 1_000_000.0
	return float64(inputTokens)/perMillion*p.Input +
		float64(outputTokens)/perMillion*p.Output +
		float64(cacheWriteTokens)/perMillion*p.CacheWrite +
		float64(cacheReadTokens)/perMillion*p.CacheRead
}
