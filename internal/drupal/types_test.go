package drupal

import (
	"testing"
	"time"
)

func TestSeverityName(t *testing.T) {
	tests := []struct {
		severity int
		want     string
	}{
		{SeverityEmergency, "emergency"},
		{SeverityAlert, "alert"},
		{SeverityCritical, "critical"},
		{SeverityError, "error"},
		{SeverityWarning, "warning"},
		{SeverityNotice, "notice"},
		{SeverityInfo, "info"},
		{SeverityDebug, "debug"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := SeverityName[tt.severity]; got != tt.want {
				t.Errorf("SeverityName[%d] = %q, want %q", tt.severity, got, tt.want)
			}
		})
	}
}

func TestSeverityFromName(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{"emergency", SeverityEmergency},
		{"alert", SeverityAlert},
		{"critical", SeverityCritical},
		{"error", SeverityError},
		{"warning", SeverityWarning},
		{"notice", SeverityNotice},
		{"info", SeverityInfo},
		{"debug", SeverityDebug},
		{"unknown", -1},
		{"", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SeverityFromName(tt.name); got != tt.want {
				t.Errorf("SeverityFromName(%q) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsCriticalSeverity(t *testing.T) {
	tests := []struct {
		severity int
		want     bool
	}{
		{SeverityEmergency, true},
		{SeverityAlert, true},
		{SeverityCritical, true},
		{SeverityError, true},
		{SeverityWarning, false},
		{SeverityNotice, false},
		{SeverityInfo, false},
		{SeverityDebug, false},
		{-1, false},
		{10, false},
	}

	for _, tt := range tests {
		t.Run(SeverityName[tt.severity], func(t *testing.T) {
			if got := IsCriticalSeverity(tt.severity); got != tt.want {
				t.Errorf("IsCriticalSeverity(%d) = %v, want %v", tt.severity, got, tt.want)
			}
		})
	}
}

func TestWatchdogEntry_Time(t *testing.T) {
	entry := WatchdogEntry{
		Timestamp: 1699900800, // 2023-11-13 16:00:00 UTC
	}

	got := entry.Time()
	want := time.Unix(1699900800, 0)

	if !got.Equal(want) {
		t.Errorf("Time() = %v, want %v", got, want)
	}
}

func TestWatchdogEntry_SeverityName(t *testing.T) {
	tests := []struct {
		severity int
		want     string
	}{
		{SeverityError, "error"},
		{SeverityWarning, "warning"},
		{99, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			entry := WatchdogEntry{Severity: tt.severity}
			if got := entry.SeverityName(); got != tt.want {
				t.Errorf("SeverityName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWatchdogEntry_IsCritical(t *testing.T) {
	tests := []struct {
		severity int
		want     bool
	}{
		{SeverityError, true},
		{SeverityWarning, false},
		{SeverityNotice, false},
	}

	for _, tt := range tests {
		entry := WatchdogEntry{Severity: tt.severity}
		if got := entry.IsCritical(); got != tt.want {
			t.Errorf("IsCritical() with severity %d = %v, want %v", tt.severity, got, tt.want)
		}
	}
}

func TestWatchdogEntry_IsWarning(t *testing.T) {
	tests := []struct {
		severity int
		want     bool
	}{
		{SeverityWarning, true},
		{SeverityError, false},
		{SeverityNotice, false},
	}

	for _, tt := range tests {
		entry := WatchdogEntry{Severity: tt.severity}
		if got := entry.IsWarning(); got != tt.want {
			t.Errorf("IsWarning() with severity %d = %v, want %v", tt.severity, got, tt.want)
		}
	}
}

func TestWatchdogReport_Stats(t *testing.T) {
	report := WatchdogReport{
		Entries: []WatchdogEntry{
			{Type: "php", Severity: SeverityError},
			{Type: "php", Severity: SeverityWarning},
			{Type: "access", Severity: SeverityNotice},
			{Type: "cron", Severity: SeverityError},
		},
	}

	stats := report.Stats()

	if stats["total_entries"] != 4 {
		t.Errorf("total_entries = %v, want 4", stats["total_entries"])
	}

	if stats["critical_count"] != 2 {
		t.Errorf("critical_count = %v, want 2", stats["critical_count"])
	}

	if stats["warning_count"] != 1 {
		t.Errorf("warning_count = %v, want 1", stats["warning_count"])
	}

	severityCounts := stats["by_severity"].(map[string]int)
	if severityCounts["error"] != 2 {
		t.Errorf("by_severity[error] = %v, want 2", severityCounts["error"])
	}

	typeCounts := stats["by_type"].(map[string]int)
	if typeCounts["php"] != 2 {
		t.Errorf("by_type[php] = %v, want 2", typeCounts["php"])
	}
}

func TestWatchdogReport_Stats_Empty(t *testing.T) {
	report := WatchdogReport{}
	stats := report.Stats()

	if stats["total_entries"] != 0 {
		t.Errorf("total_entries = %v, want 0", stats["total_entries"])
	}
}
