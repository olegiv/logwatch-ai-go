package ai

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Analysis represents the structured analysis result from Claude
type Analysis struct {
	SystemStatus    string                 `json:"systemStatus"`
	Summary         string                 `json:"summary"`
	CriticalIssues  []string               `json:"criticalIssues"`
	Warnings        []string               `json:"warnings"`
	Recommendations []string               `json:"recommendations"`
	Metrics         map[string]interface{} `json:"metrics"`
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
	prompt.WriteString(logwatchContent)
	prompt.WriteString("\n\n")

	if historicalContext != "" {
		prompt.WriteString("HISTORICAL CONTEXT:\n")
		prompt.WriteString(historicalContext)
		prompt.WriteString("\n\n")
	}

	prompt.WriteString("Please analyze the logwatch output above and provide your assessment in JSON format as specified.")

	return prompt.String()
}

// ParseAnalysis extracts and parses the JSON analysis from Claude's response
func ParseAnalysis(response string) (*Analysis, error) {
	// Extract JSON from response (handles cases where Claude adds extra text)
	jsonRegex := regexp.MustCompile(`\{[\s\S]*}`)
	jsonMatch := jsonRegex.FindString(response)

	if jsonMatch == "" {
		return nil, fmt.Errorf("no JSON object found in response")
	}

	// Parse JSON
	var analysis Analysis
	if err := json.Unmarshal([]byte(jsonMatch), &analysis); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// Validate required fields
	if err := validateAnalysis(&analysis); err != nil {
		return nil, fmt.Errorf("analysis validation failed: %w", err)
	}

	return &analysis, nil
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
		analysis.Metrics = make(map[string]interface{})
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
