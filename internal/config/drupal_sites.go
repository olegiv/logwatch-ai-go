// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
)

var systemUserPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

var watchdogFilenamePattern = regexp.MustCompile(`(^|-)watchdog\.json$`)

var watchdogOutputRoots = []string{
	"/var/log/logwatch-ai",
	"/opt/logwatch-ai/logs",
}

const maxWatchdogLimit = 100000

// DrupalSite represents configuration for a single Drupal site
type DrupalSite struct {
	Name             string   `json:"name"`            // Human-readable site name for reports
	LegacyNames      []string `json:"legacy_names"`    // Previous display names used by v2 history
	DrupalRoot       string   `json:"drupal_root"`     // Path to Drupal installation root
	SystemUser       string   `json:"system_user"`     // Non-root account used for root-cron Drush execution
	WatchdogPath     string   `json:"watchdog_path"`   // Path to watchdog export file
	WatchdogFormat   string   `json:"watchdog_format"` // JSON (default and only supported format)
	MinSeverity      int      `json:"min_severity"`    // RFC 5424 severity level (generator default: 4)
	WatchdogLimit    int      `json:"watchdog_limit"`  // Required max entries in output
	watchdogLimitSet bool
}

// UnmarshalJSON records whether watchdog_limit was explicitly supplied so an
// omitted value can retain the documented default while an explicit zero is
// rejected instead of producing a false-clean export.
func (s *DrupalSite) UnmarshalJSON(data []byte) error {
	type siteAlias DrupalSite
	var decoded siteAlias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("trailing content: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*s = DrupalSite(decoded)
	_, s.watchdogLimitSet = fields["watchdog_limit"]
	return nil
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

	// Display names are used to reconcile legacy rows that predate stable site
	// IDs. Ambiguous names would permanently assign all matching history to
	// whichever site's cron job runs first.
	displayNames := make(map[string]string, len(c.Sites))

	// Validate each site in stable order so diagnostics are deterministic.
	for _, siteID := range c.ListSites() {
		site := c.Sites[siteID]
		effectiveName := site.Name
		if effectiveName == "" {
			effectiveName = siteID
		}
		if err := registerSiteIdentityNames(displayNames, siteID, effectiveName, site.LegacyNames); err != nil {
			return err
		}
		if site.DrupalRoot == "" {
			return fmt.Errorf("site '%s': drupal_root is required", siteID)
		}
		if site.WatchdogPath == "" {
			return fmt.Errorf("site '%s': watchdog_path is required", siteID)
		}
		if err := validateWatchdogOutputPath(site.WatchdogPath); err != nil {
			return fmt.Errorf("site '%s': watchdog_path: %w", siteID, err)
		}
		if site.SystemUser != "" && !systemUserPattern.MatchString(site.SystemUser) {
			return fmt.Errorf("site '%s': system_user is not a valid Unix account name", siteID)
		}
		if site.WatchdogFormat != "" && site.WatchdogFormat != "json" {
			return fmt.Errorf("site '%s': watchdog_format must be 'json' (got: %s)", siteID, site.WatchdogFormat)
		}
		if site.MinSeverity < 0 || site.MinSeverity > 7 {
			return fmt.Errorf("site '%s': min_severity must be 0-7 (got: %d)", siteID, site.MinSeverity)
		}
		if site.WatchdogLimit < 0 || site.WatchdogLimit > maxWatchdogLimit ||
			(site.watchdogLimitSet && site.WatchdogLimit == 0) {
			return fmt.Errorf("site '%s': watchdog_limit must be 1-%d (got: %d)", siteID, maxWatchdogLimit, site.WatchdogLimit)
		}
	}

	return nil
}

func validateWatchdogOutputPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("must be an absolute normalized path")
	}
	if !watchdogFilenamePattern.MatchString(filepath.Base(path)) {
		return fmt.Errorf("basename must be watchdog.json or end in -watchdog.json")
	}
	for _, root := range watchdogOutputRoots {
		relative, err := filepath.Rel(root, path)
		if err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("must be below /var/log/logwatch-ai or /opt/logwatch-ai/logs")
}

// GetSite returns a site by ID, falling back to default_site if siteID is empty
func (c *DrupalSitesConfig) GetSite(siteID string) (*DrupalSite, error) {
	resolvedSiteID, err := resolveSiteID("Drupal", "-drupal-site", siteID, c.DefaultSite, "-list-drupal-sites")
	if err != nil {
		return nil, err
	}

	site, exists := c.Sites[resolvedSiteID]
	if !exists {
		available := c.ListSites()
		return nil, fmt.Errorf("site '%s' not found (available: %v)", resolvedSiteID, available)
	}

	return &site, nil
}

// ListSites returns all available site IDs in sorted order
func (c *DrupalSitesConfig) ListSites() []string {
	return sortedSiteIDs(c.Sites)
}

// LoadDrupalSitesConfig loads and parses the drupal-sites.json file
// If configPath is empty, it searches standard locations.
// Returns nil, nil if no config file is found (not an error - single-site mode).
func LoadDrupalSitesConfig(configPath string) (*DrupalSitesConfig, string, error) {
	data, foundPath, err := loadFirstExistingFile(
		configPath,
		"drupal sites config",
		standardDrupalSitesConfigPaths(),
	)
	if err != nil {
		return nil, "", err
	}
	if data == nil {
		return nil, "", nil
	}

	var config DrupalSitesConfig
	if err := decodeStrictJSON(data, &config); err != nil {
		return nil, "", fmt.Errorf("failed to parse %s: %w", foundPath, err)
	}

	if err := config.Validate(); err != nil {
		return nil, "", fmt.Errorf("invalid config in %s: %w", foundPath, err)
	}

	return &config, foundPath, nil
}
