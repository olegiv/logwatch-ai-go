# Production Deployment Guide

This document covers deployment best practices for Logwatch AI Analyzer.

## Validated Platforms

- **Linux Debian 12** - Primary production platform
- **macOS (Darwin)** - Development platform

## Deployment Pipeline

```
Development (macOS) → Integration (Debian 12) → QA (Debian 12) → Pre-Production (Debian 12) → Production
```

## Pre-Deployment Checklist

1. **Build for target platform**: Use `make build-linux-amd64` for Debian/Ubuntu
2. **Test in staging**: Deploy to pre-production environment first
3. **Verify credentials**: Test with actual API keys in isolated environment
4. **Check cron configuration**: Ensure logwatch runs before analyzer
5. **Monitor logs**: Watch `/opt/logwatch-ai/logs/` for first few runs

## Deployment Steps

1. Build for target platform: `make build-linux-amd64`
2. Transfer binary to target system
3. Run installation script: `sudo ./scripts/install.sh`
4. Configure `.env` with environment-specific credentials
5. Test manual run: `/opt/logwatch-ai/logwatch-analyzer`
6. Verify Telegram notifications received
7. Set up cron jobs (see `docs/CRON_SETUP.md`)
8. Monitor logs in `/opt/logwatch-ai/logs/`

## Environment-Specific Configuration

- Use separate Telegram channels for different environments
- Use different database paths to avoid conflicts
- Adjust `LOG_LEVEL` (debug for dev/integration, info for qa/prod)
- Consider using environment-specific `.env` files

## Monitoring Recommendations

| What to Monitor | Location/Command |
|-----------------|------------------|
| Log files | `logs/analyzer.log` |
| Database size | `data/summaries.db` (cleanup every 90 days) |
| Telegram delivery | Check archive and alerts channels |
| API costs | Query `cost_usd` in database |
| Cron execution | `grep CRON /var/log/syslog` |

## Security Considerations

- **API keys**: Store `.env` with restricted permissions (600)
- **Log files**: Contains sensitive system information, restrict access
- **Database**: Contains historical analysis, ensure proper file permissions
- **Network**: Use HTTPS proxy in corporate environments
- **Updates**: Regularly update dependencies for security patches
- **Credential sanitization**: All logs and errors automatically redact API keys and tokens
- **Prompt injection protection**: Log content is sanitized to filter common prompt injection patterns

## Performance Tuning

| Setting | Default | Recommendation |
|---------|---------|----------------|
| `MAX_PREPROCESSING_TOKENS` | 150,000 | Adjust based on log size |
| Historical context | 7 days | Reduce if logs are consistent |
| Database cleanup | 90 days | Adjust based on retention needs |
| Log rotation | 10MB | Adjust in logger config |

## Troubleshooting in Production

1. **Check logs first**: `/opt/logwatch-ai/logs/analyzer.log`
2. **Verify cron**: `grep CRON /var/log/syslog` or `journalctl -u cron`
3. **Test manually**: Run `/opt/logwatch-ai/logwatch-analyzer` as same user as cron
4. **Check database**: `sqlite3 /opt/logwatch-ai/data/summaries.db "SELECT COUNT(*) FROM summaries;"`
5. **Validate environment**: Ensure `.env` is in `/opt/logwatch-ai/` directory

## Cross-Platform Builds

```bash
# Use Makefile targets for cross-platform builds
make build-linux-amd64       # Linux AMD64 binary
make build-darwin-arm64      # macOS ARM64 binary
make build-all-platforms     # Build all platforms

# Manual cross-compilation (if needed for other platforms)
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -trimpath -o bin/logwatch-analyzer-linux ./cmd/analyzer
GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -trimpath -o bin/logwatch-analyzer-linux-arm64 ./cmd/analyzer
```

All production builds use optimizations:
- `-ldflags="-s -w"` - Strip symbols and debug information
- `-trimpath` - Remove file system paths from binary
