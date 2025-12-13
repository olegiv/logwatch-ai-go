#!/bin/bash
#
# Drupal Watchdog Export Script
#
# This script exports Drupal watchdog logs using drush for analysis
# with the logwatch-ai-go analyzer.
#
# Configuration is loaded from .env file (same as logwatch-ai-go analyzer).
# Command line arguments override .env values.
#
# Usage:
#   ./scripts/generate-drupal-watchdog.sh [options]
#
# Options:
#   -e, --env-file      Path to .env file (default: auto-detect)
#   -d, --drupal-root   Path to Drupal project root (env: DRUPAL_ROOT)
#   -o, --output        Output file path (env: DRUPAL_WATCHDOG_PATH)
#   -f, --format        Output format: json or table (env: DRUPAL_WATCHDOG_FORMAT)
#   -c, --count         Max entries to fetch from drush (default: 10000)
#   -l, --limit         Max entries in output file (env: DRUPAL_WATCHDOG_LIMIT, default: 100)
#   -s, --severity      Filter by severity: emergency,alert,critical,error,warning,notice,info,debug
#   -t, --type          Filter by log type (e.g., php, cron, system)
#   --since             Export entries from the last N hours (default: 24)
#   -S, --site          Drupal site ID from drupal-sites.json (for multi-site deployments)
#   --sites-config      Path to drupal-sites.json configuration file
#   --list-sites        List available Drupal sites from drupal-sites.json and exit
#   -h, --help          Show this help message
#   -v, --version       Show version information
#
# Environment Variables (from .env):
#   DRUPAL_ROOT              - Path to Drupal project root
#   DRUPAL_WATCHDOG_PATH     - Output file path for watchdog export
#   DRUPAL_WATCHDOG_FORMAT   - Output format (json or drush)
#   DRUPAL_WATCHDOG_LIMIT    - Max entries in output file (default: 100)
#   DRUPAL_MIN_SEVERITY      - Minimum severity (0-7, default: 3=error)
#                              0=emergency, 1=alert, 2=critical, 3=error,
#                              4=warning, 5=notice, 6=info, 7=debug
#
# Examples:
#   # Export using .env configuration
#   ./scripts/generate-drupal-watchdog.sh
#
#   # Export last 500 error and warning entries
#   ./scripts/generate-drupal-watchdog.sh -c 500 -s error,warning
#
#   # Override .env with custom paths
#   ./scripts/generate-drupal-watchdog.sh -d /var/www/mysite/drupal -o /tmp/mysite-watchdog.json
#
#   # Export PHP errors only
#   ./scripts/generate-drupal-watchdog.sh -t php -c 200
#
#   # Multi-site: List available sites
#   ./scripts/generate-drupal-watchdog.sh --list-sites
#
#   # Multi-site: Export from specific site
#   ./scripts/generate-drupal-watchdog.sh --site production
#
# Crontab example (export daily at 2:00 AM before analyzer runs):
#   0 2 * * * /opt/logwatch-ai/scripts/generate-drupal-watchdog.sh
#

set -e  # Exit on error

SCRIPT_NAME="$(basename "$0")"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Version from git (matches logwatch-analyzer versioning)
get_version() {
    local version
    version=$(git -C "$SCRIPT_DIR" describe --tags --always --dirty 2>/dev/null || echo "dev")
    echo "$version"
}

# Find .env file (check multiple locations)
find_env_file() {
    local locations=(
        "$ENV_FILE"                           # Explicit path from -e flag
        "$SCRIPT_DIR/../.env"                 # Project root (relative to script)
        "$SCRIPT_DIR/.env"                    # Scripts directory
        "/opt/logwatch-ai/.env"               # Production install location
        "./.env"                              # Current directory
    )

    for loc in "${locations[@]}"; do
        if [ -n "$loc" ] && [ -f "$loc" ]; then
            echo "$loc"
            return 0
        fi
    done
    return 1
}

# Load .env file
load_env() {
    local env_file="$1"
    if [ -f "$env_file" ]; then
        # Export variables from .env (ignore comments and empty lines)
        while IFS='=' read -r key value; do
            # Skip comments and empty lines
            [[ "$key" =~ ^[[:space:]]*# ]] && continue
            [[ -z "$key" ]] && continue
            # Remove leading/trailing whitespace from key
            key=$(echo "$key" | xargs)
            # Only process valid variable names
            if [[ "$key" =~ ^[A-Z_][A-Z0-9_]*$ ]]; then
                # Remove surrounding quotes from value if present
                value="${value%\"}"
                value="${value#\"}"
                value="${value%\'}"
                value="${value#\'}"
                export "$key=$value"
            fi
        done < "$env_file"
        return 0
    fi
    return 1
}

# Default configuration (will be overridden by .env and CLI args)
ENV_FILE=""
DRUPAL_ROOT=""
OUTPUT_PATH=""
FORMAT=""
COUNT="10000"
LIMIT=""
SEVERITY=""
LOG_TYPE=""
SINCE_HOURS="24"

# Multi-site configuration
DRUPAL_SITE=""
SITES_CONFIG_FILE=""
LIST_SITES=false

# Find drupal-sites.json configuration file
find_sites_config() {
    local explicit_path="$1"
    local locations=(
        "$explicit_path"
        "$SCRIPT_DIR/../drupal-sites.json"
        "$SCRIPT_DIR/../configs/drupal-sites.json"
        "/opt/logwatch-ai/drupal-sites.json"
    )

    for loc in "${locations[@]}"; do
        if [ -n "$loc" ] && [ -f "$loc" ]; then
            echo "$loc"
            return 0
        fi
    done
    return 1
}

# Get site configuration field using jq
get_site_config() {
    local config_file="$1"
    local site_id="$2"
    local field="$3"

    jq -r ".sites[\"$site_id\"].$field // empty" "$config_file" 2>/dev/null
}

# List all available sites from drupal-sites.json
list_drupal_sites() {
    local config_file="$1"
    local default_site

    if ! command -v jq &> /dev/null; then
        log_error "jq is required for multi-site configuration"
        log_error "Install jq: apt-get install jq (Debian/Ubuntu) or brew install jq (macOS)"
        exit 1
    fi

    default_site=$(jq -r '.default_site // empty' "$config_file" 2>/dev/null)
    version=$(jq -r '.version // "unknown"' "$config_file" 2>/dev/null)

    echo "Drupal sites configuration: $config_file"
    echo "Version: $version"
    echo ""
    echo "Available sites:"

    # List all sites with their details
    jq -r '.sites | to_entries[] | "\(.key)|\(.value.name // .key)|\(.value.drupal_root)|\(.value.watchdog_path)"' "$config_file" 2>/dev/null | while IFS='|' read -r site_id name drupal_root watchdog_path; do
        default_marker=""
        if [ "$site_id" = "$default_site" ]; then
            default_marker=" (default)"
        fi
        printf "  %-20s %s%s\n" "$site_id" "$name" "$default_marker"
        printf "    Drupal root:    %s\n" "$drupal_root"
        printf "    Watchdog path:  %s\n" "$watchdog_path"
        echo ""
    done

    exit 0
}

# Apply site-specific configuration from drupal-sites.json
apply_site_config() {
    local config_file="$1"
    local site_id="$2"

    if ! command -v jq &> /dev/null; then
        log_error "jq is required for multi-site configuration"
        log_error "Install jq: apt-get install jq (Debian/Ubuntu) or brew install jq (macOS)"
        exit 1
    fi

    # Validate site exists
    if ! jq -e ".sites[\"$site_id\"]" "$config_file" > /dev/null 2>&1; then
        log_error "Site '$site_id' not found in $config_file"
        log_error "Use --list-sites to see available sites"
        exit 1
    fi

    log "Using site configuration: $site_id from $config_file"

    # Get site-specific configuration
    local site_drupal_root site_watchdog_path site_watchdog_format site_min_severity site_watchdog_limit

    site_drupal_root=$(get_site_config "$config_file" "$site_id" "drupal_root")
    site_watchdog_path=$(get_site_config "$config_file" "$site_id" "watchdog_path")
    site_watchdog_format=$(get_site_config "$config_file" "$site_id" "watchdog_format")
    site_min_severity=$(get_site_config "$config_file" "$site_id" "min_severity")
    site_watchdog_limit=$(get_site_config "$config_file" "$site_id" "watchdog_limit")

    # Apply site config (CLI > site config > .env > defaults)
    [ -z "$CLI_DRUPAL_ROOT" ] && [ -n "$site_drupal_root" ] && DRUPAL_ROOT="$site_drupal_root"
    [ -z "$CLI_OUTPUT_PATH" ] && [ -n "$site_watchdog_path" ] && OUTPUT_PATH="$site_watchdog_path"
    [ -z "$CLI_FORMAT" ] && [ -n "$site_watchdog_format" ] && FORMAT="$site_watchdog_format"
    [ -z "$CLI_LIMIT" ] && [ -n "$site_watchdog_limit" ] && LIMIT="$site_watchdog_limit"
    [ -n "$site_min_severity" ] && MIN_SEVERITY="$site_min_severity"
}

# Color output (disabled if not terminal)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    NC=''
fi

# Logging function
log() {
    echo -e "[$(date +'%Y-%m-%d %H:%M:%S')] $SCRIPT_NAME: $*"
    logger -t "$SCRIPT_NAME" "$*" 2>/dev/null || true
}

log_error() {
    echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] $SCRIPT_NAME: ERROR: $*${NC}" >&2
    logger -t "$SCRIPT_NAME" "ERROR: $*" 2>/dev/null || true
}

log_success() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')] $SCRIPT_NAME: $*${NC}"
}

log_warning() {
    echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] $SCRIPT_NAME: WARNING: $*${NC}"
}

# Help function
show_help() {
    head -50 "$0" | grep -E "^#" | sed 's/^# \?//'
    exit 0
}

# Version function
show_version() {
    echo "$SCRIPT_NAME $(get_version)"
    exit 0
}

# Parse command line arguments (first pass for --env-file)
CLI_DRUPAL_ROOT=""
CLI_OUTPUT_PATH=""
CLI_FORMAT=""
CLI_LIMIT=""

while [[ $# -gt 0 ]]; do
    case $1 in
        -e|--env-file)
            ENV_FILE="$2"
            shift 2
            ;;
        -d|--drupal-root)
            CLI_DRUPAL_ROOT="$2"
            shift 2
            ;;
        -o|--output)
            CLI_OUTPUT_PATH="$2"
            shift 2
            ;;
        -f|--format)
            CLI_FORMAT="$2"
            shift 2
            ;;
        -c|--count)
            COUNT="$2"
            shift 2
            ;;
        -l|--limit)
            CLI_LIMIT="$2"
            shift 2
            ;;
        -s|--severity)
            SEVERITY="$2"
            shift 2
            ;;
        -t|--type)
            LOG_TYPE="$2"
            shift 2
            ;;
        --since)
            SINCE_HOURS="$2"
            shift 2
            ;;
        -S|--site)
            DRUPAL_SITE="$2"
            shift 2
            ;;
        --sites-config)
            SITES_CONFIG_FILE="$2"
            shift 2
            ;;
        --list-sites)
            LIST_SITES=true
            shift
            ;;
        -h|--help|-help)
            show_help
            ;;
        -v|--version|-version)
            show_version
            ;;
        *)
            log_error "Unknown option: $1"
            echo "Use -h or --help for usage information"
            exit 1
            ;;
    esac
done

# Load .env file
FOUND_ENV_FILE=$(find_env_file) || true
if [ -n "$FOUND_ENV_FILE" ]; then
    log "Loading configuration from: $FOUND_ENV_FILE"
    load_env "$FOUND_ENV_FILE"
else
    log_warning "No .env file found. Using defaults and command line arguments."
fi

# Handle --list-sites flag
if [ "$LIST_SITES" = true ]; then
    FOUND_SITES_CONFIG=$(find_sites_config "$SITES_CONFIG_FILE") || {
        log_error "No drupal-sites.json configuration file found."
        echo ""
        echo "Search locations:"
        echo "  - ./drupal-sites.json"
        echo "  - ./configs/drupal-sites.json"
        echo "  - /opt/logwatch-ai/drupal-sites.json"
        echo ""
        echo "Use --sites-config to specify a custom path."
        exit 1
    }
    list_drupal_sites "$FOUND_SITES_CONFIG"
fi

# Apply multi-site configuration if --site specified
if [ -n "$DRUPAL_SITE" ]; then
    FOUND_SITES_CONFIG=$(find_sites_config "$SITES_CONFIG_FILE") || {
        log_error "No drupal-sites.json found. Required when using --site"
        log_error "Use --list-sites to check configuration, or --sites-config to specify path"
        exit 1
    }
    apply_site_config "$FOUND_SITES_CONFIG" "$DRUPAL_SITE"
fi

# Apply configuration priority: CLI args > site config > .env > defaults
DRUPAL_ROOT="${CLI_DRUPAL_ROOT:-${DRUPAL_ROOT:-/var/www/html}}"
OUTPUT_PATH="${CLI_OUTPUT_PATH:-${DRUPAL_WATCHDOG_PATH:-/tmp/drupal-watchdog.json}}"
FORMAT="${CLI_FORMAT:-${DRUPAL_WATCHDOG_FORMAT:-json}}"
LIMIT="${CLI_LIMIT:-${DRUPAL_WATCHDOG_LIMIT:-100}}"

# Use DRUPAL_MIN_SEVERITY for jq filtering (drush doesn't support multiple severities)
# Default: 3 (error) = include emergency(0), alert(1), critical(2), error(3)
MIN_SEVERITY="${DRUPAL_MIN_SEVERITY:-3}"

# Validate format
if [[ "$FORMAT" != "json" && "$FORMAT" != "table" ]]; then
    log_error "Invalid format '$FORMAT'. Must be 'json' or 'table'"
    exit 1
fi

# Validate count is a number
if ! [[ "$COUNT" =~ ^[0-9]+$ ]]; then
    log_error "Count must be a positive number (got: $COUNT)"
    exit 1
fi

# Validate limit is a number
if ! [[ "$LIMIT" =~ ^[0-9]+$ ]]; then
    log_error "Limit must be a positive number (got: $LIMIT)"
    exit 1
fi

# Note: --since validation removed; drush watchdog:show doesn't provide
# timestamp fields suitable for filtering by time window. The --since
# option is accepted but currently has no effect on output.

log "Starting Drupal watchdog export"
log "Configuration:"
[ -n "$DRUPAL_SITE" ] && log "  Site: $DRUPAL_SITE"
log "  Drupal root: $DRUPAL_ROOT"
log "  Output: $OUTPUT_PATH"
log "  Format: $FORMAT"
log "  Count: $COUNT (max entries to fetch)"
log "  Limit: $LIMIT (max entries in output)"
log "  Min severity: $MIN_SEVERITY (0=emergency to 7=debug)"
[ -n "$LOG_TYPE" ] && log "  Type filter: $LOG_TYPE"
[ -n "$FOUND_ENV_FILE" ] && log "  Config source: $FOUND_ENV_FILE"
[ -n "$FOUND_SITES_CONFIG" ] && log "  Sites config: $FOUND_SITES_CONFIG"

# Check if Drupal root exists
if [ ! -d "$DRUPAL_ROOT" ]; then
    log_error "Drupal root directory does not exist: $DRUPAL_ROOT"
    exit 1
fi

# Locate drush
DRUSH_BIN=""
if [ -f "$DRUPAL_ROOT/vendor/bin/drush" ]; then
    DRUSH_BIN="$DRUPAL_ROOT/vendor/bin/drush"
elif command -v drush &> /dev/null; then
    DRUSH_BIN=$(command -v drush)
else
    log_error "drush not found in $DRUPAL_ROOT/vendor/bin/ or in PATH"
    log_error "Install drush with: composer require drush/drush"
    exit 1
fi

log "Using drush: $DRUSH_BIN"

# Create output directory if it doesn't exist
OUTPUT_DIR=$(dirname "$OUTPUT_PATH")
if [ ! -d "$OUTPUT_DIR" ]; then
    log "Creating output directory: $OUTPUT_DIR"
    mkdir -p "$OUTPUT_DIR"
fi

# Build drush command (severity filtering done in jq, not drush)
DRUSH_CMD=("$DRUSH_BIN" "-r" "$DRUPAL_ROOT" "watchdog:show" "--count=$COUNT" "--format=$FORMAT")

# Add type filter if specified
if [ -n "$LOG_TYPE" ]; then
    DRUSH_CMD+=("--type=$LOG_TYPE")
fi

log "Executing: ${DRUSH_CMD[*]}"

# Execute drush and capture output
if OUTPUT=$("${DRUSH_CMD[@]}" 2>&1); then
    # Debug: show raw output size
    OUTPUT_SIZE=${#OUTPUT}
    log "Drush returned $OUTPUT_SIZE bytes"

    if [ "$OUTPUT_SIZE" -lt 10 ]; then
        log_warning "Drush output is very small or empty: '$OUTPUT'"
    fi
    # For JSON format, filter by timestamp and apply limit using jq
    if [ "$FORMAT" = "json" ]; then
        if command -v jq &> /dev/null; then
            # Check JSON validity first
            if ! echo "$OUTPUT" | jq empty 2>/dev/null; then
                log_error "Drush output is not valid JSON"
                log_error "First 500 chars: ${OUTPUT:0:500}"
                exit 1
            fi

            # Drush outputs object {wid: entry, ...} - convert to array and sort by wid (higher = newer)
            # Also convert severity string to number, filter by severity, and add timestamp
            CONVERTED=$(echo "$OUTPUT" | jq --argjson minSev "$MIN_SEVERITY" '
                [to_entries | .[].value] |
                map({
                    wid: (.wid | tonumber),
                    uid: ((.uid // "0") | tonumber),
                    type: .type,
                    message: .message,
                    severity: (
                        if .severity == "Emergency" then 0
                        elif .severity == "Alert" then 1
                        elif .severity == "Critical" then 2
                        elif .severity == "Error" then 3
                        elif .severity == "Warning" then 4
                        elif .severity == "Notice" then 5
                        elif .severity == "Info" then 6
                        elif .severity == "Debug" then 7
                        else 5 end
                    ),
                    location: .location,
                    hostname: .hostname,
                    timestamp: (now | floor)
                }) |
                [.[] | select(.severity <= $minSev)] |
                sort_by(-.wid)
            ' 2>/dev/null)

            if [ $? -ne 0 ] || [ -z "$CONVERTED" ]; then
                log_error "Failed to convert drush output format"
                exit 1
            fi

            ORIGINAL_COUNT=$(echo "$CONVERTED" | jq 'length' 2>/dev/null || echo "0")
            log "Converted $ORIGINAL_COUNT entries from drush format"

            # Apply limit (already sorted by wid descending = newest first)
            FILTERED=$(echo "$CONVERTED" | jq --argjson limit "$LIMIT" '.[:$limit]' 2>/dev/null)

            if [ -n "$FILTERED" ] && [ "$FILTERED" != "[]" ]; then
                echo "$FILTERED" > "$OUTPUT_PATH"
                ENTRY_COUNT=$(echo "$FILTERED" | jq 'length' 2>/dev/null || echo "unknown")
                log "Exported $ENTRY_COUNT entries (limited from $ORIGINAL_COUNT total)"
            else
                log_warning "No watchdog entries found"
                echo "[]" > "$OUTPUT_PATH"
            fi
        else
            log_warning "jq not installed. Cannot filter by timestamp. Using drush --count=$LIMIT instead."
            # Re-run drush with limited count
            DRUSH_CMD_LIMITED=("$DRUSH_BIN" "-r" "$DRUPAL_ROOT" "watchdog:show" "--count=$LIMIT" "--format=$FORMAT")
            [ -n "$SEVERITY" ] && DRUSH_CMD_LIMITED+=("--severity=$SEVERITY")
            [ -n "$LOG_TYPE" ] && DRUSH_CMD_LIMITED+=("--type=$LOG_TYPE")
            log "Re-executing: ${DRUSH_CMD_LIMITED[*]}"
            "${DRUSH_CMD_LIMITED[@]}" > "$OUTPUT_PATH" 2>&1
        fi
    else
        # For non-JSON formats, just write the output
        echo "$OUTPUT" > "$OUTPUT_PATH"
        log_warning "Time filtering and limit only supported for JSON format"
    fi

    # Set file permissions (readable by owner and group)
    chmod 640 "$OUTPUT_PATH" 2>/dev/null || log_warning "Failed to set file permissions"

    # Log file size
    if [[ "$OSTYPE" == "darwin"* ]]; then
        FILE_SIZE=$(stat -f%z "$OUTPUT_PATH" 2>/dev/null || echo "unknown")
    else
        FILE_SIZE=$(stat -c%s "$OUTPUT_PATH" 2>/dev/null || echo "unknown")
    fi

    log_success "Export completed successfully"
    log "  File: $OUTPUT_PATH"
    log "  Size: $FILE_SIZE bytes"

    # Show usage hint
    echo ""
    echo "To analyze with logwatch-ai-go, configure .env:"
    echo "  LOG_SOURCE_TYPE=drupal_watchdog"
    echo "  DRUPAL_WATCHDOG_PATH=$OUTPUT_PATH"
    echo "  DRUPAL_WATCHDOG_FORMAT=$FORMAT"
    echo ""

    exit 0
else
    EXIT_CODE=$?
    log_error "drush watchdog:show failed with exit code $EXIT_CODE"
    log_error "Output: $OUTPUT"

    # Check for common issues
    if echo "$OUTPUT" | grep -q "database"; then
        log_error "Database connection issue. Check Drupal database settings."
    elif echo "$OUTPUT" | grep -q "permission"; then
        log_error "Permission issue. Try running with appropriate user permissions."
    elif echo "$OUTPUT" | grep -q "not found"; then
        log_error "Command not found. Ensure drush is properly installed."
    fi

    exit 1
fi
