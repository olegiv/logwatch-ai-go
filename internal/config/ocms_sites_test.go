// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOCMSSitesRegistry(t *testing.T) {
	t.Parallel()

	registry, err := ParseOCMSSitesRegistry([]byte(`
# SITE_ID INSTANCE_DIR SYSTEM_USER PORT
example_com /var/www/vhosts/example.com/ocms example_com 8081
app_example_com /var/www/vhosts/example.com/ocms/app hosting 8082 # inline comment

blog_example_com /srv/ocms/blog bloguser 8083
`))
	if err != nil {
		t.Fatalf("ParseOCMSSitesRegistry() error = %v", err)
	}

	if got := registry.ListSites(); strings.Join(got, ",") != "app_example_com,blog_example_com,example_com" {
		t.Fatalf("ListSites() = %v", got)
	}

	site, err := registry.GetSite("app_example_com")
	if err != nil {
		t.Fatalf("GetSite() error = %v", err)
	}
	if site.InstanceDir != "/var/www/vhosts/example.com/ocms/app" {
		t.Fatalf("InstanceDir = %q", site.InstanceDir)
	}
	if site.SystemUser != "hosting" {
		t.Fatalf("SystemUser = %q", site.SystemUser)
	}
	if site.Port != 8082 {
		t.Fatalf("Port = %d", site.Port)
	}
}

func TestParseOCMSSitesRegistry_InvalidRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "invalid field count",
			content: "example_com /var/www/example example_com\n",
			want:    "expected 4 fields",
		},
		{
			name: "duplicate site",
			content: `example_com /var/www/example example_com 8081
example_com /var/www/other other 8082
`,
			want: "duplicate site ID",
		},
		{
			name:    "invalid port",
			content: "example_com /var/www/example example_com nope\n",
			want:    "port must be a number",
		},
		{
			name:    "relative instance dir",
			content: "example_com var/www/example example_com 8081\n",
			want:    "INSTANCE_DIR must be an absolute path",
		},
		{
			name:    "traversal instance dir",
			content: "example_com /var/www/example/../other example_com 8081\n",
			want:    "INSTANCE_DIR must not contain path traversal components",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseOCMSSitesRegistry([]byte(tt.content))
			if err == nil {
				t.Fatal("ParseOCMSSitesRegistry() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestNormalizeOCMSLogKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty defaults to main", input: "", want: OCMSLogKindMain},
		{name: "main", input: "main", want: OCMSLogKindMain},
		{name: "error", input: "error", want: OCMSLogKindError},
		{name: "all", input: "all", want: OCMSLogKindAll},
		{name: "uppercase all", input: "ALL", want: OCMSLogKindAll},
		{name: "invalid", input: "verbose", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeOCMSLogKind(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("NormalizeOCMSLogKind() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeOCMSLogKind() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeOCMSLogKind() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOCMSSite_LogPath(t *testing.T) {
	t.Parallel()

	site := OCMSSite{InstanceDir: "/var/www/vhosts/example.com/ocms"}

	mainLog, err := site.LogPath(OCMSLogKindMain)
	if err != nil {
		t.Fatalf("LogPath(main) error = %v", err)
	}
	if mainLog != "/var/www/vhosts/example.com/ocms/logs/ocms.log" {
		t.Fatalf("LogPath(main) = %q", mainLog)
	}

	errorLog, err := site.LogPath(OCMSLogKindError)
	if err != nil {
		t.Fatalf("LogPath(error) error = %v", err)
	}
	if errorLog != "/var/www/vhosts/example.com/ocms/logs/error.log" {
		t.Fatalf("LogPath(error) = %q", errorLog)
	}

	if _, err := site.LogPath(OCMSLogKindAll); err == nil {
		t.Fatal("LogPath(all) expected error, got nil")
	}
}

func TestOCMSSite_LogPaths(t *testing.T) {
	t.Parallel()

	site := OCMSSite{InstanceDir: "/var/www/vhosts/example.com/ocms"}

	paths, err := site.LogPaths(OCMSLogKindAll)
	if err != nil {
		t.Fatalf("LogPaths(all) error = %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("len(LogPaths(all)) = %d, want 2", len(paths))
	}
	if paths[0].Kind != OCMSLogKindMain || paths[0].Path != "/var/www/vhosts/example.com/ocms/logs/ocms.log" {
		t.Fatalf("main path = %+v", paths[0])
	}
	if paths[1].Kind != OCMSLogKindError || paths[1].Path != "/var/www/vhosts/example.com/ocms/logs/error.log" {
		t.Fatalf("error path = %+v", paths[1])
	}
}

func TestLoadOCMSSitesRegistry(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	registryPath := filepath.Join(tmpDir, "sites.conf")
	content := "example_com /var/www/vhosts/example.com/ocms example_com 8081\n"
	if err := os.WriteFile(registryPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	registry, foundPath, err := LoadOCMSSitesRegistry(registryPath)
	if err != nil {
		t.Fatalf("LoadOCMSSitesRegistry() error = %v", err)
	}
	if foundPath != registryPath {
		t.Fatalf("foundPath = %q, want %q", foundPath, registryPath)
	}
	if _, err := registry.GetSite("example_com"); err != nil {
		t.Fatalf("GetSite() error = %v", err)
	}
}

func TestLoadOCMSSitesRegistry_ExplicitPathNotFound(t *testing.T) {
	t.Parallel()

	_, _, err := LoadOCMSSitesRegistry("/nonexistent/path/sites.conf")
	if err == nil {
		t.Fatal("LoadOCMSSitesRegistry() expected error for missing explicit path")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want not found", err)
	}
}

func TestLoadOCMSSitesConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "ocms-sites.json")
	content := `{
  "version": "1.0",
  "default_site": "example_com",
  "registry_path": "/etc/ocms/sites.conf",
  "default_log_kind": "main",
  "sites": {
    "example_com": {
      "name": "Example Site"
    },
    "app_example_com": {
      "name": "Example App",
      "log_kind": "error"
    },
    "all_example_com": {
      "name": "All Example",
      "log_kind": "all"
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, foundPath, err := LoadOCMSSitesConfig(configPath)
	if err != nil {
		t.Fatalf("LoadOCMSSitesConfig() error = %v", err)
	}
	if config == nil {
		t.Fatal("LoadOCMSSitesConfig() returned nil config")
	}
	if foundPath != configPath {
		t.Fatalf("foundPath = %q, want %q", foundPath, configPath)
	}
	if config.DefaultSite != "example_com" {
		t.Fatalf("DefaultSite = %q", config.DefaultSite)
	}

	site, err := config.GetSite("")
	if err != nil {
		t.Fatalf("GetSite(default) error = %v", err)
	}
	if site.Name != "Example Site" {
		t.Fatalf("site.Name = %q", site.Name)
	}

	logKind, err := config.EffectiveLogKind(new(config.Sites["app_example_com"]))
	if err != nil {
		t.Fatalf("EffectiveLogKind() error = %v", err)
	}
	if logKind != OCMSLogKindError {
		t.Fatalf("EffectiveLogKind() = %q", logKind)
	}

	logKind, err = config.EffectiveLogKind(new(config.Sites["all_example_com"]))
	if err != nil {
		t.Fatalf("EffectiveLogKind(all) error = %v", err)
	}
	if logKind != OCMSLogKindAll {
		t.Fatalf("EffectiveLogKind(all) = %q", logKind)
	}
}

func TestLoadOCMSSitesConfig_DefaultLogKindAll(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "ocms-sites.json")
	content := `{
  "version": "1.0",
  "default_site": "example_com",
  "default_log_kind": "all",
  "sites": {
    "example_com": {
      "name": "Example Site"
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, _, err := LoadOCMSSitesConfig(configPath)
	if err != nil {
		t.Fatalf("LoadOCMSSitesConfig() error = %v", err)
	}
	if config == nil {
		t.Fatal("LoadOCMSSitesConfig() returned nil config")
	}

	site, err := config.GetSite("example_com")
	if err != nil {
		t.Fatalf("GetSite() error = %v", err)
	}
	logKind, err := config.EffectiveLogKind(site)
	if err != nil {
		t.Fatalf("EffectiveLogKind() error = %v", err)
	}
	if logKind != OCMSLogKindAll {
		t.Fatalf("EffectiveLogKind() = %q, want %q", logKind, OCMSLogKindAll)
	}
}

func TestLoadOCMSSitesConfig_InvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "default site missing",
			content: `{
  "version": "1.0",
  "default_site": "missing",
  "sites": {
    "example_com": {
      "name": "Example Site"
    }
  }
}`,
			want: "default_site 'missing' does not exist",
		},
		{
			name: "invalid default log kind",
			content: `{
  "version": "1.0",
  "default_log_kind": "verbose",
  "sites": {
    "example_com": {
      "name": "Example Site"
    }
  }
}`,
			want: "default_log_kind",
		},
		{
			name: "invalid site log kind",
			content: `{
  "version": "1.0",
  "sites": {
    "example_com": {
      "name": "Example Site",
      "log_kind": "verbose"
    }
  }
}`,
			want: "site 'example_com': log_kind",
		},
		{
			name: "unknown path field rejected",
			content: `{
  "version": "1.0",
  "sites": {
    "example_com": {
      "name": "Example Site",
      "logs_path": "/tmp/ocms.log"
    }
  }
}`,
			want: `unknown field "logs_path"`,
		},
		{
			name: "instance dir field rejected",
			content: `{
  "version": "1.0",
  "sites": {
    "example_com": {
      "name": "Example Site",
      "instance_dir": "/var/www/vhosts/example.com/ocms"
    }
  }
}`,
			want: `unknown field "instance_dir"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "ocms-sites.json")
			if err := os.WriteFile(configPath, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}

			_, _, err := LoadOCMSSitesConfig(configPath)
			if err == nil {
				t.Fatal("LoadOCMSSitesConfig() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLoadOCMSSitesConfig_ExplicitPathNotFound(t *testing.T) {
	t.Parallel()

	_, _, err := LoadOCMSSitesConfig("/nonexistent/path/ocms-sites.json")
	if err == nil {
		t.Fatal("LoadOCMSSitesConfig() expected error for missing explicit path")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want not found", err)
	}
}
