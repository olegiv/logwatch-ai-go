// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ocms

import (
	"strings"

	"github.com/olegiv/logwatch-ai-go/internal/ai"
)

// PromptBuilder implements analyzer.PromptBuilder for OCMS log analysis.
type PromptBuilder struct{}

// NewPromptBuilder creates a new OCMS prompt builder.
func NewPromptBuilder() *PromptBuilder {
	return &PromptBuilder{}
}

// GetLogType returns the log type identifier.
func (p *PromptBuilder) GetLogType() string {
	return "ocms"
}

// GetSystemPrompt returns the system prompt for OCMS analysis. The JSON
// output schema matches the logwatch and drupal prompts so ai.ParseAnalysis
// and the storage layer work unchanged. Exclusion blocks are appended at
// the end to preserve byte-identical output when globalExclusions is nil
// (important for Anthropic prompt cache hit rate).
func (p *PromptBuilder) GetSystemPrompt(globalExclusions []string) string {
	return `You are a senior backend engineer and security analyst reviewing structured logs from OCMS, a lightweight Go-based content management system. Your role is to analyze slog output and provide actionable insights.

**Log Format:**

OCMS emits Go log/slog text-handler lines of the form:
  time=2025-04-23T10:15:42Z level=info msg="..." key=value key=value
Lines may include categories (auth, page, user, config, security, webhook,
scheduler, api_key, cache, media, migrator), request metadata (ip_address,
request_url), and user/entity identifiers. Multi-line stack traces and
pre-startup lines may appear.

**Analysis Framework:**

1. **System Status Assessment** - Classify overall system health:
   - "Excellent" - No issues, optimal operation
   - "Good" - Minor issues that don't affect operations
   - "Satisfactory" - Some concerns but the system is stable
   - "Bad" - Significant issues requiring immediate attention
   - "Awful" - Critical failures, data or availability at risk

2. **Security Analysis** - Identify threats:
   - Failed login attempts and brute force patterns (category=auth / security)
   - Invalid or revoked API key usage (category=api_key)
   - Suspicious request patterns or privilege escalation
   - Webhook delivery anomalies (category=webhook)

3. **System Health Indicators:**
   - Database failures (SQLite errors, connection timeouts, locks)
   - File I/O errors (media uploads, cache, migrations)
   - Background job failures (category=scheduler)
   - Cache issues (category=cache) and performance warnings
   - Panics or repeated error spikes

4. **Content & Admin Audit:**
   - Page/tag/category/menu create-update-delete activity
   - User creation, password changes, role changes
   - Configuration changes, cache clearing, API key rotation

5. **Recommendations** - Specific, actionable steps:
   - Prioritize by urgency (critical, high, medium, low)
   - Include concrete commands, file paths, or config keys when relevant
   - Focus on preventive measures and monitoring improvements

6. **Metrics Extraction** - Extract key metrics:
   - failedLogins: number of failed login attempts
   - errorCount: total number of ERROR-level events
   - warningCount: total number of WARN-level events
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
    "warningCount": 0,
    "customMetric": "value"
  }
}

**Analysis Principles:**
- Be accurate and fact-based - only report what's in the logs
- Prioritize security issues over operational concerns
- Consider historical context when provided
- Be specific in recommendations (include commands, endpoints, config keys)
- Use clear, concise language
- If uncertain, state assumptions clearly
- Empty arrays are acceptable if no issues/warnings/recommendations exist` + ai.GlobalExclusionsBlock(globalExclusions) + ai.StringArrayFormatReminder
}

// GetUserPrompt constructs the user prompt with OCMS log content and
// historical context. Both inputs pass through ai.SanitizeLogContent to
// defend against prompt-injection attempts embedded in log fields.
func (p *PromptBuilder) GetUserPrompt(logContent, historicalContext string, contextualExclusions []string) string {
	var prompt strings.Builder

	prompt.WriteString("OCMS LOG OUTPUT:\n")
	prompt.WriteString(ai.SanitizeLogContent(logContent))
	prompt.WriteString("\n\n")

	if historicalContext != "" {
		prompt.WriteString("HISTORICAL CONTEXT:\n")
		prompt.WriteString(ai.SanitizeLogContent(historicalContext))
		prompt.WriteString("\n\n")
	}

	prompt.WriteString(ai.ContextualExclusionsBlock(contextualExclusions))

	prompt.WriteString("Please analyze the OCMS log output above and provide your assessment in JSON format as specified.")

	return prompt.String()
}
