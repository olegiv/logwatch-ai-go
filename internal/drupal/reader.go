package drupal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

// NoEntriesContent is returned when the watchdog file contains no log entries.
// This is a valid state - it means there were no entries for the time period.
// Use IsNoEntriesContent() to check for this condition.
const NoEntriesContent = "=== NO WATCHDOG ENTRIES ===\n\nNo Drupal watchdog entries were found for the analyzed time period.\nThis typically means the system had no logged events during this period."

// timeFormatDateTime is the standard date-time format for watchdog entries.
const timeFormatDateTime = "2006-01-02 15:04:05"

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

	// FormatDrush is for drush watchdog-show output
	FormatDrush InputFormat = "drush"
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
	// Check file exists and get info
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("watchdog file not found: %s", sourcePath)
		}
		return "", fmt.Errorf("failed to stat watchdog file: %w", err)
	}

	// Check file permissions
	if fileInfo.Mode().Perm()&0400 == 0 {
		return "", fmt.Errorf("watchdog file is not readable: %s", sourcePath)
	}

	// Check file size
	maxBytes := int64(r.maxSizeMB) * 1024 * 1024
	if fileInfo.Size() > maxBytes {
		return "", fmt.Errorf("watchdog file exceeds maximum size of %dMB (size: %.2fMB)",
			r.maxSizeMB, float64(fileInfo.Size())/1024/1024)
	}

	// Read file content
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", fmt.Errorf("failed to read watchdog file: %w", err)
	}

	contentStr := string(content)

	// Parse entries based on format
	var entries []WatchdogEntry
	switch r.format {
	case FormatJSON:
		entries, err = r.parseJSON(contentStr)
	case FormatDrush:
		entries, err = r.parseDrush(contentStr)
	default:
		return "", fmt.Errorf("unsupported watchdog format: %s", r.format)
	}

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
func (r *Reader) GetSourceInfo(sourcePath string) (map[string]interface{}, error) {
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return nil, err
	}

	info := map[string]interface{}{
		"size_bytes": fileInfo.Size(),
		"size_mb":    float64(fileInfo.Size()) / 1024 / 1024,
		"modified":   fileInfo.ModTime(),
		"age_hours":  time.Since(fileInfo.ModTime()).Hours(),
		"format":     string(r.format),
	}

	return info, nil
}

// parseJSON parses JSON-formatted watchdog entries.
// Supports both array of entries and single entry.
func (r *Reader) parseJSON(content string) ([]WatchdogEntry, error) {
	content = strings.TrimSpace(content)

	// Try parsing as array first
	var entries []WatchdogEntry
	if err := json.Unmarshal([]byte(content), &entries); err == nil {
		return entries, nil
	}

	// Try parsing as single entry
	var entry WatchdogEntry
	if err := json.Unmarshal([]byte(content), &entry); err == nil {
		return []WatchdogEntry{entry}, nil
	}

	// Try parsing as newline-delimited JSON (NDJSON)
	entries = nil
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "[" || line == "]" {
			continue
		}
		// Remove trailing comma if present
		line = strings.TrimSuffix(line, ",")

		var entry WatchdogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // Skip invalid lines
		}
		entries = append(entries, entry)
	}

	if len(entries) > 0 {
		return entries, nil
	}

	return nil, fmt.Errorf("failed to parse JSON: no valid entries found")
}

// parseDrush parses drush watchdog-show output format.
// Expected format:
//
//	ID      Date                 Type     Severity  Message
//	------- -------------------- -------- --------- ----------------------------------------
//	12345   2024-11-13 10:00:00  php      error     PDOException: SQLSTATE[HY000]...
func (r *Reader) parseDrush(content string) ([]WatchdogEntry, error) {
	var entries []WatchdogEntry

	lines := strings.Split(content, "\n")
	headerPassed := false

	// Regex to parse drush output lines
	// Matches: ID, Date, Type, Severity, Message
	lineRegex := regexp.MustCompile(`^\s*(\d+)\s+(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s+(\S+)\s+(\S+)\s+(.*)$`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip header lines
		if strings.HasPrefix(line, "ID") || strings.HasPrefix(line, "---") {
			headerPassed = true
			continue
		}

		if !headerPassed {
			continue
		}

		matches := lineRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		wid, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil {
			continue // Skip entries with invalid WID
		}

		timestamp, err := time.Parse(timeFormatDateTime, matches[2])
		if err != nil {
			continue // Skip entries with invalid timestamp
		}

		severity := SeverityFromName(strings.ToLower(matches[4]))
		if severity == -1 {
			severity = SeverityNotice // Default
		}

		entry := WatchdogEntry{
			WID:       wid,
			Timestamp: timestamp.Unix(),
			Type:      matches[3],
			Severity:  severity,
			Message:   matches[5],
		}
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no valid entries found in drush output")
	}

	return entries, nil
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
	sb.WriteString(fmt.Sprintf("Total entries: %d\n", stats["total"]))
	sb.WriteString(fmt.Sprintf("Time range: %s to %s\n", stats["oldest"], stats["newest"]))
	sb.WriteString("\n")

	// Severity breakdown
	sb.WriteString("## Severity Breakdown\n")
	severityCounts := stats["severity_counts"].(map[string]int)
	for _, sev := range []string{"emergency", "alert", "critical", "error", "warning", "notice", "info", "debug"} {
		if count, ok := severityCounts[sev]; ok && count > 0 {
			sb.WriteString(fmt.Sprintf("- %s: %d\n", strings.ToUpper(sev), count))
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
		sb.WriteString(fmt.Sprintf("- %s: %d\n", tc.name, tc.count))
	}
	sb.WriteString("\n")

	// Critical and error entries (full detail)
	criticalEntries := r.filterBySeverity(entries, SeverityEmergency, SeverityError)
	if len(criticalEntries) > 0 {
		sb.WriteString("## Critical/Error Entries (Full Detail)\n")
		for i, entry := range criticalEntries {
			if i >= 50 { // Limit to 50 critical entries
				sb.WriteString(fmt.Sprintf("\n... and %d more critical/error entries\n", len(criticalEntries)-50))
				break
			}
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
		for pattern, group := range warningGroups {
			sb.WriteString(fmt.Sprintf("- [%dx] %s: %s\n", len(group), group[0].Type, pattern))
		}
		sb.WriteString("\n")
	}

	// Access denied entries (security relevant)
	accessDenied := r.filterByType(entries, "access denied", "access")
	if len(accessDenied) > 0 {
		sb.WriteString("## Access/Permission Events\n")
		accessGroups := r.groupByPattern(accessDenied)
		for pattern, group := range accessGroups {
			sb.WriteString(fmt.Sprintf("- [%dx] %s\n", len(group), pattern))
		}
		sb.WriteString("\n")
	}

	// Page not found (404) summary
	notFound := r.filterByType(entries, "page not found")
	if len(notFound) > 0 {
		sb.WriteString("## Page Not Found (404) Summary\n")
		sb.WriteString(fmt.Sprintf("Total 404 errors: %d\n", len(notFound)))
		notFoundGroups := r.groupByPattern(notFound)
		count := 0
		for pattern, group := range notFoundGroups {
			if count >= 10 {
				sb.WriteString(fmt.Sprintf("... and %d more unique 404 patterns\n", len(notFoundGroups)-10))
				break
			}
			sb.WriteString(fmt.Sprintf("- [%dx] %s\n", len(group), pattern))
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
				sb.WriteString(fmt.Sprintf("... and %d more notice/info entries\n", len(infoEntries)-20))
				break
			}
			sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n",
				entry.SeverityName(),
				entry.Type,
				r.truncateMessage(entry.Message, 100)))
		}
	}

	return sb.String()
}

// calculateStats calculates statistics about the entries.
func (r *Reader) calculateStats(entries []WatchdogEntry) map[string]interface{} {
	stats := make(map[string]interface{})
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
