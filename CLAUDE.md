# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Logwatch AI Analyzer is an intelligent system log analyzer that uses Claude AI to analyze logwatch reports and send actionable insights via Telegram. This is a Go port of the original Node.js implementation, optimized for single-binary deployment with no runtime dependencies.

**Key Technologies:**
- Go 1.25+ with pure Go SQLite (modernc.org/sqlite)
- Anthropic Claude Sonnet 4.5 API
- Telegram Bot API
- SQLite for analysis history

## Build Commands

### Development
```bash
make build          # Development build (verbose)
make run            # Build and run immediately
make test           # Run all tests
make fmt            # Format code with go fmt
make vet            # Run go vet
```

### Production
```bash
make build-prod     # Optimized production build (-ldflags="-s -w" -trimpath)
make install        # Install to /opt/logwatch-ai (requires sudo)
```

### Cross-Platform Builds
```bash
make build-linux-amd64    # Build for Linux AMD64 (Debian 12/Ubuntu 24)
make build-darwin-arm64   # Build for macOS ARM64 (Apple Silicon)
make build-all-platforms  # Build for all platforms at once
```

All cross-platform builds use production optimizations:
- `-ldflags="-s -w"` - Strip symbols and debug information
- `-trimpath` - Remove file system paths from binary

### Testing
```bash
make test                    # Run all tests
make test-coverage           # Generate coverage report (coverage.html)
go test -v ./internal/ai     # Run specific package tests
go test -v ./internal/logwatch
```

### Cleanup
```bash
make clean          # Remove bin/, coverage.out, coverage.html
```

## Project Architecture

### Package Structure

The project follows `golang-standards/project-layout`:

```
cmd/analyzer/           - Main application entry point (main.go)
internal/              - Private application packages (not importable)
  ├── ai/             - Claude AI client, prompts, response parsing
  ├── config/         - Configuration loading (viper + .env)
  ├── logwatch/       - Log reading, preprocessing, token estimation
  ├── notification/   - Telegram client and message formatting
  └── storage/        - SQLite operations (summaries table)
pkg/logger/           - Reusable structured logger (zerolog + lumberjack)
scripts/              - Shell scripts (install.sh, generate-logwatch.sh)
configs/              - Configuration templates (.env.example)
```

### Key Design Patterns

**1. Component Initialization Flow (cmd/analyzer/main.go)**
```
main() → run() → runAnalyzer()
  1. Load config (internal/config)
  2. Initialize logger (pkg/logger)
  3. Initialize storage (internal/storage) - SQLite connection
  4. Initialize Telegram client (internal/notification)
  5. Initialize Claude client (internal/ai)
  6. Initialize logwatch reader (internal/logwatch)
  7. Read & preprocess logs
  8. Retrieve historical context from DB
  9. Analyze with Claude
  10. Save to database
  11. Send Telegram notifications
  12. Cleanup old summaries (>90 days)
```

**2. Configuration Management (internal/config/config.go)**
- Uses `github.com/spf13/viper` for env var loading
- Supports `.env` file via `github.com/joho/godotenv`
- Comprehensive validation (API key format, Telegram IDs, paths)
- Defaults: Claude Sonnet 4.5, info logging, preprocessing enabled

**3. Intelligent Preprocessing (internal/logwatch/preprocessor.go)**

When logs exceed `MAX_PREPROCESSING_TOKENS` (default: 150,000):
1. **Section parsing**: Split by `###` headers
2. **Priority classification**: HIGH/MEDIUM/LOW based on keywords
   - HIGH: ssh, security, auth, fail, error, critical, kernel, sudo
   - MEDIUM: network, disk, service, warning, memory
   - LOW: everything else
3. **Deduplication**: Group similar lines (IP/timestamp/number normalization)
4. **Compression**: Keep 100% HIGH, 50% MEDIUM, 20% LOW priority content

**4. Token Estimation Algorithm**
```go
// Same as Node.js version
tokens = max(chars/4, words/0.75)
```

**5. Database Schema (internal/storage/sqlite.go)**
```sql
CREATE TABLE summaries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,           -- RFC3339 format
    system_status TEXT NOT NULL,       -- Good/Warning/Critical/Bad
    summary TEXT NOT NULL,
    critical_issues TEXT,              -- JSON array
    warnings TEXT,                     -- JSON array
    recommendations TEXT,              -- JSON array
    metrics TEXT,                      -- JSON object
    input_tokens INTEGER,
    output_tokens INTEGER,
    cost_usd REAL
);
```

**6. Claude AI Integration (internal/ai/client.go)**
- Retry logic: 3 attempts with exponential backoff (2^n seconds)
- Prompt caching: System prompt cached for 90% cost reduction on subsequent calls
- Cost calculation: Uses Sonnet 4.5 pricing ($3/MTok input, $15/MTok output)
- Context: Includes last 7 days of analysis history
- Max output: 8000 tokens

**7. Telegram Notifications (internal/notification/telegram.go)**
- **Archive channel**: Always receives full analysis report
- **Alerts channel**: Only for Warning/Critical/Bad statuses (optional)
- Message format: MarkdownV2 with proper escaping
- Handles 4096 char limit (splits messages if needed)
- Retry logic: 2 attempts with 5s delay

## Important Implementation Notes

### Configuration Validation Rules
- `ANTHROPIC_API_KEY` must start with `sk-ant-`
- `TELEGRAM_BOT_TOKEN` must match format `number:token`
- `TELEGRAM_CHANNEL_ARCHIVE_ID` must be < -100 (supergroup/channel ID)
- `MAX_LOG_SIZE_MB` range: 1-100
- `LOG_LEVEL`: debug, info, warn, error

### Proxy Support
Both `HTTP_PROXY` and `HTTPS_PROXY` are supported:
- Claude AI uses HTTPS proxy
- Uses `url.Parse()` + `http.Transport.Proxy`
- Applied to both Claude and Telegram clients

### Error Handling Philosophy
- Graceful degradation: Missing historical context = warning, not failure
- Database save errors = warning (notification still succeeds)
- Failed cleanup = warning (doesn't block main workflow)
- Only fail fast on: config validation, file reading, Claude API, Telegram send

### Testing Approach
- Unit tests for formatting logic (see `internal/notification/telegram_test.go`)
- Use table-driven tests for multiple scenarios
- Mock external dependencies (Telegram API, Claude API)
- Test MarkdownV2 escaping thoroughly

### Code Style
- Use zerolog for structured logging: `log.Info().Str("key", value).Msg("message")`
- Return detailed errors with context: `fmt.Errorf("failed to X: %w", err)`
- Constants for exit codes, timeouts, retry counts
- Defer cleanup: `defer store.Close()`, `defer telegramClient.Close()`

## Development Workflow

### Adding a New Feature
1. Determine which package owns the feature (ai, config, logwatch, notification, storage)
2. Add configuration fields to `internal/config/config.go` if needed
3. Update `.env.example` with new variables
4. Implement logic in appropriate package
5. Update `cmd/analyzer/main.go` workflow if needed
6. Add tests for new functionality
7. Update README.md if user-facing

### Running Tests
```bash
# Run all tests with verbose output
make test

# Run tests for a specific package
go test -v ./internal/ai
go test -v ./internal/logwatch

# Run with coverage
make test-coverage
# Opens coverage.html in browser

# Run specific test
go test -v -run TestFormatMessage ./internal/notification
```

### Testing with Real APIs
When testing with actual Anthropic/Telegram APIs:
1. Copy `configs/.env.example` to `.env`
2. Fill in real credentials
3. Run: `./bin/logwatch-analyzer` (after `make build`)
4. Check logs in `./logs/` directory

### Building for Different Platforms
```bash
# Use Makefile targets for cross-platform builds
make build-linux-amd64       # Linux AMD64 binary
make build-darwin-arm64      # macOS ARM64 binary
make build-all-platforms     # Build all platforms

# Manual cross-compilation (if needed for other platforms)
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -trimpath -o bin/logwatch-analyzer-linux ./cmd/analyzer
GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -trimpath -o bin/logwatch-analyzer-linux-arm64 ./cmd/analyzer
```

## Critical Implementation Details

### Prompt Caching Behavior
- System prompt is marked with `ephemeral` cache control
- First run creates cache (incurs cache write cost: $3.75/MTok)
- Subsequent runs (within 5 min) use cache (90% savings: $0.30/MTok vs $3/MTok)
- Historical context is included in user prompt (not cached)

### Historical Context Format
```
Previous N analysis summaries:

1. 2025-11-12 02:15 - Status: Good
   Summary: System operating normally...
   Critical Issues: 0
   Warnings: 2

2. 2025-11-11 02:15 - Status: Warning
   ...
```

### Telegram Message Structure
```
🔍 Logwatch Analysis Report
🖥 Host: {hostname}
📅 Date: {timestamp}
{status_emoji} Status: {status}

📋 Execution Stats
• Critical Issues: N
• Warnings: N
• Recommendations: N
• Cost: $X.XXXX
• Duration: X.XXs

📊 Summary
{summary_text}

{Critical Issues section if any}
{Warnings section if any}
{Recommendations section if any}
{Key Metrics section if any}
```

### Status Emoji Mapping
- `Good` → 🟢
- `Warning` → 🟡
- `Critical` → 🟠
- `Bad` → 🔴

### Alert Trigger Logic
Alerts sent when status is NOT "Good":
```go
func ShouldTriggerAlert(status string) bool {
    return status != "Good"
}
```

## Database Operations

### Cleanup Policy
- Old summaries deleted after 90 days
- Runs automatically after each analysis
- Uses RFC3339 timestamp format for all queries

### Querying Historical Data
```go
// Last 7 days (for Claude context)
summaries, err := store.GetRecentSummaries(7)

// Custom period
summaries, err := store.GetRecentSummaries(30)

// Statistics
stats, err := store.GetStatistics()
// Returns: total_summaries, status_distribution, total_cost_usd
```

## Common Issues

### "Database is locked"
- Ensure only one instance runs at a time
- Check file permissions on `./data/` directory
- SQLite uses file-level locking (modernc.org/sqlite is pure Go, no CGO)

### "Token estimation seems off"
- Algorithm is calibrated for English text
- Uses max(chars/4, words/0.75) - same as Node.js version
- For non-English, may underestimate by ~10-20%

### "Preprocessing removes too much content"
- Adjust `MAX_PREPROCESSING_TOKENS` (default: 150,000)
- Modify priority classification in `preprocessor.go` for your use case
- HIGH priority sections are never compressed

### "Claude API timeouts"
- Default timeout: 120 seconds
- Large logs may take longer to analyze
- Consider increasing preprocessing aggressiveness

## Cost Optimization

Typical daily costs with default settings:
- **First run**: $0.016-0.022 (cache creation)
- **Cached runs**: $0.011-0.015 (cache hits)
- **Monthly**: ~$0.47
- **Yearly**: ~$5.64

To reduce costs further:
1. Increase `MAX_PREPROCESSING_TOKENS` compression
2. Reduce historical context days (currently 7)
3. Adjust section priority classification
4. Use smaller model (not recommended - quality drop)
