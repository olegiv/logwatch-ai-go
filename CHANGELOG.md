# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.2.0]: https://github.com/olegiv/logwatch-ai-go/releases/tag/v0.2.0
[0.1.0]: https://github.com/olegiv/logwatch-ai-go/releases/tag/v0.1.0
[0.0.0]: https://github.com/olegiv/logwatch-ai-go/releases/tag/v0.0.0
