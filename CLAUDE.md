# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Logwatch AI Analyzer is an intelligent system log analyzer that uses LLM (Large Language Model) to analyze log reports and send actionable insights via Telegram. This is a Go port of the original Node.js implementation, optimized for single-binary deployment with no runtime dependencies.

**Supported Log Sources:**
- **Logwatch** - Linux system log aggregation and analysis
- **Drupal Watchdog** - PHP/Drupal application log analysis (JSON or drush export format)

**Supported LLM Providers:**
- **Anthropic Claude** - Cloud-based AI (Claude Sonnet 4.5 default)
- **Ollama** - Local LLM inference (llama3.3:latest recommended for high-RAM systems)
- **LM Studio** - Local LLM inference with OpenAI-compatible API

**Key Technologies:**
- Go 1.25+ with pure Go SQLite (modernc.org/sqlite)
- Anthropic Claude API, Ollama, or LM Studio local inference
- Telegram Bot API
- SQLite for analysis history

**Production Status:**
- ✅ **Production Ready** - Successfully deployed to Integration, QA, and Pre-Production environments
- ✅ **Tested on Linux Debian 12** - Primary deployment platform validated
- ✅ **Real API Testing** - Validated with live Claude AI and Telegram Bot APIs
- ✅ **End-to-End Validation** - Full workflow tested with actual logwatch data

**Shared Claude Code Tools:**

This project uses shared Claude Code support tools as a git submodule at `.claude/shared/`. These provide reusable agents, commands, and utilities across projects.

```bash
# Clone with submodules (for new clones)
git clone --recurse-submodules <repo-url>

# Initialize submodule (if cloned without --recurse-submodules)
git submodule update --init --recursive

# Update submodule to latest version
git submodule update --remote .claude/shared

# Check submodule status
git submodule status
```

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
  ├── ai/             - LLM clients (Anthropic, Ollama, LM Studio), prompts, response parsing
  ├── analyzer/       - Multi-source abstraction (interfaces, registry)
  ├── config/         - Configuration loading (viper + .env)
  ├── drupal/         - Drupal watchdog reader, preprocessor, prompts
  ├── errors/         - Error sanitization (credential redaction)
  ├── logging/        - Secure logger wrapper (credential filtering)
  ├── logwatch/       - Logwatch reader, preprocessing, token estimation
  ├── notification/   - Telegram client and message formatting
  └── storage/        - SQLite operations (summaries table)
scripts/              - Shell scripts (install.sh, generate-logwatch.sh)
configs/              - Configuration templates (.env.example)
testdata/             - Test fixtures (logwatch samples, drupal watchdog JSON)
```

**External Dependencies:**
- `github.com/olegiv/go-logger` - Reusable structured logger (zerolog + lumberjack)

### Key Design Patterns

**1. Component Initialization Flow (cmd/analyzer/main.go)**
```
main() → run() → runAnalyzer()
  1. Parse CLI arguments (ParseCLI)
  2. Handle -help, -version, -list-drupal-sites flags
  3. Load config with CLI overrides (LoadWithCLI)
     - Load .env file
     - Apply CLI overrides
     - Load drupal-sites.json for multi-site (if drupal_watchdog)
     - Apply site-specific config
  4. Initialize secure logger (internal/logging wraps go-logger)
  5. Initialize storage (internal/storage) - SQLite connection
  6. Initialize Telegram client (internal/notification)
  7. Initialize Claude client (internal/ai)
  8. Create log source based on LOG_SOURCE_TYPE:
     - logwatch → internal/logwatch (Reader, Preprocessor, PromptBuilder)
     - drupal_watchdog → internal/drupal (Reader, Preprocessor, PromptBuilder)
  9. Read & preprocess logs using source-specific implementation
  10. Retrieve historical context from DB (filtered by source type + site)
  11. Build prompts using source-specific PromptBuilder
  12. Analyze with Claude
  13. Save to database (with source type + site name)
  14. Send Telegram notifications
  15. Cleanup old summaries (>90 days)
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

**4. Multi-Source Log Analysis (internal/analyzer/)**

The analyzer package provides a pluggable architecture for different log sources:

```go
// Core interfaces (internal/analyzer/interfaces.go)
type LogReader interface {
    Read(sourcePath string) (string, error)
    Validate(content string) error
    GetSourceInfo(sourcePath string) (map[string]interface{}, error)
}

type Preprocessor interface {
    EstimateTokens(content string) int
    Process(content string) (string, error)
    ShouldProcess(content string, maxTokens int) bool
}

type PromptBuilder interface {
    GetSystemPrompt() string
    GetUserPrompt(logContent, historicalContext string) string
    GetLogType() string
}
```

**Supported Sources:**
- `LogSourceLogwatch` - Traditional logwatch reports
- `LogSourceDrupalWatchdog` - Drupal watchdog database exports

**5. Drupal Watchdog Analysis (internal/drupal/)**

Drupal-specific log analysis with RFC 5424 severity levels:
```go
// Severity levels (0=Emergency to 7=Debug)
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
```

**Supported Input Formats:**
- `json` - JSON export from watchdog table (recommended)
- `drush` - Output from `drush watchdog:show` command

**Priority Keywords for Preprocessing:**
- HIGH: security, PDO, SQL, fatal, emergency, alert, critical, access denied
- MEDIUM: php, error, warning, cron, update, module
- LOW: notice, info, debug, page not found

**6. Token Estimation Algorithm**
```go
// Same as Node.js version
tokens = max(chars/4, words/0.75)
```

**7. Database Schema (internal/storage/sqlite.go)**
```sql
-- Schema v2 (current)
CREATE TABLE summaries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,           -- RFC3339 format
    log_source_type TEXT NOT NULL DEFAULT 'logwatch',  -- v2: logwatch/drupal_watchdog
    site_name TEXT NOT NULL DEFAULT '',                -- v2: Site identifier for multi-site
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

CREATE INDEX idx_source_site ON summaries(log_source_type, site_name);
```
- **Schema versioning**: Auto-migrates from v1 to v2 (adds log_source_type/site_name)
- **Historical context filtering**: Queries filtered by source type and site name
- Connection timeout: 5s busy timeout prevents indefinite waits on locks
- Connection pool: Single connection (optimal for SQLite), 30-min lifetime

**8. LLM Provider Integration (internal/ai/)**

The `ai` package provides a `Provider` interface for pluggable LLM backends:
```go
type Provider interface {
    Analyze(ctx context.Context, systemPrompt, userPrompt string) (*Analysis, *Stats, error)
    GetModelInfo() map[string]interface{}
    GetProviderName() string
}
```

**Supported Providers:**

*Anthropic Claude (internal/ai/client.go):*
- Retry logic: 3 attempts with exponential backoff (2^n seconds)
- Prompt caching: System prompt cached for 90% cost reduction on subsequent calls
- Cost calculation: Uses Sonnet 4.5 pricing ($3/MTok input, $15/MTok output)
- HTTP proxy support for corporate environments

*Ollama Local LLM (internal/ai/ollama.go):*
- Retry logic: 3 attempts with exponential backoff (2^n seconds)
- Connection check: Verifies Ollama is running and model is available
- Zero cost: Local inference has no monetary cost
- JSON format mode: Requests structured JSON output
- Recommended models for high-RAM systems (96GB+):
  - `llama3.3:latest` - Best quality for reasoning/analysis
  - `qwen2.5:72b` - Excellent for technical analysis
  - `deepseek-coder-v2:33b` - Faster, good quality

*LM Studio (internal/ai/lmstudio.go):*
- OpenAI-compatible API: Uses `/v1/chat/completions` endpoint
- Retry logic: 3 attempts with exponential backoff (2^n seconds)
- Connection check: Verifies LM Studio is running and model is loaded
- Zero cost: Local inference has no monetary cost
- JSON output: Relies on system prompt (LM Studio doesn't support `json_object` response_format)
- Default model identifier: `local-model` (uses currently loaded model)
- Recommended models (GGUF format, download from LM Studio browser):
  - `Llama-3.3-70B-Instruct` - Best quality (~40GB VRAM)
  - `Qwen2.5-32B-Instruct` - Excellent reasoning (~20GB VRAM)
  - `Mistral-Small-24B-Instruct` - Good balance (~15GB VRAM)
  - `Phi-4-14B` - Fast, good quality (~9GB VRAM)
  - `Llama-3.2-8B-Instruct` - Lightweight (~5GB VRAM)

**Common Settings (all providers):**
- Context: Includes last 7 days of analysis history
- Configurable timeout: `AI_TIMEOUT_SECONDS` (default: 120, range: 30-600)
- Configurable max tokens: `AI_MAX_TOKENS` (default: 8000, range: 1000-16000)
- Input sanitization: Log content filtered for prompt injection attempts

**9. Telegram Notifications (internal/notification/telegram.go)**
- **Archive channel**: Always receives full analysis report
- **Alerts channel**: Only for Warning/Critical/Bad statuses (optional)
- Message format: MarkdownV2 with proper escaping
- Handles 4096 char limit (splits messages if needed)
- Rate limiting: 1s minimum between messages, detects 429 errors
- Retry logic: 3 attempts with exponential backoff (2s, 4s, 8s)

## Important Implementation Notes

### Configuration Validation Rules

**LLM Provider Settings:**
- `LLM_PROVIDER`: `anthropic` (default), `ollama`, or `lmstudio`
- When `LLM_PROVIDER=anthropic`:
  - `ANTHROPIC_API_KEY` is required and must start with `sk-ant-`
  - `CLAUDE_MODEL` is required (default: `claude-sonnet-4-5-20250929`)
- When `LLM_PROVIDER=ollama`:
  - `OLLAMA_BASE_URL` is required (default: `http://localhost:11434`)
  - `OLLAMA_MODEL` is required (default: `llama3.3:latest`)
  - Ollama must be running and the model must be available (run `ollama pull <model>` to download)
- When `LLM_PROVIDER=lmstudio`:
  - `LMSTUDIO_BASE_URL` is required (default: `http://localhost:1234`)
  - `LMSTUDIO_MODEL` is optional (default: `local-model`, uses currently loaded model)
  - LM Studio must be running with Local Server enabled and a model loaded

**Telegram Settings:**
- `TELEGRAM_BOT_TOKEN` must match format `number:token`
- `TELEGRAM_CHANNEL_ARCHIVE_ID` must be < -100 (supergroup/channel ID)

**General Settings:**
- `MAX_LOG_SIZE_MB` range: 1-100
- `LOG_LEVEL`: debug, info, warn, error
- `LOG_SOURCE_TYPE`: `logwatch` (default) or `drupal_watchdog`
- When `LOG_SOURCE_TYPE=logwatch`: `LOGWATCH_OUTPUT_PATH` is required
- When `LOG_SOURCE_TYPE=drupal_watchdog`:
  - `jq` must be installed (`apt-get install jq` or `brew install jq` / `port install jq`)
  - `drupal-sites.json` is required (see configs/drupal-sites.json.example)
  - Site must be specified via `-drupal-site` flag or `default_site` in config

### Multi-Site Drupal Configuration

Multi-site support uses `drupal-sites.json` for centralized configuration:

**Configuration File (internal/config/drupal_sites.go):**
```go
type DrupalSite struct {
    Name           string `json:"name"`            // Human-readable site name
    DrupalRoot     string `json:"drupal_root"`     // Path to Drupal installation
    WatchdogPath   string `json:"watchdog_path"`   // Path to watchdog export file
    WatchdogFormat string `json:"watchdog_format"` // "json" or "drush"
    MinSeverity    int    `json:"min_severity"`    // RFC 5424 severity (0-7)
    WatchdogLimit  int    `json:"watchdog_limit"`  // Max entries in output
}

type DrupalSitesConfig struct {
    Version     string                `json:"version"`
    DefaultSite string                `json:"default_site"`
    Sites       map[string]DrupalSite `json:"sites"`
}
```

**Search Locations** (in order):
1. Explicit path from `-drupal-sites-config` flag
2. `./drupal-sites.json`
3. `./configs/drupal-sites.json`
4. `/opt/logwatch-ai/drupal-sites.json`
5. `~/.config/logwatch-ai/drupal-sites.json`

**CLI Options for Multi-Site:**
```bash
-drupal-site string          # Site ID from drupal-sites.json
-drupal-sites-config string  # Custom path to drupal-sites.json
-list-drupal-sites           # List available sites and exit
```

**Priority:** CLI args > drupal-sites.json > .env > defaults

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
- ✅ Unit tests for formatting logic (see `internal/notification/telegram_test.go`)
- ✅ Table-driven tests for multiple scenarios
- ✅ Real API integration testing (Claude AI + Telegram Bot)
- ✅ End-to-end testing with actual logwatch output
- ✅ Multi-environment validation (Integration → QA → Pre-Production)
- ✅ Linux Debian 12 deployment testing
- ⏳ Comprehensive mock-based unit tests (pending full coverage)
- ✅ MarkdownV2 escaping validated

### Code Style
- Use SecureLogger for structured logging: `log.Info().Str("key", value).Msg("message")`
- For errors that may contain credentials, use: `internalerrors.Wrapf(err, "failed to X")`
- For other errors: `fmt.Errorf("failed to X: %w", err)`
- Constants for exit codes, timeouts, retry counts
- Defer cleanup: `defer store.Close()`, `defer telegramClient.Close()`

## Development Workflow

### Adding a New Feature
1. Determine which package owns the feature (ai, analyzer, config, drupal, logwatch, notification, storage)
2. Add configuration fields to `internal/config/config.go` if needed
3. Update `.env.example` with new variables
4. Implement logic in appropriate package
5. Update `cmd/analyzer/main.go` workflow if needed
6. Add tests for new functionality
7. Update README.md if user-facing

### Adding a New Log Source
1. Create a new package in `internal/` (e.g., `internal/newlog/`)
2. Implement the three interfaces from `internal/analyzer/interfaces.go`:
   - `LogReader` - Read and validate log files
   - `Preprocessor` - Token estimation and content reduction
   - `PromptBuilder` - System prompt and user prompt generation
3. Add source type constant to `internal/analyzer/registry.go`
4. Add configuration fields to `internal/config/config.go`:
   - Add source-specific struct fields
   - Update `validateLogSource()` for new source
   - Update `GetLogSourcePath()` helper
5. Add factory case in `cmd/analyzer/main.go:createLogSource()`
6. Add test fixtures in `testdata/newlog/`
7. Add tests for all three interface implementations
8. Update `.env.example` and documentation

### Running Tests
```bash
# Run all tests with verbose output
make test

# Run tests for a specific package
go test -v ./internal/ai
go test -v ./internal/logwatch
go test -v ./internal/drupal
go test -v ./internal/analyzer

# Run with coverage
make test-coverage
# Opens coverage.html in browser

# Run specific test
go test -v -run TestFormatMessage ./internal/notification
go test -v -run TestReadJSON ./internal/drupal
```

### Testing with Real APIs
When testing with actual Anthropic/Telegram APIs:
1. Copy `configs/.env.example` to `.env`
2. Fill in real credentials
3. Run: `./bin/logwatch-analyzer` (after `make build`)
4. Check logs in `./logs/` directory

### Deployment Environments

**Validated Platforms:**
- ✅ **Linux Debian 12** - Primary production platform
- ✅ **macOS (Darwin 25.1.0)** - Development platform

**Deployment Pipeline:**
```
Development (macOS) → Integration (Debian 12) → QA (Debian 12) → Pre-Production (Debian 12) → Production
```

**Deployment Checklist:**
1. Build for target platform: `make build-linux-amd64`
2. Transfer binary to target system
3. Run installation script: `sudo ./scripts/install.sh`
4. Configure `.env` with environment-specific credentials
5. Test manual run: `/opt/logwatch-ai/logwatch-analyzer`
6. Verify Telegram notifications received
7. Set up cron jobs (see `docs/CRON_SETUP.md`)
8. Monitor logs in `/opt/logwatch-ai/logs/`

**Environment-Specific Configuration:**
- Use separate Telegram channels for different environments
- Use different database paths to avoid conflicts
- Adjust `LOG_LEVEL` (debug for dev/integration, info for qa/prod)
- Consider using environment-specific `.env` files

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
🔍 {Source} Report[ - {site_name}]
🖥 Host: {hostname}
📅 Date: {timestamp}
🌍 Timezone: {timezone}
{status_emoji} Status: {status}

📋 Execution Stats
• LLM: {model} ({provider})
• Critical Issues: N
• Warnings: N
• Recommendations: N
• Cost: $X.XXXX
• Duration: X.XXs
• Cache Read: N tokens (if cache hit)

📊 Summary
{summary_text}

🔴 Critical Issues (N) - if any
{numbered list}

⚡ Warnings (N) - if any
{numbered list}

💡 Recommendations - if any
{numbered list}

📈 Key Metrics - if any
{key-value pairs}
```

- `{Source}` is "Logwatch" or "Drupal Watchdog" based on `LOG_SOURCE_TYPE`
- `{site_name}` shown only for multi-site Drupal deployments
- `{model}` is the LLM model name (e.g., "claude-sonnet-4-5-20250929" or "llama3.3:latest")
- `{provider}` is "Anthropic" or "Ollama" based on `LLM_PROVIDER`

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
// Last 7 days with source/site filtering (for Claude context)
filter := &storage.SourceFilter{
    LogSourceType: "drupal_watchdog",
    SiteName:      "production",  // Empty string for logwatch
}
summaries, err := store.GetRecentSummaries(7, filter)

// Custom period
summaries, err := store.GetRecentSummaries(30, filter)

// Historical context formatted for Claude (filtered by source/site)
context, err := store.GetHistoricalContext(7, filter)

// Statistics (optionally filtered)
stats, err := store.GetStatistics(filter)  // Pass nil for all sources
// Returns: total_summaries, status_distribution, total_cost_usd
```

## Common Issues

### "Database is locked"
- Ensure only one instance runs at a time
- Check file permissions on `./data/` directory
- SQLite uses file-level locking (modernc.org/sqlite is pure Go, no CGO)
- Built-in 5-second busy timeout handles temporary lock contention

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

### Drupal Watchdog Issues

**"Invalid JSON format"**
- Ensure the watchdog export is valid JSON array format
- Check for UTF-8 encoding issues in log messages
- Validate with: `jq . /path/to/watchdog.json`

**"Unsupported drush output format"**
- drush format parsing is more lenient but may miss entries
- Prefer JSON export for reliable parsing
- Export with: `drush watchdog:show --format=json > watchdog.json`

**"Missing watchdog entries"**
- Check `watchdog_format` in drupal-sites.json matches your file format
- Verify file permissions and `watchdog_path`
- Ensure watchdog table is being populated in Drupal

**"Too many 'page not found' entries"**
- Drupal preprocessing assigns LOW priority to 404s
- These are compressed during preprocessing
- For security analysis, check for patterns (wp-admin, .env probing)

### Multi-Site Drupal Issues

**"Site not found in drupal-sites.json"**
- Use `-list-drupal-sites` to see available sites
- Check that the site ID matches exactly (case-sensitive)
- Verify drupal-sites.json is in a search location

**"No drupal-sites.json found"**
- Create the file in one of the search locations
- Use `-drupal-sites-config` to specify a custom path
- See `configs/drupal-sites.json.example` for format

**Generate Script (scripts/generate-drupal-watchdog.sh):**
```bash
# List available sites
./scripts/generate-drupal-watchdog.sh --list-sites

# Export for specific site
./scripts/generate-drupal-watchdog.sh --site production

# Custom sites config path
./scripts/generate-drupal-watchdog.sh --site staging --sites-config /path/to/sites.json
```
- Requires `jq` for configuration parsing
- CLI args override site config (CLI > site config > defaults)

## Production Deployment Best Practices

### Pre-Deployment Validation
1. ✅ **Build for target platform**: Use `make build-linux-amd64` for Debian/Ubuntu
2. ✅ **Test in staging**: Deploy to pre-production environment first
3. ✅ **Verify credentials**: Test with actual API keys in isolated environment
4. ✅ **Check cron configuration**: Ensure logwatch runs before analyzer
5. ✅ **Monitor logs**: Watch `/opt/logwatch-ai/logs/` for first few runs

### Monitoring Recommendations
- **Log files**: Check `logs/analyzer.log` for errors and warnings
- **Database size**: Monitor `data/summaries.db` growth (cleanup runs every 90 days)
- **Telegram delivery**: Verify messages arrive in both archive and alerts channels
- **API costs**: Track cost_usd in database for budget monitoring
- **Cron execution**: Use cron logging to verify scheduled runs

### Security Considerations
- **API keys**: Store `.env` with restricted permissions (600)
- **Log files**: Contains sensitive system information, restrict access
- **Database**: Contains historical analysis, ensure proper file permissions
- **Network**: Use HTTPS proxy in corporate environments
- **Updates**: Regularly update dependencies for security patches
- **Credential sanitization**: All logs and errors automatically redact API keys and tokens (internal/errors, internal/logging)
- **Prompt injection protection**: Logwatch content is sanitized to filter common prompt injection patterns (internal/ai/prompt.go)

### Performance Tuning
- **Preprocessing**: Adjust `MAX_PREPROCESSING_TOKENS` based on log size
- **Historical context**: Default 7 days, reduce if logs are consistent
- **Database cleanup**: Default 90 days, adjust based on retention needs
- **Log rotation**: Analyzer logs rotate at 10MB, adjust in logger config

### Troubleshooting in Production
1. **Check logs first**: `/opt/logwatch-ai/logs/analyzer.log`
2. **Verify cron**: `grep CRON /var/log/syslog` or `journalctl -u cron`
3. **Test manually**: Run `/opt/logwatch-ai/logwatch-analyzer` as same user as cron
4. **Check database**: `sqlite3 /opt/logwatch-ai/data/summaries.db "SELECT COUNT(*) FROM summaries;"`
5. **Validate environment**: Ensure `.env` is in `/opt/logwatch-ai/` directory

## Cost Optimization

### Using Anthropic Claude (Cloud)

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

### Using Ollama (Local) - Zero Cost

For development or cost-sensitive deployments, use Ollama for **free local inference**:

```bash
# Install Ollama (macOS)
brew install ollama

# Pull recommended model (requires ~40GB disk, ~45GB RAM)
ollama pull llama3.3:latest

# Or use a smaller model for lower-RAM systems
ollama pull llama3.2:8b

# Start Ollama server
ollama serve
```

Configure in `.env`:
```
LLM_PROVIDER=ollama
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=llama3.3:latest
```

**Trade-offs:**
- ✅ Zero cost - unlimited analysis
- ✅ Data privacy - logs never leave your machine
- ✅ No rate limits
- ⚠️ Slower than cloud (depends on hardware)
- ⚠️ Quality varies by model
- ⚠️ Requires powerful hardware for large models

### Using LM Studio (Local) - Zero Cost

LM Studio provides a user-friendly desktop application for running local LLMs with an OpenAI-compatible API:

1. Download and install LM Studio from https://lmstudio.ai
2. Download a model from the Search tab (recommended models below)
3. Load the model (click on it, then "Load")
4. Enable "Local Server" mode in the left sidebar
5. Server starts on `http://localhost:1234` by default

Configure in `.env`:
```
LLM_PROVIDER=lmstudio
LMSTUDIO_BASE_URL=http://localhost:1234
LMSTUDIO_MODEL=local-model
```

**Recommended Models** (download from LM Studio's model browser):
| Model | VRAM | Quality | Speed |
|-------|------|---------|-------|
| Llama-3.3-70B-Instruct | ~40GB | Excellent | Slower |
| Qwen2.5-32B-Instruct | ~20GB | Excellent | Medium |
| Mistral-Small-24B-Instruct | ~15GB | Good | Medium |
| Phi-4-14B | ~9GB | Good | Faster |
| Llama-3.2-8B-Instruct | ~5GB | Acceptable | Fast |

**Tips:** Look for GGUF quantized versions (Q4_K_M, Q5_K_M) for better VRAM efficiency.

**Trade-offs:**
- ✅ Zero cost - unlimited analysis
- ✅ Data privacy - logs never leave your machine
- ✅ User-friendly GUI for model management
- ✅ Easy model switching without CLI
- ✅ OpenAI-compatible API (works with many tools)
- ⚠️ Slower than cloud (depends on hardware)
- ⚠️ Quality varies by model
- ⚠️ Requires powerful hardware for large models

## Claude Code Extensions

This project includes specialized Claude Code agents and slash commands tailored to the logwatch-ai-go tech stack and workflows. These tools enhance development productivity by providing context-aware assistance.

### Specialized Agents

Agents are available in `.claude/agents/` and provide deep expertise in specific areas:

#### go-dev (Go Development Specialist)
**When to use:** Development tasks, testing, code quality
- Run tests and analyze failures
- Format code and run static analysis (go vet)
- Build the application (dev and prod builds)
- Manage Go dependencies
- Add new tests or improve coverage
- Debug Go-specific issues

**Examples:**
- "Run all tests and show me the results"
- "Fix the failing test in internal/ai/client_test.go"
- "Add unit tests for the new preprocessing logic"
- "Check code quality with fmt and vet"

#### build-manager (Cross-Platform Build Specialist)
**When to use:** Building for different platforms, optimizing binaries
- Build for Linux AMD64 (Debian 12 / Ubuntu 24)
- Build for macOS ARM64 (Apple Silicon)
- Create production-optimized binaries
- Troubleshoot compilation errors
- Optimize binary size
- Prepare release builds

**Examples:**
- "Build for Linux Debian 12 deployment"
- "Create optimized production binaries for all platforms"
- "Why is the binary so large? How can we reduce it?"
- "Prepare a release build with checksums"

#### deploy-assistant (Deployment & Production Specialist)
**When to use:** Installing, configuring, deploying to production
- Install to /opt/logwatch-ai on Linux servers
- Set up cron jobs for automated analysis
- Configure .env for different environments (Integration, QA, Pre-Prod, Production)
- Troubleshoot production deployment issues
- Manage multi-environment deployments
- Handle log rotation and monitoring

**Examples:**
- "Install to /opt/logwatch-ai on Linux server"
- "Set up cron job to run daily at 2:15 AM"
- "Configure for QA environment with separate Telegram channels"
- "Check production logs for errors"

#### db-manager (SQLite Database Specialist)
**When to use:** Querying historical data, database operations, troubleshooting
- Query summaries database for historical analysis
- Troubleshoot database issues (locks, corruption, performance)
- Analyze stored summaries and statistics
- Manage database cleanup and maintenance
- Generate reports from stored analysis data
- Export data to CSV/JSON

**Examples:**
- "Show me the last 10 analysis summaries"
- "How many Critical status summaries do we have?"
- "Database is locked - how do I fix it?"
- "Export all summaries to JSON for reporting"

#### api-tester (API Integration Testing Specialist)
**When to use:** Testing APIs, validating credentials, debugging API issues
- Test Claude AI API integration
- Test Telegram Bot API (sending messages, formatting)
- Validate API credentials and configuration
- Troubleshoot API errors (rate limits, timeouts, authentication)
- Test MarkdownV2 formatting and escaping
- Verify end-to-end workflow with real APIs

**Examples:**
- "Test if my Anthropic API key is valid"
- "Send a test message to my Telegram channel"
- "Why is Claude API returning 401?"
- "Run end-to-end test with real logwatch data"

#### cost-optimizer (Cost Tracking & Optimization Specialist)
**When to use:** Analyzing costs, optimizing spending, forecasting
- Analyze Claude AI costs from database
- Generate cost reports (daily, monthly, yearly)
- Identify cost anomalies or unusual spending
- Optimize token usage and reduce costs
- Forecast future costs based on usage patterns
- Recommend preprocessing adjustments

**Examples:**
- "What are my total Claude AI costs this month?"
- "Show me cost trends over the last 30 days"
- "Why did today's analysis cost more than usual?"
- "How can I reduce costs without losing analysis quality?"

### Slash Commands

Quick commands are available for common workflows. Use them with `/command` syntax:

#### Development Workflow
- **/test** - Run all tests and analyze results
- **/test-coverage** - Generate test coverage report (opens coverage.html)
- **/build** - Build development binary with debug info
- **/build-prod** - Build optimized production binary for current platform
- **/build-all** - Build for all platforms (Linux AMD64, macOS ARM64)
- **/lint** - Run code formatting (gofmt) and static analysis (go vet)
- **/clean** - Remove build artifacts and coverage files

#### Deployment & Operations
- **/deploy-prep** - Create complete deployment package for Linux production
- **/check-logs** - Check application logs for errors and recent activity

#### Database & Cost Analysis
- **/db-stats** - Show database statistics and recent analysis summaries
- **/cost-report** - Generate comprehensive Claude AI cost report

#### Security
- **/security-audit** - Perform comprehensive security audit of codebase, dependencies, and configurations

### How to Use Agents and Commands

**Invoke an agent:**
```
@go-dev run all tests and fix any failures
@build-manager build for Linux Debian 12
@cost-optimizer show me this month's costs
```

**Run a slash command:**
```
/test
/build-all
/cost-report
```

**Choose the right tool:**
- **Simple, single tasks**: Use slash commands for quick operations
- **Complex, multi-step tasks**: Use agents for guided assistance
- **Ongoing development**: Use go-dev agent for iterative development
- **Deployment**: Use deploy-assistant for production operations
- **Cost analysis**: Use cost-optimizer for financial insights

### Agent Capabilities Matrix

| Task | Recommended Agent | Alternative |
|------|------------------|-------------|
| Run tests | go-dev | /test command |
| Build for Linux | build-manager | /build-all command |
| Deploy to production | deploy-assistant | /deploy-prep command |
| Query database | db-manager | /db-stats command |
| Test APIs | api-tester | Manual testing |
| Analyze costs | cost-optimizer | /cost-report command |
| Code quality | go-dev | /lint command |
| Debug issues | go-dev | Check logs manually |
| Optimize performance | cost-optimizer + go-dev | Manual analysis |
| Security audit | /security-audit command | Manual review |

### Best Practices

1. **Start with commands for simple tasks**: Use /test, /build, /db-stats for quick operations
2. **Use agents for complex scenarios**: Multi-step debugging, deployment planning, cost optimization
3. **Combine tools**: Use /test to run tests, then @go-dev to fix failures
4. **Context matters**: Agents understand the project's tech stack and conventions
5. **Ask for explanations**: Agents can explain "why" not just "how"

### Examples in Practice

**Scenario 1: Preparing for Production Deployment**
```
@deploy-assistant I need to deploy to production. Walk me through the process.

# Agent will guide you through:
# 1. Building for Linux AMD64
# 2. Creating deployment package
# 3. Configuring .env for production
# 4. Setting up cron jobs
# 5. Testing before going live
```

**Scenario 2: Investigating High Costs**
```
@cost-optimizer My costs seem higher than expected this week. Investigate.

# Agent will:
# 1. Query database for cost trends
# 2. Identify anomalies
# 3. Analyze token usage patterns
# 4. Recommend optimizations
# 5. Show before/after projections
```

**Scenario 3: Adding New Features**
```
@go-dev I added a new function to internal/ai/prompt.go. Create tests for it.

# Agent will:
# 1. Read the new function
# 2. Create table-driven tests
# 3. Run tests to verify
# 4. Check coverage
# 5. Suggest edge cases
```

**Scenario 4: Quick Status Check**
```
/db-stats
/check-logs
/cost-report

# Quick overview of system health, recent activity, and costs
```

**Scenario 5: Pre-Release Security Audit**
```
/security-audit

# Comprehensive security analysis including:
# 1. Code vulnerabilities (SQL injection, command injection, etc.)
# 2. Dependency CVEs and outdated packages
# 3. Credential exposure in code/logs
# 4. Configuration security issues
# 5. Deployment security best practices
# Results stored in .audit/ directory (gitignored)
```
