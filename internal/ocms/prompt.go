// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ocms

import (
	"strings"

	"github.com/olegiv/logwatch-ai-go/internal/ai"
	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

// PromptBuilder implements analyzer.PromptBuilder for OCMS log analysis.
type PromptBuilder struct {
	siteName string
}

var _ analyzer.PromptBuilder = (*PromptBuilder)(nil)

// NewPromptBuilder creates a new OCMS prompt builder.
func NewPromptBuilder() *PromptBuilder {
	return &PromptBuilder{}
}

// SetSiteName sets the site name for display in prompts.
func (p *PromptBuilder) SetSiteName(name string) {
	p.siteName = name
}

// GetSiteName returns the configured site name.
func (p *PromptBuilder) GetSiteName() string {
	return p.siteName
}

// GetLogType returns the log type identifier.
func (p *PromptBuilder) GetLogType() string {
	return "ocms"
}

// GetSystemPrompt returns the system prompt for OCMS analysis.
func (p *PromptBuilder) GetSystemPrompt(globalExclusions []string) string {
	return `You are a senior site reliability engineer and security analyst focused on OCMS platform operations. Your role is to analyze OCMS logs and provide actionable insights.

**Analysis Framework:**

1. **System Status Assessment** - Classify overall service health:
   - "Excellent" - No issues, optimal performance
   - "Good" - Minor issues that don't affect operations
   - "Satisfactory" - Some concerns but service is stable
   - "Bad" - Significant issues requiring immediate attention
   - "Awful" - Critical failures, service stability at risk

2. **Security Analysis** - Identify risks:
   - Unauthorized access attempts
   - Authentication/authorization anomalies
   - Suspicious request patterns
   - Potential abuse or brute force behavior
   - Configuration vulnerabilities

3. **Platform Health Indicators:**
   - Application errors and exceptions
   - Database and external dependency failures
   - Performance degradation and latency spikes
   - Background job and queue issues
   - Integration/API failures

4. **Recommendations** - Provide specific, actionable steps:
   - Prioritize by urgency (critical, high, medium, low)
   - Include concrete verification and remediation actions
   - Focus on preventive measures and observability improvements

5. **Metrics Extraction** - Extract key metrics:
   - failedLogins: number of failed authentication attempts
   - errorCount: total number of errors
   - requestLatency: notable latency values or trends
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
    "Specific actionable recommendation"
  ],
  "metrics": {
    "failedLogins": 0,
    "errorCount": 0,
    "requestLatency": "p95 500ms",
    "customMetric": "value"
  }
}` + ai.GlobalExclusionsBlock(globalExclusions) + ai.StringArrayFormatReminder
}

// GetUserPrompt constructs the user prompt with OCMS logs and historical context.
func (p *PromptBuilder) GetUserPrompt(logContent, historicalContext string, contextualExclusions []string) string {
	var prompt strings.Builder
	if p.siteName != "" {
		prompt.WriteString("OCMS SITE: ")
		prompt.WriteString(p.siteName)
		prompt.WriteString("\n\n")
	}

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
