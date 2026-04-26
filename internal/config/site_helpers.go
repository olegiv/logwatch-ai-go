// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func sortedSiteIDs[T any](sites map[string]T) []string {
	ids := make([]string, 0, len(sites))
	for siteID := range sites {
		ids = append(ids, siteID)
	}
	sort.Strings(ids)
	return ids
}

func resolveSiteID(sourceName, flagName, selectedID, defaultID, listFlag string) (string, error) {
	if selectedID != "" {
		return selectedID, nil
	}
	if defaultID != "" {
		return defaultID, nil
	}
	return "", fmt.Errorf("no site ID specified for %s. Use %s <site_id> or set a default site. Available sites: use %s to see options",
		sourceName, flagName, listFlag)
}

func loadFirstExistingFile(explicitPath, notFoundLabel string, searchPaths []string) ([]byte, string, error) {
	if explicitPath != "" {
		searchPaths = []string{explicitPath}
	}

	for _, path := range searchPaths {
		if path == "" {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", fmt.Errorf("failed to read %s: %w", path, err)
		}

		return data, path, nil
	}

	if explicitPath != "" {
		return nil, "", fmt.Errorf("%s not found: %s", notFoundLabel, explicitPath)
	}

	return nil, "", nil
}

func standardDrupalSitesConfigPaths() []string {
	searchPaths := []string{
		"./drupal-sites.json",
		"./configs/drupal-sites.json",
		"/opt/logwatch-ai/drupal-sites.json",
	}

	if home := os.Getenv("HOME"); home != "" {
		searchPaths = append(searchPaths,
			filepath.Join(home, ".config", "logwatch-ai", "drupal-sites.json"),
		)
	}

	return searchPaths
}

func standardOCMSSitesConfigPaths() []string {
	searchPaths := []string{
		"./ocms-sites.json",
		"./configs/ocms-sites.json",
		"/opt/logwatch-ai/ocms-sites.json",
	}

	if home := os.Getenv("HOME"); home != "" {
		searchPaths = append(searchPaths,
			filepath.Join(home, ".config", "logwatch-ai", "ocms-sites.json"),
		)
	}

	return searchPaths
}
