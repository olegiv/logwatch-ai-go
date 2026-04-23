// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ocms

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

// Compile-time interface checks
var (
	_ analyzer.Preprocessor       = (*Preprocessor)(nil)
	_ analyzer.BudgetPreprocessor = (*Preprocessor)(nil)
)

var (
	levelRegex     = regexp.MustCompile(`\blevel=([A-Za-z]+)`)
	ipRegex        = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	timeFieldRegex = regexp.MustCompile(`\btime=\S+`)
	numberRegex    = regexp.MustCompile(`\b\d+\b`)
)

// Priority order: lower index = higher priority. Error is the most
// important bucket; unknown captures non-slog lines (stack traces, etc.)
// that should stay attached to the preceding record.
const (
	priorityError = iota
	priorityWarn
	priorityInfo
	priorityDebug
	priorityUnknown
	numBuckets
)

// Preprocessor compresses OCMS slog content to fit a token budget.
// Implements analyzer.Preprocessor and analyzer.BudgetPreprocessor.
type Preprocessor struct {
	maxTokens int
}

// NewPreprocessor creates a new OCMS preprocessor.
func NewPreprocessor(maxTokens int) *Preprocessor {
	return &Preprocessor{maxTokens: maxTokens}
}

// EstimateTokens delegates to analyzer.EstimateTokens for consistency with
// the other packages.
func (p *Preprocessor) EstimateTokens(content string) int {
	return analyzer.EstimateTokens(content)
}

// ShouldProcess returns true when content exceeds maxTokens.
func (p *Preprocessor) ShouldProcess(content string, maxTokens int) bool {
	return p.EstimateTokens(content) > maxTokens
}

// Process preprocesses content using the configured token budget.
func (p *Preprocessor) Process(content string) (string, error) {
	return p.processWithMaxTokens(content, p.maxTokens)
}

// ProcessWithBudget preprocesses content using a dynamic token budget.
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

	buckets := p.bucketByLevel(content)

	// Deduplicate within each bucket first; this alone typically reclaims
	// enough budget for info-heavy logs with repetitive request tracing.
	for i := range buckets {
		buckets[i] = dedupLines(buckets[i])
	}

	rendered := renderBuckets(buckets)
	if p.EstimateTokens(rendered) <= maxTokens {
		return rendered, nil
	}

	// Drop lower-priority buckets in order until we fit. Error and warn are
	// preserved whenever possible because they carry the highest signal.
	for _, drop := range []int{priorityDebug, priorityInfo, priorityUnknown} {
		buckets[drop] = nil
		rendered = renderBuckets(buckets)
		if p.EstimateTokens(rendered) <= maxTokens {
			return rendered, nil
		}
	}

	return p.trimToTokenBudget(rendered, maxTokens), nil
}

// bucketByLevel splits content into priority buckets. Lines without a
// recognizable level (stack traces, continuation lines, non-slog output)
// are appended to the previous record's bucket so multi-line records stay
// together.
func (p *Preprocessor) bucketByLevel(content string) [][]string {
	buckets := make([][]string, numBuckets)
	currentBucket := priorityUnknown
	for _, line := range strings.Split(content, "\n") {
		if line == "" {
			continue
		}
		if m := levelRegex.FindStringSubmatch(line); m != nil {
			currentBucket = priorityForLevel(m[1])
		}
		buckets[currentBucket] = append(buckets[currentBucket], line)
	}
	return buckets
}

func priorityForLevel(level string) int {
	switch strings.ToLower(level) {
	case "error", "err", "fatal":
		return priorityError
	case "warn", "warning":
		return priorityWarn
	case "info":
		return priorityInfo
	case "debug", "trace":
		return priorityDebug
	default:
		return priorityUnknown
	}
}

// dedupLines collapses near-duplicate lines (same shape after normalizing
// timestamps, IPs, and numbers) into a single example with an occurrence
// counter. Order of first appearance is preserved.
func dedupLines(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	type entry struct {
		example string
		count   int
	}
	order := make([]string, 0, len(lines))
	seen := make(map[string]*entry, len(lines))
	for _, line := range lines {
		key := normalizeLine(line)
		if e, ok := seen[key]; ok {
			e.count++
			continue
		}
		seen[key] = &entry{example: line, count: 1}
		order = append(order, key)
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		e := seen[key]
		if e.count > 1 {
			out = append(out, fmt.Sprintf("%s (occurred %d times)", e.example, e.count))
		} else {
			out = append(out, e.example)
		}
	}
	return out
}

func normalizeLine(line string) string {
	line = timeFieldRegex.ReplaceAllString(line, "time=T")
	line = ipRegex.ReplaceAllString(line, "IP")
	line = numberRegex.ReplaceAllString(line, "N")
	return strings.TrimSpace(line)
}

func renderBuckets(buckets [][]string) string {
	labels := [numBuckets]string{
		priorityError:   "ERROR",
		priorityWarn:    "WARN",
		priorityInfo:    "INFO",
		priorityDebug:   "DEBUG",
		priorityUnknown: "OTHER",
	}
	var b strings.Builder
	for i := 0; i < numBuckets; i++ {
		if len(buckets[i]) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n################### %s ###################\n", labels[i])
		b.WriteString(strings.Join(buckets[i], "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

// trimToTokenBudget uses binary search over line count to find the
// largest prefix that fits within maxTokens, appending a truncation notice.
func (p *Preprocessor) trimToTokenBudget(content string, maxTokens int) string {
	if maxTokens <= 0 || p.EstimateTokens(content) <= maxTokens {
		return content
	}
	lines := strings.Split(content, "\n")
	const truncationNotice = "[... truncated to fit token budget ...]"

	low, high := 0, len(lines)
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
