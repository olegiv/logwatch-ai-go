// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	DefaultOCMSSitesRegistryPath = "/etc/ocms/sites.conf"

	OCMSLogKindMain  = "main"
	OCMSLogKindError = "error"
	OCMSLogKindAll   = "all"
)

// OCMSSite represents one site entry from /etc/ocms/sites.conf.
type OCMSSite struct {
	ID          string
	InstanceDir string
	SystemUser  string
	Port        int
}

// OCMSSitesRegistry represents the OCMS multisite registry.
type OCMSSitesRegistry struct {
	Sites map[string]OCMSSite
}

// OCMSLogPath represents one derived OCMS log file.
type OCMSLogPath struct {
	Kind string
	Path string
}

// OCMSSiteConfig represents logwatch-ai settings for a single OCMS site.
type OCMSSiteConfig struct {
	Name    string `json:"name"`
	LogKind string `json:"log_kind"`
}

// OCMSSitesConfig represents the logwatch-ai OCMS multi-site JSON file.
type OCMSSitesConfig struct {
	Version        string                    `json:"version"`
	DefaultSite    string                    `json:"default_site"`
	RegistryPath   string                    `json:"registry_path"`
	DefaultLogKind string                    `json:"default_log_kind"`
	Sites          map[string]OCMSSiteConfig `json:"sites"`
}

// NormalizeOCMSLogKind validates and normalizes an OCMS log kind.
func NormalizeOCMSLogKind(logKind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(logKind)) {
	case "", OCMSLogKindMain:
		return OCMSLogKindMain, nil
	case OCMSLogKindError:
		return OCMSLogKindError, nil
	case OCMSLogKindAll:
		return OCMSLogKindAll, nil
	default:
		return "", fmt.Errorf("OCMS log kind must be 'main', 'error', or 'all' (got: %s)", logKind)
	}
}

// Validate checks the logwatch-ai OCMS sites JSON configuration.
func (c *OCMSSitesConfig) Validate() error {
	if c == nil || len(c.Sites) == 0 {
		return fmt.Errorf("no sites defined in configuration")
	}

	if c.DefaultSite != "" {
		if _, exists := c.Sites[c.DefaultSite]; !exists {
			return fmt.Errorf("default_site '%s' does not exist in sites", c.DefaultSite)
		}
	}

	if _, err := NormalizeOCMSLogKind(c.DefaultLogKind); err != nil {
		return fmt.Errorf("default_log_kind: %w", err)
	}

	for siteID, site := range c.Sites {
		if _, err := NormalizeOCMSLogKind(site.LogKind); err != nil {
			return fmt.Errorf("site '%s': log_kind: %w", siteID, err)
		}
	}

	return nil
}

// GetSite returns a configured OCMS site by ID, falling back to default_site.
func (c *OCMSSitesConfig) GetSite(siteID string) (*OCMSSiteConfig, error) {
	resolvedSiteID, err := resolveSiteID("OCMS", "-ocms-site", siteID, c.DefaultSite, "-list-ocms-sites")
	if err != nil {
		return nil, err
	}

	site, exists := c.Sites[resolvedSiteID]
	if !exists {
		return nil, fmt.Errorf("site '%s' not found (available: %v)", resolvedSiteID, c.ListSites())
	}

	return &site, nil
}

// ListSites returns all configured OCMS site IDs in sorted order.
func (c *OCMSSitesConfig) ListSites() []string {
	if c == nil {
		return nil
	}
	return sortedSiteIDs(c.Sites)
}

// EffectiveLogKind returns the site-specific log kind with config defaults applied.
func (c *OCMSSitesConfig) EffectiveLogKind(site *OCMSSiteConfig) (string, error) {
	logKind := c.DefaultLogKind
	if site != nil && site.LogKind != "" {
		logKind = site.LogKind
	}
	return NormalizeOCMSLogKind(logKind)
}

// LoadOCMSSitesConfig loads and parses ocms-sites.json.
// If configPath is empty, it searches standard locations.
func LoadOCMSSitesConfig(configPath string) (*OCMSSitesConfig, string, error) {
	data, foundPath, err := loadFirstExistingFile(
		configPath,
		"ocms sites config",
		standardOCMSSitesConfigPaths(),
	)
	if err != nil {
		return nil, "", err
	}
	if data == nil {
		return nil, "", nil
	}

	var config OCMSSitesConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, "", fmt.Errorf("failed to parse %s: %w", foundPath, err)
	}

	if err := config.Validate(); err != nil {
		return nil, "", fmt.Errorf("invalid config in %s: %w", foundPath, err)
	}

	return &config, foundPath, nil
}

// LogPath returns the derived log path for the requested log kind.
func (s OCMSSite) LogPath(logKind string) (string, error) {
	normalized, err := NormalizeOCMSLogKind(logKind)
	if err != nil {
		return "", err
	}
	if normalized == OCMSLogKindAll {
		return "", fmt.Errorf("OCMS log kind 'all' resolves to multiple log paths")
	}

	switch normalized {
	case OCMSLogKindError:
		return filepath.Join(s.InstanceDir, "logs", "error.log"), nil
	default:
		return filepath.Join(s.InstanceDir, "logs", "ocms.log"), nil
	}
}

// LogPaths returns all derived log paths for the requested log kind.
func (s OCMSSite) LogPaths(logKind string) ([]OCMSLogPath, error) {
	normalized, err := NormalizeOCMSLogKind(logKind)
	if err != nil {
		return nil, err
	}

	mainPath := filepath.Join(s.InstanceDir, "logs", "ocms.log")
	errorPath := filepath.Join(s.InstanceDir, "logs", "error.log")

	switch normalized {
	case OCMSLogKindAll:
		return []OCMSLogPath{
			{Kind: OCMSLogKindMain, Path: mainPath},
			{Kind: OCMSLogKindError, Path: errorPath},
		}, nil
	case OCMSLogKindError:
		return []OCMSLogPath{{Kind: OCMSLogKindError, Path: errorPath}}, nil
	default:
		return []OCMSLogPath{{Kind: OCMSLogKindMain, Path: mainPath}}, nil
	}
}

// Validate checks the parsed OCMS sites registry for errors.
func (r *OCMSSitesRegistry) Validate() error {
	if r == nil || len(r.Sites) == 0 {
		return fmt.Errorf("no sites defined in OCMS registry")
	}
	return nil
}

// GetSite returns a site by ID.
func (r *OCMSSitesRegistry) GetSite(siteID string) (*OCMSSite, error) {
	if r == nil {
		return nil, fmt.Errorf("OCMS sites registry is not loaded")
	}
	if strings.TrimSpace(siteID) == "" {
		return nil, fmt.Errorf("no OCMS site ID specified")
	}

	site, exists := r.Sites[siteID]
	if !exists {
		return nil, fmt.Errorf("OCMS site '%s' not found (available: %v)", siteID, r.ListSites())
	}

	return &site, nil
}

// ListSites returns all available OCMS site IDs in sorted order.
func (r *OCMSSitesRegistry) ListSites() []string {
	if r == nil {
		return nil
	}
	return sortedSiteIDs(r.Sites)
}

// LoadOCMSSitesRegistry loads and parses the OCMS sites registry.
// If registryPath is empty, it looks for /etc/ocms/sites.conf.
func LoadOCMSSitesRegistry(registryPath string) (*OCMSSitesRegistry, string, error) {
	data, foundPath, err := loadFirstExistingFile(
		registryPath,
		"OCMS sites registry",
		[]string{DefaultOCMSSitesRegistryPath},
	)
	if err != nil {
		return nil, "", err
	}
	if data == nil {
		return nil, "", nil
	}

	registry, err := ParseOCMSSitesRegistry(data)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse %s: %w", foundPath, err)
	}
	if err := registry.Validate(); err != nil {
		return nil, "", fmt.Errorf("invalid OCMS registry in %s: %w", foundPath, err)
	}

	return registry, foundPath, nil
}

// ParseOCMSSitesRegistry parses sites.conf content.
func ParseOCMSSitesRegistry(data []byte) (*OCMSSitesRegistry, error) {
	registry := &OCMSSitesRegistry{
		Sites: make(map[string]OCMSSite),
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := stripOCMSRegistryComment(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 4 {
			return nil, fmt.Errorf("line %d: expected 4 fields: SITE_ID INSTANCE_DIR SYSTEM_USER PORT", lineNumber)
		}

		siteID := fields[0]
		if _, exists := registry.Sites[siteID]; exists {
			return nil, fmt.Errorf("line %d: duplicate site ID %q", lineNumber, siteID)
		}
		instanceDir, err := validateOCMSInstanceDir(lineNumber, fields[1])
		if err != nil {
			return nil, err
		}

		port, err := strconv.Atoi(fields[3])
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("line %d: port must be a number between 1 and 65535", lineNumber)
		}

		registry.Sites[siteID] = OCMSSite{
			ID:          siteID,
			InstanceDir: instanceDir,
			SystemUser:  fields[2],
			Port:        port,
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan registry: %w", err)
	}

	return registry, nil
}

func validateOCMSInstanceDir(lineNumber int, instanceDir string) (string, error) {
	if !filepath.IsAbs(instanceDir) {
		return "", fmt.Errorf("line %d: INSTANCE_DIR must be an absolute path", lineNumber)
	}
	cleaned := filepath.Clean(instanceDir)
	if cleaned != instanceDir {
		return "", fmt.Errorf("line %d: INSTANCE_DIR must not contain path traversal components", lineNumber)
	}
	return instanceDir, nil
}

func stripOCMSRegistryComment(line string) string {
	if commentIndex := strings.Index(line, "#"); commentIndex >= 0 {
		line = line[:commentIndex]
	}
	return strings.TrimSpace(line)
}
