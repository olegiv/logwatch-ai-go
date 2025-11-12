# Cron Setup Guide

This guide explains how to set up automated daily logwatch analysis using cron.

## Overview

The logwatch-ai-go system requires two cron jobs:

1. **Root cron** (2:00 AM): Generate logwatch report
2. **User cron** (2:15 AM): Analyze the report with AI

The 15-minute delay ensures logwatch has finished generating the report before analysis begins.

## Prerequisites

- Logwatch installed on the system
- logwatch-ai-go installed (preferably to `/opt/logwatch-ai`)
- `.env` file configured with API keys

## Step 1: Root Cron (Generate Logwatch)

Logwatch typically requires root privileges to access all log files.

### Edit Root Crontab

```bash
sudo crontab -e
```

### Add Cron Entry

```bash
# Generate logwatch report daily at 2:00 AM
0 2 * * * /opt/logwatch-ai/scripts/generate-logwatch.sh
```

### Alternative: Specify Custom Output Path

```bash
# Generate to custom location
0 2 * * * LOGWATCH_OUTPUT_PATH=/var/log/logwatch-daily.txt /opt/logwatch-ai/scripts/generate-logwatch.sh
```

### Verify Root Cron

```bash
sudo crontab -l
```

## Step 2: User Cron (Run Analyzer)

The analyzer should run as a regular user (not root) for security.

### Edit User Crontab

```bash
crontab -e
```

### Add Cron Entry

```bash
# Run logwatch AI analysis daily at 2:15 AM
15 2 * * * cd /opt/logwatch-ai && ./logwatch-analyzer >> logs/cron.log 2>&1
```

### Alternative: With Environment File

```bash
# Explicitly load environment
15 2 * * * cd /opt/logwatch-ai && source .env && ./logwatch-analyzer >> logs/cron.log 2>&1
```

### Verify User Cron

```bash
crontab -l
```

## Step 3: Test the Setup

### Manual Test of Logwatch Generation

```bash
sudo /opt/logwatch-ai/scripts/generate-logwatch.sh
```

Check output:
```bash
ls -lh /tmp/logwatch-output.txt
```

### Manual Test of Analyzer

```bash
cd /opt/logwatch-ai
./logwatch-analyzer
```

### Monitor Logs

```bash
# Watch analyzer logs
tail -f /opt/logwatch-ai/logs/logwatch-analyzer.log

# Watch cron logs
tail -f /opt/logwatch-ai/logs/cron.log
```

## Cron Schedule Examples

### Different Frequencies

**Twice Daily** (morning and evening):
```bash
# Root cron
0 2,14 * * * /opt/logwatch-ai/scripts/generate-logwatch.sh

# User cron
15 2,14 * * * cd /opt/logwatch-ai && ./logwatch-analyzer >> logs/cron.log 2>&1
```

**Weekly** (Sunday at 3:00 AM):
```bash
# Root cron
0 3 * * 0 /opt/logwatch-ai/scripts/generate-logwatch.sh

# User cron
15 3 * * 0 cd /opt/logwatch-ai && ./logwatch-analyzer >> logs/cron.log 2>&1
```

**Hourly** (on the hour):
```bash
# Root cron
0 * * * * LOGWATCH_RANGE='--range "1 hour"' /opt/logwatch-ai/scripts/generate-logwatch.sh

# User cron
5 * * * * cd /opt/logwatch-ai && ./logwatch-analyzer >> logs/cron.log 2>&1
```

## Environment Variables for Cron

### Setting in Crontab

```bash
# Set environment variables at the top of crontab
SHELL=/bin/bash
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
LOGWATCH_OUTPUT_PATH=/tmp/logwatch-output.txt

# Then add cron jobs
0 2 * * * /opt/logwatch-ai/scripts/generate-logwatch.sh
```

### Using .env File

If cron doesn't load your .env automatically:

```bash
15 2 * * * cd /opt/logwatch-ai && export $(cat .env | xargs) && ./logwatch-analyzer >> logs/cron.log 2>&1
```

## Troubleshooting

### Cron Not Running

1. **Check cron service is running**:
   ```bash
   sudo systemctl status cron     # Debian/Ubuntu
   sudo systemctl status crond    # RHEL/CentOS
   ```

2. **Check system logs**:
   ```bash
   sudo grep CRON /var/log/syslog
   ```

3. **Verify cron has permission to execute scripts**:
   ```bash
   ls -l /opt/logwatch-ai/scripts/
   # Should show -rwxr-xr-x
   ```

### Cron Runs But Fails

1. **Check cron output logs**:
   ```bash
   cat /opt/logwatch-ai/logs/cron.log
   ```

2. **Test scripts manually**:
   ```bash
   # As root
   sudo /opt/logwatch-ai/scripts/generate-logwatch.sh

   # As user
   cd /opt/logwatch-ai && ./logwatch-analyzer
   ```

3. **Check file permissions**:
   ```bash
   ls -l /tmp/logwatch-output.txt
   # Should be readable by the user running analyzer
   ```

### No Notifications Received

1. **Check analyzer logs**:
   ```bash
   tail -50 /opt/logwatch-ai/logs/logwatch-analyzer.log
   ```

2. **Verify Telegram configuration**:
   ```bash
   grep TELEGRAM /opt/logwatch-ai/.env
   ```

3. **Test Telegram bot**:
   ```bash
   curl https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getMe
   ```

## Email Notifications (Optional)

Configure cron to send email on failures:

### Install mail utilities

```bash
# Debian/Ubuntu
sudo apt-get install mailutils

# RHEL/CentOS
sudo yum install mailx
```

### Set MAILTO in crontab

```bash
MAILTO=admin@example.com

0 2 * * * /opt/logwatch-ai/scripts/generate-logwatch.sh
15 2 * * * cd /opt/logwatch-ai && ./logwatch-analyzer
```

Cron will email output only on errors (non-zero exit code).

## Advanced Configuration

### Redirect Output to Separate Files

```bash
# Separate stdout and stderr
15 2 * * * cd /opt/logwatch-ai && ./logwatch-analyzer >> logs/cron-out.log 2>> logs/cron-err.log
```

### Add Timestamp to Logs

```bash
15 2 * * * cd /opt/logwatch-ai && echo "=== Run at $(date) ===" >> logs/cron.log && ./logwatch-analyzer >> logs/cron.log 2>&1
```

### Lock File to Prevent Concurrent Runs

```bash
15 2 * * * cd /opt/logwatch-ai && flock -n /tmp/logwatch-ai.lock ./logwatch-analyzer >> logs/cron.log 2>&1
```

## Security Considerations

1. **Don't run analyzer as root** - Use a dedicated service user
2. **Protect .env file** - Ensure proper file permissions:
   ```bash
   chmod 600 /opt/logwatch-ai/.env
   ```
3. **Rotate logs** - The application handles log rotation automatically
4. **Limit cron email** - Only send on errors, not every run
5. **Use absolute paths** - Always use full paths in cron commands

## Monitoring

### Check Last Run Time

```bash
# Check last modified time of output file
stat /tmp/logwatch-output.txt

# Check last database entry
sqlite3 /opt/logwatch-ai/data/summaries.db "SELECT timestamp FROM summaries ORDER BY timestamp DESC LIMIT 1;"
```

### Monitor Cron Success Rate

```bash
# Count successful runs in the last 7 days
grep "Analysis completed successfully" /opt/logwatch-ai/logs/logwatch-analyzer.log | tail -7
```

### Alert on Failures

Add a wrapper script to send alerts on failures:

```bash
#!/bin/bash
cd /opt/logwatch-ai
if ! ./logwatch-analyzer >> logs/cron.log 2>&1; then
    echo "Logwatch AI analysis failed at $(date)" | mail -s "Logwatch AI Alert" admin@example.com
fi
```

## See Also

- [README.md](../README.md) - Main documentation
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) - Troubleshooting guide
- Logwatch documentation: `/usr/share/doc/logwatch/`
