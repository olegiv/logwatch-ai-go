// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package exclusions

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olegiv/logwatch-ai-go/internal/ai"
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

func TestConfig_Filter(t *testing.T) {
	baseCfg := &Config{
		Version: "1.0",
		Global:  []string{"TLS certificate validation failures", "Deprecated PHP warning"},
		Sites: map[string][]string{
			"production": {"cron run exceeded the time limit"},
			"staging":    {"Email delivery delayed"},
		},
	}

	tests := []struct {
		name                 string
		cfg                  *Config
		siteID               string
		in                   ai.Analysis
		wantCritical         []string
		wantWarnings         []string
		wantRecommendations  []string
		wantCriticalDropped  int
		wantWarningsDropped  int
		wantRecommendDropped int
	}{
		{
			name:   "case-insensitive substring match across all categories",
			cfg:    baseCfg,
			siteID: "",
			in: ai.Analysis{
				CriticalIssues:  []string{"tls CERTIFICATE validation failures on host alpha", "SSH brute force"},
				Warnings:        []string{"Some Deprecated PHP Warning appeared", "Disk almost full"},
				Recommendations: []string{"Rotate TLS certificate validation failures report", "Keep watching"},
			},
			wantCritical:         []string{"SSH brute force"},
			wantWarnings:         []string{"Disk almost full"},
			wantRecommendations:  []string{"Keep watching"},
			wantCriticalDropped:  1,
			wantWarningsDropped:  1,
			wantRecommendDropped: 1,
		},
		{
			name:   "no match passthrough",
			cfg:    baseCfg,
			siteID: "production",
			in: ai.Analysis{
				CriticalIssues: []string{"Kernel panic"},
				Warnings:       []string{"High load average"},
			},
			wantCritical: []string{"Kernel panic"},
			wantWarnings: []string{"High load average"},
		},
		{
			name:   "per-site pattern combines with global",
			cfg:    baseCfg,
			siteID: "production",
			in: ai.Analysis{
				CriticalIssues: []string{
					"Cron run exceeded the time limit on node_1",
					"TLS certificate validation failures",
					"Real issue",
				},
			},
			wantCritical:        []string{"Real issue"},
			wantCriticalDropped: 2,
		},
		{
			name:   "unknown siteID falls back to global only",
			cfg:    baseCfg,
			siteID: "nonexistent",
			in: ai.Analysis{
				CriticalIssues: []string{
					"cron run exceeded the time limit",
					"TLS certificate validation failures",
				},
			},
			wantCritical:        []string{"cron run exceeded the time limit"},
			wantCriticalDropped: 1,
		},
		{
			name:   "empty siteID uses global only (logwatch case)",
			cfg:    baseCfg,
			siteID: "",
			in: ai.Analysis{
				CriticalIssues: []string{
					"Email delivery delayed notice",
					"Deprecated PHP warning again",
				},
			},
			wantCritical:        []string{"Email delivery delayed notice"},
			wantCriticalDropped: 1,
		},
		{
			name:   "empty pattern list is a no-op",
			cfg:    &Config{Version: "1.0"},
			siteID: "production",
			in: ai.Analysis{
				CriticalIssues: []string{"anything"},
			},
			wantCritical: []string{"anything"},
		},
		{
			name:   "all findings removed yields empty (non-nil) slices",
			cfg:    baseCfg,
			siteID: "",
			in: ai.Analysis{
				CriticalIssues:  []string{"TLS certificate validation failures"},
				Warnings:        []string{"Deprecated PHP warning"},
				Recommendations: []string{},
			},
			wantCritical:         []string{},
			wantWarnings:         []string{},
			wantRecommendations:  []string{},
			wantCriticalDropped:  1,
			wantWarningsDropped:  1,
			wantRecommendDropped: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := tt.in // copy
			stats := tt.cfg.Filter(&a, tt.siteID)

			if !equalStringSlice(a.CriticalIssues, tt.wantCritical) {
				t.Errorf("CriticalIssues = %#v, want %#v", a.CriticalIssues, tt.wantCritical)
			}
			if tt.wantWarnings != nil && !equalStringSlice(a.Warnings, tt.wantWarnings) {
				t.Errorf("Warnings = %#v, want %#v", a.Warnings, tt.wantWarnings)
			}
			if tt.wantRecommendations != nil && !equalStringSlice(a.Recommendations, tt.wantRecommendations) {
				t.Errorf("Recommendations = %#v, want %#v", a.Recommendations, tt.wantRecommendations)
			}
			if stats.CriticalExcluded != tt.wantCriticalDropped {
				t.Errorf("CriticalExcluded = %d, want %d", stats.CriticalExcluded, tt.wantCriticalDropped)
			}
			if stats.WarningsExcluded != tt.wantWarningsDropped {
				t.Errorf("WarningsExcluded = %d, want %d", stats.WarningsExcluded, tt.wantWarningsDropped)
			}
			if stats.RecommendationsExcluded != tt.wantRecommendDropped {
				t.Errorf("RecommendationsExcluded = %d, want %d", stats.RecommendationsExcluded, tt.wantRecommendDropped)
			}
			if got, want := stats.Total(), tt.wantCriticalDropped+tt.wantWarningsDropped+tt.wantRecommendDropped; got != want {
				t.Errorf("Total() = %d, want %d", got, want)
			}
		})
	}
}

func TestConfig_Filter_NilSafety(t *testing.T) {
	var cfg *Config
	got := cfg.Filter(&ai.Analysis{CriticalIssues: []string{"x"}}, "")
	if got.Total() != 0 {
		t.Errorf("nil Config Filter should be no-op, got %+v", got)
	}

	cfg = &Config{Version: "1.0", Global: []string{"x"}}
	if stats := cfg.Filter(nil, ""); stats.Total() != 0 {
		t.Errorf("nil Analysis Filter should be no-op, got %+v", stats)
	}
}

func TestConfig_Filter_DoesNotMutatePatterns(t *testing.T) {
	globalPatterns := []string{"TLS"}
	sitePatterns := []string{"cron limit"}
	cfg := &Config{
		Version: "1.0",
		Global:  globalPatterns,
		Sites:   map[string][]string{"prod": sitePatterns},
	}

	a := &ai.Analysis{CriticalIssues: []string{"TLS cert", "cron limit reached"}}
	cfg.Filter(a, "prod")

	if globalPatterns[0] != "TLS" {
		t.Errorf("Global pattern mutated: %q", globalPatterns[0])
	}
	if sitePatterns[0] != "cron limit" {
		t.Errorf("Site pattern mutated: %q", sitePatterns[0])
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

func TestLoad_ValidFromTestdata(t *testing.T) {
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
