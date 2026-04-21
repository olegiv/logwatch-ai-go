# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- `ParseAnalysis` no longer fails the whole run when the LLM returns an
  object (e.g. `{"description": "..."}`) where the prompt specifies a
  plain string inside `criticalIssues`, `warnings`, or `recommendations`.
  Array fields now flow through a tolerant coercion step
  (`internal/ai/coerce.go`) that extracts `description`/`message`/`text`/
  `issue`/`recommendation`/`warning`/`summary`/`detail`/`title`/`name`
  in priority order, joins unknown-key objects on `" — "`, skips numbers
  and nulls, and wraps scalars into a single-item slice. The downstream
  `[]string` contract consumed by storage, Telegram rendering, and the
  exclusions filter is unchanged.
- Previously the failure surfaced as
  `json: cannot unmarshal object into Go struct field
  Analysis.recommendations of type string` and left the operator with
  no notification, no DB row, and no cost record.

### Added
- Exported `ai.StringArrayFormatReminder` constant appended to both the
  logwatch and Drupal system prompts. Explicitly shows a CORRECT vs
  INCORRECT example so the LLM is less likely to emit object-wrapped
  findings that would otherwise trigger the coercion path.

### Security
- **M-01 SSRF**: `OLLAMA_BASE_URL` and `LMSTUDIO_BASE_URL` are now
  rejected when they resolve to loopback, link-local (including
  `169.254.169.254/latest/meta-data/`), RFC-1918 private, or unspecified
  IP literals. Loopback is always allowed; other local ranges require
  `ALLOW_LOCAL_LLM=true` as an explicit opt-in.
- **L-04 cleartext warning**: a warning is now logged when
  `OLLAMA_BASE_URL` / `LMSTUDIO_BASE_URL` uses `http://` with a non-
  loopback host, so operators notice that log content (which may
  contain PII) is flowing unencrypted.

## [0.9.0] - 2026-04-20

### Changed

#### Anthropic model default and cost tracking
- Default `CLAUDE_MODEL` is now `claude-haiku-4-5-20251001` (previously
  `claude-sonnet-4-5-20250929`). Haiku 4.5 matches Sonnet-tier quality for
  this workload at roughly one-third the cost.
- New `internal/ai/pricing.go` with a per-model pricing table and
  longest-prefix lookup (`ResolvePricing`). Supported families: Haiku 4.5,
  Sonnet 4.5, Sonnet 4.6, Opus 4.6, Opus 4.7. Dated snapshot IDs resolve
  to their family entry automatically.
- Unknown models now log a warning on startup and fall back to Sonnet-tier
  pricing (previously cost was hardcoded to Sonnet 4.5 rates, silently
  mis-reporting `cost_usd` for any other model).
- Historical DB rows are not backfilled; they retain the cost that was
  reported at the time of the original run.

### Fixed
- `cost_usd` is now correct when `CLAUDE_MODEL` is set to Haiku 4.5,
  Opus 4.x, or Sonnet 4.6. The previous hardcoded Sonnet 4.5 formula
  would over-report Haiku cost ~3× and under-report Opus cost ~5×.

### Security
- **I-03**: Reject malformed `CLAUDE_MODEL` values at config load via a
  `^claude-[a-z0-9-]+$` format check in `validateLLMProvider`. Prevents
  a mis-pasted credential from flowing into the new unknown-model warning
  logged in `ai.NewClient`. Rejects API-key shapes, uppercase, paths,
  whitespace, and prefix-only strings.

## [0.8.0] - 2026-04-20

### Added

#### Finding Exclusions (opt-in)
- New `internal/exclusions/` package that loads operator-authored
  `exclusions.json` and removes matching findings from an analysis
  before it reaches SQLite or Telegram
- `-exclusions-config <path>` CLI flag with auto-discovery across
  `./`, `./configs/`, `/opt/logwatch-ai/`, and `~/.config/logwatch-ai/`
  (same search order as `drupal-sites.json`)
- `global` patterns apply to every run; per-Drupal-site patterns stack
  on top when `-drupal-site` matches
- Uniform application across `criticalIssues`, `warnings`, and
  `recommendations`
- Plain case-insensitive substring match (no regex, no ReDoS surface);
  1 MiB config file cap; pattern text deliberately not logged
- Strict schema: `version` must be exactly `"1.0"` (future bumps force
  a conscious migration)
- `configs/exclusions.json.example` template, `docs/EXCLUSIONS.md`
  full spec, `scripts/install.sh` installs the example

### Changed

#### Developer tooling
- Pin `.golangci.yml` (v2 config) enabling `modernize`, full
  `staticcheck` QF\* suite, `revive`, `gocritic`, `errorlint`,
  `unconvert`, `unparam`, `usetesting`, `dupl`, `gocyclo`, and
  `unusedwrite` via `govet`
- Drive lint baseline from 217 findings to 0 across auto-fix and
  hand-edit passes: `interface{}` → `any`, `omitempty` → `omitzero`
  on nested struct fields, `os.Setenv`/`os.Chdir` → `t.Setenv`/
  `t.Chdir` in tests, 15 new `// Package foo ...` doc comments,
  `WriteString(fmt.Sprintf(...))` → `fmt.Fprintf(...)`

### Fixed
- Cap HTTP response bodies from LLM providers to prevent memory
  exhaustion (LM Studio / generic clients and Ollama `/api/tags`)
- NDJSON watchdog parser no longer truncates oversized lines
- Logwatch pipeline failures are detected instead of masked by the
  downstream `while read` loop
- Drupal watchdog filtering now uses epoch timestamps, fixing
  day-boundary skew across timezones
- Schema version table kept single-row so stale rows no longer cause
  migration mis-reads
- Preprocessing error wrappers in `cmd/analyzer/prompt_fit.go` now go
  through `internalerrors.Wrapf` so any credential in a wrapped error
  is scrubbed before logging
- Shell-script hardening in `scripts/`:
  - `generate-logwatch.sh`: allowlist `RANGE` to
    `yesterday|today|all|help` before forwarding to logwatch
  - `generate-drupal-watchdog.sh`: pass `site_id` and `field` via
    `jq --arg` instead of shell-interpolating into the filter string
  - `generate-drupal-watchdog.sh`: validate `LOG_TYPE` and `SEVERITY`
    against an identifier charset (allowing spaces for type names like
    `page not found` and commas for `-s error,warning`) while rejecting
    leading `-` that could reach drush as an extra flag
- `.gitignore` now excludes `exclusions.json` (keeping the shipped
  template and testdata fixtures tracked)

### Security
- `govulncheck ./...` reports zero vulnerabilities on the release
  commit
- Full security audit run; all five actionable findings fixed, two
  deferred with documented rationale (operator `.env` credential
  rotation, accepted TOCTOU on exclusions-config load)

### Dependencies
- Update `modernc.org/sqlite` v1.48.2 → v1.49.1
- Update `modernc.org/libc` v1.71.0 → v1.72.0 (transitive)

## [0.7.0] - 2026-04-09

### Added

#### Exact Prompt Fitting for Anthropic
- Iterative prompt sizing using Anthropic's token counting API
  for precise context window utilization (`cmd/analyzer/prompt_fit.go`)
- `PromptTokenCounter` interface for providers that support exact
  token counting (`internal/ai/provider.go`)
- `BudgetPreprocessor` interface for dynamic token budget-aware
  preprocessing (`internal/analyzer/interfaces.go`)
- Token budget calculation with configurable safety margins
  (`internal/analyzer/budget.go`)
- Progressive compression profiles for logwatch preprocessing
  (100/50/20% → 85/35/10% → 70/20/5% → 50/10/2%)
- Aggressive compression and hard truncation as final fallbacks

#### Preprocessing Improvements
- Budget-aware `ProcessWithBudget` for logwatch and drupal
  preprocessors with section-level priority classification
- HIGH/MEDIUM/LOW priority classification for log sections
  (ssh, security, auth → HIGH; network, disk → MEDIUM)
- Deduplication of similar log lines within sections

### Changed
- Prompt preparation now uses exact Anthropic token counts with
  up to 4 iterative recompression attempts before falling back
  to heuristic sizing
- Non-Anthropic providers continue using heuristic budget
  calculation
- Reader preprocessing disabled at source level; preprocessing
  now handled centrally by `preparePromptForAnalysis`

### Fixed
- Graceful fallback to heuristic sizing when Anthropic token
  counting API fails (transient 429/5xx no longer abort the run)
- Removed artificial 1000-token floor from prompt fit budget to
  allow small feasible budgets for high-overhead prompts
- Added retry logic with exponential backoff to
  `CountPromptTokens`, matching `Analyze` retry behavior

### Dependencies
- Update Go 1.25.6 → 1.26.0
- Update go-anthropic/v2 v2.17.0 → v2.18.0
- Update go-logger v0.2.1 → v0.2.2
- Update zerolog v1.34.0 → v1.35.0
- Update sqlite v1.46.1 → v1.48.2
- Update go-toml/v2 v2.2.4 → v2.3.0
- Update x/sys v0.41.0 → v0.43.0
- Update x/text v0.34.0 → v0.36.0

## [0.6.0] - 2026-01-26

### Changed
- **BREAKING**: License changed from MIT to GPL-3.0-or-later
  - All source files now include SPDX license headers
  - `-version` output now displays GPL copyright notice
  - Users must comply with GPL terms for redistribution

### Added
- SPDX short form license headers to all Go source files
- GPL copyright notice in `-version` output
- License header documentation in CLAUDE.md

### Dependencies
- Update go-logger v0.2.0 → v0.2.1
- Update modernc.org/sqlite v1.41.0 → v1.44.3
- Update Go 1.25.5 → 1.25.6
- Various indirect dependency updates

## [0.5.1] - 2025-12-14

### Added
- Short CLI options `-h` (help) and `-v` (version) as aliases for `-help` and `-version`
- Rate limit detection with extended backoff for Anthropic API (429 status handling with exponential backoff up to 60s)

### Fixed
- Telegram MarkdownV2 escaping for backslash characters (proper `\\` handling)
- JSON parsing for invalid escape sequences in LLM responses (graceful handling of malformed JSON)

## [0.5.0] - 2025-12-14

### Added

#### Local LLM Provider Support
Three LLM backends now supported via `LLM_PROVIDER` configuration:

**Ollama Integration (`internal/ai/ollama.go`)**
- Full Ollama REST API client implementation
- Connection check to verify Ollama server and model availability before analysis
- JSON format mode for structured output from local models
- Zero-cost tracking for local inference (CostUSD = 0)
- Recommended models: llama3.3:latest, qwen2.5:72b, deepseek-coder-v2:33b

**LM Studio Integration (`internal/ai/lmstudio.go`)**
- OpenAI-compatible API client for LM Studio's `/v1/chat/completions` endpoint
- Connection check to verify LM Studio is running and model is loaded
- Zero-cost tracking for local inference (CostUSD = 0)
- Recommended models: Llama-3.3-70B-Instruct, Qwen2.5-32B-Instruct, Mistral-Small-24B-Instruct, Phi-4-14B

#### Provider Architecture
- `internal/ai/provider.go` with `Provider` interface for pluggable LLM backends
- `internal/ai/retry.go` with shared retry logic (3 attempts, exponential backoff)
- `internal/ai/http.go` with HTTP helper functions for API clients

#### Configuration
- `LLM_PROVIDER` environment variable: `anthropic` (default), `ollama`, or `lmstudio`
- `OLLAMA_BASE_URL` for custom Ollama server location (default: `http://localhost:11434`)
- `OLLAMA_MODEL` for model selection (default: `llama3.3:latest`)
- `LMSTUDIO_BASE_URL` for LM Studio server location (default: `http://localhost:1234`)
- `LMSTUDIO_MODEL` for model identifier (default: `local-model`)
- Validation ensures appropriate settings based on selected provider

#### Telegram Notifications
- LLM model and provider info now displayed in Telegram reports (e.g., "LLM: llama3.3:latest (Ollama)")
- `Provider` and `Model` fields added to `ai.Stats` struct for tracking

#### Dependencies
- Added `jq` dependency check for Drupal watchdog support

### Changed
- AI client initialization now uses provider factory pattern based on `LLM_PROVIDER`
- Configuration validation adapts to selected LLM provider (Anthropic API key only required for anthropic)
- Removed deprecated `AnalyzeLogwatch` method from Anthropic client
- Extracted retry logic into shared helper function for all providers

### Fixed
- Shell script exit code checking in Drupal watchdog export
- Static analysis warnings across codebase
- Linter warnings and code style issues
- Unchecked error returns in test files
- Struct field alignment in lmstudio.go

## [0.4.0] - 2025-12-13

### Added

#### Drupal Watchdog Analysis
- New log source type for PHP/Drupal application log analysis
- `internal/drupal/` package with reader, preprocessor, and prompt builder
- Support for JSON and drush export formats (`DRUPAL_WATCHDOG_FORMAT`)
- RFC 5424 severity level handling (Emergency through Debug)
- Priority-based preprocessing for Drupal-specific keywords

#### Multi-Site Drupal Configuration
- Centralized configuration via `drupal-sites.json`
- Site-specific settings: drupal_root, watchdog_path, format, min_severity, watchdog_limit
- CLI flags: `-drupal-site`, `-drupal-sites-config`, `-list-drupal-sites`
- Automatic site config file discovery in multiple locations

#### CLI Enhancements
- `-version` flag for version information with build details
- `-help` flag for comprehensive usage information
- Configuration overrides via CLI flags for all major settings
- Unified `PrintUsage` function for consistent help output

#### Log Source Abstraction Layer
- `internal/analyzer/` package with pluggable architecture
- `LogReader`, `Preprocessor`, `PromptBuilder` interfaces
- Registry pattern for extensible log source support

#### Database Schema v2
- Added `log_source_type` column for multi-source support
- Added `site_name` column for multi-site filtering
- Automatic migration from v1 to v2
- Source-filtered queries for historical context

#### Scripts
- `scripts/generate-drupal-watchdog.sh` for exporting Drupal watchdog logs
- Multi-site support with `--site` and `--list-sites` options
- Configurable severity filtering and entry limits

#### Telegram Notifications
- Log source-specific headers (Logwatch Analysis vs Drupal Watchdog Analysis)
- Site name display for multi-site deployments

#### Claude Code Extensions
- Specialized agents: go-dev, build-manager, deploy-assistant, db-manager, api-tester, cost-optimizer
- Slash commands for common workflows: /test, /build, /deploy-prep, /db-stats, /cost-report

### Changed

- Go version requirement updated to 1.25+
- Schema migration logic refactored to use loop with switch for sequential execution
- `PrintUsage` extracted for unified CLI usage handling
- Dependencies updated (zerolog promoted to direct dependency)

### Fixed

- Tests switched from `t.Error` to `t.Fatal` for proper test failure handling
- Time range filtering in Drupal watchdog export script

## [0.3.0] - 2025-12-09

### Security

#### Medium Severity
- **M-01**: Sanitize credentials in error messages to prevent accidental exposure in logs
- **M-02**: Add secure logger wrapper with automatic credential filtering (API keys, tokens, passwords)
- **M-04**: Use constant-time comparison for sensitive string comparisons

#### Low Severity
- **L-01**: Add Telegram rate limiting (1s between messages, 429 detection with retry)
- **L-02**: Make AI settings configurable via environment variables (`AI_TIMEOUT_SECONDS`, `AI_MAX_TOKENS`)
- **L-03**: Add prompt injection sanitization for logwatch content before Claude analysis
- **L-04**: Add database connection timeout (5s busy timeout) to prevent indefinite waits

#### Infrastructure Security
- Fix crypto/x509 security vulnerabilities in dependencies
- Harden file permissions in install script (restrictive umask, proper ownership)

### Fixed

- Fix logger resource leak in main function (proper cleanup on all exit paths)
- Fix resource leak in storage initialization (close connection on error)
- Fix static analysis warnings in tests
- Fix error handling in deferred cleanup operations

### Changed

- Simplify regex patterns in error sanitizer for better maintainability
- Refactor duplicated test comparison code into helper functions
- Simplify Analysis struct comparison helpers

### Added

- Comprehensive troubleshooting guide (`docs/TROUBLESHOOTING.md`)
- CI/CD workflow badges to README
- Contributor Covenant Code of Conduct
- Updated issue templates for better bug reports

### Dependencies

- Update Go dependencies to latest versions
- Update go-logger dependency to tagged version

## [0.2.0] - 2025-11-15

### Added
- Production deployment documentation for Integration, QA, and Pre-Production environments
- Production best practices section in CLAUDE.md covering monitoring, security, performance tuning, and troubleshooting
- Deployment pipeline documentation with environment-specific configuration guidance
- Dependency Review GitHub Action workflow for automated vulnerability scanning in pull requests
- Validated production readiness across multiple Linux Debian 12 environments

### Changed
- **BREAKING**: Configuration priority now favors .env file variables over OS environment variables
  - Previously: OS env vars > .env file
  - Now: .env file > OS env vars
  - **Migration**: Users relying on OS environment variables to override .env settings must update their deployment configurations
- Extracted logger to external reusable module `github.com/olegiv/go-logger` for sharing across multiple Go projects
- HTTP_PROXY and HTTPS_PROXY configuration now uses `viper.GetString()` for consistency with other config values
- Updated PROJECT_SUMMARY.md to reflect production-ready status with completed validations
- Enhanced CLAUDE.md with comprehensive deployment checklist and troubleshooting guides

### Fixed
- Error handling in deferred cleanup operations (store.Close, telegramClient.Close)
- GitHub Actions workflow permissions for code scanning compliance
- Code formatting and alignment in prompt.go, telegram.go, and sqlite.go
- Proper error capture in deferred functions to prevent variable shadowing

### Removed
- `pkg/logger/` directory (moved to external module `github.com/olegiv/go-logger`)
  - Removed logger.go (128 lines)
  - Removed logger_test.go (465 lines)

### Migration Guide

**Configuration Priority Change:**
If you're using OS environment variables to override .env file settings, you'll need to either:
1. Remove or comment out conflicting entries in your .env file, OR
2. Update your deployment scripts to set environment variables after loading .env

**Logger Module Change:**
The logger has been extracted to an external module. Update your imports:
- Old: `github.com/olegiv/logwatch-ai-go/pkg/logger`
- New: `github.com/olegiv/go-logger`

This change is transparent for binary users (no action required).

## [0.1.0] - 2025-11-13

### Added
- Comprehensive test suite for all packages (ai, config, logwatch, notification, storage)
- Test coverage reporting with `make test-coverage` target
- Unit tests for critical functionality (formatting, parsing, cost calculation)

### Changed
- Update Go version requirement to 1.23 in GitHub Actions workflow
- Update Go module dependencies to latest versions

### Fixed
- Fix floating-point precision in cost calculation test to prevent intermittent failures

## [0.0.0] - 2025-11-12

### Added

#### Build System
- Production build optimizations with `-ldflags="-s -w"` for smaller binaries
- `-trimpath` flag to remove file system paths from compiled binaries
- Cross-platform build target for Linux AMD64 (Debian 12/Ubuntu 24)
- Cross-platform build target for macOS ARM64 (Apple Silicon - M1, M2, M3)
- `make build-all-platforms` target to build for all supported platforms at once
- Enhanced `make help` output with all new build targets

#### Core Features
- Complete Go port of logwatch-ai Node.js implementation
- AI-powered log analysis using Anthropic Claude Sonnet 4.5
- Smart Telegram notifications with dual-channel support (archive + alerts)
- SQLite database for analysis history and trend detection
- Intelligent log preprocessing for large files (up to 800KB-1MB)
- Claude prompt caching for 90% cost reduction on subsequent runs
- Full HTTP/HTTPS proxy support for corporate environments
- Pure Go implementation with no CGO dependencies

#### Packages
- `cmd/analyzer` - Main application entry point
- `internal/ai` - Claude AI client with retry logic and prompt caching
- `internal/config` - Configuration management with validation
- `internal/logwatch` - Log file reading and intelligent preprocessing
- `internal/notification` - Telegram Bot API integration with MarkdownV2 formatting
- `internal/storage` - SQLite database operations with pure Go driver
- `github.com/olegiv/go-logger` - External structured logging library with zerolog and log rotation

#### Documentation
- Comprehensive README.md with quick start guide
- CLAUDE.md with AI assistant guidance for development
- PROJECT_SUMMARY.md with complete project overview
- CRON_SETUP.md with detailed cron configuration instructions
- Cross-platform build documentation and examples

#### Scripts
- `scripts/install.sh` - System-wide installation script
- `scripts/generate-logwatch.sh` - Logwatch report generation script

#### Configuration
- `.env` file support with validation
- Configuration templates in `configs/.env.example`
- Support for all essential settings (API keys, Telegram, paths, preprocessing)

### Changed
- Migrated from Node.js to Go for better performance and simpler deployment
- Reduced binary size from ~120MB (Node.js SEA) to ~10-15MB (Go)
- Improved startup time with near-instant execution
- Enhanced type safety with compile-time error checking

### Technical Details

#### Build Optimizations
- **Symbol Stripping**: `-ldflags="-s -w"` removes symbol table and DWARF debug info
- **Path Trimming**: `-trimpath` removes absolute file system paths
- **Size Reduction**: 20-40% smaller binaries compared to unoptimized builds
- **Security**: No local directory structure leaked in binaries
- **Reproducibility**: Identical binaries from same source across different build environments

#### Cross-Compilation
- Supports building for Linux AMD64 from any platform
- Supports building for macOS ARM64 from any platform
- Uses Go's built-in GOOS/GOARCH environment variables
- No additional toolchains or cross-compilers required

#### Dependencies
- `github.com/liushuangls/go-anthropic/v2` v2.16.2 - Anthropic Claude SDK
- `github.com/go-telegram-bot-api/telegram-bot-api/v5` v5.5.1 - Telegram Bot API
- `modernc.org/sqlite` v1.40.0 - Pure Go SQLite
- `github.com/spf13/viper` v1.21.0 - Configuration management
- `github.com/rs/zerolog` v1.34.0 - Structured logging
- `gopkg.in/natefinch/lumberjack.v2` v2.2.1 - Log rotation
- `github.com/joho/godotenv` v1.5.1 - .env file support

### Performance
- Faster startup time compared to Node.js version
- Lower memory footprint
- Efficient SQLite operations with pure Go driver
- Optimized preprocessing algorithm for large log files

### Cost Optimization
- First run: $0.016-0.022 (cache creation)
- Cached run: $0.011-0.015 (cache hits, 90% savings)
- Monthly (daily runs): ~$0.47/month
- Yearly: ~$5.64/year

[Unreleased]: https://github.com/olegiv/logwatch-ai-go/compare/v0.9.0...HEAD
[0.9.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/olegiv/logwatch-ai-go/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.0.0...v0.1.0
[0.0.0]: https://github.com/olegiv/logwatch-ai-go/releases/tag/v0.0.0
