// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package drupal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
	"github.com/olegiv/logwatch-ai-go/internal/securefile"
)

// NoEntriesContent is returned when the watchdog file contains no log entries.
// This is a valid state - it means there were no entries for the time period.
// Use IsNoEntriesContent() to check for this condition.
const NoEntriesContent = "=== NO WATCHDOG ENTRIES ===\n\nNo Drupal watchdog entries were found for the analyzed time period.\nThis typically means the system had no logged events during this period."

// timeFormatDateTime is the standard date-time format for watchdog entries.
const timeFormatDateTime = "2006-01-02 15:04:05"

var ipv4AddressPattern = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)

// maxNDJSONLineBytes is the maximum allowed size for a single NDJSON line.
// It is set to 10MB to support very large watchdog messages while preventing
// unbounded memory usage during parsing.
const maxNDJSONLineBytes = 10 * 1024 * 1024

// maxWatchdogFileAge catches stale exports when the producer was not run.
// The 26-hour window allows daily schedules to cross daylight-saving changes.
const maxWatchdogFileAge = 26 * time.Hour

// IsNoEntriesContent checks if the content indicates no watchdog entries were found.
func IsNoEntriesContent(content string) bool {
	return strings.HasPrefix(content, "=== NO WATCHDOG ENTRIES ===")
}

// Compile-time interface check
var _ analyzer.LogReader = (*Reader)(nil)

// InputFormat specifies the format of the watchdog input file.
type InputFormat string

const (
	// FormatJSON is for JSON-exported watchdog entries (recommended)
	FormatJSON InputFormat = "json"
)

// Reader handles reading and validating Drupal watchdog log files.
// Implements analyzer.LogReader interface.
type Reader struct {
	maxSizeMB           int
	enablePreprocessing bool
	maxTokens           int
	format              InputFormat
	preprocessor        *Preprocessor
}

// NewReader creates a new Drupal watchdog reader.
func NewReader(maxSizeMB int, enablePreprocessing bool, maxTokens int, format InputFormat) *Reader {
	return &Reader{
		maxSizeMB:           maxSizeMB,
		enablePreprocessing: enablePreprocessing,
		maxTokens:           maxTokens,
		format:              format,
		preprocessor:        NewPreprocessor(maxTokens),
	}
}

// Read implements analyzer.LogReader.Read.
// Reads and processes the Drupal watchdog file.
func (r *Reader) Read(sourcePath string) (string, error) {
	// Open once and run all checks against the handle so the metadata that
	// is validated always describes the same inode that is read (no
	// stat-then-read TOCTOU window).
	file, err := securefile.OpenNoFollow(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("watchdog file not found: %s", sourcePath)
		}
		if os.IsPermission(err) {
			return "", fmt.Errorf("watchdog file is not readable: %s", sourcePath)
		}
		return "", fmt.Errorf("failed to open watchdog file: %w", err)
	}
	defer func() { _ = file.Close() }()

	fileInfo, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat watchdog file: %w", err)
	}
	if !fileInfo.Mode().IsRegular() {
		return "", fmt.Errorf("watchdog path is not a regular file: %s", sourcePath)
	}
	if age := time.Since(fileInfo.ModTime()); age > maxWatchdogFileAge {
		return "", fmt.Errorf("watchdog file is too old (%.1f hours), refusing stale export", age.Hours())
	}

	// Check file permissions
	if fileInfo.Mode().Perm()&0o400 == 0 {
		return "", fmt.Errorf("watchdog file is not readable: %s", sourcePath)
	}

	// Check file size
	maxBytes := int64(r.maxSizeMB) * 1024 * 1024
	if fileInfo.Size() > maxBytes {
		return "", fmt.Errorf("watchdog file exceeds maximum size of %dMB (size: %.2fMB)",
			r.maxSizeMB, float64(fileInfo.Size())/1024/1024)
	}

	// Read file content
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read watchdog file: %w", err)
	}
	if int64(len(content)) > maxBytes {
		return "", fmt.Errorf("watchdog file exceeded maximum size of %dMB while being read", r.maxSizeMB)
	}

	contentStr := string(content)

	if r.format != FormatJSON {
		return "", fmt.Errorf("unsupported watchdog format: %s", r.format)
	}
	entries, err := r.parseJSON(contentStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse watchdog content: %w", err)
	}

	// Format entries for analysis
	formattedContent := r.formatEntriesForAnalysis(entries)

	// Validate content
	if err := r.Validate(formattedContent); err != nil {
		return "", fmt.Errorf("watchdog content validation failed: %w", err)
	}

	// Apply preprocessing if enabled and needed
	if r.enablePreprocessing && r.preprocessor.ShouldProcess(formattedContent, r.maxTokens) {
		processedContent, err := r.preprocessor.Process(formattedContent)
		if err != nil {
			return "", fmt.Errorf("preprocessing failed: %w", err)
		}
		return processedContent, nil
	}

	return formattedContent, nil
}

// Validate implements analyzer.LogReader.Validate.
// Performs basic validation on watchdog content.
func (r *Reader) Validate(content string) error {
	if len(content) == 0 {
		return fmt.Errorf("watchdog content is empty")
	}

	// NoEntriesContent is a valid state - no entries for the time period
	if IsNoEntriesContent(content) {
		return nil
	}

	// Check for minimal expected content
	if len(content) < 50 {
		return fmt.Errorf("watchdog content seems too small to be valid (only %d bytes)", len(content))
	}

	return nil
}

// GetSourceInfo implements analyzer.LogReader.GetSourceInfo.
// Returns metadata about the watchdog file.
func (r *Reader) GetSourceInfo(sourcePath string) (map[string]any, error) {
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return nil, err
	}

	info := map[string]any{
		"size_bytes": fileInfo.Size(),
		"size_mb":    float64(fileInfo.Size()) / 1024 / 1024,
		"modified":   fileInfo.ModTime(),
		"age_hours":  time.Since(fileInfo.ModTime()).Hours(),
		"format":     string(r.format),
	}

	return info, nil
}

// parseJSON parses arrays, single entries, Drush's wid-keyed object map, and
// newline-delimited entries. Structurally valid but entry-less objects are
// rejected so an unexpected Drush response cannot be reported as a clean day.
func (r *Reader) parseJSON(content string) ([]WatchdogEntry, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("failed to parse JSON: content is empty")
	}

	var entries []WatchdogEntry
	if strings.HasPrefix(content, "[") {
		if err := json.Unmarshal([]byte(content), &entries); err != nil {
			return nil, fmt.Errorf("failed to parse JSON array: %w", err)
		}
		if err := validateWatchdogEntries(entries); err != nil {
			return nil, err
		}
		return entries, nil
	}

	if strings.HasPrefix(content, "{") {
		if objectEntries, ok, err := parseWatchdogJSONObject(content); ok {
			return objectEntries, err
		}
	}

	return parseWatchdogNDJSON(content)
}

func parseWatchdogJSONObject(content string) ([]WatchdogEntry, bool, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &object); err != nil {
		return nil, false, nil
	}
	if len(object) == 0 {
		return nil, true, fmt.Errorf("failed to parse JSON: object contains no watchdog entries")
	}
	if _, isEntry := object["wid"]; isEntry {
		var entry WatchdogEntry
		if err := json.Unmarshal([]byte(content), &entry); err != nil {
			return nil, true, fmt.Errorf("failed to parse watchdog entry: %w", err)
		}
		if err := validateWatchdogEntry(entry); err != nil {
			return nil, true, err
		}
		return []WatchdogEntry{entry}, true, nil
	}

	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]WatchdogEntry, 0, len(keys))
	for _, key := range keys {
		var entry WatchdogEntry
		if err := json.Unmarshal(object[key], &entry); err != nil {
			return nil, true, fmt.Errorf("invalid Drush watchdog entry %q: %w", key, err)
		}
		if err := validateWatchdogEntry(entry); err != nil {
			return nil, true, fmt.Errorf("invalid Drush watchdog entry %q: %w", key, err)
		}
		entries = append(entries, entry)
	}
	return entries, true, nil
}

func parseWatchdogNDJSON(content string) ([]WatchdogEntry, error) {
	var entries []WatchdogEntry
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 1024), maxNDJSONLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "[" || line == "]" {
			continue
		}
		// Remove trailing comma if present
		line = strings.TrimSuffix(line, ",")

		var entry WatchdogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("failed to parse NDJSON entry: %w", err)
		}
		if err := validateWatchdogEntry(entry); err != nil {
			return nil, fmt.Errorf("invalid NDJSON entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to parse NDJSON: %w", err)
	}

	if len(entries) > 0 {
		return entries, nil
	}

	return nil, fmt.Errorf("failed to parse JSON: no valid entries found")
}

func validateWatchdogEntries(entries []WatchdogEntry) error {
	for i, entry := range entries {
		if err := validateWatchdogEntry(entry); err != nil {
			return fmt.Errorf("invalid watchdog entry %d: %w", i, err)
		}
	}
	return nil
}

func validateWatchdogEntry(entry WatchdogEntry) error {
	if entry.WID <= 0 {
		return fmt.Errorf("wid must be positive")
	}
	if entry.Timestamp <= 0 {
		return fmt.Errorf("timestamp must be positive")
	}
	if strings.TrimSpace(entry.Type) == "" {
		return fmt.Errorf("type is required")
	}
	if strings.TrimSpace(entry.Message) == "" {
		return fmt.Errorf("message is required")
	}
	if entry.Severity < SeverityEmergency || entry.Severity > SeverityDebug {
		return fmt.Errorf("severity must be between %d and %d", SeverityEmergency, SeverityDebug)
	}
	return nil
}

// formatEntriesForAnalysis formats watchdog entries into a readable format for Claude.
func (r *Reader) formatEntriesForAnalysis(entries []WatchdogEntry) string {
	if len(entries) == 0 {
		return NoEntriesContent
	}

	var sb strings.Builder

	// Sort entries by timestamp (newest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp > entries[j].Timestamp
	})

	// Write summary header
	sb.WriteString("=== DRUPAL WATCHDOG LOG ANALYSIS ===\n\n")

	// Calculate statistics
	stats := r.calculateStats(entries)
	sb.WriteString("## Summary Statistics\n")
	fmt.Fprintf(&sb, "Total entries: %d\n", stats["total"])
	fmt.Fprintf(&sb, "Time range: %s to %s\n", stats["oldest"], stats["newest"])
	sb.WriteString("\n")

	// Severity breakdown
	sb.WriteString("## Severity Breakdown\n")
	severityCounts := stats["severity_counts"].(map[string]int)
	for _, sev := range []string{"emergency", "alert", "critical", "error", "warning", "notice", "info", "debug"} {
		if count, ok := severityCounts[sev]; ok && count > 0 {
			fmt.Fprintf(&sb, "- %s: %d\n", strings.ToUpper(sev), count)
		}
	}
	sb.WriteString("\n")

	// Type breakdown
	sb.WriteString("## Entry Types\n")
	typeCounts := stats["type_counts"].(map[string]int)
	// Sort types by count
	type typeCount struct {
		name  string
		count int
	}
	var sortedTypes []typeCount
	for name, count := range typeCounts {
		sortedTypes = append(sortedTypes, typeCount{name, count})
	}
	sort.Slice(sortedTypes, func(i, j int) bool {
		return sortedTypes[i].count > sortedTypes[j].count
	})
	for _, tc := range sortedTypes {
		fmt.Fprintf(&sb, "- %s: %d\n", tc.name, tc.count)
	}
	sb.WriteString("\n")

	// Critical and error entries (full detail)
	criticalEntries := r.filterBySeverity(entries, SeverityEmergency, SeverityError)
	if len(criticalEntries) > 0 {
		// Severity outranks recency inside the critical section. Without this,
		// a large burst of newer errors can consume the prompt budget before an
		// older emergency or alert is ever exposed to the model.
		sort.SliceStable(criticalEntries, func(i, j int) bool {
			if criticalEntries[i].Severity != criticalEntries[j].Severity {
				return criticalEntries[i].Severity < criticalEntries[j].Severity
			}
			return criticalEntries[i].Timestamp > criticalEntries[j].Timestamp
		})
		sb.WriteString("## Critical/Error Entries (Full Detail)\n")
		for _, entry := range criticalEntries {
			sb.WriteString(r.formatEntry(entry))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Warning entries (summarized)
	warningEntries := r.filterBySeverity(entries, SeverityWarning, SeverityWarning)
	if len(warningEntries) > 0 {
		sb.WriteString("## Warning Entries\n")
		// Group by type and message pattern
		warningGroups := r.groupByPattern(warningEntries)
		for _, pattern := range sortedGroupPatterns(warningGroups) {
			group := warningGroups[pattern]
			fmt.Fprintf(&sb, "- [%dx] %s: %s (sources: %s)\n",
				len(group), group[0].Type, pattern, strings.Join(groupOrigins(group), ", "))
		}
		sb.WriteString("\n")
	}

	// Access denied entries (security relevant)
	accessDenied := r.filterByType(entries, "access denied", "access")
	if len(accessDenied) > 0 {
		sb.WriteString("## Access/Permission Events\n")
		accessGroups := r.groupByPattern(accessDenied)
		for _, pattern := range sortedGroupPatterns(accessGroups) {
			group := accessGroups[pattern]
			fmt.Fprintf(&sb, "- [%dx] %s (sources: %s)\n",
				len(group), pattern, strings.Join(groupOrigins(group), ", "))
		}
		sb.WriteString("\n")
	}

	// Page not found (404) summary
	notFound := r.filterByType(entries, "page not found")
	if len(notFound) > 0 {
		sb.WriteString("## Page Not Found (404) Summary\n")
		fmt.Fprintf(&sb, "Total 404 errors: %d\n", len(notFound))
		notFoundGroups := r.groupByPattern(notFound)
		count := 0
		for _, pattern := range sortedGroupPatterns(notFoundGroups) {
			group := notFoundGroups[pattern]
			if count >= 10 {
				fmt.Fprintf(&sb, "... and %d more unique 404 patterns\n", len(notFoundGroups)-10)
				break
			}
			fmt.Fprintf(&sb, "- [%dx] %s\n", len(group), pattern)
			count++
		}
		sb.WriteString("\n")
	}

	// Recent info/notice entries (sample)
	infoEntries := r.filterBySeverity(entries, SeverityNotice, SeverityInfo)
	if len(infoEntries) > 0 {
		sb.WriteString("## Recent Notice/Info Entries (Sample)\n")
		for i, entry := range infoEntries {
			if i >= 20 { // Only show first 20
				fmt.Fprintf(&sb, "... and %d more notice/info entries\n", len(infoEntries)-20)
				break
			}
			fmt.Fprintf(&sb, "- [%s] %s: %s\n",
				entry.SeverityName(),
				entry.Type,
				r.truncateMessage(entry.Message, 100))
		}
	}

	return sb.String()
}

// calculateStats calculates statistics about the entries.
func (r *Reader) calculateStats(entries []WatchdogEntry) map[string]any {
	stats := make(map[string]any)
	stats["total"] = len(entries)

	if len(entries) == 0 {
		return stats
	}

	// Find time range
	oldest := entries[0].Timestamp
	newest := entries[0].Timestamp
	for _, e := range entries {
		if e.Timestamp < oldest {
			oldest = e.Timestamp
		}
		if e.Timestamp > newest {
			newest = e.Timestamp
		}
	}
	stats["oldest"] = time.Unix(oldest, 0).Format(timeFormatDateTime)
	stats["newest"] = time.Unix(newest, 0).Format(timeFormatDateTime)

	// Count by severity
	severityCounts := make(map[string]int)
	for _, e := range entries {
		severityCounts[e.SeverityName()]++
	}
	stats["severity_counts"] = severityCounts

	// Count by type
	typeCounts := make(map[string]int)
	for _, e := range entries {
		typeCounts[e.Type]++
	}
	stats["type_counts"] = typeCounts

	return stats
}

// filterBySeverity filters entries by severity range (inclusive).
func (r *Reader) filterBySeverity(entries []WatchdogEntry, minSev, maxSev int) []WatchdogEntry {
	var filtered []WatchdogEntry
	for _, e := range entries {
		if e.Severity >= minSev && e.Severity <= maxSev {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// filterByType filters entries by type (case-insensitive, partial match).
func (r *Reader) filterByType(entries []WatchdogEntry, types ...string) []WatchdogEntry {
	var filtered []WatchdogEntry
	for _, e := range entries {
		for _, t := range types {
			if strings.Contains(strings.ToLower(e.Type), strings.ToLower(t)) {
				filtered = append(filtered, e)
				break
			}
		}
	}
	return filtered
}

// formatEntry formats a single entry for display.
func (r *Reader) formatEntry(entry WatchdogEntry) string {
	return fmt.Sprintf("[%s] %s | %s | %s | %s\n  Message: %s",
		entry.Time().Format(timeFormatDateTime),
		strings.ToUpper(entry.SeverityName()),
		entry.Type,
		entry.Hostname,
		entry.Location,
		entry.Message)
}

// groupByPattern groups entries by normalized message pattern.
func (r *Reader) groupByPattern(entries []WatchdogEntry) map[string][]WatchdogEntry {
	groups := make(map[string][]WatchdogEntry)
	for _, e := range entries {
		pattern := r.normalizeMessage(e.Message)
		groups[pattern] = append(groups[pattern], e)
	}
	return groups
}

func sortedGroupPatterns(groups map[string][]WatchdogEntry) []string {
	patterns := make([]string, 0, len(groups))
	for pattern := range groups {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	return patterns
}

func groupOrigins(entries []WatchdogEntry) []string {
	origins := make(map[string]struct{})
	for _, entry := range entries {
		if host := strings.TrimSpace(entry.Hostname); host != "" {
			origins[host] = struct{}{}
		}
		for _, address := range ipv4AddressPattern.FindAllString(entry.Message, -1) {
			origins[address] = struct{}{}
		}
	}
	if len(origins) == 0 {
		return []string{"unknown"}
	}
	result := make([]string, 0, len(origins))
	for origin := range origins {
		result = append(result, origin)
	}
	sort.Strings(result)
	return result
}

// normalizeMessage normalizes a message for pattern grouping.
func (r *Reader) normalizeMessage(msg string) string {
	// Truncate long messages
	if len(msg) > 80 {
		msg = msg[:80] + "..."
	}

	// Replace common variable patterns
	// UUIDs FIRST (before numbers, since UUIDs contain hex digits and numbers)
	msg = regexp.MustCompile(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).ReplaceAllString(msg, "[UUID]")
	// IPs (before numbers)
	msg = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`).ReplaceAllString(msg, "[IP]")
	// Numbers
	msg = regexp.MustCompile(`\b\d+\b`).ReplaceAllString(msg, "[N]")
	// Paths
	msg = regexp.MustCompile(`/[a-zA-Z0-9/_-]+`).ReplaceAllString(msg, "[PATH]")

	return msg
}

// truncateMessage truncates a message to the specified length.
func (r *Reader) truncateMessage(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen-3] + "..."
}
