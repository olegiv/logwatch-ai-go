package drupal

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

// Compile-time interface check
var _ analyzer.Preprocessor = (*Preprocessor)(nil)

// Preprocessor handles Drupal watchdog content preprocessing for large logs.
// Implements analyzer.Preprocessor interface.
type Preprocessor struct {
	maxTokens int
}

// NewPreprocessor creates a new Drupal preprocessor.
func NewPreprocessor(maxTokens int) *Preprocessor {
	return &Preprocessor{
		maxTokens: maxTokens,
	}
}

// EstimateTokens estimates the number of tokens in the content.
// Delegates to the shared analyzer.EstimateTokens function.
func (p *Preprocessor) EstimateTokens(content string) int {
	return analyzer.EstimateTokens(content)
}

// ShouldProcess determines if preprocessing is needed based on token count.
func (p *Preprocessor) ShouldProcess(content string, maxTokens int) bool {
	return p.EstimateTokens(content) > maxTokens
}

// Process preprocesses the content to reduce size while preserving critical info.
// Drupal-specific preprocessing focuses on:
// - Keeping all critical/error severity entries
// - Summarizing warning entries
// - Sampling info/notice entries
// - Grouping similar messages
func (p *Preprocessor) Process(content string) (string, error) {
	// Return empty content as-is
	if content == "" {
		return "", nil
	}

	// Parse sections from the formatted content
	sections := p.parseSections(content)
	if len(sections) == 0 {
		return content, nil
	}

	// Classify and process sections
	var result strings.Builder

	for _, section := range sections {
		priority := p.determinePriority(section.name, section.content)
		processedContent := p.compressByPriority(section, priority)
		if processedContent != "" {
			result.WriteString(fmt.Sprintf("\n## %s\n", section.name))
			result.WriteString(processedContent)
			result.WriteString("\n")
		}
	}

	processed := result.String()

	// If still too large, apply more aggressive compression
	if p.EstimateTokens(processed) > p.maxTokens {
		processed = p.aggressiveCompress(processed)
	}

	return processed, nil
}

// section represents a section of the formatted content.
type section struct {
	name    string
	content string
}

// parseSections parses the formatted content into sections.
func (p *Preprocessor) parseSections(content string) []section {
	var sections []section

	// Split by section headers (## or ===)
	sectionRegex := regexp.MustCompile(`(?m)^(?:##|===)\s*(.+?)\s*(?:===)?$`)
	matches := sectionRegex.FindAllStringSubmatchIndex(content, -1)

	if len(matches) == 0 {
		// No sections found, treat entire content as one section
		return []section{{
			name:    "General",
			content: content,
		}}
	}

	for i, match := range matches {
		sectionName := content[match[2]:match[3]]
		startIdx := match[1]

		var endIdx int
		if i+1 < len(matches) {
			endIdx = matches[i+1][0]
		} else {
			endIdx = len(content)
		}

		sectionContent := strings.TrimSpace(content[startIdx:endIdx])

		sections = append(sections, section{
			name:    strings.TrimSpace(sectionName),
			content: sectionContent,
		})
	}

	return sections
}

// Priority levels for Drupal content.
const (
	priorityHigh   = 1
	priorityMedium = 2
	priorityLow    = 3
)

// Drupal-specific high priority keywords.
var drupalHighPriorityKeywords = []string{
	// Security
	"security", "access denied", "permission", "unauthorized", "forbidden",
	"login failed", "authentication", "csrf", "xss", "sql injection",
	"brute force", "blocked", "banned",

	// Critical errors
	"critical", "emergency", "alert", "error", "exception", "fatal",
	"pdoexception", "database", "mysql", "connection refused",
	"out of memory", "segfault", "core dump",

	// System issues
	"cron failed", "queue failed", "batch failed",
	"update failed", "migration failed",
}

// Drupal-specific medium priority keywords.
var drupalMediumPriorityKeywords = []string{
	// Warnings
	"warning", "deprecated", "notice",

	// Functional issues
	"cron", "queue", "batch", "cache", "session",
	"module", "theme", "update", "migration",

	// Performance
	"slow", "timeout", "memory", "performance",
}

// determinePriority determines section priority based on name and content.
func (p *Preprocessor) determinePriority(name, content string) int {
	nameLower := strings.ToLower(name)
	contentLower := strings.ToLower(content)

	// Check high priority keywords
	for _, keyword := range drupalHighPriorityKeywords {
		if strings.Contains(nameLower, keyword) || strings.Contains(contentLower, keyword) {
			return priorityHigh
		}
	}

	// Check medium priority keywords
	for _, keyword := range drupalMediumPriorityKeywords {
		if strings.Contains(nameLower, keyword) {
			return priorityMedium
		}
	}

	// Everything else is low priority
	return priorityLow
}

// compressByPriority compresses section content based on its priority.
func (p *Preprocessor) compressByPriority(s section, priority int) string {
	lines := strings.Split(s.content, "\n")

	var keepRatio float64
	switch priority {
	case priorityHigh:
		keepRatio = 1.0 // Keep all
	case priorityMedium:
		keepRatio = 0.5 // Keep 50%
	case priorityLow:
		keepRatio = 0.2 // Keep 20%
	default:
		keepRatio = 0.5
	}

	if keepRatio >= 1.0 {
		return s.content
	}

	// Deduplicate before sampling
	deduped := p.deduplicateLines(lines)

	// Calculate how many lines to keep
	keepCount := int(math.Ceil(float64(len(deduped)) * keepRatio))
	if keepCount <= 0 {
		keepCount = 1
	}

	var result strings.Builder
	for i := 0; i < keepCount && i < len(deduped); i++ {
		result.WriteString(deduped[i] + "\n")
	}

	if keepCount < len(deduped) {
		result.WriteString(fmt.Sprintf("\n[... %d similar entries omitted ...]\n", len(deduped)-keepCount))
	}

	return result.String()
}

// deduplicateLines groups similar lines and shows counts.
func (p *Preprocessor) deduplicateLines(lines []string) []string {
	if len(lines) <= 5 {
		return lines
	}

	// Count occurrences of similar lines
	lineCounts := make(map[string]int)
	lineExamples := make(map[string]string)

	for _, line := range lines {
		normalized := p.normalizeLine(line)
		if normalized == "" {
			continue
		}
		lineCounts[normalized]++
		if lineExamples[normalized] == "" {
			lineExamples[normalized] = line
		}
	}

	// Rebuild with grouped duplicates
	var result []string
	processed := make(map[string]bool)

	for _, line := range lines {
		normalized := p.normalizeLine(line)
		if normalized == "" {
			result = append(result, line)
			continue
		}

		if processed[normalized] {
			continue
		}
		processed[normalized] = true

		count := lineCounts[normalized]
		if count > 1 {
			result = append(result, fmt.Sprintf("%s (x%d)", lineExamples[normalized], count))
		} else {
			result = append(result, line)
		}
	}

	return result
}

// normalizeLine normalizes a line for deduplication.
func (p *Preprocessor) normalizeLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	// Replace UUIDs FIRST (before numbers, since UUIDs contain numbers)
	line = regexp.MustCompile(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).ReplaceAllString(line, "UUID")

	// Replace IPs
	line = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`).ReplaceAllString(line, "IP")

	// Replace timestamps
	line = regexp.MustCompile(`\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}`).ReplaceAllString(line, "TIMESTAMP")
	line = regexp.MustCompile(`\b\d{2}:\d{2}:\d{2}\b`).ReplaceAllString(line, "TIME")

	// Replace numbers with unit suffixes (e.g., 500ms, 10KB, 5s)
	line = regexp.MustCompile(`\b\d+([a-zA-Z]+)\b`).ReplaceAllString(line, "N$1")

	// Replace standalone numbers
	line = regexp.MustCompile(`\b\d+\b`).ReplaceAllString(line, "N")

	return line
}

// aggressiveCompress applies more aggressive compression when content is still too large.
func (p *Preprocessor) aggressiveCompress(content string) string {
	lines := strings.Split(content, "\n")

	// Keep only essential lines
	var essential []string
	for _, line := range lines {
		lineLower := strings.ToLower(line)

		// Keep section headers
		if strings.HasPrefix(line, "##") || strings.HasPrefix(line, "===") {
			essential = append(essential, line)
			continue
		}

		// Keep lines with critical keywords
		isCritical := false
		for _, kw := range []string{"error", "critical", "emergency", "alert", "security", "failed", "exception"} {
			if strings.Contains(lineLower, kw) {
				isCritical = true
				break
			}
		}
		if isCritical {
			essential = append(essential, line)
		}
	}

	// If still too many lines, limit to first N
	maxLines := p.maxTokens / 10 // Rough estimate
	if len(essential) > maxLines {
		essential = essential[:maxLines]
		essential = append(essential, fmt.Sprintf("\n[... truncated to %d lines due to size limits ...]", maxLines))
	}

	return strings.Join(essential, "\n")
}
