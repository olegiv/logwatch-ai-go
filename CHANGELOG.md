# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/olegiv/logwatch-ai-go/compare/v0.5.1...HEAD
[0.5.1]: https://github.com/olegiv/logwatch-ai-go/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/olegiv/logwatch-ai-go/compare/v0.0.0...v0.1.0
[0.0.0]: https://github.com/olegiv/logwatch-ai-go/releases/tag/v0.0.0
