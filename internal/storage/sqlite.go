package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Storage handles database operations
type Storage struct {
	db *sql.DB
}

// Summary represents a logwatch analysis summary
type Summary struct {
	ID              int64
	Timestamp       time.Time
	SystemStatus    string
	Summary         string
	CriticalIssues  []string
	Warnings        []string
	Recommendations []string
	Metrics         map[string]interface{}
	InputTokens     int
	OutputTokens    int
	CostUSD         float64
}

// New creates a new storage instance
func New(dbPath string) (*Storage, error) {
	// Create directory if it doesn't exist (0700 for security - owner only)
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	storage := &Storage{db: db}

	// Initialize schema
	if err := storage.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return storage, nil
}

// initSchema creates the database schema if it doesn't exist
func (s *Storage) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS summaries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		system_status TEXT NOT NULL,
		summary TEXT NOT NULL,
		critical_issues TEXT,
		warnings TEXT,
		recommendations TEXT,
		metrics TEXT,
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		cost_usd REAL DEFAULT 0.0
	);

	CREATE INDEX IF NOT EXISTS idx_timestamp ON summaries(timestamp);
	CREATE INDEX IF NOT EXISTS idx_system_status ON summaries(system_status);
	`

	_, err := s.db.Exec(schema)
	return err
}

// SaveSummary saves a new summary to the database
func (s *Storage) SaveSummary(summary *Summary) error {
	// Marshal JSON fields
	criticalIssuesJSON, err := json.Marshal(summary.CriticalIssues)
	if err != nil {
		return fmt.Errorf("failed to marshal critical issues: %w", err)
	}

	warningsJSON, err := json.Marshal(summary.Warnings)
	if err != nil {
		return fmt.Errorf("failed to marshal warnings: %w", err)
	}

	recommendationsJSON, err := json.Marshal(summary.Recommendations)
	if err != nil {
		return fmt.Errorf("failed to marshal recommendations: %w", err)
	}

	metricsJSON, err := json.Marshal(summary.Metrics)
	if err != nil {
		return fmt.Errorf("failed to marshal metrics: %w", err)
	}

	// Insert into database
	query := `
		INSERT INTO summaries (
			timestamp, system_status, summary, critical_issues,
			warnings, recommendations, metrics, input_tokens,
			output_tokens, cost_usd
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.db.Exec(
		query,
		summary.Timestamp.Format(time.RFC3339),
		summary.SystemStatus,
		summary.Summary,
		string(criticalIssuesJSON),
		string(warningsJSON),
		string(recommendationsJSON),
		string(metricsJSON),
		summary.InputTokens,
		summary.OutputTokens,
		summary.CostUSD,
	)
	if err != nil {
		return fmt.Errorf("failed to insert summary: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}

	summary.ID = id
	return nil
}

// GetRecentSummaries retrieves summaries from the last N days
func (s *Storage) GetRecentSummaries(days int) ([]*Summary, error) {
	cutoffDate := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)

	query := `
		SELECT id, timestamp, system_status, summary, critical_issues,
		       warnings, recommendations, metrics, input_tokens,
		       output_tokens, cost_usd
		FROM summaries
		WHERE timestamp >= ?
		ORDER BY timestamp DESC
	`

	rows, err := s.db.Query(query, cutoffDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query summaries: %w", err)
	}
	defer func(rows *sql.Rows) {
		err = rows.Close()
		if err != nil {
			log.Printf("storage: failed to close database rows: %v", err)
		}
	}(rows)

	var summaries []*Summary
	for rows.Next() {
		summary, err := s.scanSummary(rows)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}

	return summaries, rows.Err()
}

// GetHistoricalContext retrieves recent summaries formatted for Claude context
func (s *Storage) GetHistoricalContext(days int) (string, error) {
	summaries, err := s.GetRecentSummaries(days)
	if err != nil {
		return "", err
	}

	if len(summaries) == 0 {
		return "", nil
	}

	var context string
	context += fmt.Sprintf("Previous %d analysis summaries:\n\n", len(summaries))

	for i, sum := range summaries {
		context += fmt.Sprintf("%d. %s - Status: %s\n",
			i+1,
			sum.Timestamp.Format("2006-01-02 15:04"),
			sum.SystemStatus,
		)
		context += fmt.Sprintf("   Summary: %s\n", sum.Summary)
		if len(sum.CriticalIssues) > 0 {
			context += fmt.Sprintf("   Critical Issues: %d\n", len(sum.CriticalIssues))
		}
		if len(sum.Warnings) > 0 {
			context += fmt.Sprintf("   Warnings: %d\n", len(sum.Warnings))
		}
		context += "\n"
	}

	return context, nil
}

// CleanupOldSummaries deletes summaries older than N days
func (s *Storage) CleanupOldSummaries(days int) (int64, error) {
	cutoffDate := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)

	query := `DELETE FROM summaries WHERE timestamp < ?`
	result, err := s.db.Exec(query, cutoffDate)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old summaries: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return affected, nil
}

// GetStatistics returns database statistics
func (s *Storage) GetStatistics() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total count
	var total int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM summaries`).Scan(&total)
	if err != nil {
		return nil, err
	}
	stats["total_summaries"] = total

	// Status distribution
	rows, err := s.db.Query(`
		SELECT system_status, COUNT(*)
		FROM summaries
		GROUP BY system_status
	`)
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		err = rows.Close()
		if err != nil {
			log.Printf("storage: failed to close database rows: %v", err)
		}
	}(rows)

	statusDist := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		statusDist[status] = count
	}
	stats["status_distribution"] = statusDist

	// Total cost
	var totalCost float64
	err = s.db.QueryRow(`SELECT COALESCE(SUM(cost_usd), 0) FROM summaries`).Scan(&totalCost)
	if err != nil {
		return nil, err
	}
	stats["total_cost_usd"] = totalCost

	return stats, nil
}

// scanSummary scans a database row into a Summary struct
func (s *Storage) scanSummary(rows *sql.Rows) (*Summary, error) {
	var (
		id                                                    int64
		timestamp                                             string
		systemStatus, summaryText                             string
		criticalIssuesJSON, warningsJSON, recommendationsJSON string
		metricsJSON                                           string
		inputTokens, outputTokens                             int
		costUSD                                               float64
	)

	err := rows.Scan(
		&id, &timestamp, &systemStatus, &summaryText,
		&criticalIssuesJSON, &warningsJSON, &recommendationsJSON,
		&metricsJSON, &inputTokens, &outputTokens, &costUSD,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan row: %w", err)
	}

	// Parse timestamp
	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse timestamp: %w", err)
	}

	// Unmarshal JSON fields
	var criticalIssues, warnings, recommendations []string
	var metrics map[string]interface{}

	if err := json.Unmarshal([]byte(criticalIssuesJSON), &criticalIssues); err != nil {
		return nil, fmt.Errorf("failed to unmarshal critical issues: %w", err)
	}
	if err := json.Unmarshal([]byte(warningsJSON), &warnings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal warnings: %w", err)
	}
	if err := json.Unmarshal([]byte(recommendationsJSON), &recommendations); err != nil {
		return nil, fmt.Errorf("failed to unmarshal recommendations: %w", err)
	}
	if err := json.Unmarshal([]byte(metricsJSON), &metrics); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics: %w", err)
	}

	return &Summary{
		ID:              id,
		Timestamp:       ts,
		SystemStatus:    systemStatus,
		Summary:         summaryText,
		CriticalIssues:  criticalIssues,
		Warnings:        warnings,
		Recommendations: recommendations,
		Metrics:         metrics,
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		CostUSD:         costUSD,
	}, nil
}

// Close closes the database connection
func (s *Storage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
