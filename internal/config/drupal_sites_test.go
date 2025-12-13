package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDrupalSitesConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  DrupalSitesConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with multiple sites",
			config: DrupalSitesConfig{
				Version:     "1.0",
				DefaultSite: "production",
				Sites: map[string]DrupalSite{
					"production": {
						Name:           "Production",
						DrupalRoot:     "/var/www/prod",
						WatchdogPath:   "/tmp/prod.json",
						WatchdogFormat: "json",
						MinSeverity:    3,
					},
					"staging": {
						Name:           "Staging",
						DrupalRoot:     "/var/www/staging",
						WatchdogPath:   "/tmp/staging.json",
						WatchdogFormat: "drush",
						MinSeverity:    4,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid config without default_site",
			config: DrupalSitesConfig{
				Version: "1.0",
				Sites: map[string]DrupalSite{
					"mysite": {
						Name:         "My Site",
						DrupalRoot:   "/var/www/mysite",
						WatchdogPath: "/tmp/mysite.json",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty sites",
			config: DrupalSitesConfig{
				Version: "1.0",
				Sites:   map[string]DrupalSite{},
			},
			wantErr: true,
			errMsg:  "no sites defined",
		},
		{
			name: "nil sites",
			config: DrupalSitesConfig{
				Version: "1.0",
			},
			wantErr: true,
			errMsg:  "no sites defined",
		},
		{
			name: "default_site references non-existent site",
			config: DrupalSitesConfig{
				Version:     "1.0",
				DefaultSite: "nonexistent",
				Sites: map[string]DrupalSite{
					"production": {
						DrupalRoot:   "/var/www/prod",
						WatchdogPath: "/tmp/prod.json",
					},
				},
			},
			wantErr: true,
			errMsg:  "default_site 'nonexistent' does not exist",
		},
		{
			name: "missing drupal_root",
			config: DrupalSitesConfig{
				Version: "1.0",
				Sites: map[string]DrupalSite{
					"mysite": {
						WatchdogPath: "/tmp/mysite.json",
					},
				},
			},
			wantErr: true,
			errMsg:  "drupal_root is required",
		},
		{
			name: "missing watchdog_path",
			config: DrupalSitesConfig{
				Version: "1.0",
				Sites: map[string]DrupalSite{
					"mysite": {
						DrupalRoot: "/var/www/mysite",
					},
				},
			},
			wantErr: true,
			errMsg:  "watchdog_path is required",
		},
		{
			name: "invalid watchdog_format",
			config: DrupalSitesConfig{
				Version: "1.0",
				Sites: map[string]DrupalSite{
					"mysite": {
						DrupalRoot:     "/var/www/mysite",
						WatchdogPath:   "/tmp/mysite.json",
						WatchdogFormat: "invalid",
					},
				},
			},
			wantErr: true,
			errMsg:  "watchdog_format must be 'json' or 'drush'",
		},
		{
			name: "invalid min_severity too high",
			config: DrupalSitesConfig{
				Version: "1.0",
				Sites: map[string]DrupalSite{
					"mysite": {
						DrupalRoot:   "/var/www/mysite",
						WatchdogPath: "/tmp/mysite.json",
						MinSeverity:  8,
					},
				},
			},
			wantErr: true,
			errMsg:  "min_severity must be 0-7",
		},
		{
			name: "invalid min_severity negative",
			config: DrupalSitesConfig{
				Version: "1.0",
				Sites: map[string]DrupalSite{
					"mysite": {
						DrupalRoot:   "/var/www/mysite",
						WatchdogPath: "/tmp/mysite.json",
						MinSeverity:  -1,
					},
				},
			},
			wantErr: true,
			errMsg:  "min_severity must be 0-7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestDrupalSitesConfig_GetSite(t *testing.T) {
	config := &DrupalSitesConfig{
		Version:     "1.0",
		DefaultSite: "production",
		Sites: map[string]DrupalSite{
			"production": {
				Name:         "Production",
				DrupalRoot:   "/var/www/prod",
				WatchdogPath: "/tmp/prod.json",
				MinSeverity:  3,
			},
			"staging": {
				Name:         "Staging",
				DrupalRoot:   "/var/www/staging",
				WatchdogPath: "/tmp/staging.json",
				MinSeverity:  4,
			},
		},
	}

	tests := []struct {
		name       string
		siteID     string
		wantName   string
		wantErr    bool
		errContain string
	}{
		{
			name:     "get existing site by ID",
			siteID:   "production",
			wantName: "Production",
			wantErr:  false,
		},
		{
			name:     "get another existing site",
			siteID:   "staging",
			wantName: "Staging",
			wantErr:  false,
		},
		{
			name:     "empty siteID uses default",
			siteID:   "",
			wantName: "Production",
			wantErr:  false,
		},
		{
			name:       "non-existent site",
			siteID:     "nonexistent",
			wantErr:    true,
			errContain: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site, err := config.GetSite(tt.siteID)
			if tt.wantErr {
				if err == nil {
					t.Errorf("GetSite() expected error, got nil")
					return
				}
				if tt.errContain != "" && !contains(err.Error(), tt.errContain) {
					t.Errorf("GetSite() error = %v, want error containing %q", err, tt.errContain)
				}
			} else {
				if err != nil {
					t.Errorf("GetSite() unexpected error = %v", err)
					return
				}
				if site.Name != tt.wantName {
					t.Errorf("GetSite() site.Name = %q, want %q", site.Name, tt.wantName)
				}
			}
		})
	}
}

func TestDrupalSitesConfig_GetSite_NoDefault(t *testing.T) {
	config := &DrupalSitesConfig{
		Version: "1.0",
		// No default_site set
		Sites: map[string]DrupalSite{
			"production": {
				DrupalRoot:   "/var/www/prod",
				WatchdogPath: "/tmp/prod.json",
			},
		},
	}

	_, err := config.GetSite("")
	if err == nil {
		t.Fatal("GetSite('') expected error when no default_site, got nil")
	}
	if !contains(err.Error(), "no site ID specified") {
		t.Errorf("GetSite('') error = %v, want error about no site ID", err)
	}
}

func TestDrupalSitesConfig_ListSites(t *testing.T) {
	config := &DrupalSitesConfig{
		Sites: map[string]DrupalSite{
			"zebra": {DrupalRoot: "/a", WatchdogPath: "/a"},
			"alpha": {DrupalRoot: "/b", WatchdogPath: "/b"},
			"beta":  {DrupalRoot: "/c", WatchdogPath: "/c"},
		},
	}

	sites := config.ListSites()

	if len(sites) != 3 {
		t.Errorf("ListSites() returned %d sites, want 3", len(sites))
	}

	// Should be sorted
	expected := []string{"alpha", "beta", "zebra"}
	for i, want := range expected {
		if sites[i] != want {
			t.Errorf("ListSites()[%d] = %q, want %q", i, sites[i], want)
		}
	}
}

func TestLoadDrupalSitesConfig_FromTestdata(t *testing.T) {
	// Load from testdata fixture
	configPath := filepath.Join("..", "..", "testdata", "config", "drupal-sites.json")

	config, foundPath, err := LoadDrupalSitesConfig(configPath)
	if err != nil {
		t.Fatalf("LoadDrupalSitesConfig() error = %v", err)
	}
	if config == nil {
		t.Fatal("LoadDrupalSitesConfig() returned nil config")
	}
	if foundPath != configPath {
		t.Errorf("LoadDrupalSitesConfig() foundPath = %q, want %q", foundPath, configPath)
	}

	// Verify loaded data
	if config.Version != "1.0" {
		t.Errorf("config.Version = %q, want %q", config.Version, "1.0")
	}
	if config.DefaultSite != "production" {
		t.Errorf("config.DefaultSite = %q, want %q", config.DefaultSite, "production")
	}
	if len(config.Sites) != 3 {
		t.Errorf("len(config.Sites) = %d, want 3", len(config.Sites))
	}

	// Check production site
	prod, exists := config.Sites["production"]
	if !exists {
		t.Error("config.Sites['production'] does not exist")
	} else {
		if prod.Name != "Production Site" {
			t.Errorf("production.Name = %q, want %q", prod.Name, "Production Site")
		}
		if prod.WatchdogFormat != "json" {
			t.Errorf("production.WatchdogFormat = %q, want %q", prod.WatchdogFormat, "json")
		}
		if prod.MinSeverity != 3 {
			t.Errorf("production.MinSeverity = %d, want %d", prod.MinSeverity, 3)
		}
	}

	// Check dev site uses drush format
	dev, exists := config.Sites["dev"]
	if !exists {
		t.Error("config.Sites['dev'] does not exist")
	} else {
		if dev.WatchdogFormat != "drush" {
			t.Errorf("dev.WatchdogFormat = %q, want %q", dev.WatchdogFormat, "drush")
		}
	}
}

func TestLoadDrupalSitesConfig_NotFound(t *testing.T) {
	// Search for config in non-existent path (no explicit path, search standard locations)
	// This should return nil, nil (not an error, just no config found)

	// Save and restore HOME to control search paths
	origHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", origHome) }()
	_ = os.Setenv("HOME", "/nonexistent-home-for-test")

	// Change to temp dir to avoid finding any config
	origDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(origDir) }()
	tmpDir := t.TempDir()
	_ = os.Chdir(tmpDir)

	config, path, err := LoadDrupalSitesConfig("")
	if err != nil {
		t.Errorf("LoadDrupalSitesConfig('') error = %v, want nil", err)
	}
	if config != nil {
		t.Errorf("LoadDrupalSitesConfig('') config = %v, want nil", config)
	}
	if path != "" {
		t.Errorf("LoadDrupalSitesConfig('') path = %q, want empty", path)
	}
}

func TestLoadDrupalSitesConfig_ExplicitPathNotFound(t *testing.T) {
	_, _, err := LoadDrupalSitesConfig("/nonexistent/path/drupal-sites.json")
	if err == nil {
		t.Fatal("LoadDrupalSitesConfig() expected error for explicit non-existent path")
	}
	if !contains(err.Error(), "not found") {
		t.Errorf("LoadDrupalSitesConfig() error = %v, want error containing 'not found'", err)
	}
}

func TestLoadDrupalSitesConfig_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "drupal-sites.json")

	// Write invalid JSON
	if err := os.WriteFile(configPath, []byte("{ invalid json }"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	_, _, err := LoadDrupalSitesConfig(configPath)
	if err == nil {
		t.Fatal("LoadDrupalSitesConfig() expected error for invalid JSON")
	}
	if !contains(err.Error(), "failed to parse") {
		t.Errorf("LoadDrupalSitesConfig() error = %v, want error containing 'failed to parse'", err)
	}
}

func TestLoadDrupalSitesConfig_ValidationFails(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "drupal-sites.json")

	// Write valid JSON but invalid config (empty sites)
	invalidConfig := `{"version": "1.0", "sites": {}}`
	if err := os.WriteFile(configPath, []byte(invalidConfig), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	_, _, err := LoadDrupalSitesConfig(configPath)
	if err == nil {
		t.Fatal("LoadDrupalSitesConfig() expected error for invalid config")
	}
	if !contains(err.Error(), "invalid config") {
		t.Errorf("LoadDrupalSitesConfig() error = %v, want error containing 'invalid config'", err)
	}
}

// contains checks if s contains substr (case-sensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
