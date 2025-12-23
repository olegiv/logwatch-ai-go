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

	// Calculate target sizes based on priority
	totalTokens := 0
	for _, section := range sections {
		totalTokens += p.EstimateTokens(section.Content)
	}

	if totalTokens <= p.maxTokens {
		return content, nil // No compression needed
	}

	// Compress sections based on priority
	var result strings.Builder
	for _, section := range sections {
		compressedContent := p.compressByPriority(section)
		result.WriteString(fmt.Sprintf("\n################### %s ###################\n", section.Name))
		result.WriteString(compressedContent)
		result.WriteString("\n")
	}

	return result.String(), nil
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
			result.WriteString(fmt.Sprintf("%s (occurred %d times)\n", lineExamples[normalized], count))
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
	lines := strings.Split(section.Content, "\n")

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

	if keepRatio >= 1.0 {
		return section.Content
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
		result.WriteString(fmt.Sprintf("\n[... %d more lines omitted for brevity ...]\n", len(lines)-keepCount))
	}

	return result.String()
}
