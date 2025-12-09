# Troubleshooting Guide

This guide covers common issues and their solutions when using Logwatch AI Analyzer.

## Table of Contents

- [Installation Issues](#installation-issues)
- [Configuration Issues](#configuration-issues)
- [Runtime Errors](#runtime-errors)
- [API Integration Issues](#api-integration-issues)
- [Database Issues](#database-issues)
- [Cron/Scheduling Issues](#cronscheduling-issues)
- [Log Processing Issues](#log-processing-issues)
- [Network/Proxy Issues](#networkproxy-issues)
- [Getting Help](#getting-help)

---

## Installation Issues

### Binary Not Found After Installation

**Symptom:**
```bash
$ logwatch-analyzer
command not found: logwatch-analyzer
```

**Cause:** The `/usr/local/bin` directory is not in your PATH, or the symlink wasn't created.

**Solution:**
```bash
# Check if the binary exists
ls -l /opt/logwatch-ai/logwatch-analyzer
ls -l /usr/local/bin/logwatch-analyzer

# If symlink is missing, create it manually
sudo ln -s /opt/logwatch-ai/logwatch-analyzer /usr/local/bin/logwatch-analyzer

# Verify PATH includes /usr/local/bin
echo $PATH | grep -o "/usr/local/bin"

# If not in PATH, add to ~/.bashrc or ~/.zshrc
echo 'export PATH="/usr/local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

### Permission Denied When Running Binary

**Symptom:**
```bash
$ ./logwatch-analyzer
-bash: ./logwatch-analyzer: Permission denied
```

**Cause:** Binary doesn't have execute permissions.

**Solution:**
```bash
chmod +x /opt/logwatch-ai/logwatch-analyzer
# Or during build
chmod +x ./bin/logwatch-analyzer
```

### Build Fails with "Go Version Too Old"

**Symptom:**
```
go: cannot find main module; see 'go help modules'
or: requires Go 1.25 or later
```

**Cause:** Go version is too old.

**Solution:**
```bash
# Check Go version
go version

# Update Go (Linux)
sudo rm -rf /usr/local/go
wget https://go.dev/dl/go1.23.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.23.0.linux-amd64.tar.gz

# Update Go (macOS with Homebrew)
brew upgrade go
```

---

## Configuration Issues

### "Configuration validation failed: ANTHROPIC_API_KEY is required"

**Symptom:**
```
Configuration error: ANTHROPIC_API_KEY is required
```

**Cause:** Missing or invalid API key in configuration.

**Solution:**
```bash
# Check if .env file exists
ls -la /opt/logwatch-ai/.env

# If missing, copy template
cp /opt/logwatch-ai/configs/.env.example /opt/logwatch-ai/.env

# Edit .env and add your API key
nano /opt/logwatch-ai/.env
# Add: ANTHROPIC_API_KEY=sk-ant-xxxxx

# Verify the key starts with 'sk-ant-'
grep ANTHROPIC_API_KEY /opt/logwatch-ai/.env
```

### "Invalid Telegram bot token format"

**Symptom:**
```
Configuration error: TELEGRAM_BOT_TOKEN must match format number:token
```

**Cause:** Invalid bot token format.

**Solution:**
```bash
# Correct format: 1234567890:ABC-DEF1234ghIkl-zyx57W2v1u123ew11
# Check your token from @BotFather

# Test token validity
curl https://api.telegram.org/bot<YOUR_TOKEN>/getMe

# If valid, you'll see:
# {"ok":true,"result":{"id":123456789,"is_bot":true,"first_name":"..."}}
```

### "TELEGRAM_CHANNEL_ARCHIVE_ID must be less than -100"

**Symptom:**
```
Configuration error: TELEGRAM_CHANNEL_ARCHIVE_ID must be less than -100 (supergroup or channel)
```

**Cause:** Using incorrect channel ID format.

**Solution:**
```bash
# Correct format for channels/supergroups: -1001234567890
# Forward a message from your channel to @userinfobot to get the ID

# Common mistakes:
# ❌ Using positive number: 1234567890
# ❌ Using username: @mychannel
# ✅ Correct: -1001234567890
```

### Environment Variables Not Loading from .env File (v0.2.0+)

**Symptom:** Settings in .env file are ignored, using OS environment variables instead.

**Cause:** Since v0.2.0, .env file takes priority. You may have conflicting OS env vars.

**Solution:**
```bash
# Check current environment variables
env | grep -E "(ANTHROPIC|TELEGRAM|LOGWATCH)"

# Clear conflicting OS variables
unset ANTHROPIC_API_KEY
unset TELEGRAM_BOT_TOKEN
# etc...

# Or update your .env file to match your intended configuration
nano /opt/logwatch-ai/.env
```

---

## Runtime Errors

### "Logwatch file not found"

**Symptom:**
```
Error: failed to read logwatch file: open /tmp/logwatch-output.txt: no such file or directory
```

**Cause:** Logwatch output file doesn't exist or wrong path.

**Solution:**
```bash
# Check if logwatch is installed
which logwatch

# Check if logwatch output exists
ls -lh /tmp/logwatch-output.txt

# Generate logwatch report manually
sudo /opt/logwatch-ai/scripts/generate-logwatch.sh

# Or run logwatch directly
sudo logwatch --output file --filename /tmp/logwatch-output.txt --range today --detail high

# Verify .env has correct path
grep LOGWATCH_OUTPUT_PATH /opt/logwatch-ai/.env
```

### "Logwatch file is too old"

**Symptom:**
```
Warning: logwatch file is more than 24 hours old
Error: logwatch file is too old (48h limit)
```

**Cause:** Logwatch cron job isn't running or failed.

**Solution:**
```bash
# Check root crontab
sudo crontab -l | grep logwatch

# If missing, add it
sudo crontab -e
# Add: 0 2 * * * /opt/logwatch-ai/scripts/generate-logwatch.sh

# Check cron service is running
systemctl status cron  # Debian/Ubuntu
systemctl status crond # RHEL/CentOS

# Check cron logs
grep CRON /var/log/syslog | grep logwatch  # Debian/Ubuntu
journalctl -u cron | grep logwatch          # systemd

# Generate fresh report
sudo /opt/logwatch-ai/scripts/generate-logwatch.sh
```

### "Logwatch file too large"

**Symptom:**
```
Error: logwatch file size (15.2 MB) exceeds maximum (10.0 MB)
```

**Cause:** Logwatch output is larger than configured maximum.

**Solution:**
```bash
# Option 1: Increase max size in .env
echo "MAX_LOG_SIZE_MB=20" >> /opt/logwatch-ai/.env

# Option 2: Reduce logwatch detail level
# Edit /opt/logwatch-ai/scripts/generate-logwatch.sh
# Change: --detail high
# To:     --detail med

# Option 3: Enable preprocessing (should handle up to ~100MB)
echo "ENABLE_PREPROCESSING=true" >> /opt/logwatch-ai/.env
echo "MAX_PREPROCESSING_TOKENS=200000" >> /opt/logwatch-ai/.env
```

---

## API Integration Issues

### Claude API: "Invalid API Key"

**Symptom:**
```
Error: failed to analyze with Claude: authentication error (401)
```

**Cause:** Invalid or expired API key.

**Solution:**
```bash
# Verify API key format (should start with sk-ant-)
grep ANTHROPIC_API_KEY /opt/logwatch-ai/.env

# Test API key directly
curl https://api.anthropic.com/v1/messages \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{"model":"claude-sonnet-4-5-20250929","max_tokens":1024,"messages":[{"role":"user","content":"test"}]}'

# If invalid, get new key from https://console.anthropic.com/
# Update .env with new key
```

### Claude API: "Rate Limit Exceeded"

**Symptom:**
```
Error: failed to analyze with Claude: rate limit exceeded (429)
```

**Cause:** Too many requests to Claude API.

**Solution:**
```bash
# Wait and retry (the tool has built-in retry logic)
# If persistent, check your API usage at https://console.anthropic.com/

# Reduce frequency of runs if hitting limits
# Edit crontab to run less frequently:
# Instead of: 15 2 * * *      (daily)
# Use:        15 2 * * 0      (weekly)
# Or:         15 2 1 * *      (monthly)
```

### Telegram: "Failed to send to archive channel"

**Symptom:**
```
Error: failed to send to archive channel: Forbidden: bot is not a member of the channel
```

**Cause:** Bot is not added to the channel or doesn't have permissions.

**Solution:**
```bash
# 1. Add bot to channel:
#    - Go to channel settings
#    - Add administrators
#    - Search for your bot username
#    - Add as administrator

# 2. Grant necessary permissions:
#    - Post messages: YES
#    - Edit messages: NO (optional)
#    - Delete messages: NO (optional)

# 3. Verify bot membership
curl "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getChat?chat_id=<CHANNEL_ID>"

# 4. Test send message
curl -X POST "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/sendMessage" \
  -d "chat_id=<CHANNEL_ID>" \
  -d "text=Test message"
```

### Telegram: "Chat not found"

**Symptom:**
```
Error: failed to send message: Bad Request: chat not found
```

**Cause:** Incorrect channel ID format or bot not added.

**Solution:**
```bash
# Verify channel ID format
# Channels/supergroups should start with -100
# Example: -1001234567890

# Get correct channel ID:
# 1. Forward a message from your channel to @userinfobot
# 2. Bot will reply with the channel ID

# Update .env with correct ID
nano /opt/logwatch-ai/.env
```

---

## Database Issues

### "Database is locked"

**Symptom:**
```
Error: database is locked
```

**Cause:** Multiple instances running simultaneously or improper shutdown.

**Solution:**
```bash
# Check for running instances
ps aux | grep logwatch-analyzer

# Kill any hung processes
pkill -9 logwatch-analyzer

# Check file permissions
ls -l /opt/logwatch-ai/data/summaries.db
ls -ld /opt/logwatch-ai/data/

# Ensure proper permissions
sudo chown -R <your-user>:<your-group> /opt/logwatch-ai/data/
chmod 755 /opt/logwatch-ai/data/
chmod 644 /opt/logwatch-ai/data/summaries.db

# If still locked, check for .db-shm or .db-wal files
ls -la /opt/logwatch-ai/data/
# Remove if found (only if no process is running!)
rm /opt/logwatch-ai/data/summaries.db-shm
rm /opt/logwatch-ai/data/summaries.db-wal
```

### "Failed to create database"

**Symptom:**
```
Error: failed to initialize storage: unable to open database file
```

**Cause:** Permission issues or invalid path.

**Solution:**
```bash
# Create data directory if missing
mkdir -p /opt/logwatch-ai/data

# Set correct permissions
chmod 755 /opt/logwatch-ai/data
chown <your-user>:<your-group> /opt/logwatch-ai/data

# Verify DATABASE_PATH in .env
grep DATABASE_PATH /opt/logwatch-ai/.env
# Should be: DATABASE_PATH=./data/summaries.db
# Or absolute: DATABASE_PATH=/opt/logwatch-ai/data/summaries.db

# Test database creation
cd /opt/logwatch-ai
sqlite3 data/summaries.db "SELECT 1;"
```

### Database Corruption

**Symptom:**
```
Error: database disk image is malformed
```

**Cause:** Power loss, disk full, or improper shutdown.

**Solution:**
```bash
# Backup current database
cp /opt/logwatch-ai/data/summaries.db /opt/logwatch-ai/data/summaries.db.backup

# Try to recover
sqlite3 /opt/logwatch-ai/data/summaries.db "PRAGMA integrity_check;"

# If corrupted, dump and restore
sqlite3 /opt/logwatch-ai/data/summaries.db.backup ".dump" | \
  sqlite3 /opt/logwatch-ai/data/summaries.db.recovered

# Replace with recovered
mv /opt/logwatch-ai/data/summaries.db.recovered /opt/logwatch-ai/data/summaries.db

# If recovery fails, start fresh (LOSES HISTORY!)
rm /opt/logwatch-ai/data/summaries.db
# Next run will create new database
```

---

## Cron/Scheduling Issues

### Analyzer Doesn't Run via Cron

**Symptom:** Manual run works, but cron doesn't execute.

**Cause:** Cron environment differs from shell environment.

**Solution:**
```bash
# 1. Check cron logs
grep CRON /var/log/syslog | tail -20  # Debian/Ubuntu
journalctl -u cron | tail -20          # systemd

# 2. Verify crontab entry
crontab -l | grep logwatch

# 3. Add full paths and redirect output for debugging
# Edit crontab
crontab -e

# Update entry to:
15 2 * * * cd /opt/logwatch-ai && /opt/logwatch-ai/logwatch-analyzer >> /opt/logwatch-ai/logs/cron.log 2>&1

# 4. Check logs after next run
tail -f /opt/logwatch-ai/logs/cron.log

# 5. Ensure .env is in the working directory
ls -la /opt/logwatch-ai/.env
```

### Cron Runs But No Notifications

**Symptom:** Cron executes but no Telegram messages received.

**Cause:** Configuration not loaded or errors silently failing.

**Solution:**
```bash
# Enable logging
echo "LOG_LEVEL=debug" >> /opt/logwatch-ai/.env

# Check analyzer logs
tail -50 /opt/logwatch-ai/logs/analyzer.log

# Check cron output
tail -50 /opt/logwatch-ai/logs/cron.log

# Test manually as the cron user
sudo -u <cron-user> bash
cd /opt/logwatch-ai
./logwatch-analyzer

# Verify .env loads correctly
cat /opt/logwatch-ai/.env | grep -v "^#" | grep -v "^$"
```

---

## Log Processing Issues

### "Token estimation seems off"

**Symptom:** Token usage is higher/lower than expected.

**Cause:** Algorithm is calibrated for English text.

**Solution:**
```bash
# Token estimation uses: max(chars/4, words/0.75)

# For non-English text, adjust MAX_PREPROCESSING_TOKENS
# to compensate for differences
echo "MAX_PREPROCESSING_TOKENS=100000" >> /opt/logwatch-ai/.env

# Monitor actual token usage in logs
grep "Token usage" /opt/logwatch-ai/logs/analyzer.log

# Check database for historical usage
sqlite3 /opt/logwatch-ai/data/summaries.db \
  "SELECT AVG(input_tokens), AVG(output_tokens) FROM summaries;"
```

### "Preprocessing removes too much content"

**Symptom:** Important information missing from analysis.

**Cause:** Aggressive preprocessing reducing critical content.

**Solution:**
```bash
# 1. Increase preprocessing token limit
echo "MAX_PREPROCESSING_TOKENS=200000" >> /opt/logwatch-ai/.env

# 2. Adjust priority keywords (requires code change)
# Edit: internal/logwatch/preprocessor.go
# Add your important keywords to HIGH or MEDIUM priority

# 3. Disable preprocessing for detailed analysis
echo "ENABLE_PREPROCESSING=false" >> /opt/logwatch-ai/.env
# Note: This may fail for very large logs

# 4. Reduce logwatch detail level to generate smaller reports
# Edit: /opt/logwatch-ai/scripts/generate-logwatch.sh
# Change --detail high to --detail med
```

### Claude Analysis Returns "Bad" Status Frequently

**Symptom:** Most analyses return "Bad" or "Critical" status.

**Cause:** System genuinely has issues, or prompts need tuning.

**Solution:**
```bash
# 1. Review recent analyses
sqlite3 /opt/logwatch-ai/data/summaries.db \
  "SELECT timestamp, system_status, summary FROM summaries ORDER BY timestamp DESC LIMIT 10;"

# 2. Check actual log content
cat /tmp/logwatch-output.txt | less

# 3. Review critical issues
sqlite3 /opt/logwatch-ai/data/summaries.db \
  "SELECT critical_issues FROM summaries WHERE system_status='Critical' LIMIT 1;"

# 4. If false positives, adjust system (may require prompt tuning in code)
# The prompts are in: internal/ai/prompt.go
```

---

## Network/Proxy Issues

### "Connection timeout" to Claude API

**Symptom:**
```
Error: failed to analyze with Claude: context deadline exceeded
```

**Cause:** Network issues, firewall, or slow connection.

**Solution:**
```bash
# 1. Test connectivity
curl -I https://api.anthropic.com

# 2. Check firewall rules
sudo iptables -L -n | grep -i drop
sudo ufw status

# 3. If behind corporate firewall, configure proxy
echo "HTTPS_PROXY=http://proxy.example.com:8080" >> /opt/logwatch-ai/.env

# 4. Test proxy
curl -x http://proxy.example.com:8080 https://api.anthropic.com

# 5. Increase timeout if network is slow
# (Currently hardcoded to 120s, requires code change if needed)
```

### Proxy Authentication Required

**Symptom:**
```
Error: Proxy Authentication Required (407)
```

**Cause:** Proxy requires authentication.

**Solution:**
```bash
# Add credentials to proxy URL
echo "HTTPS_PROXY=http://username:password@proxy.example.com:8080" >> /opt/logwatch-ai/.env

# Or use environment variables
export HTTPS_PROXY="http://username:password@proxy.example.com:8080"

# For special characters in password, URL encode them:
# @ = %40
# : = %3A
# Example: p@ss:word = p%40ss%3Aword
```

---

## Getting Help

### Enable Debug Logging

```bash
# Set log level to debug
echo "LOG_LEVEL=debug" >> /opt/logwatch-ai/.env

# Run analyzer
./logwatch-analyzer

# Check detailed logs
tail -100 /opt/logwatch-ai/logs/analyzer.log
```

### Collect Diagnostic Information

```bash
# System info
uname -a
go version

# Binary info
ls -lh /opt/logwatch-ai/logwatch-analyzer
file /opt/logwatch-ai/logwatch-analyzer

# Configuration (REMOVE SENSITIVE DATA!)
cat /opt/logwatch-ai/.env | grep -v "API_KEY\|BOT_TOKEN"

# Recent logs
tail -50 /opt/logwatch-ai/logs/analyzer.log

# Database stats
sqlite3 /opt/logwatch-ai/data/summaries.db \
  "SELECT COUNT(*) as total, system_status, COUNT(*) as count FROM summaries GROUP BY system_status;"

# Disk space
df -h /opt/logwatch-ai
```

### Report an Issue

When reporting issues, include:

1. **Version**: `git describe --tags` or release number
2. **Platform**: OS, architecture (Linux Debian 12, macOS ARM64, etc.)
3. **Error message**: Full error output
4. **Configuration**: .env contents (remove secrets!)
5. **Logs**: Recent entries from analyzer.log
6. **Steps to reproduce**: What commands you ran

Submit issues at: https://github.com/olegiv/logwatch-ai-go/issues

### Community Support

- **GitHub Discussions**: https://github.com/olegiv/logwatch-ai-go/discussions
- **Original Project**: https://github.com/olegiv/logwatch-ai

---

## Common Quick Fixes

### Reset Everything

```bash
# Stop all processes
pkill logwatch-analyzer

# Backup database (optional)
cp /opt/logwatch-ai/data/summaries.db ~/summaries.db.backup

# Remove database
rm -f /opt/logwatch-ai/data/summaries.db*

# Reset configuration
cp /opt/logwatch-ai/configs/.env.example /opt/logwatch-ai/.env
nano /opt/logwatch-ai/.env  # Add your credentials

# Test fresh run
cd /opt/logwatch-ai
./logwatch-analyzer
```

### Verify Installation

```bash
# Check all components
echo "Binary: $(ls -lh /opt/logwatch-ai/logwatch-analyzer 2>&1)"
echo "Config: $(ls -lh /opt/logwatch-ai/.env 2>&1)"
echo "Data dir: $(ls -ld /opt/logwatch-ai/data 2>&1)"
echo "Logs dir: $(ls -ld /opt/logwatch-ai/logs 2>&1)"
echo "Scripts: $(ls -lh /opt/logwatch-ai/scripts/*.sh 2>&1)"

# Test connectivity
echo "Anthropic API: $(curl -sI https://api.anthropic.com | head -1)"
echo "Telegram API: $(curl -sI https://api.telegram.org | head -1)"
```

### Performance Issues

```bash
# Check system resources
free -h          # Memory
df -h            # Disk space
top -bn1 | head  # CPU usage

# Check log file sizes
du -sh /opt/logwatch-ai/logs/
du -sh /var/log/

# Clean old logs if needed
find /opt/logwatch-ai/logs/ -name "*.log" -mtime +30 -delete

# Database size
du -h /opt/logwatch-ai/data/summaries.db

# Clean old database entries (older than 90 days is automatic)
sqlite3 /opt/logwatch-ai/data/summaries.db \
  "DELETE FROM summaries WHERE datetime(timestamp) < datetime('now', '-90 days');"
```
