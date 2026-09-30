// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/olegiv/logwatch-ai-go/internal/securefile"
)

const maxSiteConfigBytes = 1024 * 1024

func decodeStrictJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("trailing content: %w", err)
	}
	return nil
}

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

func registerSiteIdentityNames(
	identities map[string]string,
	siteID, displayName string,
	legacyNames []string,
) error {
	names := make([]string, 0, len(legacyNames)+1)
	names = append(names, displayName)
	names = append(names, legacyNames...)
	for index, name := range names {
		if name == "" || name != strings.TrimSpace(name) {
			field := "display name"
			if index > 0 {
				field = "legacy_names entry"
			}
			return fmt.Errorf("site '%s': %s must be non-empty without surrounding whitespace", siteID, field)
		}
		if existingSiteID, exists := identities[name]; exists {
			return fmt.Errorf(
				"sites '%s' and '%s' use the same display name or legacy name %q",
				existingSiteID,
				siteID,
				name,
			)
		}
		identities[name] = siteID
	}
	return nil
}

func loadFirstExistingFile(explicitPath, notFoundLabel string, searchPaths []string) ([]byte, string, error) {
	if explicitPath != "" {
		searchPaths = []string{explicitPath}
	}

	for _, path := range searchPaths {
		if path == "" {
			continue
		}

		data, err := securefile.ReadLimitedRegular(path, maxSiteConfigBytes)
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
