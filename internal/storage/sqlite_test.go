package storage

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// assertSummaryFieldsEqual compares two Summary structs and reports differences
func assertSummaryFieldsEqual(t *testing.T, got, want *Summary) {
	t.Helper()
	if got.LogSourceType != want.LogSourceType {
		t.Errorf("LogSourceType mismatch: got %s, want %s", got.LogSourceType, want.LogSourceType)
	}
	if got.SiteName != want.SiteName {
		t.Errorf("SiteName mismatch: got %s, want %s", got.SiteName, want.SiteName)
	}
	if got.SystemStatus != want.SystemStatus {
		t.Errorf("SystemStatus mismatch: got %s, want %s", got.SystemStatus, want.SystemStatus)
	}
	if got.Summary != want.Summary {
		t.Errorf("Summary mismatch: got %s, want %s", got.Summary, want.Summary)
	}
	if !reflect.DeepEqual(got.CriticalIssues, want.CriticalIssues) {
		t.Errorf("CriticalIssues mismatch: got %v, want %v", got.CriticalIssues, want.CriticalIssues)
	}
	if !reflect.DeepEqual(got.Warnings, want.Warnings) {
		t.Errorf("Warnings mismatch: got %v, want %v", got.Warnings, want.Warnings)
	}
	if !reflect.DeepEqual(got.Recommendations, want.Recommendations) {
		t.Errorf("Recommendations mismatch: got %v, want %v", got.Recommendations, want.Recommendations)
	}
	if got.InputTokens != want.InputTokens {
		t.Errorf("InputTokens mismatch: got %d, want %d", got.InputTokens, want.InputTokens)
	}
	if got.OutputTokens != want.OutputTokens {
		t.Errorf("OutputTokens mismatch: got %d, want %d", got.OutputTokens, want.OutputTokens)
	}
	if got.CostUSD != want.CostUSD {
		t.Errorf("CostUSD mismatch: got %.4f, want %.4f", got.CostUSD, want.CostUSD)
	}
}

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	if storage == nil {
		t.Fatal("Expected storage to be created")
	}

	if storage.db == nil {
		t.Fatal("Expected database connection to be initialized")
	}
}

func TestNewCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "subdir", "nested", "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	if storage == nil {
		t.Fatal("Expected storage to be created with nested directories")
	}
}

func TestSaveSummary(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	summary := &Summary{
		Timestamp:    time.Now(),
		SystemStatus: "Good",
		Summary:      "Test summary",
		CriticalIssues: []string{
			"Issue 1",
			"Issue 2",
		},
		Warnings: []string{
			"Warning 1",
		},
		Recommendations: []string{
			"Recommendation 1",
		},
		Metrics: map[string]interface{}{
			"failedLogins": float64(5),
			"diskUsage":    "85%",
		},
		InputTokens:  1000,
		OutputTokens: 500,
		CostUSD:      0.0105,
	}

	err = storage.SaveSummary(summary)
	if err != nil {
		t.Fatalf("Failed to save summary: %v", err)
	}

	if summary.ID == 0 {
		t.Error("Expected ID to be set after save")
	}
}

func TestGetRecentSummaries(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	// Save multiple summaries with different timestamps
	now := time.Now()
	summaries := []*Summary{
		{
			Timestamp:       now.AddDate(0, 0, -1),
			SystemStatus:    "Good",
			Summary:         "Yesterday",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     1000,
			OutputTokens:    500,
			CostUSD:         0.01,
		},
		{
			Timestamp:       now.AddDate(0, 0, -5),
			SystemStatus:    "Warning",
			Summary:         "5 days ago",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     1000,
			OutputTokens:    500,
			CostUSD:         0.01,
		},
		{
			Timestamp:       now.AddDate(0, 0, -10),
			SystemStatus:    "Critical",
			Summary:         "10 days ago",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     1000,
			OutputTokens:    500,
			CostUSD:         0.01,
		},
	}

	for _, s := range summaries {
		if err := storage.SaveSummary(s); err != nil {
			t.Fatalf("Failed to save summary: %v", err)
		}
	}

	// Get recent summaries (last 7 days) - nil filter returns all
	recent, err := storage.GetRecentSummaries(7, nil)
	if err != nil {
		t.Fatalf("Failed to get recent summaries: %v", err)
	}

	if len(recent) != 2 {
		t.Errorf("Expected 2 recent summaries (last 7 days), got %d", len(recent))
	}

	// Verify they're sorted by timestamp DESC
	if len(recent) > 1 && recent[0].Timestamp.Before(recent[1].Timestamp) {
		t.Error("Summaries should be sorted by timestamp DESC")
	}
}

func TestGetHistoricalContext(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	// Save a summary
	now := time.Now()
	summary := &Summary{
		Timestamp:    now,
		SystemStatus: "Good",
		Summary:      "Test summary",
		CriticalIssues: []string{
			"Issue 1",
		},
		Warnings: []string{
			"Warning 1",
			"Warning 2",
		},
		Recommendations: []string{},
		Metrics:         map[string]interface{}{},
		InputTokens:     1000,
		OutputTokens:    500,
		CostUSD:         0.01,
	}

	if err := storage.SaveSummary(summary); err != nil {
		t.Fatalf("Failed to save summary: %v", err)
	}

	// Get historical context (nil filter returns all)
	context, err := storage.GetHistoricalContext(7, nil)
	if err != nil {
		t.Fatalf("Failed to get historical context: %v", err)
	}

	if context == "" {
		t.Error("Expected non-empty context")
	}

	// Verify context contains key information
	if !strings.Contains(context, "Status: Good") {
		t.Error("Context should contain status")
	}

	if !strings.Contains(context, "Test summary") {
		t.Error("Context should contain summary text")
	}

	if !strings.Contains(context, "Critical Issues: 1") {
		t.Error("Context should contain critical issues count")
	}

	if !strings.Contains(context, "Warnings: 2") {
		t.Error("Context should contain warnings count")
	}
}

func TestGetHistoricalContext_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	// Get historical context with no data
	context, err := storage.GetHistoricalContext(7, nil)
	if err != nil {
		t.Fatalf("Failed to get historical context: %v", err)
	}

	if context != "" {
		t.Error("Expected empty context when no summaries exist")
	}
}

func TestCleanupOldSummaries(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	// Save summaries with different ages
	now := time.Now()
	summaries := []*Summary{
		{
			Timestamp:       now.AddDate(0, 0, -5),
			SystemStatus:    "Good",
			Summary:         "Recent",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     1000,
			OutputTokens:    500,
			CostUSD:         0.01,
		},
		{
			Timestamp:       now.AddDate(0, 0, -100),
			SystemStatus:    "Good",
			Summary:         "Old",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     1000,
			OutputTokens:    500,
			CostUSD:         0.01,
		},
	}

	for _, s := range summaries {
		if err := storage.SaveSummary(s); err != nil {
			t.Fatalf("Failed to save summary: %v", err)
		}
	}

	// Cleanup old summaries (older than 90 days)
	affected, err := storage.CleanupOldSummaries(90)
	if err != nil {
		t.Fatalf("Failed to cleanup old summaries: %v", err)
	}

	if affected != 1 {
		t.Errorf("Expected 1 summary to be deleted, got %d", affected)
	}

	// Verify only recent summary remains
	recent, err := storage.GetRecentSummaries(365, nil)
	if err != nil {
		t.Fatalf("Failed to get summaries: %v", err)
	}

	if len(recent) != 1 {
		t.Fatalf("Expected 1 summary remaining, got %d", len(recent))
	}

	if recent[0].Summary != "Recent" {
		t.Error("Wrong summary was deleted")
	}
}

func TestGetStatistics(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	// Save summaries with different statuses
	summaries := []*Summary{
		{
			Timestamp:       time.Now(),
			SystemStatus:    "Good",
			Summary:         "Summary 1",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     1000,
			OutputTokens:    500,
			CostUSD:         0.01,
		},
		{
			Timestamp:       time.Now(),
			SystemStatus:    "Good",
			Summary:         "Summary 2",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     1000,
			OutputTokens:    500,
			CostUSD:         0.02,
		},
		{
			Timestamp:       time.Now(),
			SystemStatus:    "Warning",
			Summary:         "Summary 3",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     1000,
			OutputTokens:    500,
			CostUSD:         0.015,
		},
	}

	for _, s := range summaries {
		if err := storage.SaveSummary(s); err != nil {
			t.Fatalf("Failed to save summary: %v", err)
		}
	}

	// Get statistics (nil filter returns all)
	stats, err := storage.GetStatistics(nil)
	if err != nil {
		t.Fatalf("Failed to get statistics: %v", err)
	}

	if stats == nil {
		t.Fatal("Expected statistics but got nil")
	}

	// Verify total count
	total, ok := stats["total_summaries"].(int)
	if !ok {
		t.Fatal("Expected total_summaries to be int")
	}
	if total != 3 {
		t.Errorf("Expected 3 total summaries, got %d", total)
	}

	// Verify status distribution
	statusDist, ok := stats["status_distribution"].(map[string]int)
	if !ok {
		t.Fatal("Expected status_distribution to be map[string]int")
	}
	if statusDist["Good"] != 2 {
		t.Errorf("Expected 2 Good statuses, got %d", statusDist["Good"])
	}
	if statusDist["Warning"] != 1 {
		t.Errorf("Expected 1 Warning status, got %d", statusDist["Warning"])
	}

	// Verify total cost
	totalCost, ok := stats["total_cost_usd"].(float64)
	if !ok {
		t.Fatal("Expected total_cost_usd to be float64")
	}
	expectedCost := 0.01 + 0.02 + 0.015
	if totalCost != expectedCost {
		t.Errorf("Expected total cost %.4f, got %.4f", expectedCost, totalCost)
	}
}

func TestGetStatistics_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	stats, err := storage.GetStatistics(nil)
	if err != nil {
		t.Fatalf("Failed to get statistics: %v", err)
	}

	total, ok := stats["total_summaries"].(int)
	if !ok {
		t.Fatal("Expected total_summaries to be int")
	}
	if total != 0 {
		t.Errorf("Expected 0 summaries, got %d", total)
	}

	totalCost, ok := stats["total_cost_usd"].(float64)
	if !ok {
		t.Fatal("Expected total_cost_usd to be float64")
	}
	if totalCost != 0.0 {
		t.Errorf("Expected 0 cost, got %.4f", totalCost)
	}
}

func TestClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	err = storage.Close()
	if err != nil {
		t.Errorf("Failed to close storage: %v", err)
	}

	// Second close should not error
	err = storage.Close()
	if err != nil {
		t.Errorf("Second close should not error: %v", err)
	}
}

func TestSummaryStructure(t *testing.T) {
	summary := &Summary{
		ID:           1,
		Timestamp:    time.Now(),
		SystemStatus: "Good",
		Summary:      "Test",
		CriticalIssues: []string{
			"Issue 1",
		},
		Warnings: []string{
			"Warning 1",
		},
		Recommendations: []string{
			"Rec 1",
		},
		Metrics: map[string]interface{}{
			"failedLogins": float64(5),
		},
		InputTokens:  1000,
		OutputTokens: 500,
		CostUSD:      0.01,
	}

	if summary.ID != 1 {
		t.Error("ID not set correctly")
	}

	if summary.SystemStatus != "Good" {
		t.Error("SystemStatus not set correctly")
	}

	if len(summary.CriticalIssues) != 1 {
		t.Error("CriticalIssues not set correctly")
	}

	if summary.InputTokens != 1000 {
		t.Error("InputTokens not set correctly")
	}
}

func TestSaveAndRetrieveSummary(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	// Save a summary
	original := &Summary{
		Timestamp:     time.Now().Truncate(time.Second),
		LogSourceType: "logwatch", // Set explicitly to match default behavior
		SiteName:      "",
		SystemStatus:  "Excellent",
		Summary:       "All systems operational",
		CriticalIssues: []string{
			"Critical issue 1",
			"Critical issue 2",
		},
		Warnings: []string{
			"Warning 1",
		},
		Recommendations: []string{
			"Recommendation 1",
			"Recommendation 2",
		},
		Metrics: map[string]interface{}{
			"failedLogins": float64(10),
			"diskUsage":    "75%",
			"errorCount":   float64(0),
		},
		InputTokens:  5000,
		OutputTokens: 2500,
		CostUSD:      0.0525,
	}

	err = storage.SaveSummary(original)
	if err != nil {
		t.Fatalf("Failed to save summary: %v", err)
	}

	// Retrieve it (nil filter returns all)
	summaries, err := storage.GetRecentSummaries(1, nil)
	if err != nil {
		t.Fatalf("Failed to retrieve summaries: %v", err)
	}

	if len(summaries) != 1 {
		t.Fatalf("Expected 1 summary, got %d", len(summaries))
	}

	retrieved := summaries[0]

	// Verify all fields match
	assertSummaryFieldsEqual(t, retrieved, original)

	// Verify metrics separately (map comparison with type assertions)
	if failedLogins, ok := retrieved.Metrics["failedLogins"].(float64); !ok || failedLogins != 10 {
		t.Error("Metrics not restored correctly")
	}
}

func TestCleanupOldSummaries_NoData(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	affected, err := storage.CleanupOldSummaries(90)
	if err != nil {
		t.Fatalf("Failed to cleanup: %v", err)
	}

	if affected != 0 {
		t.Errorf("Expected 0 rows affected, got %d", affected)
	}
}

func TestInitSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	// Verify that the table was created by trying to insert
	summary := &Summary{
		Timestamp:       time.Now(),
		SystemStatus:    "Good",
		Summary:         "Test",
		CriticalIssues:  []string{},
		Warnings:        []string{},
		Recommendations: []string{},
		Metrics:         map[string]interface{}{},
		InputTokens:     100,
		OutputTokens:    50,
		CostUSD:         0.001,
	}

	err = storage.SaveSummary(summary)
	if err != nil {
		t.Errorf("Failed to save to newly created schema: %v", err)
	}
}

func TestDatabaseConnectionPoolSettings(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	// Verify database is working with configured pool settings
	// by performing multiple operations
	for i := 0; i < 10; i++ {
		summary := &Summary{
			Timestamp:       time.Now(),
			SystemStatus:    "Good",
			Summary:         "Pool test summary",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     100,
			OutputTokens:    50,
			CostUSD:         0.001,
		}

		err = storage.SaveSummary(summary)
		if err != nil {
			t.Fatalf("Failed to save summary %d: %v", i, err)
		}
	}

	// Verify all were saved
	summaries, err := storage.GetRecentSummaries(1, nil)
	if err != nil {
		t.Fatalf("Failed to get summaries: %v", err)
	}

	if len(summaries) != 10 {
		t.Errorf("Expected 10 summaries, got %d", len(summaries))
	}
}

// Tests for source/site filtering

func TestSaveAndRetrieveWithSourceFilter(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	now := time.Now()

	// Save summaries with different source types
	summaries := []*Summary{
		{
			Timestamp:       now,
			LogSourceType:   "logwatch",
			SiteName:        "",
			SystemStatus:    "Good",
			Summary:         "Logwatch summary",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     1000,
			OutputTokens:    500,
			CostUSD:         0.01,
		},
		{
			Timestamp:       now,
			LogSourceType:   "drupal_watchdog",
			SiteName:        "production",
			SystemStatus:    "Warning",
			Summary:         "Drupal production summary",
			CriticalIssues:  []string{},
			Warnings:        []string{"Warning 1"},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     2000,
			OutputTokens:    1000,
			CostUSD:         0.02,
		},
		{
			Timestamp:       now,
			LogSourceType:   "drupal_watchdog",
			SiteName:        "staging",
			SystemStatus:    "Good",
			Summary:         "Drupal staging summary",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     1500,
			OutputTokens:    750,
			CostUSD:         0.015,
		},
	}

	for _, s := range summaries {
		if err := storage.SaveSummary(s); err != nil {
			t.Fatalf("Failed to save summary: %v", err)
		}
	}

	// Test: nil filter returns all summaries
	all, err := storage.GetRecentSummaries(7, nil)
	if err != nil {
		t.Fatalf("Failed to get all summaries: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("Expected 3 summaries with nil filter, got %d", len(all))
	}

	// Test: filter by logwatch
	logwatchFilter := &SourceFilter{LogSourceType: "logwatch", SiteName: ""}
	logwatchSummaries, err := storage.GetRecentSummaries(7, logwatchFilter)
	if err != nil {
		t.Fatalf("Failed to get logwatch summaries: %v", err)
	}
	if len(logwatchSummaries) != 1 {
		t.Fatalf("Expected 1 logwatch summary, got %d", len(logwatchSummaries))
	}
	if logwatchSummaries[0].Summary != "Logwatch summary" {
		t.Errorf("Wrong summary returned for logwatch filter")
	}

	// Test: filter by drupal_watchdog + production site
	prodFilter := &SourceFilter{LogSourceType: "drupal_watchdog", SiteName: "production"}
	prodSummaries, err := storage.GetRecentSummaries(7, prodFilter)
	if err != nil {
		t.Fatalf("Failed to get production summaries: %v", err)
	}
	if len(prodSummaries) != 1 {
		t.Fatalf("Expected 1 production summary, got %d", len(prodSummaries))
	}
	if prodSummaries[0].Summary != "Drupal production summary" {
		t.Errorf("Wrong summary returned for production filter")
	}

	// Test: filter by drupal_watchdog + staging site
	stagingFilter := &SourceFilter{LogSourceType: "drupal_watchdog", SiteName: "staging"}
	stagingSummaries, err := storage.GetRecentSummaries(7, stagingFilter)
	if err != nil {
		t.Fatalf("Failed to get staging summaries: %v", err)
	}
	if len(stagingSummaries) != 1 {
		t.Fatalf("Expected 1 staging summary, got %d", len(stagingSummaries))
	}
	if stagingSummaries[0].Summary != "Drupal staging summary" {
		t.Errorf("Wrong summary returned for staging filter")
	}

	// Test: filter with non-existent site returns empty
	nonExistentFilter := &SourceFilter{LogSourceType: "drupal_watchdog", SiteName: "nonexistent"}
	emptySummaries, err := storage.GetRecentSummaries(7, nonExistentFilter)
	if err != nil {
		t.Fatalf("Failed to get nonexistent summaries: %v", err)
	}
	if len(emptySummaries) != 0 {
		t.Errorf("Expected 0 summaries for nonexistent site, got %d", len(emptySummaries))
	}
}

func TestGetHistoricalContextWithFilter(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	now := time.Now()

	// Save summaries for different sources
	logwatchSummary := &Summary{
		Timestamp:       now,
		LogSourceType:   "logwatch",
		SiteName:        "",
		SystemStatus:    "Good",
		Summary:         "Logwatch test",
		CriticalIssues:  []string{"Logwatch issue"},
		Warnings:        []string{},
		Recommendations: []string{},
		Metrics:         map[string]interface{}{},
		InputTokens:     1000,
		OutputTokens:    500,
		CostUSD:         0.01,
	}

	drupalSummary := &Summary{
		Timestamp:       now,
		LogSourceType:   "drupal_watchdog",
		SiteName:        "production",
		SystemStatus:    "Warning",
		Summary:         "Drupal test",
		CriticalIssues:  []string{},
		Warnings:        []string{"Drupal warning"},
		Recommendations: []string{},
		Metrics:         map[string]interface{}{},
		InputTokens:     2000,
		OutputTokens:    1000,
		CostUSD:         0.02,
	}

	if err := storage.SaveSummary(logwatchSummary); err != nil {
		t.Fatalf("Failed to save logwatch summary: %v", err)
	}
	if err := storage.SaveSummary(drupalSummary); err != nil {
		t.Fatalf("Failed to save drupal summary: %v", err)
	}

	// Get context for logwatch only
	logwatchFilter := &SourceFilter{LogSourceType: "logwatch", SiteName: ""}
	logwatchContext, err := storage.GetHistoricalContext(7, logwatchFilter)
	if err != nil {
		t.Fatalf("Failed to get logwatch context: %v", err)
	}

	if !strings.Contains(logwatchContext, "Logwatch test") {
		t.Error("Logwatch context should contain logwatch summary")
	}
	if strings.Contains(logwatchContext, "Drupal test") {
		t.Error("Logwatch context should NOT contain drupal summary")
	}
	if !strings.Contains(logwatchContext, "Critical Issues: 1") {
		t.Error("Logwatch context should contain logwatch critical issues count")
	}

	// Get context for drupal only
	drupalFilter := &SourceFilter{LogSourceType: "drupal_watchdog", SiteName: "production"}
	drupalContext, err := storage.GetHistoricalContext(7, drupalFilter)
	if err != nil {
		t.Fatalf("Failed to get drupal context: %v", err)
	}

	if !strings.Contains(drupalContext, "Drupal test") {
		t.Error("Drupal context should contain drupal summary")
	}
	if strings.Contains(drupalContext, "Logwatch test") {
		t.Error("Drupal context should NOT contain logwatch summary")
	}
	if !strings.Contains(drupalContext, "Warnings: 1") {
		t.Error("Drupal context should contain drupal warnings count")
	}
}

func TestGetStatisticsWithFilter(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	now := time.Now()

	// Save summaries with different source types and costs
	summaries := []*Summary{
		{
			Timestamp:       now,
			LogSourceType:   "logwatch",
			SiteName:        "",
			SystemStatus:    "Good",
			Summary:         "Logwatch 1",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     1000,
			OutputTokens:    500,
			CostUSD:         0.01,
		},
		{
			Timestamp:       now,
			LogSourceType:   "logwatch",
			SiteName:        "",
			SystemStatus:    "Warning",
			Summary:         "Logwatch 2",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     1000,
			OutputTokens:    500,
			CostUSD:         0.02,
		},
		{
			Timestamp:       now,
			LogSourceType:   "drupal_watchdog",
			SiteName:        "production",
			SystemStatus:    "Good",
			Summary:         "Drupal prod",
			CriticalIssues:  []string{},
			Warnings:        []string{},
			Recommendations: []string{},
			Metrics:         map[string]interface{}{},
			InputTokens:     2000,
			OutputTokens:    1000,
			CostUSD:         0.05,
		},
	}

	for _, s := range summaries {
		if err := storage.SaveSummary(s); err != nil {
			t.Fatalf("Failed to save summary: %v", err)
		}
	}

	// Test: nil filter returns all stats
	allStats, err := storage.GetStatistics(nil)
	if err != nil {
		t.Fatalf("Failed to get all stats: %v", err)
	}
	if allStats["total_summaries"].(int) != 3 {
		t.Errorf("Expected 3 total summaries, got %d", allStats["total_summaries"].(int))
	}
	expectedTotal := 0.01 + 0.02 + 0.05
	if allStats["total_cost_usd"].(float64) != expectedTotal {
		t.Errorf("Expected total cost %.4f, got %.4f", expectedTotal, allStats["total_cost_usd"].(float64))
	}

	// Test: filter by logwatch
	logwatchFilter := &SourceFilter{LogSourceType: "logwatch", SiteName: ""}
	logwatchStats, err := storage.GetStatistics(logwatchFilter)
	if err != nil {
		t.Fatalf("Failed to get logwatch stats: %v", err)
	}
	if logwatchStats["total_summaries"].(int) != 2 {
		t.Errorf("Expected 2 logwatch summaries, got %d", logwatchStats["total_summaries"].(int))
	}
	expectedLogwatchCost := 0.01 + 0.02
	if logwatchStats["total_cost_usd"].(float64) != expectedLogwatchCost {
		t.Errorf("Expected logwatch cost %.4f, got %.4f", expectedLogwatchCost, logwatchStats["total_cost_usd"].(float64))
	}
	statusDist := logwatchStats["status_distribution"].(map[string]int)
	if statusDist["Good"] != 1 || statusDist["Warning"] != 1 {
		t.Error("Logwatch status distribution incorrect")
	}

	// Test: filter by drupal_watchdog + production
	drupalFilter := &SourceFilter{LogSourceType: "drupal_watchdog", SiteName: "production"}
	drupalStats, err := storage.GetStatistics(drupalFilter)
	if err != nil {
		t.Fatalf("Failed to get drupal stats: %v", err)
	}
	if drupalStats["total_summaries"].(int) != 1 {
		t.Errorf("Expected 1 drupal summary, got %d", drupalStats["total_summaries"].(int))
	}
	if drupalStats["total_cost_usd"].(float64) != 0.05 {
		t.Errorf("Expected drupal cost 0.05, got %.4f", drupalStats["total_cost_usd"].(float64))
	}
}

func TestSaveWithDefaultLogSourceType(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	// Save a summary without setting LogSourceType (should default to "logwatch")
	summary := &Summary{
		Timestamp:       time.Now(),
		SystemStatus:    "Good",
		Summary:         "Test without source type",
		CriticalIssues:  []string{},
		Warnings:        []string{},
		Recommendations: []string{},
		Metrics:         map[string]interface{}{},
		InputTokens:     1000,
		OutputTokens:    500,
		CostUSD:         0.01,
	}

	if err := storage.SaveSummary(summary); err != nil {
		t.Fatalf("Failed to save summary: %v", err)
	}

	// Retrieve with logwatch filter
	logwatchFilter := &SourceFilter{LogSourceType: "logwatch", SiteName: ""}
	summaries, err := storage.GetRecentSummaries(7, logwatchFilter)
	if err != nil {
		t.Fatalf("Failed to get summaries: %v", err)
	}

	if len(summaries) != 1 {
		t.Fatalf("Expected 1 summary with default logwatch type, got %d", len(summaries))
	}

	if summaries[0].LogSourceType != "logwatch" {
		t.Errorf("Expected LogSourceType 'logwatch', got '%s'", summaries[0].LogSourceType)
	}
}
