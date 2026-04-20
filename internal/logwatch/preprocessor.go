// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package logwatch

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

// Compile-time interface check
var _ analyzer.Preprocessor = (*Preprocessor)(nil)
var _ analyzer.BudgetPreprocessor = (*Preprocessor)(nil)

// Preprocessor handles logwatch content preprocessing for large files.
// Implements analyzer.Preprocessor interface.
type Preprocessor struct {
	maxTokens int
}

// Section represents a logwatch section with its priority
type Section struct {
	Name     string
	Content  string
	Priority int // 1=HIGH, 2=MEDIUM, 3=LOW
}

// NewPreprocessor creates a new preprocessor
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
// Returns true if the estimated tokens exceed maxTokens.
func (p *Preprocessor) ShouldProcess(content string, maxTokens int) bool {
	return p.EstimateTokens(content) > maxTokens
}

// Process preprocesses the content to reduce its size while preserving critical information
func (p *Preprocessor) Process(content string) (string, error) {
	return p.processWithMaxTokens(content, p.maxTokens)
}

// ProcessWithBudget preprocesses the content using a dynamic token budget.
func (p *Preprocessor) ProcessWithBudget(content string, maxTokens int) (string, error) {
	return p.processWithMaxTokens(content, maxTokens)
}

func (p *Preprocessor) processWithMaxTokens(content string, maxTokens int) (string, error) {
	if content == "" {
		return "", nil
	}
	if maxTokens <= 0 {
		maxTokens = p.maxTokens
	}
	if p.EstimateTokens(content) <= maxTokens {
		return content, nil
	}

	// Parse sections
	sections := p.parseSections(content)
	if len(sections) == 0 {
		return content, nil // Return original if parsing failed
	}

	// Classify sections by priority
	p.classifySections(sections)

	// Deduplicate and compress sections
	for i := range sections {
		sections[i].Content = p.deduplicateContent(sections[i].Content)
	}

	// Deduplicated content often removes enough repetition on its own.
	deduplicated := p.renderSections(sections, compressionProfile{
		high:   1.0,
		medium: 1.0,
		low:    1.0,
	})
	if p.EstimateTokens(deduplicated) <= maxTokens {
		return deduplicated, nil
	}

	// Apply progressively more aggressive section compression until we fit.
	profiles := []compressionProfile{
		{high: 1.0, medium: 0.5, low: 0.2},
		{high: 0.85, medium: 0.35, low: 0.1},
		{high: 0.7, medium: 0.2, low: 0.05},
		{high: 0.5, medium: 0.1, low: 0.02},
	}

	for _, profile := range profiles {
		candidate := p.renderSections(sections, profile)
		if p.EstimateTokens(candidate) <= maxTokens {
			return candidate, nil
		}
	}

	aggressive := p.aggressiveCompress(sections)
	if p.EstimateTokens(aggressive) <= maxTokens {
		return aggressive, nil
	}

	return p.trimToTokenBudget(aggressive, maxTokens), nil
}

// parseSections parses logwatch output into sections
func (p *Preprocessor) parseSections(content string) []*Section {
	var sections []*Section

	// Split by section headers (lines with multiple # characters)
	sectionRegex := regexp.MustCompile(`(?m)^#{3,}\s*(.+?)\s*#{3,}$`)
	matches := sectionRegex.FindAllStringSubmatchIndex(content, -1)

	if len(matches) == 0 {
		// No sections found, treat entire content as one section
		return []*Section{{
			Name:     "General",
			Content:  content,
			Priority: 1,
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

		sections = append(sections, &Section{
			Name:    strings.TrimSpace(sectionName),
			Content: sectionContent,
		})
	}

	return sections
}

// classifySections assigns priority to sections based on their content
func (p *Preprocessor) classifySections(sections []*Section) {
	for _, section := range sections {
		section.Priority = p.determinePriority(section.Name, section.Content)
	}
}

// determinePriority determines section priority based on name and content
func (p *Preprocessor) determinePriority(name, content string) int {
	nameLower := strings.ToLower(name)
	contentLower := strings.ToLower(content)

	// HIGH priority keywords
	highPriority := []string{
		"ssh", "security", "auth", "fail", "error", "critical",
		"kernel", "panic", "segfault", "oom", "firewall",
		"sudo", "root", "unauthorized", "denied",
	}

	for _, keyword := range highPriority {
		if strings.Contains(nameLower, keyword) || strings.Contains(contentLower, keyword) {
			return 1 // HIGH
		}
	}

	// MEDIUM priority keywords
	mediumPriority := []string{
		"network", "disk", "mount", "service", "daemon",
		"warning", "load", "memory", "cpu",
	}

	for _, keyword := range mediumPriority {
		if strings.Contains(nameLower, keyword) {
			return 2 // MEDIUM
		}
	}

	// Everything else is LOW priority
	return 3 // LOW
}

// deduplicateContent deduplicates similar log lines and groups them
func (p *Preprocessor) deduplicateContent(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= 10 {
		return content // Too small to deduplicate
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

	// Rebuild content with grouped duplicates
	var result strings.Builder
	processed := make(map[string]bool)

	for _, line := range lines {
		normalized := p.normalizeLine(line)
		if normalized == "" {
			result.WriteString(line + "\n")
			continue
		}

		if processed[normalized] {
			continue
		}
		processed[normalized] = true

		count := lineCounts[normalized]
		if count > 1 {
			fmt.Fprintf(&result, "%s (occurred %d times)\n", lineExamples[normalized], count)
		} else {
			result.WriteString(line + "\n")
		}
	}

	return result.String()
}

// normalizeLine normalizes a log line for deduplication
func (p *Preprocessor) normalizeLine(line string) string {
	// Remove timestamps, IPs, and numbers for grouping similar messages
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	// Replace IPs with placeholder
	ipRegex := regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	line = ipRegex.ReplaceAllString(line, "IP")

	// Replace timestamps
	timestampRegex := regexp.MustCompile(`\b\d{1,2}:\d{2}:\d{2}\b`)
	line = timestampRegex.ReplaceAllString(line, "TIME")

	// Replace dates
	dateRegex := regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b|\b\d{2}/\d{2}/\d{4}\b`)
	line = dateRegex.ReplaceAllString(line, "DATE")

	// Replace numbers
	numberRegex := regexp.MustCompile(`\b\d+\b`)
	line = numberRegex.ReplaceAllString(line, "N")

	return line
}

// compressByPriority compresses section content based on its priority
func (p *Preprocessor) compressByPriority(section *Section) string {
	var keepRatio float64
	switch section.Priority {
	case 1: // HIGH - keep all
		keepRatio = 1.0
	case 2: // MEDIUM - keep 50%
		keepRatio = 0.5
	case 3: // LOW - keep 20%
		keepRatio = 0.2
	default:
		keepRatio = 0.5
	}

	return p.compressContent(section.Content, keepRatio)
}

type compressionProfile struct {
	high   float64
	medium float64
	low    float64
}

func (p *Preprocessor) renderSections(sections []*Section, profile compressionProfile) string {
	var result strings.Builder
	for _, section := range sections {
		compressedContent := p.compressContent(section.Content, p.keepRatioForPriority(section.Priority, profile))
		fmt.Fprintf(&result, "\n################### %s ###################\n", section.Name)
		result.WriteString(compressedContent)
		result.WriteString("\n")
	}
	return result.String()
}

func (p *Preprocessor) keepRatioForPriority(priority int, profile compressionProfile) float64 {
	switch priority {
	case 1:
		return profile.high
	case 2:
		return profile.medium
	case 3:
		return profile.low
	default:
		return profile.medium
	}
}

func (p *Preprocessor) compressContent(content string, keepRatio float64) string {
	lines := strings.Split(content, "\n")

	if keepRatio >= 1.0 {
		return content
	}

	// Calculate how many lines to keep
	keepCount := int(math.Ceil(float64(len(lines)) * keepRatio))
	if keepCount <= 0 {
		keepCount = 1
	}

	// Keep first N lines and add summary
	var result strings.Builder
	for i := 0; i < keepCount && i < len(lines); i++ {
		result.WriteString(lines[i] + "\n")
	}

	if keepCount < len(lines) {
		fmt.Fprintf(&result, "\n[... %d more lines omitted for brevity ...]\n", len(lines)-keepCount)
	}

	return result.String()
}

func (p *Preprocessor) aggressiveCompress(sections []*Section) string {
	var result strings.Builder

	for _, section := range sections {
		fmt.Fprintf(&result, "\n################### %s ###################\n", section.Name)
		result.WriteString(p.extractEssentialLines(section))
		result.WriteString("\n")
	}

	return result.String()
}

func (p *Preprocessor) extractEssentialLines(section *Section) string {
	lines := strings.Split(section.Content, "\n")
	if len(lines) <= 6 {
		return section.Content
	}

	var essential []string
	seen := make(map[string]bool)

	appendLine := func(line string) {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			return
		}

		key := p.normalizeLine(line)
		if key == "" {
			key = line
		}
		if seen[key] {
			return
		}

		seen[key] = true
		essential = append(essential, line)
	}

	for i := 0; i < len(lines) && i < 2; i++ {
		appendLine(lines[i])
	}

	for _, line := range lines {
		if p.isEssentialLine(line) {
			appendLine(line)
		}
	}

	startLast := max(len(lines)-2, 0)
	for i := startLast; i < len(lines); i++ {
		appendLine(lines[i])
	}

	if len(essential) == 0 {
		for i := 0; i < len(lines) && i < 3; i++ {
			appendLine(lines[i])
		}
	}

	omitted := len(lines) - len(essential)
	if omitted > 0 {
		essential = append(essential, fmt.Sprintf("[... %d more lines omitted for brevity ...]", omitted))
	}

	return strings.Join(essential, "\n")
}

func (p *Preprocessor) isEssentialLine(line string) bool {
	lineLower := strings.ToLower(line)
	if strings.TrimSpace(lineLower) == "" {
		return false
	}

	keywords := []string{
		"error", "fail", "failed", "critical", "panic", "denied", "unauthorized",
		"security", "sudo", "root", "ssh", "warning", "disk", "memory", "cpu",
		"load", "network", "service", "daemon", "kernel", "oom", "%",
	}

	for _, keyword := range keywords {
		if strings.Contains(lineLower, keyword) {
			return true
		}
	}

	return false
}

func (p *Preprocessor) trimToTokenBudget(content string, maxTokens int) string {
	if maxTokens <= 0 || p.EstimateTokens(content) <= maxTokens {
		return content
	}

	lines := strings.Split(content, "\n")
	truncationNotice := "[... truncated to fit token budget ...]"

	low := 0
	high := len(lines)

	for low < high {
		mid := (low + high + 1) / 2
		candidate := strings.Join(lines[:mid], "\n")
		if mid < len(lines) {
			candidate += "\n" + truncationNotice
		}

		if p.EstimateTokens(candidate) <= maxTokens {
			low = mid
		} else {
			high = mid - 1
		}
	}

	if low == 0 {
		if p.EstimateTokens(truncationNotice) <= maxTokens {
			return truncationNotice
		}
		return ""
	}

	result := strings.Join(lines[:low], "\n")
	if low < len(lines) {
		candidate := result + "\n" + truncationNotice
		if p.EstimateTokens(candidate) <= maxTokens {
			return candidate
		}
	}

	return result
}
