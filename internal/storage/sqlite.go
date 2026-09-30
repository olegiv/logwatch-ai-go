// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

// Package storage persists analysis summaries to a local SQLite database.
package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	internalerrors "github.com/olegiv/logwatch-ai-go/internal/errors"
	"github.com/olegiv/logwatch-ai-go/internal/securefile"
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
	LogSourceType   string // "logwatch", "drupal_watchdog", or "ocms"
	SiteID          string // Stable site identifier (empty for logwatch/single-site sources)
	SiteName        string // Human-readable display name
	SystemStatus    string
	Summary         string
	CriticalIssues  []string
	Warnings        []string
	Recommendations []string
	Metrics         map[string]any
	InputTokens     int
	OutputTokens    int
	CostUSD         float64
}

// SourceFilter specifies filtering criteria for log source and site
type SourceFilter struct {
	LogSourceType string // Required: "logwatch", "drupal_watchdog", or "ocms"
	SiteID        string // Stable site identifier for Drupal/OCMS multi-site
	SiteName      string // Deprecated compatibility fallback for callers predating SiteID
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
	// Historical context is deliberately bounded so stored model output cannot
	// consume the next request's entire context window.
	maxHistoricalSummaries  = 20
	maxHistoricalContextLen = 64 * 1024
	maxHistoricalSummaryLen = 2048
)

// New creates a new storage instance
func New(dbPath string) (*Storage, error) {
	absolutePath, err := validateDatabasePath(dbPath, true)
	if err != nil {
		return nil, err
	}

	// Open database with busy timeout to prevent indefinite waits (L-04 fix)
	// The _busy_timeout pragma prevents "database is locked" errors by waiting
	dsn := fmt.Sprintf("%s?_busy_timeout=%d", absolutePath, busyTimeoutMs)
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

// ValidatePath applies the runtime database path checks without opening or
// migrating SQLite. Missing private components are created exactly as they
// would be during startup, while unsafe existing paths are never mutated.
func ValidatePath(dbPath string) error {
	_, err := validateDatabasePath(dbPath, false)
	return err
}

func validateDatabasePath(dbPath string, createMissing bool) (string, error) {
	if dbPath == "" || strings.HasPrefix(strings.ToLower(dbPath), "file:") || strings.ContainsAny(dbPath, "?\x00") {
		return "", fmt.Errorf("database path must be a literal filesystem path without URI syntax")
	}
	absolutePath, err := filepath.Abs(dbPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve database path: %w", err)
	}

	// The parent must be a dedicated 0700 directory. Missing components are
	// created privately; existing directories are validated without mutation.
	dir := filepath.Dir(absolutePath)
	var canonicalDir string
	if createMissing {
		canonicalDir, err = securefile.EnsurePrivateDirectory(dir)
	} else {
		canonicalDir, err = securefile.ValidatePrivateDirectory(dir)
	}
	if err != nil {
		return "", fmt.Errorf("invalid database directory: %w", err)
	}
	absolutePath = filepath.Join(canonicalDir, filepath.Base(absolutePath))
	if createMissing {
		err = securefile.PreparePrivateRegular(absolutePath)
	} else {
		err = securefile.ValidatePrivateRegular(absolutePath)
	}
	if err != nil {
		return "", fmt.Errorf("database path must be a private regular file: %w", err)
	}
	return absolutePath, nil
}

// Schema version constants
const (
	// currentSchemaVersion is the latest schema version
	// Increment this when adding new migrations
	currentSchemaVersion = 3
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
	err := s.db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version)
	if err != nil {
		return 0 // No version set, needs full migration
	}
	return version
}

// setSchemaVersion updates the schema version within the migration transaction.
func setSchemaVersion(tx *sql.Tx, version int) error {
	if _, err := tx.Exec(`DELETE FROM schema_version`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, version); err != nil {
		return err
	}
	return nil
}

func (s *Storage) setSchemaVersion(version int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := setSchemaVersion(tx, version); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateSchema runs migrations from currentVersion to latest
func (s *Storage) migrateSchema(currentVersion int) error {
	if currentVersion > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", currentVersion, currentSchemaVersion)
	}
	if currentVersion == currentSchemaVersion {
		return nil // Already up to date
	}

	log.Printf("storage: migrating schema from version %d to %d", currentVersion, currentSchemaVersion)

	// Commit each migration and its version marker in one transaction.
	for v := currentVersion; v < currentSchemaVersion; v++ {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration v%d: %w", v+1, err)
		}
		migrationErr := func() error {
			defer func() { _ = tx.Rollback() }()

			switch v {
			case 0:
				// Migration 0 -> 1: Create base summaries table
				if err := migrateV1(tx); err != nil {
					return fmt.Errorf("migration v1 failed: %w", err)
				}
			case 1:
				// Migration 1 -> 2: Add log_source_type and site_name columns
				if err := migrateV2(tx); err != nil {
					return fmt.Errorf("migration v2 failed: %w", err)
				}
			case 2:
				// Migration 2 -> 3: Add stable site IDs and normalize timestamps to UTC.
				if err := migrateV3(tx); err != nil {
					return fmt.Errorf("migration v3 failed: %w", err)
				}
			}
			if err := setSchemaVersion(tx, v+1); err != nil {
				return fmt.Errorf("record schema version %d: %w", v+1, err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit migration v%d: %w", v+1, err)
			}
			return nil
		}()
		if migrationErr != nil {
			return migrationErr
		}
	}

	log.Printf("storage: schema migration completed successfully (now at version %d)", currentSchemaVersion)
	return nil
}

func migrateV3(tx *sql.Tx) error {
	log.Printf("storage: running migration v3 - add stable site IDs and normalize UTC timestamps")

	var siteIDColumns int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('summaries') WHERE name = 'site_id'`).Scan(&siteIDColumns); err != nil {
		return fmt.Errorf("inspect site_id column: %w", err)
	}
	if siteIDColumns == 0 {
		if _, err := tx.Exec(`ALTER TABLE summaries ADD COLUMN site_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add site_id column: %w", err)
		}
	}
	// A legacy site_name is a display label, not a stable identifier. Leave
	// site_id empty until startup can reconcile it against the active site
	// configuration. This also makes rows written by a rolled-back v2 binary
	// recoverable when v3 starts again.
	if _, err := tx.Exec(`
		UPDATE summaries
		SET timestamp = strftime('%Y-%m-%dT%H:%M:%SZ', timestamp)
		WHERE strftime('%Y-%m-%dT%H:%M:%SZ', timestamp) IS NOT NULL
	`); err != nil {
		return fmt.Errorf("normalize timestamps: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_source_site_id ON summaries(log_source_type, site_id)`); err != nil {
		return fmt.Errorf("create source_site_id index: %w", err)
	}
	return nil
}

// migrateV1 creates the base summaries table (original schema)
func migrateV1(tx *sql.Tx) error {
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

	_, err := tx.Exec(schema)
	return err
}

// migrateV2 adds log_source_type and site_name columns
func migrateV2(tx *sql.Tx) error {
	log.Printf("storage: running migration v2 - add log_source_type and site_name columns")

	// Check if columns already exist (for databases migrated before version tracking)
	columns := make(map[string]bool)
	rows, err := tx.Query("PRAGMA table_info(summaries)")
	if err != nil {
		return fmt.Errorf("failed to get table info: %w", err)
	}
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("failed to scan column info: %w", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate table info: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close table info: %w", err)
	}

	// Check each column independently to repair databases left partially
	// migrated by older, non-transactional releases.
	if !columns["log_source_type"] {
		if _, err := tx.Exec(`ALTER TABLE summaries ADD COLUMN log_source_type TEXT NOT NULL DEFAULT 'logwatch'`); err != nil {
			return fmt.Errorf("failed to add log_source_type column: %w", err)
		}
	}
	if !columns["site_name"] {
		if _, err := tx.Exec(`ALTER TABLE summaries ADD COLUMN site_name TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("failed to add site_name column: %w", err)
		}
	}

	// Create index (IF NOT EXISTS handles duplicates)
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_source_site ON summaries(log_source_type, site_name)`); err != nil {
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
			timestamp, log_source_type, site_id, site_name, system_status, summary,
			critical_issues, warnings, recommendations, metrics,
			input_tokens, output_tokens, cost_usd
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.db.Exec(
		query,
		summary.Timestamp.UTC().Format(time.RFC3339Nano),
		logSourceType,
		stableSiteID(summary.SiteID, summary.SiteName),
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

func stableSiteID(siteID, legacySiteName string) string {
	if siteID != "" {
		return siteID
	}
	return legacySiteName
}

// ReconcileSiteIdentity assigns the configured stable identifier to legacy
// rows for the same source and any explicitly configured current or previous
// display name. It repairs both v2 migrations and rows inserted by a v2 binary
// after the database had already reached v3.
func (s *Storage) ReconcileSiteIdentity(logSourceType, siteID string, siteNames ...string) (int64, error) {
	if logSourceType == "" || siteID == "" || len(siteNames) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin site identity reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	seen := make(map[string]struct{}, len(siteNames))
	var affected int64
	for _, siteName := range siteNames {
		if siteName == "" {
			continue
		}
		if _, exists := seen[siteName]; exists {
			continue
		}
		seen[siteName] = struct{}{}

		result, execErr := tx.Exec(`
			UPDATE summaries
			SET site_id = ?
			WHERE log_source_type = ?
			  AND site_name = ?
			  AND (site_id = '' OR site_id = site_name)
		`, siteID, logSourceType, siteName)
		if execErr != nil {
			return 0, fmt.Errorf("reconcile site identity for %q: %w", siteName, execErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return 0, fmt.Errorf("count reconciled site rows for %q: %w", siteName, rowsErr)
		}
		affected += rows
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit site identity reconciliation: %w", err)
	}
	return affected, nil
}

// GetRecentSummaries retrieves summaries from the last N days, filtered by source and site
func (s *Storage) GetRecentSummaries(days int, filter *SourceFilter) ([]*Summary, error) {
	return s.getRecentSummaries(days, filter, 0)
}

func (s *Storage) getRecentSummaries(days int, filter *SourceFilter, limit int) ([]*Summary, error) {
	cutoffUnix := time.Now().UTC().AddDate(0, 0, -days).Unix()

	var query string
	var args []any

	// Build complete query based on filter to avoid SQL fragment concatenation
	if filter != nil && filter.LogSourceType != "" {
		if filter.SiteName != "" {
			query = `
				SELECT id, timestamp, log_source_type, site_id, site_name, system_status, summary,
				       critical_issues, warnings, recommendations, metrics,
				       input_tokens, output_tokens, cost_usd
				FROM summaries
				WHERE unixepoch(timestamp) >= ? AND log_source_type = ?
				  AND (site_id = ? OR ((site_id = '' OR site_id = site_name) AND site_name = ?))
				ORDER BY unixepoch(timestamp) DESC, id DESC
			`
			args = []any{
				cutoffUnix,
				filter.LogSourceType,
				stableSiteID(filter.SiteID, filter.SiteName),
				filter.SiteName,
			}
		} else {
			query = `
				SELECT id, timestamp, log_source_type, site_id, site_name, system_status, summary,
				       critical_issues, warnings, recommendations, metrics,
				       input_tokens, output_tokens, cost_usd
				FROM summaries
				WHERE unixepoch(timestamp) >= ? AND log_source_type = ? AND site_id = ?
				ORDER BY unixepoch(timestamp) DESC, id DESC
			`
			args = []any{cutoffUnix, filter.LogSourceType, stableSiteID(filter.SiteID, filter.SiteName)}
		}
	} else {
		query = `
			SELECT id, timestamp, log_source_type, site_id, site_name, system_status, summary,
			       critical_issues, warnings, recommendations, metrics,
			       input_tokens, output_tokens, cost_usd
			FROM summaries
			WHERE unixepoch(timestamp) >= ?
			ORDER BY unixepoch(timestamp) DESC, id DESC
		`
		args = []any{cutoffUnix}
	}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query summaries: %w", err)
	}
	defer func(rows *sql.Rows) {
		err = rows.Close()
		if err != nil {
			log.Printf("storage: failed to close database rows: %s",
				internalerrors.SanitizeString(err.Error()))
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
	summaries, err := s.getRecentSummaries(days, filter, maxHistoricalSummaries)
	if err != nil {
		return "", err
	}

	if len(summaries) == 0 {
		return "", nil
	}

	entries := make([]string, 0, len(summaries))
	totalLen := 64
	for _, sum := range summaries {
		var entry strings.Builder
		fmt.Fprintf(&entry, "%d. %s - Status: %s\n",
			len(entries)+1,
			sum.Timestamp.Format("2006-01-02 15:04"),
			truncateText(sum.SystemStatus, 32),
		)
		fmt.Fprintf(&entry, "   Summary: %s\n", truncateText(sum.Summary, maxHistoricalSummaryLen))
		if len(sum.CriticalIssues) > 0 {
			fmt.Fprintf(&entry, "   Critical Issues: %d\n", len(sum.CriticalIssues))
		}
		if len(sum.Warnings) > 0 {
			fmt.Fprintf(&entry, "   Warnings: %d\n", len(sum.Warnings))
		}
		entry.WriteString("\n")
		if totalLen+entry.Len() > maxHistoricalContextLen {
			break
		}
		entries = append(entries, entry.String())
		totalLen += entry.Len()
	}

	var context strings.Builder
	fmt.Fprintf(&context, "Previous %d analysis summaries:\n\n", len(entries))
	for _, entry := range entries {
		context.WriteString(entry)
	}

	return context.String(), nil
}

func truncateText(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	cut := maxLen - len("...")
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut] + "..."
}

// CleanupOldSummaries deletes summaries older than N days
func (s *Storage) CleanupOldSummaries(days int) (int64, error) {
	cutoffUnix := time.Now().UTC().AddDate(0, 0, -days).Unix()

	query := `DELETE FROM summaries WHERE unixepoch(timestamp) < ?`
	result, err := s.db.Exec(query, cutoffUnix)
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
func (s *Storage) GetStatistics(filter *SourceFilter) (map[string]any, error) {
	stats := make(map[string]any)

	var countQuery, statusQuery, costQuery string
	var args []any

	// Build complete queries based on filter to avoid SQL fragment concatenation
	if filter != nil && filter.LogSourceType != "" {
		if filter.SiteName != "" {
			args = []any{filter.LogSourceType, stableSiteID(filter.SiteID, filter.SiteName), filter.SiteName}
			countQuery = `SELECT COUNT(*) FROM summaries WHERE log_source_type = ? AND (site_id = ? OR ((site_id = '' OR site_id = site_name) AND site_name = ?))`
			statusQuery = `SELECT system_status, COUNT(*) FROM summaries WHERE log_source_type = ? AND (site_id = ? OR ((site_id = '' OR site_id = site_name) AND site_name = ?)) GROUP BY system_status`
			costQuery = `SELECT COALESCE(SUM(cost_usd), 0) FROM summaries WHERE log_source_type = ? AND (site_id = ? OR ((site_id = '' OR site_id = site_name) AND site_name = ?))`
		} else {
			args = []any{filter.LogSourceType, stableSiteID(filter.SiteID, filter.SiteName)}
			countQuery = `SELECT COUNT(*) FROM summaries WHERE log_source_type = ? AND site_id = ?`
			statusQuery = `SELECT system_status, COUNT(*) FROM summaries WHERE log_source_type = ? AND site_id = ? GROUP BY system_status`
			costQuery = `SELECT COALESCE(SUM(cost_usd), 0) FROM summaries WHERE log_source_type = ? AND site_id = ?`
		}
	} else {
		countQuery = `SELECT COUNT(*) FROM summaries`
		statusQuery = `SELECT system_status, COUNT(*) FROM summaries GROUP BY system_status`
		costQuery = `SELECT COALESCE(SUM(cost_usd), 0) FROM summaries`
	}

	// Total count
	var total int
	err := s.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, err
	}
	stats["total_summaries"] = total

	// Status distribution
	rows, err := s.db.Query(statusQuery, args...)
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		err = rows.Close()
		if err != nil {
			log.Printf("storage: failed to close database rows: %s",
				internalerrors.SanitizeString(err.Error()))
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
		logSourceType, siteID, siteName                       string
		systemStatus, summaryText                             string
		criticalIssuesJSON, warningsJSON, recommendationsJSON string
		metricsJSON                                           string
		inputTokens, outputTokens                             int
		costUSD                                               float64
	)

	err := rows.Scan(
		&id, &timestamp, &logSourceType, &siteID, &siteName, &systemStatus, &summaryText,
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
	var metrics map[string]any

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
		SiteID:          siteID,
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
