package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// DrupalSite represents configuration for a single Drupal site
type DrupalSite struct {
	Name           string `json:"name"`            // Human-readable site name for reports
	DrupalRoot     string `json:"drupal_root"`     // Path to Drupal installation root
	WatchdogPath   string `json:"watchdog_path"`   // Path to watchdog export file
	WatchdogFormat string `json:"watchdog_format"` // "json" or "drush" (default: "json")
	MinSeverity    int    `json:"min_severity"`    // RFC 5424 severity level (default: 3)
	WatchdogLimit  int    `json:"watchdog_limit"`  // Max entries in output (default: 100)
}

// DrupalSitesConfig represents the multi-site configuration file
type DrupalSitesConfig struct {
	Version     string                `json:"version"`      // Config file version
	DefaultSite string                `json:"default_site"` // Default site ID if --drupal-site not specified
	Sites       map[string]DrupalSite `json:"sites"`        // Site configurations keyed by site ID
}

// Validate checks the configuration for errors
func (c *DrupalSitesConfig) Validate() error {
	if len(c.Sites) == 0 {
		return fmt.Errorf("no sites defined in configuration")
	}

	// Validate default_site references an existing site
	if c.DefaultSite != "" {
		if _, exists := c.Sites[c.DefaultSite]; !exists {
			return fmt.Errorf("default_site '%s' does not exist in sites", c.DefaultSite)
		}
	}

	// Validate each site
	for siteID, site := range c.Sites {
		if site.DrupalRoot == "" {
			return fmt.Errorf("site '%s': drupal_root is required", siteID)
		}
		if site.WatchdogPath == "" {
			return fmt.Errorf("site '%s': watchdog_path is required", siteID)
		}
		if site.WatchdogFormat != "" && site.WatchdogFormat != "json" && site.WatchdogFormat != "drush" {
			return fmt.Errorf("site '%s': watchdog_format must be 'json' or 'drush' (got: %s)", siteID, site.WatchdogFormat)
		}
		if site.MinSeverity < 0 || site.MinSeverity > 7 {
			return fmt.Errorf("site '%s': min_severity must be 0-7 (got: %d)", siteID, site.MinSeverity)
		}
	}

	return nil
}

// GetSite returns a site by ID, falling back to default_site if siteID is empty
func (c *DrupalSitesConfig) GetSite(siteID string) (*DrupalSite, error) {
	// Use default site if no site ID provided
	if siteID == "" {
		if c.DefaultSite == "" {
			return nil, fmt.Errorf("no site ID specified and no default_site configured")
		}
		siteID = c.DefaultSite
	}

	site, exists := c.Sites[siteID]
	if !exists {
		available := c.ListSites()
		return nil, fmt.Errorf("site '%s' not found (available: %v)", siteID, available)
	}

	return &site, nil
}

// ListSites returns all available site IDs in sorted order
func (c *DrupalSitesConfig) ListSites() []string {
	sites := make([]string, 0, len(c.Sites))
	for siteID := range c.Sites {
		sites = append(sites, siteID)
	}
	sort.Strings(sites)
	return sites
}

// LoadDrupalSitesConfig loads and parses the drupal-sites.json file
// If configPath is empty, it searches standard locations.
// Returns nil, nil if no config file is found (not an error - single-site mode).
func LoadDrupalSitesConfig(configPath string) (*DrupalSitesConfig, string, error) {
	var searchPaths []string

	// If explicit path provided, only search that
	if configPath != "" {
		searchPaths = append(searchPaths, configPath)
	} else {
		// Standard search paths in priority order
		searchPaths = append(searchPaths,
			"./drupal-sites.json",
			"./configs/drupal-sites.json",
			"/opt/logwatch-ai/drupal-sites.json",
		)

		// Add user config directory if HOME is set
		if home := os.Getenv("HOME"); home != "" {
			searchPaths = append(searchPaths,
				filepath.Join(home, ".config", "logwatch-ai", "drupal-sites.json"),
			)
		}
	}

	for _, path := range searchPaths {
		if path == "" {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Try next path
			}
			return nil, "", fmt.Errorf("failed to read %s: %w", path, err)
		}

		var config DrupalSitesConfig
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, "", fmt.Errorf("failed to parse %s: %w", path, err)
		}

		if err := config.Validate(); err != nil {
			return nil, "", fmt.Errorf("invalid config in %s: %w", path, err)
		}

		return &config, path, nil
	}

	// If explicit path was provided but not found, that's an error
	if configPath != "" {
		return nil, "", fmt.Errorf("drupal sites config not found: %s", configPath)
	}

	// No config file found (not an error - single-site mode)
	return nil, "", nil
}
