# Production Deployment Guide

This document covers deployment best practices for Logwatch AI Analyzer.

## Validated Platforms

- **Linux Debian 12** - Primary production platform
- **macOS (Darwin)** - Development platform

## Deployment Pipeline

```
Development (macOS) → Integration (Debian 12) → QA (Debian 12) → Pre-Production (Debian 12) → Production
```

## LLM Provider Selection

Choose the appropriate LLM provider for your deployment:

| Provider | Use Case | Requirements |
|----------|----------|--------------|
| **Anthropic Claude** | Production (best quality) | API key, internet access |
| **Ollama** | Privacy-sensitive, cost-free | 8-45GB RAM depending on model |
| **LM Studio** | Development, testing | Desktop app, 8-45GB RAM |

### Anthropic Claude (Recommended for Production)
- Best analysis quality
- Prompt caching reduces costs (~$0.47/month)
- Requires `ANTHROPIC_API_KEY`

### Ollama (Recommended for Air-Gapped/Privacy)
- Zero cost, unlimited analysis
- Logs never leave the server
- Requires Ollama installed and model pulled
- Slower than cloud (depends on hardware)

### LM Studio (Recommended for Development)
- User-friendly GUI
- Easy model switching
- Not recommended for headless servers

## Pre-Deployment Checklist

1. **Choose LLM provider**: Select based on requirements above
2. **Build for target platform**: Use `make build-linux-amd64` for Debian/Ubuntu
3. **Test in staging**: Deploy to pre-production environment first
4. **Verify credentials**: Test with actual API keys in isolated environment
5. **Check cron configuration**: Ensure logwatch runs before analyzer
6. **Monitor logs**: Watch `/opt/logwatch-ai/logs/` for first few runs

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

## Drupal Watchdog Deployment

For Drupal log analysis, additional setup is required:

1. **Install jq**: `apt-get install jq` (required for multi-site config)
2. **Create drupal-sites.json** in `/opt/logwatch-ai/` or `./configs/`
3. **Set up watchdog export cron**:
   ```bash
   0 2 * * * /opt/logwatch-ai/scripts/generate-drupal-watchdog.sh --site production
   ```
4. **Set up analyzer cron**:
   ```bash
   15 2 * * * cd /opt/logwatch-ai && ./logwatch-analyzer -drupal-site production >> logs/cron.log 2>&1
   ```

See `configs/drupal-sites.json.example` for configuration format.

## Ollama Server Deployment

For local LLM inference on production servers:

1. **Install Ollama**:
   ```bash
   curl -fsSL https://ollama.ai/install.sh | sh
   ```

2. **Pull model**:
   ```bash
   ollama pull llama3.3:latest
   ```

3. **Enable as systemd service**:
   ```bash
   sudo systemctl enable ollama
   sudo systemctl start ollama
   ```

4. **Configure `.env`**:
   ```
   LLM_PROVIDER=ollama
   OLLAMA_BASE_URL=http://localhost:11434
   OLLAMA_MODEL=llama3.3:latest
   ```

**Hardware Requirements:**
- llama3.3:latest: ~45GB RAM
- llama3.2:8b: ~5GB RAM (acceptable quality)
