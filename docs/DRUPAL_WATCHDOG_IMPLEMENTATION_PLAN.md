# Drupal Watchdog Analysis - Implementation Plan

## Overview

This document outlines the plan to extend logwatch-ai-go to support Drupal watchdog log analysis alongside the existing logwatch functionality.

**Goal:** Single binary that can analyze either logwatch reports OR Drupal watchdog logs, configurable at runtime.

**Estimated Effort:** 2-3 weeks

---

## Phase 1: Architecture Refactoring (3-4 days)

### 1.1 Define Common Interfaces

Create `internal/analyzer/interfaces.go`:

```go
package analyzer

import "context"

// LogReader reads and validates log content from a source
type LogReader interface {
    // Read reads log content from the specified source (file path, stdin, etc.)
    Read(source string) (string, error)

    // Validate checks if the content is valid for this log type
    Validate(content string) error

    // GetSourceInfo returns metadata about the log source
    GetSourceInfo(source string) (map[string]interface{}, error)
}

// Preprocessor handles content preprocessing for large logs
type Preprocessor interface {
    // EstimateTokens estimates the number of tokens in the content
    EstimateTokens(content string) int

    // Process preprocesses content to reduce size while preserving critical info
    Process(content string) (string, error)

    // ShouldProcess determines if preprocessing is needed
    ShouldProcess(content string, maxTokens int) bool
}

// PromptBuilder constructs prompts for Claude AI analysis
type PromptBuilder interface {
    // GetSystemPrompt returns the system prompt for this log type
    GetSystemPrompt() string

    // GetUserPrompt constructs the user prompt with log content and history
    GetUserPrompt(logContent, historicalContext string) string

    // GetLogType returns the type identifier (e.g., "logwatch", "drupal_watchdog")
    GetLogType() string
}
```

### 1.2 Create Log Source Registry

Create `internal/analyzer/registry.go`:

```go
package analyzer

// LogSourceType identifies the type of log source
type LogSourceType string

const (
    LogSourceLogwatch       LogSourceType = "logwatch"
    LogSourceDrupalWatchdog LogSourceType = "drupal_watchdog"
)

// LogSource bundles all components for a specific log type
type LogSource struct {
    Type         LogSourceType
    Reader       LogReader
    Preprocessor Preprocessor
    PromptBuilder PromptBuilder
}

// Registry holds all registered log sources
type Registry struct {
    sources map[LogSourceType]*LogSource
}

func NewRegistry() *Registry {
    return &Registry{
        sources: make(map[LogSourceType]*LogSource),
    }
}

func (r *Registry) Register(source *LogSource) {
    r.sources[source.Type] = source
}

func (r *Registry) Get(sourceType LogSourceType) (*LogSource, bool) {
    source, ok := r.sources[sourceType]
    return source, ok
}
```

### 1.3 Refactor Existing Logwatch Code

Modify existing packages to implement interfaces:

**`internal/logwatch/reader.go`** - Already close to interface, minor changes:
- Rename `ReadLogwatchOutput` → `Read`
- Ensure `validateContent` is public as `Validate`

**`internal/logwatch/preprocessor.go`** - Already close to interface:
- Add `ShouldProcess` method

**`internal/ai/prompt.go`** - Extract to interface:
- Create `LogwatchPromptBuilder` struct implementing `PromptBuilder`

---

## Phase 2: Drupal Watchdog Implementation (5-7 days)

### 2.1 Understand Drupal Watchdog Format

Drupal watchdog logs can be exported in several formats:

**Option A: JSON export (recommended)**
```json
[
  {
    "wid": 12345,
    "uid": 1,
    "type": "php",
    "message": "PDOException: SQLSTATE[HY000]: General error: 1205 Lock wait timeout",
    "variables": "a:0:{}",
    "severity": 3,
    "link": "",
    "location": "https://example.com/admin/content",
    "referer": "https://example.com/admin",
    "hostname": "192.168.1.100",
    "timestamp": 1699900800
  }
]
```

**Option B: Drush watchdog-show output**
```
 ID      Date                 Type     Severity  Message
 ------- -------------------- -------- --------- ----------------------------------------
 12345   2024-11-13 10:00:00  php      error     PDOException: SQLSTATE[HY000]...
 12344   2024-11-13 09:55:00  access   notice    Login attempt failed for admin
```

**Option C: Syslog format (if Drupal configured to use syslog)**
```
Nov 13 10:00:00 webserver drupal: https://example.com|1699900800|php|192.168.1.100|...|error|PDOException...
```

**Recommendation:** Support JSON as primary format (most complete), with drush output as secondary.

### 2.2 Create Drupal Package Structure

```
internal/drupal/
├── reader.go           # DrupalReader implementing LogReader
├── reader_test.go
├── preprocessor.go     # DrupalPreprocessor implementing Preprocessor
├── preprocessor_test.go
├── prompt.go          # DrupalPromptBuilder implementing PromptBuilder
├── prompt_test.go
├── types.go           # Drupal-specific types (WatchdogEntry, etc.)
└── severity.go        # Drupal severity level handling
```

### 2.3 Implement Drupal Reader

Create `internal/drupal/reader.go`:

```go
package drupal

import (
    "encoding/json"
    "fmt"
    "os"
    "time"
)

// WatchdogEntry represents a single Drupal watchdog log entry
type WatchdogEntry struct {
    WID       int64             `json:"wid"`
    UID       int64             `json:"uid"`
    Type      string            `json:"type"`
    Message   string            `json:"message"`
    Variables string            `json:"variables"`
    Severity  int               `json:"severity"`
    Link      string            `json:"link"`
    Location  string            `json:"location"`
    Referer   string            `json:"referer"`
    Hostname  string            `json:"hostname"`
    Timestamp int64             `json:"timestamp"`
}

// Severity levels (RFC 5424)
const (
    SeverityEmergency = 0  // System is unusable
    SeverityAlert     = 1  // Action must be taken immediately
    SeverityCritical  = 2  // Critical conditions
    SeverityError     = 3  // Error conditions
    SeverityWarning   = 4  // Warning conditions
    SeverityNotice    = 5  // Normal but significant condition
    SeverityInfo      = 6  // Informational messages
    SeverityDebug     = 7  // Debug-level messages
)

type Reader struct {
    maxSizeMB           int
    enablePreprocessing bool
    maxTokens           int
    preprocessor        *Preprocessor
    format              string // "json" or "drush"
}

func NewReader(maxSizeMB int, enablePreprocessing bool, maxTokens int, format string) *Reader {
    return &Reader{
        maxSizeMB:           maxSizeMB,
        enablePreprocessing: enablePreprocessing,
        maxTokens:           maxTokens,
        preprocessor:        NewPreprocessor(maxTokens),
        format:              format,
    }
}

// Read implements LogReader.Read
func (r *Reader) Read(source string) (string, error) {
    // Check file exists and size
    fileInfo, err := os.Stat(source)
    if err != nil {
        return "", fmt.Errorf("watchdog file not found: %s", source)
    }

    maxBytes := int64(r.maxSizeMB) * 1024 * 1024
    if fileInfo.Size() > maxBytes {
        return "", fmt.Errorf("watchdog file exceeds %dMB", r.maxSizeMB)
    }

    content, err := os.ReadFile(source)
    if err != nil {
        return "", fmt.Errorf("failed to read watchdog file: %w", err)
    }

    contentStr := string(content)

    // Parse and normalize to consistent format
    if r.format == "json" {
        entries, err := r.parseJSON(contentStr)
        if err != nil {
            return "", err
        }
        contentStr = r.formatEntriesForAnalysis(entries)
    }

    // Validate
    if err := r.Validate(contentStr); err != nil {
        return "", err
    }

    // Preprocess if needed
    if r.enablePreprocessing && r.preprocessor.ShouldProcess(contentStr, r.maxTokens) {
        return r.preprocessor.Process(contentStr)
    }

    return contentStr, nil
}

func (r *Reader) parseJSON(content string) ([]WatchdogEntry, error) {
    var entries []WatchdogEntry
    if err := json.Unmarshal([]byte(content), &entries); err != nil {
        return nil, fmt.Errorf("failed to parse watchdog JSON: %w", err)
    }
    return entries, nil
}

func (r *Reader) formatEntriesForAnalysis(entries []WatchdogEntry) string {
    // Format entries in a way that's easy for Claude to analyze
    // Group by type and severity, show timestamps, counts, etc.
    // ... implementation details
}

// Validate implements LogReader.Validate
func (r *Reader) Validate(content string) error {
    if len(content) == 0 {
        return fmt.Errorf("watchdog content is empty")
    }
    if len(content) < 50 {
        return fmt.Errorf("watchdog content too small to be valid")
    }
    return nil
}

// GetSourceInfo implements LogReader.GetSourceInfo
func (r *Reader) GetSourceInfo(source string) (map[string]interface{}, error) {
    fileInfo, err := os.Stat(source)
    if err != nil {
        return nil, err
    }
    return map[string]interface{}{
        "size_bytes": fileInfo.Size(),
        "size_mb":    float64(fileInfo.Size()) / 1024 / 1024,
        "modified":   fileInfo.ModTime(),
        "age_hours":  time.Since(fileInfo.ModTime()).Hours(),
        "format":     r.format,
    }, nil
}
```

### 2.4 Implement Drupal Preprocessor

Create `internal/drupal/preprocessor.go`:

```go
package drupal

import (
    "fmt"
    "strings"
)

type Preprocessor struct {
    maxTokens int
}

func NewPreprocessor(maxTokens int) *Preprocessor {
    return &Preprocessor{maxTokens: maxTokens}
}

// Priority keywords for Drupal
var drupalHighPriority = []string{
    "security", "access denied", "permission", "login failed",
    "sql injection", "xss", "csrf", "unauthorized",
    "exception", "fatal", "critical", "emergency",
    "database", "pdo", "mysql", "connection refused",
}

var drupalMediumPriority = []string{
    "warning", "deprecated", "notice", "cron",
    "cache", "performance", "memory", "timeout",
    "module", "theme", "update",
}

func (p *Preprocessor) EstimateTokens(content string) int {
    chars := len(content)
    words := len(strings.Fields(content))
    charsEstimate := chars / 4
    wordsEstimate := int(float64(words) / 0.75)
    if charsEstimate > wordsEstimate {
        return charsEstimate
    }
    return wordsEstimate
}

func (p *Preprocessor) ShouldProcess(content string, maxTokens int) bool {
    return p.EstimateTokens(content) > maxTokens
}

func (p *Preprocessor) Process(content string) (string, error) {
    // Group entries by type (php, access, cron, system, etc.)
    // Prioritize by severity and keywords
    // Deduplicate similar messages
    // Keep all severity 0-3 (emergency through error)
    // Sample severity 4-5 (warning, notice)
    // Heavily sample severity 6-7 (info, debug)

    // ... implementation
    return content, nil
}
```

### 2.5 Implement Drupal Prompt Builder

Create `internal/drupal/prompt.go`:

```go
package drupal

// DrupalPromptBuilder implements PromptBuilder for Drupal watchdog analysis
type DrupalPromptBuilder struct{}

func NewPromptBuilder() *DrupalPromptBuilder {
    return &DrupalPromptBuilder{}
}

func (p *DrupalPromptBuilder) GetLogType() string {
    return "drupal_watchdog"
}

func (p *DrupalPromptBuilder) GetSystemPrompt() string {
    return `You are a senior Drupal developer and security analyst with expertise in Drupal application security, performance, and operations. Your role is to analyze Drupal watchdog logs and provide actionable insights.

**Drupal Watchdog Severity Levels (RFC 5424):**
- 0 (Emergency): System is unusable
- 1 (Alert): Action must be taken immediately
- 2 (Critical): Critical conditions
- 3 (Error): Error conditions
- 4 (Warning): Warning conditions
- 5 (Notice): Normal but significant condition
- 6 (Info): Informational messages
- 7 (Debug): Debug-level messages

**Analysis Framework:**

1. **Application Status Assessment** - Classify overall Drupal health:
   - "Excellent" - No issues, optimal performance
   - "Good" - Minor issues that don't affect operations
   - "Satisfactory" - Some concerns but application is stable
   - "Bad" - Significant issues requiring attention
   - "Awful" - Critical failures, application stability at risk

2. **Security Analysis** - Identify threats:
   - Failed login attempts (brute force detection)
   - Access denied patterns (permission issues or attacks)
   - SQL injection attempts
   - XSS/CSRF attempts
   - Unauthorized access to admin paths
   - Suspicious user behavior patterns

3. **Application Health Indicators:**
   - PHP errors and exceptions
   - Database connection issues
   - Module/theme errors
   - Cron job failures
   - Cache problems
   - Memory exhaustion
   - Performance bottlenecks

4. **Common Drupal Issues to Watch:**
   - Views query errors
   - Entity/field access problems
   - File permission issues
   - Update/migration problems
   - API integration failures
   - Search indexing issues

5. **Recommendations** - Provide specific, actionable steps:
   - Drupal-specific fixes (drush commands, module settings)
   - Security hardening recommendations
   - Performance optimization suggestions
   - Monitoring improvements

6. **Metrics Extraction:**
   - failedLogins: number of failed login attempts
   - accessDenied: number of access denied events
   - phpErrors: count of PHP errors by severity
   - dbErrors: database-related errors
   - cronStatus: cron execution status
   - topErrorTypes: most frequent error types

**Output Requirements:**

You MUST respond with a valid JSON object (and ONLY JSON) in this exact format:

{
  "systemStatus": "Excellent|Good|Satisfactory|Bad|Awful",
  "summary": "2-3 sentence overview of Drupal application state",
  "criticalIssues": [
    "Urgent issue requiring immediate action"
  ],
  "warnings": [
    "Concerning issue that should be monitored"
  ],
  "recommendations": [
    "Specific Drupal recommendation with drush commands if applicable"
  ],
  "metrics": {
    "failedLogins": 0,
    "accessDenied": 0,
    "phpErrors": {"error": 5, "warning": 12},
    "dbErrors": 0,
    "topErrorTypes": ["php", "access", "cron"]
  }
}

**Analysis Principles:**
- Focus on Drupal-specific patterns and issues
- Prioritize security issues (especially login/access patterns)
- Consider historical context for trend analysis
- Provide Drupal-specific recommendations (drush, admin UI paths)
- Group similar errors to identify patterns
- Be specific about affected modules/themes when identifiable`
}

func (p *DrupalPromptBuilder) GetUserPrompt(logContent, historicalContext string) string {
    var prompt strings.Builder

    prompt.WriteString("DRUPAL WATCHDOG LOGS:\n")
    prompt.WriteString(logContent) // Should be sanitized before passing
    prompt.WriteString("\n\n")

    if historicalContext != "" {
        prompt.WriteString("HISTORICAL CONTEXT:\n")
        prompt.WriteString(historicalContext)
        prompt.WriteString("\n\n")
    }

    prompt.WriteString("Please analyze the Drupal watchdog logs above and provide your assessment in JSON format as specified.")

    return prompt.String()
}
```

---

## Phase 3: Configuration Updates (1-2 days)

### 3.1 Update Config Structure

Modify `internal/config/config.go`:

```go
type Config struct {
    // ... existing fields ...

    // Log Source Selection (NEW)
    LogSourceType string  // "logwatch" or "drupal_watchdog"

    // Drupal-specific (NEW)
    DrupalWatchdogPath   string  // Path to watchdog export file
    DrupalWatchdogFormat string  // "json" or "drush"
    DrupalSiteName       string  // Optional: site identifier for multi-site
}
```

### 3.2 Update .env.example

Add new configuration options:

```bash
# Log Source Configuration
# Options: "logwatch" (default) or "drupal_watchdog"
LOG_SOURCE_TYPE=logwatch

# Logwatch Configuration (used when LOG_SOURCE_TYPE=logwatch)
LOGWATCH_OUTPUT_PATH=/tmp/logwatch-output.txt

# Drupal Watchdog Configuration (used when LOG_SOURCE_TYPE=drupal_watchdog)
DRUPAL_WATCHDOG_PATH=/var/log/drupal-watchdog.json
DRUPAL_WATCHDOG_FORMAT=json
DRUPAL_SITE_NAME=production
```

### 3.3 Update Validation

Add validation for Drupal configuration:

```go
func (c *Config) Validate() error {
    // ... existing validation ...

    // Validate based on log source type
    switch c.LogSourceType {
    case "logwatch":
        if c.LogwatchOutputPath == "" {
            return fmt.Errorf("LOGWATCH_OUTPUT_PATH is required when LOG_SOURCE_TYPE=logwatch")
        }
    case "drupal_watchdog":
        if c.DrupalWatchdogPath == "" {
            return fmt.Errorf("DRUPAL_WATCHDOG_PATH is required when LOG_SOURCE_TYPE=drupal_watchdog")
        }
        if c.DrupalWatchdogFormat != "json" && c.DrupalWatchdogFormat != "drush" {
            return fmt.Errorf("DRUPAL_WATCHDOG_FORMAT must be 'json' or 'drush'")
        }
    default:
        return fmt.Errorf("LOG_SOURCE_TYPE must be 'logwatch' or 'drupal_watchdog'")
    }

    return nil
}
```

---

## Phase 4: Main Workflow Refactoring (2-3 days)

### 4.1 Update main.go

Refactor `cmd/analyzer/main.go` to use interfaces:

```go
func runAnalyzer(ctx context.Context, cfg *config.Config, log *logging.SecureLogger) error {
    startTime := time.Now()

    // Initialize components (unchanged)
    // ... storage, telegram, claude initialization ...

    // NEW: Select log source based on configuration
    var logSource *analyzer.LogSource

    switch cfg.LogSourceType {
    case "logwatch":
        logSource = &analyzer.LogSource{
            Type:   analyzer.LogSourceLogwatch,
            Reader: logwatch.NewReader(cfg.MaxLogSizeMB, cfg.EnablePreprocessing, cfg.MaxPreprocessingTokens),
            Preprocessor: logwatch.NewPreprocessor(cfg.MaxPreprocessingTokens),
            PromptBuilder: logwatch.NewPromptBuilder(),
        }
    case "drupal_watchdog":
        logSource = &analyzer.LogSource{
            Type:   analyzer.LogSourceDrupalWatchdog,
            Reader: drupal.NewReader(cfg.MaxLogSizeMB, cfg.EnablePreprocessing, cfg.MaxPreprocessingTokens, cfg.DrupalWatchdogFormat),
            Preprocessor: drupal.NewPreprocessor(cfg.MaxPreprocessingTokens),
            PromptBuilder: drupal.NewPromptBuilder(),
        }
    default:
        return fmt.Errorf("unsupported log source type: %s", cfg.LogSourceType)
    }

    // Get source path
    sourcePath := cfg.GetLogSourcePath() // NEW method

    // Read log content using interface
    log.Info().Str("path", sourcePath).Str("type", string(logSource.Type)).Msg("Reading log content...")
    logContent, err := logSource.Reader.Read(sourcePath)
    if err != nil {
        return fmt.Errorf("failed to read log content: %w", err)
    }

    // ... rest of workflow uses interfaces ...

    // Analyze with Claude (using prompt builder)
    systemPrompt := logSource.PromptBuilder.GetSystemPrompt()
    userPrompt := logSource.PromptBuilder.GetUserPrompt(logContent, historicalContext)

    analysis, stats, err := claudeClient.Analyze(ctx, systemPrompt, userPrompt)
    // ...
}
```

### 4.2 Update Claude Client

Modify `internal/ai/client.go` to accept dynamic prompts:

```go
// Current: AnalyzeLogwatch(ctx, content, history) - hardcoded prompt
// New: Analyze(ctx, systemPrompt, userPrompt) - flexible

func (c *Client) Analyze(ctx context.Context, systemPrompt, userPrompt string) (*Analysis, *Stats, error) {
    // Use provided prompts instead of hardcoded ones
    // ... implementation
}

// Keep AnalyzeLogwatch for backward compatibility
func (c *Client) AnalyzeLogwatch(ctx context.Context, content, history string) (*Analysis, *Stats, error) {
    return c.Analyze(ctx, GetSystemPrompt(), GetUserPrompt(content, history))
}
```

---

## Phase 5: Testing (2-3 days)

### 5.1 Unit Tests

Create comprehensive tests for all new code:

```
internal/drupal/reader_test.go
internal/drupal/preprocessor_test.go
internal/drupal/prompt_test.go
internal/analyzer/registry_test.go
```

### 5.2 Test Data

Create sample Drupal watchdog data files:

```
testdata/drupal/
├── watchdog_clean.json       # Normal operation
├── watchdog_security.json    # Security incidents
├── watchdog_errors.json      # Various PHP/DB errors
├── watchdog_large.json       # Large file for preprocessing tests
└── watchdog_drush.txt        # Drush output format
```

### 5.3 Integration Tests

Test end-to-end workflow with both log types:

```go
func TestLogwatchWorkflow(t *testing.T) { ... }
func TestDrupalWatchdogWorkflow(t *testing.T) { ... }
func TestSwitchingBetweenSources(t *testing.T) { ... }
```

---

## Phase 6: Documentation (1 day)

### 6.1 Update README.md

- Add Drupal watchdog section
- Document configuration options
- Provide example .env configurations

### 6.2 Update CLAUDE.md

- Add Drupal-specific patterns
- Document new package structure
- Update architecture diagrams

### 6.3 Create Drupal Setup Guide

Create `docs/DRUPAL_WATCHDOG_SETUP.md`:

- How to export watchdog logs
- Drush commands for export
- Cron job configuration
- Multi-site considerations

---

## Phase 7: Drupal Export Scripts (Optional, 1 day)

### 7.1 Create Drupal Drush Script

Create `scripts/generate-drupal-watchdog.sh`:

```bash
#!/bin/bash
# Export Drupal watchdog to JSON for analysis

DRUPAL_ROOT="${DRUPAL_ROOT:-/var/www/html}"
OUTPUT_PATH="${OUTPUT_PATH:-/tmp/drupal-watchdog.json}"
HOURS="${HOURS:-24}"

cd "$DRUPAL_ROOT" || exit 1

# Calculate timestamp for time filter
SINCE=$(date -d "-${HOURS} hours" +%s)

# Export using drush with custom script or Views data export
drush sql:query "
  SELECT
    wid, uid, type, message, variables, severity,
    link, location, referer, hostname, timestamp
  FROM watchdog
  WHERE timestamp > $SINCE
  ORDER BY timestamp DESC
" --format=json > "$OUTPUT_PATH"

echo "Exported watchdog entries to $OUTPUT_PATH"
```

### 7.2 Create Drupal Module (Optional)

If needed, create a simple Drupal module for better export:

```
scripts/drupal/
├── watchdog_export/
│   ├── watchdog_export.info.yml
│   └── watchdog_export.module
```

---

## File Structure After Implementation

```
logwatch-ai-go/
├── cmd/analyzer/
│   └── main.go                    # Updated to use interfaces
├── internal/
│   ├── analyzer/                  # NEW: Common interfaces
│   │   ├── interfaces.go
│   │   ├── registry.go
│   │   └── registry_test.go
│   ├── ai/
│   │   ├── client.go             # Updated: Analyze() method
│   │   ├── prompt.go             # Refactored: LogwatchPromptBuilder
│   │   └── ...
│   ├── config/
│   │   └── config.go             # Updated: LogSourceType, Drupal fields
│   ├── drupal/                   # NEW: Drupal watchdog package
│   │   ├── reader.go
│   │   ├── reader_test.go
│   │   ├── preprocessor.go
│   │   ├── preprocessor_test.go
│   │   ├── prompt.go
│   │   ├── prompt_test.go
│   │   ├── types.go
│   │   └── severity.go
│   ├── logwatch/                 # Refactored: implements interfaces
│   │   ├── reader.go
│   │   ├── preprocessor.go
│   │   ├── prompt.go             # NEW: LogwatchPromptBuilder
│   │   └── ...
│   └── ...
├── scripts/
│   ├── generate-drupal-watchdog.sh # NEW: Drupal export script
│   └── ...
├── testdata/
│   └── drupal/                   # NEW: Test fixtures
│       ├── watchdog_clean.json
│       ├── watchdog_security.json
│       └── ...
├── docs/
│   ├── DRUPAL_WATCHDOG_SETUP.md  # NEW: Setup guide
│   └── ...
└── configs/
    └── .env.example              # Updated: Drupal options
```

---

## Migration Path

### For Existing Logwatch Users

No changes required! Default `LOG_SOURCE_TYPE=logwatch` maintains backward compatibility.

### For New Drupal Users

1. Set `LOG_SOURCE_TYPE=drupal_watchdog`
2. Configure `DRUPAL_WATCHDOG_PATH`
3. Set up export cron job
4. Run analyzer

---

## Implementation Order

1. **Week 1:**
   - Phase 1: Create interfaces (analyzer package)
   - Phase 2.1-2.3: Drupal reader implementation
   - Begin Phase 2.4: Drupal preprocessor

2. **Week 2:**
   - Complete Phase 2.4-2.5: Preprocessor and prompt builder
   - Phase 3: Configuration updates
   - Phase 4: Main workflow refactoring

3. **Week 3:**
   - Phase 5: Testing
   - Phase 6: Documentation
   - Phase 7: Export scripts (optional)
   - Final integration testing and polish

---

## Success Criteria

- [ ] Both logwatch and drupal_watchdog modes work correctly
- [ ] Switching between modes via config only (no code changes)
- [ ] All existing tests pass
- [ ] New tests for Drupal functionality pass
- [ ] Documentation complete
- [ ] Backward compatible with existing deployments
- [ ] Cost tracking works for both log types
- [ ] Historical context works across log types (separate or combined)

---

## Future Enhancements

After initial implementation, consider:

1. **Multiple sources in single run** - Analyze both logwatch AND Drupal in one execution
2. **Custom log formats** - Plugin system for other log types (nginx, apache, syslog)
3. **Real-time mode** - Watch files for changes and analyze incrementally
4. **Web dashboard** - View historical analysis via web interface
5. **Alerting rules** - Configurable alert thresholds per log type
