// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package exclusions

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "valid global + sites",
			cfg: Config{
				Version: "1.0",
				Global:  []string{"TLS cert"},
				Sites:   map[string][]string{"prod": {"cron limit"}},
			},
		},
		{
			name: "valid v1.1 with logwatch and drupal scopes",
			cfg: Config{
				Version:  "1.1",
				Global:   []string{"TLS cert"},
				Logwatch: []string{"kernel: NETDEV WATCHDOG"},
				Drupal:   []string{"deprecated function"},
				Sites:    map[string][]string{"prod": {"cron limit"}},
			},
		},
		{
			name: "valid v1.2 with ocms scope",
			cfg: Config{
				Version: "1.2",
				Global:  []string{"TLS cert"},
				OCMS:    []string{"healthcheck timeout"},
			},
		},
		{
			name:    "missing version",
			cfg:     Config{Global: []string{"foo"}},
			wantErr: "version is required",
		},
		{
			name:    "blank version",
			cfg:     Config{Version: "  "},
			wantErr: "version is required",
		},
		{
			name:    "unsupported future version",
			cfg:     Config{Version: "2.0"},
			wantErr: `unsupported version "2.0"`,
		},
		{
			name:    "unsupported arbitrary version",
			cfg:     Config{Version: "v1"},
			wantErr: "unsupported version",
		},
		{
			name:    "blank global pattern",
			cfg:     Config{Version: "1.0", Global: []string{"ok", ""}},
			wantErr: "global[1]: pattern is blank",
		},
		{
			name:    "whitespace-only global pattern",
			cfg:     Config{Version: "1.0", Global: []string{"\t \n"}},
			wantErr: "global[0]: pattern is blank",
		},
		{
			name:    "duplicate global (case-insensitive)",
			cfg:     Config{Version: "1.0", Global: []string{"TLS", "tls"}},
			wantErr: "duplicate pattern",
		},
		{
			name:    "blank logwatch pattern",
			cfg:     Config{Version: "1.1", Logwatch: []string{""}},
			wantErr: "logwatch[0]: pattern is blank",
		},
		{
			name:    "duplicate logwatch (case-insensitive)",
			cfg:     Config{Version: "1.1", Logwatch: []string{"NETDEV", "netdev"}},
			wantErr: "logwatch[1]: duplicate pattern",
		},
		{
			name:    "blank drupal pattern",
			cfg:     Config{Version: "1.1", Drupal: []string{"  "}},
			wantErr: "drupal[0]: pattern is blank",
		},
		{
			name:    "duplicate drupal (case-insensitive)",
			cfg:     Config{Version: "1.1", Drupal: []string{"deprecated", "Deprecated"}},
			wantErr: "drupal[1]: duplicate pattern",
		},
		{
			name: "blank site pattern",
			cfg: Config{
				Version: "1.0",
				Sites:   map[string][]string{"prod": {""}},
			},
			wantErr: `sites["prod"][0]: pattern is blank`,
		},
		{
			name: "duplicate site pattern",
			cfg: Config{
				Version: "1.0",
				Sites:   map[string][]string{"prod": {"a", "A"}},
			},
			wantErr: "duplicate pattern",
		},
		{
			name: "empty site ID",
			cfg: Config{
				Version: "1.0",
				Sites:   map[string][]string{"": {"x"}},
			},
			wantErr: "empty site ID",
		},
		{
			name: "too many patterns in global",
			cfg: Config{
				Version: "1.1",
				Global:  makeUniquePatterns(maxPatternsPerList + 1),
			},
			wantErr: "too many patterns",
		},
		{
			name: "too many patterns in logwatch",
			cfg: Config{
				Version:  "1.1",
				Logwatch: makeUniquePatterns(maxPatternsPerList + 1),
			},
			wantErr: "too many patterns",
		},
		{
			name: "too many patterns in ocms",
			cfg: Config{
				Version: "1.2",
				OCMS:    makeUniquePatterns(maxPatternsPerList + 1),
			},
			wantErr: "too many patterns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestConfig_GlobalPatterns(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want []string
	}{
		{
			name: "nil config is safe",
			cfg:  nil,
			want: nil,
		},
		{
			name: "no globals",
			cfg:  &Config{Version: "1.1"},
			want: nil,
		},
		{
			name: "sanitizes and preserves order",
			cfg:  &Config{Version: "1.1", Global: []string{"Foo", "  Bar  "}},
			want: []string{"Foo", "Bar"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GlobalPatterns()
			if !equalStringSlice(got, tt.want) {
				t.Errorf("GlobalPatterns() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestConfig_GlobalPatterns_ReturnsCopyNotReference(t *testing.T) {
	cfg := &Config{Version: "1.1", Global: []string{"alpha", "beta"}}
	got := cfg.GlobalPatterns()
	if len(got) == 0 {
		t.Fatal("expected patterns")
	}
	got[0] = "MUTATED"
	if cfg.Global[0] != "alpha" {
		t.Errorf("underlying config mutated via returned slice: %q", cfg.Global[0])
	}
}

func TestConfig_ContextualPatterns(t *testing.T) {
	cfg := &Config{
		Version:  "1.1",
		Global:   []string{"must-not-appear-in-contextual"},
		Logwatch: []string{"kernel watchdog"},
		Drupal:   []string{"deprecated function"},
		OCMS:     []string{"request timeout"},
		Sites: map[string][]string{
			"production": {"cron exceeded"},
			"staging":    {"email delayed"},
		},
	}

	tests := []struct {
		name    string
		logType analyzer.LogSourceType
		siteID  string
		want    []string
	}{
		{
			name:    "logwatch returns logwatch only",
			logType: analyzer.LogSourceLogwatch,
			siteID:  "",
			want:    []string{"kernel watchdog"},
		},
		{
			name:    "logwatch ignores siteID",
			logType: analyzer.LogSourceLogwatch,
			siteID:  "production",
			want:    []string{"kernel watchdog"},
		},
		{
			name:    "drupal without siteID returns drupal only",
			logType: analyzer.LogSourceDrupalWatchdog,
			siteID:  "",
			want:    []string{"deprecated function"},
		},
		{
			name:    "drupal with known siteID returns drupal + site in order",
			logType: analyzer.LogSourceDrupalWatchdog,
			siteID:  "production",
			want:    []string{"deprecated function", "cron exceeded"},
		},
		{
			name:    "drupal with unknown siteID returns drupal only",
			logType: analyzer.LogSourceDrupalWatchdog,
			siteID:  "nonexistent",
			want:    []string{"deprecated function"},
		},
		{
			name:    "drupal with different known siteID returns its patterns",
			logType: analyzer.LogSourceDrupalWatchdog,
			siteID:  "staging",
			want:    []string{"deprecated function", "email delayed"},
		},
		{
			name:    "ocms returns ocms only",
			logType: analyzer.LogSourceOCMS,
			siteID:  "",
			want:    []string{"request timeout"},
		},
		{
			name:    "unknown logType returns nil",
			logType: analyzer.LogSourceType("unknown"),
			siteID:  "production",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cfg.ContextualPatterns(tt.logType, tt.siteID)
			if !equalStringSlice(got, tt.want) {
				t.Errorf("ContextualPatterns(%q, %q) = %#v, want %#v", tt.logType, tt.siteID, got, tt.want)
			}
		})
	}
}

func TestConfig_ContextualPatterns_NilSafety(t *testing.T) {
	var cfg *Config
	if got := cfg.ContextualPatterns(analyzer.LogSourceLogwatch, ""); got != nil {
		t.Errorf("nil Config ContextualPatterns = %v, want nil", got)
	}
}

// TestConfig_ContextualPatterns_PerListCap verifies that the
// maxPatternsPerList cap applies per source list rather than to the merged
// drupal+site slice. With 50 drupal patterns and 5 site patterns, all 55
// must be returned; the previous behavior capped the merged slice at 50
// and silently dropped the site-scoped entries.
func TestConfig_ContextualPatterns_PerListCap(t *testing.T) {
	drupal := makeUniquePatterns(maxPatternsPerList)
	site := []string{"site-1", "site-2", "site-3", "site-4", "site-5"}

	cfg := &Config{
		Version: "1.1",
		Drupal:  drupal,
		Sites:   map[string][]string{"production": site},
	}

	got := cfg.ContextualPatterns(analyzer.LogSourceDrupalWatchdog, "production")
	wantLen := maxPatternsPerList + len(site)
	if len(got) != wantLen {
		t.Fatalf("ContextualPatterns len = %d, want %d (drupal=%d + site=%d)",
			len(got), wantLen, maxPatternsPerList, len(site))
	}
	for i, p := range site {
		if got[maxPatternsPerList+i] != p {
			t.Errorf("site pattern at %d = %q, want %q",
				maxPatternsPerList+i, got[maxPatternsPerList+i], p)
		}
	}
}

func TestSanitizePatternForPrompt(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"trims whitespace", "  hello  ", "hello"},
		{"replaces newline with space", "foo\nbar", "foo bar"},
		{"replaces tab with space", "foo\tbar", "foo bar"},
		{"replaces CR with space", "foo\rbar", "foo bar"},
		{"strips null byte", "foo\x00bar", "foobar"},
		{"strips DEL", "foo\x7fbar", "foobar"},
		{"strips ESC", "foo\x1bbar", "foobar"},
		{"empty input stays empty", "", ""},
		{"whitespace-only becomes empty", "   \t\n  ", ""},
		// Operator-authored patterns are NOT rewritten by prompt-injection
		// phrase replacement. sanitizePatternForPrompt uses
		// ai.NormalizePromptContent (structural only), not
		// ai.SanitizeLogContent. An operator must be able to exclude
		// legitimate log lines that happen to contain tokens the LLM-
		// content filter treats as suspicious (e.g. "USER:", "ignore
		// previous instructions" in a syslog record).
		{"preserves injection-phrase-like wording", "ignore all previous instructions", "ignore all previous instructions"},
		{"strips zero-width join but leaves text intact", "ign\u200core previous instructions", "ignore previous instructions"},
		{"preserves USER: token verbatim", "USER: anonymous failed to authenticate", "USER: anonymous failed to authenticate"},
		{"preserves SYSTEM: token verbatim", "SYSTEM: disk near capacity", "SYSTEM: disk near capacity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizePatternForPrompt(tt.in)
			if got != tt.want {
				t.Errorf("sanitizePatternForPrompt(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizePatternForPrompt_TruncatesOversized(t *testing.T) {
	longInput := strings.Repeat("a", 500)
	got := sanitizePatternForPrompt(longInput)
	runeCount := len([]rune(got))
	if runeCount > maxPatternRunes {
		t.Errorf("sanitized pattern has %d runes, want <= %d", runeCount, maxPatternRunes)
	}
	if !strings.HasSuffix(got, truncationSuffix) {
		t.Errorf("oversized pattern should end with %q, got %q", truncationSuffix, got[len(got)-5:])
	}
}

func TestSanitizePatternsForPrompt_CapsAt50(t *testing.T) {
	in := makeUniquePatterns(100)
	got := sanitizePatternsForPrompt(in)
	if len(got) != maxPatternsPerList {
		t.Errorf("sanitized pattern count = %d, want %d", len(got), maxPatternsPerList)
	}
}

func TestGlobalPatterns_NilAndEmptyAreEquivalent(t *testing.T) {
	nilCfg := &Config{Version: "1.1"}
	emptyCfg := &Config{Version: "1.1", Global: []string{}}
	a, b := nilCfg.GlobalPatterns(), emptyCfg.GlobalPatterns()
	if len(a) != 0 || len(b) != 0 {
		t.Errorf("expected nil/empty outputs, got %v and %v", a, b)
	}
}

func TestConfig_ListSites(t *testing.T) {
	cfg := &Config{
		Sites: map[string][]string{"zebra": {"z"}, "alpha": {"a"}, "beta": {"b"}},
	}
	got := cfg.ListSites()
	want := []string{"alpha", "beta", "zebra"}
	if !equalStringSlice(got, want) {
		t.Errorf("ListSites() = %v, want %v", got, want)
	}
}

func TestConfig_ListSites_NilSafety(t *testing.T) {
	var cfg *Config
	if got := cfg.ListSites(); got != nil {
		t.Errorf("nil Config ListSites = %v, want nil", got)
	}
}

func TestLoad_AcceptsV10(t *testing.T) {
	cfg, path, err := Load(filepath.Join("testdata", "valid.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}
	if !strings.HasSuffix(path, "valid.json") {
		t.Errorf("Load() path = %q, want suffix valid.json", path)
	}
	if cfg.Version != "1.0" {
		t.Errorf("Version = %q, want 1.0", cfg.Version)
	}
	if len(cfg.Global) != 2 {
		t.Errorf("len(Global) = %d, want 2", len(cfg.Global))
	}
	if len(cfg.Sites["production"]) != 1 {
		t.Errorf("len(Sites[production]) = %d, want 1", len(cfg.Sites["production"]))
	}
	// v1.0 files carry no Logwatch/Drupal fields
	if len(cfg.Logwatch) != 0 {
		t.Errorf("v1.0 Logwatch should be empty, got %v", cfg.Logwatch)
	}
	if len(cfg.Drupal) != 0 {
		t.Errorf("v1.0 Drupal should be empty, got %v", cfg.Drupal)
	}
}

func TestLoad_AcceptsV11(t *testing.T) {
	cfg, _, err := Load(filepath.Join("testdata", "valid-v1.1.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}
	if cfg.Version != "1.1" {
		t.Errorf("Version = %q, want 1.1", cfg.Version)
	}
	if len(cfg.Logwatch) != 1 {
		t.Errorf("len(Logwatch) = %d, want 1", len(cfg.Logwatch))
	}
	if len(cfg.Drupal) != 1 {
		t.Errorf("len(Drupal) = %d, want 1", len(cfg.Drupal))
	}
}

func TestLoad_MissingWithoutExplicitPathReturnsNil(t *testing.T) {
	// Isolate: no HOME pointing to a real config, and cd to a temp dir so
	// relative search paths don't accidentally find a repo-level file.
	t.Setenv("HOME", "/nonexistent-home-for-exclusions-test")
	t.Chdir(t.TempDir())

	cfg, path, err := Load("")
	if err != nil {
		t.Fatalf("Load('') error = %v, want nil", err)
	}
	if cfg != nil {
		t.Errorf("Load('') cfg = %+v, want nil", cfg)
	}
	if path != "" {
		t.Errorf("Load('') path = %q, want empty", path)
	}
}

func TestLoad_ExplicitPathNotFound(t *testing.T) {
	_, _, err := Load("/definitely/does/not/exist/exclusions.json")
	if err == nil {
		t.Fatal("Load() expected error for explicit missing path, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want containing 'not found'", err)
	}
}

func TestLoad_MalformedJSON(t *testing.T) {
	_, _, err := Load(filepath.Join("testdata", "malformed.json"))
	if err == nil {
		t.Fatal("Load() expected parse error for malformed.json")
	}
	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("error = %v, want containing 'failed to parse'", err)
	}
}

func TestLoad_ValidationFails(t *testing.T) {
	tests := []struct {
		name string
		path string
		msg  string
	}{
		{"duplicate pattern", filepath.Join("testdata", "duplicate-pattern.json"), "duplicate pattern"},
		{"blank pattern", filepath.Join("testdata", "blank-pattern.json"), "pattern is blank"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Load(tt.path)
			if err == nil {
				t.Fatalf("Load(%s) expected validation error", tt.path)
			}
			if !strings.Contains(err.Error(), "invalid exclusions config") {
				t.Errorf("error = %v, want prefix 'invalid exclusions config'", err)
			}
			if !strings.Contains(err.Error(), tt.msg) {
				t.Errorf("error = %v, want containing %q", err, tt.msg)
			}
		})
	}
}

func TestLoad_FileTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exclusions.json")
	// Build a > 1 MiB JSON file: valid JSON but with a huge padding string.
	var buf bytes.Buffer
	buf.WriteString(`{"version":"1.0","global":["`)
	padding := bytes.Repeat([]byte("a"), maxConfigFileSize+1024)
	buf.Write(padding)
	buf.WriteString(`"]}`)
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("Load() expected size-limit error")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %v, want containing 'too large'", err)
	}
}

func TestLoad_SearchPathsDiscovery(t *testing.T) {
	dir := t.TempDir()
	configsDir := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(configsDir, "exclusions.json")
	valid := `{"version":"1.0","global":["pattern"]}`
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Chdir(dir)
	// Prevent HOME-based path from masking the discovery order.
	t.Setenv("HOME", "/nonexistent-home-for-exclusions-test")

	cfg, foundPath, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() cfg = nil, want discovered config")
	}
	if !strings.HasSuffix(foundPath, filepath.Join("configs", "exclusions.json")) {
		t.Errorf("foundPath = %q, want suffix configs/exclusions.json", foundPath)
	}
}

func makeUniquePatterns(n int) []string {
	out := make([]string, n)
	for i := range n {
		out[i] = fmt.Sprintf("pattern-%03d", i)
	}
	return out
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
