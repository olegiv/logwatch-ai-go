# Logwatch AI Analyzer (Go)

[![Go](https://github.com/olegiv/logwatch-ai-go/actions/workflows/go.yml/badge.svg)](https://github.com/olegiv/logwatch-ai-go/actions/workflows/go.yml)
[![CodeQL](https://github.com/olegiv/logwatch-ai-go/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/olegiv/logwatch-ai-go/actions/workflows/github-code-scanning/codeql)
[![Dependency review](https://github.com/olegiv/logwatch-ai-go/actions/workflows/dependency-review.yml/badge.svg)](https://github.com/olegiv/logwatch-ai-go/actions/workflows/dependency-review.yml)

An intelligent log analyzer that uses Claude AI to analyze log reports and send actionable insights via Telegram. This is a Go port of the original Node.js [logwatch-ai](https://github.com/olegiv/logwatch-ai) project.

**Supported Log Sources:**
- **Logwatch** - Linux system log aggregation (syslog, auth, mail, etc.)
- **Drupal Watchdog** - PHP/Drupal application logs (JSON or drush export)

## Features

- **AI-Powered Analysis**: Uses Anthropic's Claude Sonnet 4.5 to analyze log reports
- **Multi-Source Support**: Analyze Logwatch reports or Drupal watchdog logs
- **Smart Notifications**: Dual-channel Telegram notifications (archive + alerts)
- **Historical Tracking**: SQLite database stores analysis history for trend detection
- **Intelligent Preprocessing**: Handles large log files (up to 800KB-1MB) with smart content reduction
- **Cost Optimization**: Implements Claude prompt caching (16-30% cost savings)
- **Proxy Support**: Full HTTP/HTTPS proxy support for corporate environments
- **Secure Logging**: Automatic credential sanitization prevents API keys from appearing in logs
- **Rate Limiting**: Telegram API rate limiting with exponential backoff retry
- **Prompt Injection Protection**: Input sanitization filters adversarial content from logs
- **Pure Go**: No CGO dependencies, easy cross-platform deployment

## Quick Start

### Prerequisites

- Go 1.25+ (for building from source)
- Logwatch installed and configured
- Anthropic API key
- Telegram bot token and channel IDs

### Installation

1. **Clone the repository**
```bash
git clone https://github.com/olegiv/logwatch-ai-go.git
cd logwatch-ai-go
```

2. **Build the application**
```bash
make build
```

3. **Configure environment**
```bash
cp configs/.env.example .env
# Edit .env with your credentials
```

4. **Install system-wide** (optional)
```bash
sudo make install
```

### Configuration

Create a `.env` file with the following settings:

```bash
# AI Provider
ANTHROPIC_API_KEY=sk-ant-xxxxx
CLAUDE_MODEL=claude-sonnet-4-5-20250929
AI_TIMEOUT_SECONDS=120
AI_MAX_TOKENS=8000

# Telegram
TELEGRAM_BOT_TOKEN=1234567890:ABC-DEF1234ghIkl-zyx57W2v1u123ew11
TELEGRAM_CHANNEL_ARCHIVE_ID=-1001234567890    # Required
TELEGRAM_CHANNEL_ALERTS_ID=-1009876543210     # Optional

# Log Source Configuration
# Options: "logwatch" (default) or "drupal_watchdog"
LOG_SOURCE_TYPE=logwatch

# Logwatch Configuration (used when LOG_SOURCE_TYPE=logwatch)
LOGWATCH_OUTPUT_PATH=/tmp/logwatch-output.txt

# Drupal Watchdog Configuration (used when LOG_SOURCE_TYPE=drupal_watchdog)
# DRUPAL_WATCHDOG_PATH=/var/log/drupal-watchdog.json
# DRUPAL_WATCHDOG_FORMAT=json    # "json" (recommended) or "drush"
# DRUPAL_SITE_NAME=              # Optional: for multi-site deployments

# Common Log Settings
MAX_LOG_SIZE_MB=10

# Application
LOG_LEVEL=info
ENABLE_DATABASE=true
DATABASE_PATH=./data/summaries.db

# Preprocessing
ENABLE_PREPROCESSING=true
MAX_PREPROCESSING_TOKENS=150000

# Proxy (optional)
HTTP_PROXY=http://proxy.example.com:8080
HTTPS_PROXY=http://proxy.example.com:8080
```

### Setting up Telegram

1. **Create a Telegram bot**:
   - Message [@BotFather](https://t.me/BotFather)
   - Send `/newbot` and follow the instructions
   - Save the bot token

2. **Create Telegram channel(s)**:
   - Create a channel for archives
   - (Optional) Create a separate channel for alerts
   - Add your bot as an administrator to both channels

3. **Get channel IDs**:
   - Forward a message from your channel to [@userinfobot](https://t.me/userinfobot)
   - The bot will reply with the channel ID (should start with `-100`)

### Cron Setup

Run logwatch analysis daily at 2:00 AM:

**Root crontab** (generate logwatch report):
```bash
0 2 * * * /opt/logwatch-ai/scripts/generate-logwatch.sh
```

**User crontab** (run analyzer):
```bash
15 2 * * * cd /opt/logwatch-ai && ./logwatch-analyzer >> logs/cron.log 2>&1
```

See [docs/CRON_SETUP.md](docs/CRON_SETUP.md) for detailed setup instructions.

### Drupal Watchdog Setup

To analyze Drupal watchdog logs instead of logwatch:

1. **Export watchdog logs as JSON** (recommended):
```bash
# Via drush
drush watchdog:show --format=json > /var/log/drupal-watchdog.json

# Or via MySQL/MariaDB
mysql -u user -p drupal_db -e "SELECT * FROM watchdog ORDER BY wid DESC LIMIT 1000" | \
  jq -s '.' > /var/log/drupal-watchdog.json
```

2. **Configure environment**:
```bash
LOG_SOURCE_TYPE=drupal_watchdog
DRUPAL_WATCHDOG_PATH=/var/log/drupal-watchdog.json
DRUPAL_WATCHDOG_FORMAT=json
```

3. **Set up automated export** (cron example):
```bash
# Export watchdog logs daily at 2:00 AM
0 2 * * * drush -r /var/www/html watchdog:show --format=json --count=1000 > /var/log/drupal-watchdog.json

# Run analyzer at 2:15 AM
15 2 * * * cd /opt/logwatch-ai && ./logwatch-analyzer >> logs/cron.log 2>&1
```

**Drupal Watchdog JSON Format:**
```json
[
  {
    "wid": 1234,
    "uid": 1,
    "type": "php",
    "message": "PDOException: SQLSTATE[HY000] [2002] Connection refused",
    "variables": "a:0:{}",
    "severity": 3,
    "link": "",
    "location": "https://example.com/",
    "referer": "",
    "hostname": "127.0.0.1",
    "timestamp": 1699900800
  }
]
```

## Usage

### Manual Run

```bash
# From the project directory
./bin/logwatch-analyzer

# Or if installed system-wide
logwatch-analyzer
```

### Build Options

```bash
make build                # Development build
make build-prod           # Production build (optimized, smaller binary)
make build-linux-amd64    # Build for Linux AMD64 (Debian 12/Ubuntu 24)
make build-darwin-arm64   # Build for macOS ARM64 (Apple Silicon)
make build-all-platforms  # Build for all platforms
make test                 # Run tests
make test-coverage        # Run tests with coverage
make clean                # Clean build artifacts
make install              # Install to /opt/logwatch-ai
```

### Cross-Platform Builds

Go's built-in cross-compilation makes it easy to build for different platforms:

**Linux AMD64** (Debian 12, Ubuntu 24, most Linux distributions):
```bash
make build-linux-amd64
# Output: bin/logwatch-analyzer-linux-amd64
```

**macOS ARM64** (Apple Silicon - M1, M2, M3):
```bash
make build-darwin-arm64
# Output: bin/logwatch-analyzer-darwin-arm64
```

**All platforms at once**:
```bash
make build-all-platforms
# Outputs both binaries and shows file sizes
```

All cross-platform builds include production optimizations:
- `-ldflags="-s -w"` - Strips symbols and debug information
- `-trimpath` - Removes file system paths from binary
- Result: Smaller binaries (~20-40% size reduction) and improved security

## Architecture

### Project Structure

```
logwatch-ai-go/
├── cmd/
│   └── analyzer/           # Main application entry point
├── internal/
│   ├── ai/                 # Claude AI client and prompts
│   ├── analyzer/           # Multi-source abstraction (interfaces)
│   ├── config/             # Configuration management
│   ├── drupal/             # Drupal watchdog reader and prompts
│   ├── errors/             # Error sanitization (credential redaction)
│   ├── logging/            # Secure logger (credential filtering)
│   ├── logwatch/           # Logwatch file reading and preprocessing
│   ├── notification/       # Telegram notifications
│   └── storage/            # SQLite database operations
├── scripts/                # Helper scripts
├── configs/                # Configuration templates
├── docs/                   # Documentation
├── testdata/               # Test fixtures
└── Makefile               # Build automation
```

### How It Works

1. **Log Generation**:
   - *Logwatch*: Root cron runs `generate-logwatch.sh` to create daily report
   - *Drupal*: drush exports watchdog entries to JSON file
2. **Source Selection**: Application loads appropriate reader based on `LOG_SOURCE_TYPE`
3. **File Reading**: Source-specific reader validates and parses log content
4. **Preprocessing**: Large files are intelligently compressed with source-aware priority
5. **Historical Context**: Retrieves last 7 days of analysis from database
6. **AI Analysis**: Claude Sonnet 4.5 analyzes with source-specific prompts
7. **Storage**: Results saved to SQLite database
8. **Notifications**: Sent to Telegram (archive channel always, alerts channel conditionally)
9. **Cleanup**: Old database entries (>90 days) are removed

## Cost Estimation

### Claude AI Pricing (Sonnet 4.5)

- Input: $3.00 per million tokens
- Output: $15.00 per million tokens
- Cache write: $3.75 per million tokens
- Cache read: $0.30 per million tokens (90% savings)

### Typical Costs

- **First run**: $0.0160-0.0220 (cache creation)
- **Cached run**: $0.0107-0.0154 (cache hits)
- **Monthly (daily)**: ~$0.47/month
- **Yearly**: ~$5.64/year

## Telegram Notification Format

```
🔍 Logwatch Analysis Report
🖥 Host: server01
📅 Date: 2025-11-12 02:15:00
🟢 Status: Good

📋 Execution Stats
• Critical Issues: 0
• Warnings: 2
• Recommendations: 3
• Cost: $0.0154
• Duration: 12.62s

📊 Summary
System is operating normally with minor warnings...

⚡ Warnings (2)
1. Disk usage at 85% on /var partition
2. 5 failed SSH attempts from 192.168.1.50

💡 Recommendations
1. Clean up old log files in /var/log
2. Monitor IP 192.168.1.50 for activity

📈 Key Metrics
• Failed Logins: 5
• Disk Usage: 85% on /var
• Error Count: 0
```

## Differences from Node.js Version

This Go implementation provides feature parity with the original Node.js version while offering:

### Advantages

- **Pure Go**: No CGO dependencies (using modernc.org/sqlite)
- **Simpler Deployment**: Single binary, no runtime dependencies
- **Better Performance**: Faster startup and lower memory usage
- **Type Safety**: Compile-time type checking
- **Smaller Binary**: ~10-15MB (vs ~120MB Node.js SEA)
- **Easier Cross-Compilation**: Build for any platform

### Maintained Features

- ✅ Identical AI prompts and analysis logic
- ✅ Same database schema (compatible with Node.js version)
- ✅ Same preprocessing algorithm
- ✅ Same notification format and dual-channel logic
- ✅ Same cost tracking and token estimation
- ✅ Prompt caching support
- ✅ Proxy configuration

## Development

### Running Tests

```bash
# Run all tests
make test

# Run with coverage
make test-coverage

# Run specific package tests
go test -v ./internal/ai
go test -v ./internal/logwatch
```

### Project Dependencies

- **github.com/liushuangls/go-anthropic/v2** - Anthropic Claude SDK
- **github.com/go-telegram-bot-api/telegram-bot-api/v5** - Telegram Bot API
- **modernc.org/sqlite** - Pure Go SQLite implementation
- **github.com/spf13/viper** - Configuration management
- **github.com/rs/zerolog** - Structured logging
- **gopkg.in/natefinch/lumberjack.v2** - Log rotation

## Troubleshooting

### Common Issues

**"Configuration validation failed: ANTHROPIC_API_KEY is required"**
- Ensure `.env` file exists and contains valid API key

**"Logwatch file is too old"**
- Check if logwatch cron is running: `sudo crontab -l`
- Verify logwatch output exists: `ls -lh /tmp/logwatch-output.txt`

**"Failed to send to archive channel"**
- Verify bot is added as admin to the channel
- Check channel ID starts with `-100`
- Test bot token: `curl https://api.telegram.org/bot<TOKEN>/getMe`

**Database locked errors**
- Ensure only one instance is running
- Check file permissions on `data/` directory
- Built-in 5-second busy timeout handles temporary lock contention

**Drupal Watchdog: "Invalid JSON format"**
- Ensure watchdog export is valid JSON array format
- Check for UTF-8 encoding issues in log messages
- Validate with: `jq . /path/to/watchdog.json`

**Drupal Watchdog: "Missing entries"**
- Check `DRUPAL_WATCHDOG_FORMAT` matches your file format
- Verify file permissions and path
- Ensure drush export includes `--count` parameter

See [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) for more solutions.

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Acknowledgments

- Original [logwatch-ai](https://github.com/olegiv/logwatch-ai) Node.js implementation
- [Anthropic](https://www.anthropic.com/) for Claude AI
- The Go community for excellent libraries

## Support

- **Issues**: [GitHub Issues](https://github.com/olegiv/logwatch-ai-go/issues)
- **Discussions**: [GitHub Discussions](https://github.com/olegiv/logwatch-ai-go/discussions)
- **Original Project**: [logwatch-ai](https://github.com/olegiv/logwatch-ai)
