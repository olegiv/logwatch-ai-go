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

// Summary represents a log analysis summary
type Summary struct {
	ID              int64
	Timestamp       time.Time
	LogSourceType   string // "logwatch" or "drupal_watchdog"
	SiteName        string // Site identifier (empty for logwatch, site ID for Drupal multi-site)
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

// SourceFilter specifies filtering criteria for log source and site
type SourceFilter struct {
	LogSourceType string // Required: "logwatch" or "drupal_watchdog"
	SiteName      string // Optional: site identifier for Drupal multi-site
}

// Database configuration constants (L-04 fix)
const (
	// busyTimeoutMs is how long SQLite waits when database is locked (5 seconds)
	busyTimeoutMs = 5000
	// maxOpenConns limits concurrent connections (SQLite works best with 1)
	maxOpenConns = 1
	// maxIdleConns is the number of idle connections to keep
	maxIdleConns = 1
	// connMaxLifetime is how long a connection can be reused
	connMaxLifetime = 30 * time.Minute
)

// New creates a new storage instance
func New(dbPath string) (*Storage, error) {
	// Create directory if it doesn't exist (0700 for security - owner only)
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database with busy timeout to prevent indefinite waits (L-04 fix)
	// The _busy_timeout pragma prevents "database is locked" errors by waiting
	dsn := fmt.Sprintf("%s?_busy_timeout=%d", dbPath, busyTimeoutMs)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool (L-04 fix)
	// SQLite works best with a single connection to avoid lock contention
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)

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

// Schema version constants
const (
	// currentSchemaVersion is the latest schema version
	// Increment this when adding new migrations
	currentSchemaVersion = 2
)

// initSchema creates the database schema if it doesn't exist
func (s *Storage) initSchema() error {
	// Create schema_version table first (tracks migration state)
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY
		)
	`); err != nil {
		return fmt.Errorf("failed to create schema_version table: %w", err)
	}

	// Get current schema version
	version := s.getSchemaVersion()

	// Run migrations based on current version
	if err := s.migrateSchema(version); err != nil {
		return fmt.Errorf("schema migration failed: %w", err)
	}

	return nil
}

// getSchemaVersion returns the current schema version (0 if not set)
func (s *Storage) getSchemaVersion() int {
	var version int
	err := s.db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&version)
	if err != nil {
		return 0 // No version set, needs full migration
	}
	return version
}

// setSchemaVersion updates the schema version
func (s *Storage) setSchemaVersion(version int) error {
	// Delete existing and insert new (simpler than upsert for single row)
	if _, err := s.db.Exec(`DELETE FROM schema_version`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`INSERT INTO schema_version (version) VALUES (?)`, version); err != nil {
		return err
	}
	return nil
}

// migrateSchema runs migrations from currentVersion to latest
func (s *Storage) migrateSchema(currentVersion int) error {
	if currentVersion >= currentSchemaVersion {
		return nil // Already up to date
	}

	log.Printf("storage: migrating schema from version %d to %d", currentVersion, currentSchemaVersion)

	// Migration 0 -> 1: Create base summaries table
	if currentVersion < 1 {
		if err := s.migrateV1(); err != nil {
			return fmt.Errorf("migration v1 failed: %w", err)
		}
	}

	// Migration 1 -> 2: Add log_source_type and site_name columns
	if currentVersion < 2 {
		if err := s.migrateV2(); err != nil {
			return fmt.Errorf("migration v2 failed: %w", err)
		}
	}

	// Update schema version
	if err := s.setSchemaVersion(currentSchemaVersion); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	log.Printf("storage: schema migration completed successfully (now at version %d)", currentSchemaVersion)
	return nil
}

// migrateV1 creates the base summaries table (original schema)
func (s *Storage) migrateV1() error {
	log.Printf("storage: running migration v1 - create base tables")

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

// migrateV2 adds log_source_type and site_name columns
func (s *Storage) migrateV2() error {
	log.Printf("storage: running migration v2 - add log_source_type and site_name columns")

	// Check if columns already exist (for databases migrated before version tracking)
	var hasLogSourceType bool
	rows, err := s.db.Query("PRAGMA table_info(summaries)")
	if err != nil {
		return fmt.Errorf("failed to get table info: %w", err)
	}
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue interface{}
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("failed to scan column info: %w", err)
		}
		if name == "log_source_type" {
			hasLogSourceType = true
			break
		}
	}
	_ = rows.Close()

	// Only add columns if they don't exist
	if !hasLogSourceType {
		if _, err := s.db.Exec(`ALTER TABLE summaries ADD COLUMN log_source_type TEXT NOT NULL DEFAULT 'logwatch'`); err != nil {
			return fmt.Errorf("failed to add log_source_type column: %w", err)
		}

		if _, err := s.db.Exec(`ALTER TABLE summaries ADD COLUMN site_name TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("failed to add site_name column: %w", err)
		}
	}

	// Create index (IF NOT EXISTS handles duplicates)
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_source_site ON summaries(log_source_type, site_name)`); err != nil {
		return fmt.Errorf("failed to create source_site index: %w", err)
	}

	return nil
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

	// Default to "logwatch" if not specified
	logSourceType := summary.LogSourceType
	if logSourceType == "" {
		logSourceType = "logwatch"
	}

	// Insert into database
	query := `
		INSERT INTO summaries (
			timestamp, log_source_type, site_name, system_status, summary,
			critical_issues, warnings, recommendations, metrics,
			input_tokens, output_tokens, cost_usd
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.db.Exec(
		query,
		summary.Timestamp.Format(time.RFC3339),
		logSourceType,
		summary.SiteName,
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

// GetRecentSummaries retrieves summaries from the last N days, filtered by source and site
func (s *Storage) GetRecentSummaries(days int, filter *SourceFilter) ([]*Summary, error) {
	cutoffDate := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)

	query := `
		SELECT id, timestamp, log_source_type, site_name, system_status, summary,
		       critical_issues, warnings, recommendations, metrics,
		       input_tokens, output_tokens, cost_usd
		FROM summaries
		WHERE timestamp >= ?
	`
	args := []interface{}{cutoffDate}

	// Apply source filter if provided
	if filter != nil && filter.LogSourceType != "" {
		query += ` AND log_source_type = ?`
		args = append(args, filter.LogSourceType)

		// Filter by site name (empty string matches empty site_name)
		query += ` AND site_name = ?`
		args = append(args, filter.SiteName)
	}

	query += ` ORDER BY timestamp DESC`

	rows, err := s.db.Query(query, args...)
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
// If filter is provided, only summaries matching the source type and site are included
func (s *Storage) GetHistoricalContext(days int, filter *SourceFilter) (string, error) {
	summaries, err := s.GetRecentSummaries(days, filter)
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

// GetStatistics returns database statistics, optionally filtered by source and site
func (s *Storage) GetStatistics(filter *SourceFilter) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Build WHERE clause for filtering
	whereClause := ""
	var args []interface{}
	if filter != nil && filter.LogSourceType != "" {
		whereClause = " WHERE log_source_type = ? AND site_name = ?"
		args = []interface{}{filter.LogSourceType, filter.SiteName}
	}

	// Total count
	var total int
	countQuery := `SELECT COUNT(*) FROM summaries` + whereClause
	err := s.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, err
	}
	stats["total_summaries"] = total

	// Status distribution
	statusQuery := `SELECT system_status, COUNT(*) FROM summaries` + whereClause + ` GROUP BY system_status`
	rows, err := s.db.Query(statusQuery, args...)
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
	costQuery := `SELECT COALESCE(SUM(cost_usd), 0) FROM summaries` + whereClause
	err = s.db.QueryRow(costQuery, args...).Scan(&totalCost)
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
		logSourceType, siteName                               string
		systemStatus, summaryText                             string
		criticalIssuesJSON, warningsJSON, recommendationsJSON string
		metricsJSON                                           string
		inputTokens, outputTokens                             int
		costUSD                                               float64
	)

	err := rows.Scan(
		&id, &timestamp, &logSourceType, &siteName, &systemStatus, &summaryText,
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
		LogSourceType:   logSourceType,
		SiteName:        siteName,
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
