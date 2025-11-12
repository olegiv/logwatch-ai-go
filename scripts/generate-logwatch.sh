#!/bin/bash
# Script to generate logwatch report
# This should be run by root via cron (e.g., at 2:00 AM)
# Example crontab entry:
# 0 2 * * * /opt/logwatch-ai/scripts/generate-logwatch.sh

set -e

# Configuration
OUTPUT_FILE="${LOGWATCH_OUTPUT_PATH:-/tmp/logwatch-output.txt}"
LOGWATCH_RANGE="${LOGWATCH_RANGE:-yesterday}"
LOGWATCH_DETAIL="${LOGWATCH_DETAIL:-0}"

# Log function
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

# Check if logwatch is installed
if ! command -v logwatch &> /dev/null; then
    log "ERROR: logwatch is not installed"
    log "Install with: apt-get install logwatch (Debian/Ubuntu) or yum install logwatch (RHEL/CentOS)"
    exit 1
fi

# Generate logwatch report
log "Generating logwatch report..."
log "Output: $OUTPUT_FILE"
log "Range: $LOGWATCH_RANGE"
log "Detail level: $LOGWATCH_DETAIL"

logwatch \
    --output file \
    --filename "$OUTPUT_FILE" \
    --format text \
    --detail "$LOGWATCH_DETAIL" \
    --range "$LOGWATCH_RANGE"

if [ $? -eq 0 ]; then
    log "Logwatch report generated successfully"
    log "File size: $(du -h "$OUTPUT_FILE" | cut -f1)"

    # Set permissions so the analyzer can read it
    chmod 644 "$OUTPUT_FILE"

    log "Done!"
else
    log "ERROR: Failed to generate logwatch report"
    exit 1
fi
