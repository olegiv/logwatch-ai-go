// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Analysis represents the structured analysis result from Claude
type Analysis struct {
	SystemStatus    string         `json:"systemStatus"`
	Summary         string         `json:"summary"`
	CriticalIssues  []string       `json:"criticalIssues"`
	Warnings        []string       `json:"warnings"`
	Recommendations []string       `json:"recommendations"`
	Metrics         map[string]any `json:"metrics"`
}

// StringArrayFormatReminder is appended verbatim to every PromptBuilder's
// system prompt. It reinforces the string-array contract for criticalIssues,
// warnings, and recommendations so the LLM is less likely to emit objects
// such as {"description": "..."} that would otherwise trigger coercion.
const StringArrayFormatReminder = `

**CRITICAL FORMAT RULE (strict):**
Each element of "criticalIssues", "warnings", and "recommendations" MUST be
a plain JSON string. Never an object, number, or null.

  CORRECT:   "recommendations": ["Configure certificate trust on smtprelay"]
  INCORRECT: "recommendations": [{"description": "Configure certificate trust"}]
`

// GlobalExclusionsBlock renders operator-defined global exclusion patterns
// as a system-prompt section. Returns an empty string when the list is
// empty so the no-exclusions case produces byte-identical prompt output
// (important for Anthropic prompt cache hit rate).
//
// The block instructs the LLM not only to omit matching findings but also
// to avoid letting them influence systemStatus, summary, and metrics —
// so the stored analysis is coherent with what reaches Telegram.
func GlobalExclusionsBlock(patterns []string) string {
	if len(patterns) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n**OPERATOR-DEFINED EXCLUSIONS (global):**\n")
	b.WriteString("The operator has classified the following conditions as known and accepted.\n")
	b.WriteString("You MUST NOT report them as criticalIssues, warnings, or recommendations.\n")
	b.WriteString("You MUST NOT let them influence systemStatus, summary, or any numeric metric\n")
	b.WriteString("(failedLogins, errorCount, etc.). Treat matching log lines as if absent.\n")
	b.WriteString("Match case-insensitively by substring against the finding text you would\n")
	b.WriteString("otherwise emit. Excluded patterns:\n")
	for _, p := range patterns {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	return b.String()
}

// ContextualExclusionsBlock renders run-scoped exclusion patterns (source-
// wide and/or site-specific) as a user-prompt section. Returns an empty
// string when the list is empty.
//
// Placed immediately before the closing "Please analyze..." directive in
// the user prompt so recency reinforces the instruction.
func ContextualExclusionsBlock(patterns []string) string {
	if len(patterns) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("RUN-SCOPED EXCLUSIONS (apply in addition to any global exclusions in the\n")
	b.WriteString("system prompt): do not report, and do not let affect status/summary/metrics:\n")
	for _, p := range patterns {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

// GetSystemPrompt returns the system prompt with cache control
func GetSystemPrompt() string {
	return `You are a senior system administrator and security analyst with expertise in Linux system security and operations. Your role is to analyze logwatch reports and provide actionable insights.

**Analysis Framework:**

1. **System Status Assessment** - Classify overall system health:
   - "Excellent" - No issues, optimal performance
   - "Good" - Minor issues that don't affect operations
   - "Satisfactory" - Some concerns but system is stable
   - "Bad" - Significant issues requiring immediate attention
   - "Awful" - Critical failures, system stability at risk

2. **Security Analysis** - Identify threats:
   - Brute force attacks (failed login attempts)
   - Privilege escalation attempts
   - Unauthorized access attempts
   - Suspicious network activity
   - Configuration vulnerabilities

3. **System Health Indicators:**
   - Disk space usage and trends
   - Memory and swap usage
   - Service failures or restarts
   - Kernel errors or warnings
   - Network connectivity issues

4. **Recommendations** - Provide specific, actionable steps:
   - Prioritize by urgency (critical, high, medium, low)
   - Include specific commands or configurations when relevant
   - Focus on preventive measures
   - Suggest monitoring improvements

5. **Metrics Extraction** - Extract key metrics:
   - failedLogins: number of failed login attempts
   - errorCount: total number of errors
   - diskUsage: disk usage percentage or description
   - Any other relevant numerical indicators

**Output Requirements:**

You MUST respond with a valid JSON object (and ONLY JSON) in this exact format:

{
  "systemStatus": "Excellent|Good|Satisfactory|Bad|Awful",
  "summary": "2-3 sentence overview of system state",
  "criticalIssues": [
    "Urgent issue requiring immediate action"
  ],
  "warnings": [
    "Concerning issue that should be monitored"
  ],
  "recommendations": [
    "Specific actionable recommendation with commands if applicable"
  ],
  "metrics": {
    "failedLogins": 0,
    "errorCount": 0,
    "diskUsage": "75% on /var",
    "customMetric": "value"
  }
}

**Analysis Principles:**
- Be accurate and fact-based - only report what's in the logs
- Prioritize security issues over operational concerns
- Consider historical context when provided
- Be specific in recommendations (include commands, file paths, etc.)
- Use clear, concise language
- If uncertain, state assumptions clearly
- Empty arrays are acceptable if no issues/warnings/recommendations exist`
}

// GetUserPrompt constructs the user prompt with logwatch content and historical context
func GetUserPrompt(logwatchContent, historicalContext string) string {
	var prompt strings.Builder

	prompt.WriteString("LOGWATCH OUTPUT:\n")
	prompt.WriteString(SanitizeLogContent(logwatchContent)) // L-03 fix: sanitize input
	prompt.WriteString("\n\n")

	if historicalContext != "" {
		prompt.WriteString("HISTORICAL CONTEXT:\n")
		prompt.WriteString(SanitizeLogContent(historicalContext)) // L-03 fix: sanitize input
		prompt.WriteString("\n\n")
	}

	prompt.WriteString("Please analyze the logwatch output above and provide your assessment in JSON format as specified.")

	return prompt.String()
}

// promptInjectionPatterns contains regex patterns for common prompt injection attempts
var promptInjectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above)\s+(instructions?|prompts?|rules?)`),
	regexp.MustCompile(`(?i)disregard\s+(all\s+)?(previous|prior|above)\s+(instructions?|prompts?|rules?)`),
	regexp.MustCompile(`(?i)forget\s+(all\s+)?(previous|prior|above)\s+(instructions?|prompts?|rules?)`),
	regexp.MustCompile(`(?i)you\s+are\s+now\s+a`),
	regexp.MustCompile(`(?i)new\s+instructions?:`),
	regexp.MustCompile(`(?i)system\s*prompt\s*:`),
	regexp.MustCompile(`(?i)\bASSISTANT\s*:`),
	regexp.MustCompile(`(?i)\bHUMAN\s*:`),
	regexp.MustCompile(`(?i)\bUSER\s*:`),
	regexp.MustCompile(`(?i)\bSYSTEM\s*:`),
}

// zeroWidthChars matches Unicode zero-width and bidi-control characters that
// an attacker could use to disguise injection payloads (e.g. "ign[ZWJ]ore")
// so they slip past the ASCII-oriented promptInjectionPatterns.
var zeroWidthChars = regexp.MustCompile(`[\x{200B}-\x{200F}\x{202A}-\x{202E}\x{2060}-\x{206F}\x{FEFF}]`)

// NormalizePromptContent applies the structural normalization steps shared
// between SanitizeLogContent and operator-authored exclusion patterns:
//
//   - NFKC normalization (collapses fullwidth/ligature forms to ASCII so
//     "ＩＧＮＯＲＥ" folds to "IGNORE")
//   - Zero-width and bidi-control character stripping (prevents
//     "ign[ZWJ]ore" from evading ASCII-oriented checks)
//   - Non-printable character removal (preserves \n \t \r)
//
// It does NOT apply prompt-injection phrase replacement. Callers that need
// that behavior layer it on top (see SanitizeLogContent). Splitting the
// passes lets exclusion patterns share the unicode-normalization defense
// without having their legitimate text rewritten to "[FILTERED]" by the
// phrase matcher — which was an issue when a pattern contained tokens like
// "USER:" that the LLM-facing phrase rules target.
func NormalizePromptContent(content string) string {
	normalized := norm.NFKC.String(content)
	normalized = zeroWidthChars.ReplaceAllString(normalized, "")

	var sanitized strings.Builder
	sanitized.Grow(len(normalized))
	for _, r := range normalized {
		if unicode.IsPrint(r) || r == '\n' || r == '\t' || r == '\r' {
			sanitized.WriteRune(r)
		}
	}
	return sanitized.String()
}

// excessiveNewlines collapses runs of 4+ newlines to 3 inside SanitizeLogContent.
var excessiveNewlines = regexp.MustCompile(`\n{4,}`)

// SanitizeLogContent sanitizes untrusted log content to prevent prompt injection
// (L-03 fix). This runs NormalizePromptContent first, then applies prompt-injection
// phrase replacement and newline collapsing. Use NormalizePromptContent directly
// for operator-authored strings where phrase replacement would corrupt intended
// matching text.
func SanitizeLogContent(content string) string {
	result := NormalizePromptContent(content)

	for _, pattern := range promptInjectionPatterns {
		result = pattern.ReplaceAllString(result, "[FILTERED]")
	}

	result = excessiveNewlines.ReplaceAllString(result, "\n\n\n")

	return result
}

// Maximum allowed JSON response size (1MB) to prevent memory exhaustion
const maxJSONResponseSize = 1024 * 1024

// sanitizeJSONEscapes fixes invalid JSON escape sequences in LLM responses.
// JSON only allows: \" \\ \/ \b \f \n \r \t \uXXXX
// LLMs sometimes produce invalid sequences like \. \( \) \- etc.
func sanitizeJSONEscapes(s string) string {
	var result strings.Builder
	result.Grow(len(s))

	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			next := s[i+1]
			// Valid JSON escapes: " \ / b f n r t u
			if next == '"' || next == '\\' || next == '/' ||
				next == 'b' || next == 'f' || next == 'n' ||
				next == 'r' || next == 't' || next == 'u' {
				result.WriteByte(s[i])
				result.WriteByte(next)
				i += 2
				continue
			}
			// Invalid escape - skip the backslash, keep the character
			result.WriteByte(next)
			i += 2
			continue
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

// ParseAnalysis extracts and parses the JSON analysis from Claude's response.
// Array fields (criticalIssues/warnings/recommendations) are normalized via
// coerceStringArray so object-valued items (e.g. {"description": "..."}) that
// the LLM occasionally emits despite prompt instructions do not fail the run.
func ParseAnalysis(response string) (*Analysis, error) {
	// Extract JSON from response using balanced brace matching
	jsonMatch := extractJSON(response)

	if jsonMatch == "" {
		return nil, fmt.Errorf("no JSON object found in response")
	}

	// Check JSON size limit to prevent memory exhaustion (M-05)
	if len(jsonMatch) > maxJSONResponseSize {
		return nil, fmt.Errorf("JSON response too large: %d bytes (max: %d)", len(jsonMatch), maxJSONResponseSize)
	}

	// Sanitize invalid JSON escape sequences that LLMs sometimes produce
	sanitizedJSON := sanitizeJSONEscapes(jsonMatch)

	var raw rawAnalysis
	if err := json.Unmarshal([]byte(sanitizedJSON), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	analysis := &Analysis{
		SystemStatus:    raw.SystemStatus,
		Summary:         raw.Summary,
		CriticalIssues:  coerceStringArray(raw.CriticalIssues),
		Warnings:        coerceStringArray(raw.Warnings),
		Recommendations: coerceStringArray(raw.Recommendations),
		Metrics:         raw.Metrics,
	}

	// Validate required fields
	if err := validateAnalysis(analysis); err != nil {
		return nil, fmt.Errorf("analysis validation failed: %w", err)
	}

	return analysis, nil
}

// validateAnalysis validates the analysis structure
func validateAnalysis(analysis *Analysis) error {
	if analysis.SystemStatus == "" {
		return fmt.Errorf("systemStatus is required")
	}

	validStatuses := map[string]bool{
		"Excellent":    true,
		"Good":         true,
		"Satisfactory": true,
		"Bad":          true,
		"Awful":        true,
	}

	if !validStatuses[analysis.SystemStatus] {
		return fmt.Errorf("invalid systemStatus: %s", analysis.SystemStatus)
	}

	if analysis.Summary == "" {
		return fmt.Errorf("summary is required")
	}

	// Initialize empty arrays if nil
	if analysis.CriticalIssues == nil {
		analysis.CriticalIssues = []string{}
	}
	if analysis.Warnings == nil {
		analysis.Warnings = []string{}
	}
	if analysis.Recommendations == nil {
		analysis.Recommendations = []string{}
	}
	if analysis.Metrics == nil {
		analysis.Metrics = make(map[string]any)
	}

	return nil
}

// GetStatusEmoji returns the emoji for a given system status
func GetStatusEmoji(status string) string {
	emojiMap := map[string]string{
		"Excellent":    "✅",
		"Good":         "🟢",
		"Satisfactory": "🟡",
		"Bad":          "🟠",
		"Awful":        "🔴",
	}

	if emoji, ok := emojiMap[status]; ok {
		return emoji
	}
	return "⚪"
}

// ShouldTriggerAlert determines if the analysis should trigger an alert
func ShouldTriggerAlert(status string) bool {
	alertStatuses := map[string]bool{
		"Satisfactory": true,
		"Bad":          true,
		"Awful":        true,
	}
	return alertStatuses[status]
}

// extractJSON extracts the first balanced JSON object from a response string.
// This is more reliable than greedy regex matching (M-06 fix).
func extractJSON(response string) string {
	// Find the first opening brace
	startIdx := strings.Index(response, "{")
	if startIdx == -1 {
		return ""
	}

	// Track brace depth to find matching closing brace
	depth := 0
	inString := false
	escaped := false

	for i := startIdx; i < len(response); i++ {
		char := response[i]

		if escaped {
			escaped = false
			continue
		}

		if char == '\\' && inString {
			escaped = true
			continue
		}

		if char == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		switch char {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return response[startIdx : i+1]
			}
		}
	}

	return ""
}
