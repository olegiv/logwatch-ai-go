# Logwatch AI Analyzer - Project Summary

## Overview

Go port of the Node.js logwatch-ai project. Intelligent log analyzer using LLM to analyze system and application logs with actionable insights delivered via Telegram.

**Current Version:** v0.5.1

## Supported Log Sources

| Source | Description | Format |
|--------|-------------|--------|
| **Logwatch** | Linux system log aggregation | Text (syslog, auth, mail, etc.) |
| **Drupal Watchdog** | PHP/Drupal application logs | JSON or drush export |

## Supported LLM Providers

| Provider | Type | Cost | Quality |
|----------|------|------|---------|
| **Anthropic Claude** | Cloud | $0.47/month | Excellent |
| **Ollama** | Local | Free | Varies by model |
| **LM Studio** | Local | Free | Varies by model |

## Project Structure

```
logwatch-ai-go/
├── cmd/analyzer/           # Main application entry point
├── internal/
│   ├── ai/                # LLM clients (Anthropic, Ollama, LM Studio)
│   ├── analyzer/          # Multi-source abstraction interfaces
│   ├── config/            # Configuration management
│   ├── drupal/            # Drupal watchdog reader and prompts
│   ├── errors/            # Error sanitization (credential redaction)
│   ├── logging/           # Secure logger (credential filtering)
│   ├── logwatch/          # Logwatch reader and preprocessing
│   ├── notification/      # Telegram client
│   └── storage/           # SQLite operations
├── scripts/               # Installation and helper scripts
├── configs/               # Configuration templates
├── docs/                  # Extended documentation
└── testdata/              # Test fixtures
```

## Core Components

### Configuration (`internal/config/`)
- Environment variable loading with `.env` support
- Provider-specific validation (Anthropic, Ollama, LM Studio)
- Proxy configuration support

### AI Package (`internal/ai/`)
- **Anthropic Claude**: Cloud API with prompt caching
- **Ollama**: Local REST API for models like llama3.3
- **LM Studio**: OpenAI-compatible API for local models
- Shared retry logic with exponential backoff
- Token usage and cost tracking

### Log Sources (`internal/analyzer/`)
- Pluggable architecture via interfaces
- `LogReader`, `Preprocessor`, `PromptBuilder` interfaces
- Registry pattern for extensibility

### Storage (`internal/storage/`)
- Pure Go SQLite (modernc.org/sqlite)
- Schema v2 with log_source_type and site_name columns
- Automatic migration from v1
- 90-day retention cleanup

### Notifications (`internal/notification/`)
- Telegram Bot API integration
- Dual-channel support (archive + alerts)
- MarkdownV2 formatting
- Rate limiting with exponential backoff

### Security (`internal/errors/`, `internal/logging/`)
- Automatic credential sanitization in logs
- Prompt injection protection
- Secure logger wrapper

## Build System

| Command | Description |
|---------|-------------|
| `make build` | Development build |
| `make build-prod` | Optimized production build |
| `make build-linux-amd64` | Linux AMD64 (Debian 12/Ubuntu) |
| `make build-darwin-arm64` | macOS ARM64 (Apple Silicon) |
| `make build-all-platforms` | All platforms |
| `make test` | Run tests |
| `make test-coverage` | Tests with coverage report |

## Dependencies

| Package | Purpose |
|---------|---------|
| github.com/liushuangls/go-anthropic/v2 | Anthropic Claude SDK |
| github.com/go-telegram-bot-api/telegram-bot-api/v5 | Telegram Bot API |
| modernc.org/sqlite | Pure Go SQLite |
| github.com/spf13/viper | Configuration |
| github.com/rs/zerolog | Structured logging |

## Key Features

- **Multi-Source Support**: Logwatch and Drupal Watchdog
- **Multi-Provider LLM**: Anthropic (cloud), Ollama (local), LM Studio (local)
- **Multi-Site Drupal**: Centralized `drupal-sites.json` configuration
- **Intelligent Preprocessing**: Handles large logs (up to 1MB)
- **Prompt Caching**: 90% cost savings on subsequent runs
- **Historical Context**: Last 7 days for trend detection
- **Pure Go**: No CGO dependencies
- **Cross-Platform**: Single binary deployment

## Deployment Status

| Environment | Platform | Status |
|-------------|----------|--------|
| Integration | Debian 12 | Validated |
| QA | Debian 12 | Validated |
| Pre-Production | Debian 12 | Validated |
| Production | Debian 12 | Ready |

## Cost Estimation (Anthropic Claude)

| Run Type | Cost |
|----------|------|
| First run (cache creation) | $0.016-0.022 |
| Cached runs | $0.011-0.015 |
| Monthly (daily runs) | ~$0.47 |
| Yearly | ~$5.64 |

Local LLM providers (Ollama, LM Studio): **$0.00**

## Documentation

- [README.md](README.md) - Quick start and configuration
- [CHANGELOG.md](CHANGELOG.md) - Version history
- [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) - Production deployment
- [docs/COST_OPTIMIZATION.md](docs/COST_OPTIMIZATION.md) - Cost analysis
- [docs/CRON_SETUP.md](docs/CRON_SETUP.md) - Cron configuration
- [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) - Common issues

## Advantages Over Node.js Version

| Aspect | Go | Node.js |
|--------|-----|---------|
| Binary size | ~10-15MB | ~120MB |
| Dependencies | None (pure Go) | Node.js runtime |
| Startup time | Near-instant | Slower |
| Memory usage | Lower | Higher |
| Cross-compilation | Built-in | Complex |
| CGO | Not required | N/A |

## Status

**Production Ready** - Deployed and validated across multiple environments.
