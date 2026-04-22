// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

// Package exclusions loads operator-defined finding exclusion patterns and
// makes them available for injection into the LLM prompt.
//
// Exclusions are NOT applied as a post-filter on the model output. Instead,
// patterns are rendered into the system prompt (for `global`) and the user
// prompt (for source-wide and per-site scopes) with an explicit instruction
// telling the LLM to ignore matching findings AND to ignore their influence
// on `systemStatus`, `summary`, and `metrics`. This keeps the stored
// summary, the message sent to Telegram, and the KPIs coherent with what
// the operator considers actionable.
//
// The matching is nominally case-insensitive plain substring (the LLM is
// instructed to treat regex metacharacters as literal text), so the
// configuration file is not a ReDoS vector. Patterns are run through
// `ai.NormalizePromptContent` before injection so they normalize the same
// way the LLM-facing log content does (NFKC, zero-width / bidi strip,
// non-printable drop) — this is required so operator patterns still
// substring-match the normalized finding text. The LLM-content-only
// `ai.SanitizeLogContent` (which rewrites phrases like "ignore previous
// instructions" to `[FILTERED]`) is deliberately NOT applied here, because
// operator patterns must match real log lines that can legitimately
// contain such tokens. The containment boundary for exclusion patterns is
// the rendered bullet-list framing plus the "MUST NOT / treat as absent"
// instruction, not text rewriting.
//
// Because the exclusions now reach the LLM as plain text, the operator
// MUST NOT place secrets (API keys, passwords, PII) into patterns.
package exclusions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/olegiv/logwatch-ai-go/internal/ai"
	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

// maxConfigFileSize caps the size of exclusions.json read from disk to
// prevent pathological files from consuming unbounded memory during parse.
// The file is operator-authored and should be tiny; 1 MiB is far beyond any
// realistic use.
const maxConfigFileSize = 1 << 20 // 1 MiB

// supportedVersions lists the exclusions.json schema versions this build
// understands. "1.0" is accepted for backward compatibility; "1.1" adds the
// optional `logwatch` and `drupal` scope lists.
var supportedVersions = []string{"1.0", "1.1"}

// maxPatternsPerList caps the number of patterns allowed in any single list
// (global, logwatch, drupal, or a single sites entry). Set to a value that
// comfortably covers real operator use while preventing a misconfigured file
// from inflating every prompt by thousands of tokens.
const maxPatternsPerList = 50

// maxPatternRunes caps the rune length of a single pattern after
// sanitization. Longer patterns are truncated with an ellipsis suffix.
const maxPatternRunes = 200

// Config represents the parsed exclusions.json file.
//
// Global patterns apply to every analysis and are injected into the system
// prompt (stable, cache-friendly for Anthropic). Logwatch, Drupal, and
// Sites[id] patterns are injected into the user prompt (per-run variable).
//
// Resolution:
//   - logwatch runs: global (system) + logwatch (user)
//   - drupal runs:   global (system) + drupal + sites[siteID] (both user)
type Config struct {
	Version  string              `json:"version"`
	Global   []string            `json:"global,omitempty"`
	Logwatch []string            `json:"logwatch,omitempty"`
	Drupal   []string            `json:"drupal,omitempty"`
	Sites    map[string][]string `json:"sites,omitempty"`
}

// Validate checks the configuration for structural errors. It is called
// after Load parses the file, and may also be called on hand-constructed
// Config values in tests.
func (c *Config) Validate() error {
	version := strings.TrimSpace(c.Version)
	if version == "" {
		return fmt.Errorf("version is required")
	}
	if !isSupportedVersion(version) {
		return fmt.Errorf("unsupported version %q: this build supports %v", c.Version, supportedVersions)
	}

	if err := validatePatternList("global", c.Global); err != nil {
		return err
	}
	if err := validatePatternList("logwatch", c.Logwatch); err != nil {
		return err
	}
	if err := validatePatternList("drupal", c.Drupal); err != nil {
		return err
	}

	for siteID, patterns := range c.Sites {
		if strings.TrimSpace(siteID) == "" {
			return fmt.Errorf("sites: empty site ID")
		}
		if err := validatePatternList(fmt.Sprintf("sites[%q]", siteID), patterns); err != nil {
			return err
		}
	}

	return nil
}

// validatePatternList enforces the common blank/duplicate/overflow rules on
// a single list. Callers provide a label used in error messages so the
// operator can locate the offending entry.
func validatePatternList(name string, patterns []string) error {
	if len(patterns) > maxPatternsPerList {
		return fmt.Errorf("%s: too many patterns (%d); maximum allowed is %d", name, len(patterns), maxPatternsPerList)
	}
	seen := make(map[string]struct{}, len(patterns))
	for i, p := range patterns {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("%s[%d]: pattern is blank", name, i)
		}
		key := strings.ToLower(strings.TrimSpace(p))
		if _, dup := seen[key]; dup {
			return fmt.Errorf("%s[%d]: duplicate pattern %q", name, i, p)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func isSupportedVersion(v string) bool {
	return slices.Contains(supportedVersions, v)
}

// ListSites returns the site IDs that have at least one pattern, sorted.
// Used by the config layer to warn when a site ID in exclusions.json does
// not appear in drupal-sites.json.
func (c *Config) ListSites() []string {
	if c == nil {
		return nil
	}
	sites := make([]string, 0, len(c.Sites))
	for id := range c.Sites {
		sites = append(sites, id)
	}
	sort.Strings(sites)
	return sites
}

// GlobalPatterns returns sanitized global patterns ready for injection into
// the system prompt. Empty/whitespace-only inputs are dropped; long
// patterns are truncated. The result is safe to embed verbatim in a
// markdown bullet list.
func (c *Config) GlobalPatterns() []string {
	if c == nil {
		return nil
	}
	return sanitizePatternsForPrompt(c.Global)
}

// ContextualPatterns returns sanitized source-scoped and site-scoped
// patterns ready for injection into the user prompt. Resolution rules:
//
//   - logType == analyzer.LogSourceLogwatch:       c.Logwatch
//   - logType == analyzer.LogSourceDrupalWatchdog: c.Drupal + c.Sites[siteID]
//
// An empty or unknown siteID for drupal_watchdog returns just c.Drupal.
// Other logTypes return nil (defensive).
//
// Each source list is sanitized independently before merging so that the
// per-list maxPatternsPerList cap applies per scope rather than to the
// merged slice — otherwise a full c.Drupal list (50) would silently drop
// every c.Sites[siteID] pattern.
func (c *Config) ContextualPatterns(logType analyzer.LogSourceType, siteID string) []string {
	if c == nil {
		return nil
	}

	switch logType {
	case analyzer.LogSourceLogwatch:
		return sanitizePatternsForPrompt(c.Logwatch)
	case analyzer.LogSourceDrupalWatchdog:
		out := sanitizePatternsForPrompt(c.Drupal)
		if siteID != "" {
			out = append(out, sanitizePatternsForPrompt(c.Sites[siteID])...)
		}
		return out
	default:
		return nil
	}
}

// sanitizePatternsForPrompt maps sanitizePatternForPrompt across a slice,
// dropping empty results. The output is capped at maxPatternsPerList.
func sanitizePatternsForPrompt(patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if len(out) >= maxPatternsPerList {
			break
		}
		s := sanitizePatternForPrompt(p)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// truncationSuffix is appended to patterns that exceed maxPatternRunes.
// Three ASCII dots are stable under NFKC normalization (unlike U+2026 "…"
// which decomposes to "..."), so truncation length is predictable even
// after ai.NormalizePromptContent runs its NFKC pass.
const truncationSuffix = "..."

// sanitizePatternForPrompt defends against patterns that would otherwise
// break the prompt structure. Steps:
//
//  1. TrimSpace
//  2. Replace \r, \n, \t with a single space (prevents bullet-list breakout)
//  3. Drop Unicode control characters
//  4. Pass through ai.NormalizePromptContent (NFKC normalize, strip zero-
//     width/bidi chars, drop non-printables). This matches how the LLM-
//     facing log content is normalized so patterns still substring-match
//     against finding text.
//  5. TrimSpace again
//  6. Truncate to maxPatternRunes with truncationSuffix on overflow
//
// It deliberately does NOT call ai.SanitizeLogContent: that function replaces
// tokens like "USER:" / "SYSTEM:" and phrases like "ignore previous
// instructions" with "[FILTERED]", which is correct for untrusted log
// content but would rewrite operator-authored exclusion strings and break
// the intended substring match.
func sanitizePatternForPrompt(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(p))
	for _, r := range p {
		switch r {
		case '\r', '\n', '\t':
			b.WriteRune(' ')
		default:
			if unicode.IsControl(r) {
				continue
			}
			b.WriteRune(r)
		}
	}
	p = b.String()

	p = ai.NormalizePromptContent(p)
	p = strings.TrimSpace(p)

	runes := []rune(p)
	if len(runes) > maxPatternRunes {
		keep := max(maxPatternRunes-len([]rune(truncationSuffix)), 0)
		p = string(runes[:keep]) + truncationSuffix
	}
	return p
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
