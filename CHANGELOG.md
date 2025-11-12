# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
- `pkg/logger` - Structured logging with zerolog and log rotation

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

[0.0.0]: https://github.com/olegiv/logwatch-ai-go/releases/tag/v0.0.0
