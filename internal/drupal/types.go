// Package drupal provides log analysis for Drupal watchdog logs.
// It implements the analyzer interfaces to enable Drupal watchdog
// analysis alongside other log sources like logwatch.
package drupal

import (
	"time"
)

// Severity levels based on RFC 5424 (syslog).
// Drupal uses these standard severity levels for watchdog entries.
const (
	SeverityEmergency = 0 // System is unusable
	SeverityAlert     = 1 // Action must be taken immediately
	SeverityCritical  = 2 // Critical conditions
	SeverityError     = 3 // Error conditions
	SeverityWarning   = 4 // Warning conditions
	SeverityNotice    = 5 // Normal but significant condition
	SeverityInfo      = 6 // Informational messages
	SeverityDebug     = 7 // Debug-level messages
)

// SeverityName maps severity levels to human-readable names.
var SeverityName = map[int]string{
	SeverityEmergency: "emergency",
	SeverityAlert:     "alert",
	SeverityCritical:  "critical",
	SeverityError:     "error",
	SeverityWarning:   "warning",
	SeverityNotice:    "notice",
	SeverityInfo:      "info",
	SeverityDebug:     "debug",
}

// SeverityFromName returns the severity level for a given name.
// Returns -1 if the name is not recognized.
func SeverityFromName(name string) int {
	for level, n := range SeverityName {
		if n == name {
			return level
		}
	}
	return -1
}

// IsCriticalSeverity returns true if the severity level is critical or higher.
// Levels 0-3 (emergency, alert, critical, error) are considered critical.
func IsCriticalSeverity(severity int) bool {
	return severity >= SeverityEmergency && severity <= SeverityError
}

// IsWarningSeverity returns true if the severity level is warning.
func IsWarningSeverity(severity int) bool {
	return severity == SeverityWarning
}

// WatchdogEntry represents a single Drupal watchdog log entry.
// This structure matches the Drupal watchdog database table schema.
type WatchdogEntry struct {
	// WID is the unique watchdog entry ID
	WID int64 `json:"wid"`

	// UID is the user ID who triggered this entry (0 for anonymous)
	UID int64 `json:"uid"`

	// Type is the category of the log entry (e.g., "php", "access", "cron", "system")
	Type string `json:"type"`

	// Message is the log message (may contain placeholders like @variable)
	Message string `json:"message"`

	// Variables contains serialized PHP array of placeholder values
	// Format: PHP serialized array (a:N:{...}) or JSON in newer Drupal versions
	Variables string `json:"variables"`

	// Severity is the RFC 5424 severity level (0-7)
	Severity int `json:"severity"`

	// Link is an optional link associated with this entry
	Link string `json:"link"`

	// Location is the URL where this event occurred
	Location string `json:"location"`

	// Referer is the referring URL
	Referer string `json:"referer"`

	// Hostname is the IP address or hostname of the client
	Hostname string `json:"hostname"`

	// Timestamp is the Unix timestamp when this entry was created
	Timestamp int64 `json:"timestamp"`
}

// Time returns the entry timestamp as a time.Time.
func (e *WatchdogEntry) Time() time.Time {
	return time.Unix(e.Timestamp, 0)
}

// SeverityName returns the human-readable severity name.
func (e *WatchdogEntry) SeverityName() string {
	if name, ok := SeverityName[e.Severity]; ok {
		return name
	}
	return "unknown"
}

// IsCritical returns true if this entry has critical severity or higher.
func (e *WatchdogEntry) IsCritical() bool {
	return IsCriticalSeverity(e.Severity)
}

// IsWarning returns true if this entry has warning severity.
func (e *WatchdogEntry) IsWarning() bool {
	return IsWarningSeverity(e.Severity)
}

// WatchdogReport represents a collection of watchdog entries for analysis.
type WatchdogReport struct {
	// Entries contains all watchdog log entries
	Entries []WatchdogEntry `json:"entries"`

	// SiteName is an optional identifier for multi-site deployments
	SiteName string `json:"site_name,omitempty"`

	// GeneratedAt is when this report was generated
	GeneratedAt time.Time `json:"generated_at"`

	// PeriodStart is the start of the reporting period
	PeriodStart time.Time `json:"period_start,omitempty"`

	// PeriodEnd is the end of the reporting period
	PeriodEnd time.Time `json:"period_end,omitempty"`
}

// Stats returns statistics about the watchdog entries.
func (r *WatchdogReport) Stats() map[string]interface{} {
	stats := map[string]interface{}{
		"total_entries": len(r.Entries),
	}

	// Count by severity
	severityCounts := make(map[string]int)
	for _, entry := range r.Entries {
		name := entry.SeverityName()
		severityCounts[name]++
	}
	stats["by_severity"] = severityCounts

	// Count by type
	typeCounts := make(map[string]int)
	for _, entry := range r.Entries {
		typeCounts[entry.Type]++
	}
	stats["by_type"] = typeCounts

	// Count critical entries
	criticalCount := 0
	warningCount := 0
	for _, entry := range r.Entries {
		if entry.IsCritical() {
			criticalCount++
		}
		if entry.IsWarning() {
			warningCount++
		}
	}
	stats["critical_count"] = criticalCount
	stats["warning_count"] = warningCount

	return stats
}
