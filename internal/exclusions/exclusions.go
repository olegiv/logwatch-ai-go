// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

// Package exclusions loads operator-defined finding exclusion patterns and
// applies them to an ai.Analysis, removing findings whose text contains any
// configured pattern as a case-insensitive substring.
//
// The matching is intentionally plain substring (strings.Contains on
// lowercased text): regex metacharacters are inert, so the configuration
// file is not a ReDoS vector. A future "add regex" change must be a
// conscious decision that re-evaluates that trade-off.
package exclusions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/olegiv/logwatch-ai-go/internal/ai"
)

// maxConfigFileSize caps the size of exclusions.json read from disk to
// prevent pathological files from consuming unbounded memory during parse.
// The file is operator-authored and should be tiny; 1 MiB is far beyond any
// realistic use.
const maxConfigFileSize = 1 << 20 // 1 MiB

// supportedVersion is the only exclusions.json schema version this build
// understands. Introducing a new version bumps this and adds a migration
// path, so that a v2 file is not silently processed with v1 semantics.
const supportedVersion = "1.0"

// Config represents the parsed exclusions.json file.
//
// Global patterns apply to every analysis. Per-site patterns apply only
// when the analyzer runs against the corresponding Drupal site ID. The
// siteID is empty for logwatch runs, so those only use Global.
type Config struct {
	Version string              `json:"version"`
	Global  []string            `json:"global"`
	Sites   map[string][]string `json:"sites"`
}

// FilterStats reports how many findings were removed per category.
type FilterStats struct {
	CriticalExcluded        int
	WarningsExcluded        int
	RecommendationsExcluded int
}

// Total returns the sum of all excluded-finding counts.
func (s FilterStats) Total() int {
	return s.CriticalExcluded + s.WarningsExcluded + s.RecommendationsExcluded
}

// Validate checks the configuration for structural errors. It is called
// after Load parses the file, and may also be called on hand-constructed
// Config values in tests.
func (c *Config) Validate() error {
	version := strings.TrimSpace(c.Version)
	if version == "" {
		return fmt.Errorf("version is required")
	}
	if version != supportedVersion {
		return fmt.Errorf("unsupported version %q: this build only supports %q", c.Version, supportedVersion)
	}

	seen := make(map[string]struct{}, len(c.Global))
	for i, p := range c.Global {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("global[%d]: pattern is blank", i)
		}
		key := strings.ToLower(strings.TrimSpace(p))
		if _, dup := seen[key]; dup {
			return fmt.Errorf("global[%d]: duplicate pattern %q", i, p)
		}
		seen[key] = struct{}{}
	}

	for siteID, patterns := range c.Sites {
		if strings.TrimSpace(siteID) == "" {
			return fmt.Errorf("sites: empty site ID")
		}
		siteSeen := make(map[string]struct{}, len(patterns))
		for i, p := range patterns {
			if strings.TrimSpace(p) == "" {
				return fmt.Errorf("sites[%q][%d]: pattern is blank", siteID, i)
			}
			key := strings.ToLower(strings.TrimSpace(p))
			if _, dup := siteSeen[key]; dup {
				return fmt.Errorf("sites[%q][%d]: duplicate pattern %q", siteID, i, p)
			}
			siteSeen[key] = struct{}{}
		}
	}

	return nil
}

// ListSites returns the site IDs that have at least one pattern, sorted.
// Used by the config layer to warn when a site ID in exclusions.json does
// not appear in drupal-sites.json.
func (c *Config) ListSites() []string {
	sites := make([]string, 0, len(c.Sites))
	for id := range c.Sites {
		sites = append(sites, id)
	}
	sort.Strings(sites)
	return sites
}

// Filter removes findings from a that match any effective exclusion
// pattern and returns stats about what was removed.
//
// The effective pattern list is Global ++ Sites[siteID]. An empty siteID
// (logwatch runs) or an unknown siteID falls back to Global only. Each
// category slice is shortened in place via append(dst[:0], ...), reusing
// the backing array; nil slices stay nil. Input patterns are not mutated.
func (c *Config) Filter(a *ai.Analysis, siteID string) FilterStats {
	if a == nil || c == nil {
		return FilterStats{}
	}

	lowered := c.loweredPatterns(siteID)
	if len(lowered) == 0 {
		return FilterStats{}
	}

	var stats FilterStats
	a.CriticalIssues, stats.CriticalExcluded = filterSlice(a.CriticalIssues, lowered)
	a.Warnings, stats.WarningsExcluded = filterSlice(a.Warnings, lowered)
	a.Recommendations, stats.RecommendationsExcluded = filterSlice(a.Recommendations, lowered)
	return stats
}

// loweredPatterns builds the lowercase pattern list for a given siteID.
// It allocates once per Filter call rather than inside the inner loop.
func (c *Config) loweredPatterns(siteID string) []string {
	total := len(c.Global)
	var sitePatterns []string
	if siteID != "" {
		sitePatterns = c.Sites[siteID]
		total += len(sitePatterns)
	}
	if total == 0 {
		return nil
	}
	out := make([]string, 0, total)
	for _, p := range c.Global {
		out = appendLoweredTrimmed(out, p)
	}
	for _, p := range sitePatterns {
		out = appendLoweredTrimmed(out, p)
	}
	return out
}

func appendLoweredTrimmed(dst []string, p string) []string {
	p = strings.TrimSpace(p)
	if p == "" {
		return dst
	}
	return append(dst, strings.ToLower(p))
}

// filterSlice removes entries from in whose lowercased form contains any
// pattern in loweredPatterns. It reuses in's backing array and returns
// the shortened slice plus the number of removed entries.
func filterSlice(in []string, loweredPatterns []string) ([]string, int) {
	if len(in) == 0 {
		return in, 0
	}
	out := in[:0]
	removed := 0
	for _, item := range in {
		if matchesAny(item, loweredPatterns) {
			removed++
			continue
		}
		out = append(out, item)
	}
	// Zero out the tail so removed strings are eligible for GC.
	for i := len(out); i < len(in); i++ {
		in[i] = ""
	}
	return out, removed
}

func matchesAny(item string, loweredPatterns []string) bool {
	lowered := strings.ToLower(item)
	for _, p := range loweredPatterns {
		if strings.Contains(lowered, p) {
			return true
		}
	}
	return false
}

// Load reads and parses exclusions.json.
//
// If explicitPath is non-empty, only that path is tried and a missing file
// is an error. Otherwise the standard search paths are tried in priority
// order; if none exist, Load returns (nil, "", nil) so the caller can
// treat the feature as optional.
//
// File size is capped at maxConfigFileSize to guard against memory abuse
// by a malicious or corrupted config.
func Load(explicitPath string) (*Config, string, error) {
	searchPaths := buildSearchPaths(explicitPath)

	for _, path := range searchPaths {
		if path == "" {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", fmt.Errorf("failed to stat %s: %w", path, err)
		}
		if info.Size() > maxConfigFileSize {
			return nil, "", fmt.Errorf("exclusions config %s too large: %d bytes (max %d)", path, info.Size(), maxConfigFileSize)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read %s: %w", path, err)
		}

		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, "", fmt.Errorf("failed to parse %s: %w", path, err)
		}

		if err := cfg.Validate(); err != nil {
			return nil, "", fmt.Errorf("invalid exclusions config in %s: %w", path, err)
		}

		return &cfg, path, nil
	}

	if explicitPath != "" {
		return nil, "", fmt.Errorf("exclusions config not found: %s", explicitPath)
	}

	return nil, "", nil
}

func buildSearchPaths(explicitPath string) []string {
	if explicitPath != "" {
		return []string{explicitPath}
	}
	paths := []string{
		"./exclusions.json",
		"./configs/exclusions.json",
		"/opt/logwatch-ai/exclusions.json",
	}
	if home := os.Getenv("HOME"); home != "" {
		paths = append(paths, filepath.Join(home, ".config", "logwatch-ai", "exclusions.json"))
	}
	return paths
}
