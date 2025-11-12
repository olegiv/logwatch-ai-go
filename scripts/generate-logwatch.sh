#!/bin/bash
#
# Logwatch Generation Script for Cron
#
# This script generates logwatch reports and is designed to run via cron as root.
# It does NOT use sudo - it should be run directly as root by cron.
#
# Usage:
#   ./scripts/generate-logwatch.sh [output_path] [detail_level] [range]
#
# Arguments:
#   output_path  - Path where logwatch output will be saved (default: /tmp/logwatch-output.txt)
#   detail_level - Detail level: 0-10 (default: 0, where 0=minimal, 10=maximum detail)
#   range        - Time range: yesterday, today, all (default: yesterday)
#
# Example crontab entry (runs daily at 2:00 AM as root):
#   0 2 * * * /path/to/logwatch-ai/scripts/generate-logwatch.sh
#
# To install:
#   sudo crontab -e
#   Add the line above with correct path
#

set -e  # Exit on error

# Default configuration
OUTPUT_PATH="${1:-/tmp/logwatch-output.txt}"
DETAIL_LEVEL="${2:-0}"
RANGE="${3:-yesterday}"
SCRIPT_NAME="$(basename "$0")"

# Validate detail level (must be 0-10)
if ! [[ "$DETAIL_LEVEL" =~ ^[0-9]+$ ]] || [ "$DETAIL_LEVEL" -lt 0 ] || [ "$DETAIL_LEVEL" -gt 10 ]; then
    echo "ERROR: Detail level must be a number between 0 and 10 (got: $DETAIL_LEVEL)"
    exit 1
fi

# Auto-detect logwatch location
if [ -f "/opt/local/bin/logwatch" ]; then
    LOGWATCH_BIN="/opt/local/bin/logwatch"  # macOS (MacPorts)
elif [ -f "/usr/sbin/logwatch" ]; then
    LOGWATCH_BIN="/usr/sbin/logwatch"  # Linux
else
    LOGWATCH_BIN=$(command -v logwatch 2>/dev/null || echo "")
fi

# Logging function (logs to syslog for cron)
log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] $SCRIPT_NAME: $*"
    logger -t "$SCRIPT_NAME" "$*" 2>/dev/null || true
}

log "Starting logwatch generation"
log "Configuration: output=$OUTPUT_PATH, detail=$DETAIL_LEVEL, range=$RANGE"

# Check if logwatch is installed
if [ -z "$LOGWATCH_BIN" ] || [ ! -f "$LOGWATCH_BIN" ]; then
    log "ERROR: logwatch is not installed or not found"
    if [[ "$OSTYPE" == "darwin"* ]]; then
        log "Install with: port install logwatch (macOS)"
    else
        log "Install with: apt-get install logwatch (Debian/Ubuntu) or yum install logwatch (RHEL/CentOS)"
    fi
    exit 1
fi

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    log "WARNING: Not running as root. Logwatch may not have access to all log files."
    log "For cron, add to root crontab: sudo crontab -e"
fi

# Create output directory if it doesn't exist
OUTPUT_DIR=$(dirname "$OUTPUT_PATH")
if [ ! -d "$OUTPUT_DIR" ]; then
    log "Creating output directory: $OUTPUT_DIR"
    mkdir -p "$OUTPUT_DIR"
fi

# Generate logwatch report
log "Generating logwatch report with $LOGWATCH_BIN..."
if "$LOGWATCH_BIN" \
    --output file \
    --filename "$OUTPUT_PATH" \
    --format text \
    --detail "$DETAIL_LEVEL" \
    --range "$RANGE" 2>&1 | while read -r line; do
        log "logwatch: $line"
    done; then

    log "Logwatch report generated successfully"

    # Set permissions so the analyzer can read it (world-readable)
    if chmod 644 "$OUTPUT_PATH" 2>/dev/null; then
        log "File permissions set to 644"
    else
        log "WARNING: Failed to set file permissions on $OUTPUT_PATH"
    fi

    # Log file size for monitoring
    if [[ "$OSTYPE" == "darwin"* ]]; then
        FILE_SIZE=$(stat -f%z "$OUTPUT_PATH" 2>/dev/null || echo "unknown")
    else
        FILE_SIZE=$(stat -c%s "$OUTPUT_PATH" 2>/dev/null || echo "unknown")
    fi

    log "Report size: $FILE_SIZE bytes"
    log "Report location: $OUTPUT_PATH"
    log "Logwatch generation completed successfully"
    exit 0

else
    EXIT_CODE=$?
    log "ERROR: Logwatch generation failed with exit code $EXIT_CODE"
    exit 1
fi
