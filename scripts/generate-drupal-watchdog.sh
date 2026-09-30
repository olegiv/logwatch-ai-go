#!/bin/bash
#
# Drupal Watchdog Export Script
#
# This script exports Drupal watchdog logs using drush for analysis
# with the logwatch-ai-go analyzer.
#
# Configuration is loaded from drupal-sites.json (same as logwatch-ai-go analyzer).
# Command line arguments override site configuration values.
#
# Usage:
#   ./scripts/generate-drupal-watchdog.sh [options]
#
# Options:
#   -S, --site          Drupal site ID from drupal-sites.json (required unless default_site set)
#   --sites-config      Path to drupal-sites.json configuration file
#   --list-sites        List available Drupal sites from drupal-sites.json and exit
#   -d, --drupal-root   Override Drupal project root from site config
#   -o, --output        Override output file path from site config
#   -f, --format        Override analyzer input format (json only)
#   -c, --count         Max entries to fetch from drush (default: 10000)
#   -l, --limit         Override max entries in output file from site config
#   -s, --severity      Filter by severity: emergency,alert,critical,error,warning,notice,info,debug
#   -t, --type          Filter by log type (e.g., php, cron, system)
#   -h, --help          Show this help message
#   -v, --version       Show version information
#
# Configuration:
#   Site configuration is loaded from drupal-sites.json (see configs/drupal-sites.json.example)
#   Search locations:
#     - ./drupal-sites.json
#     - ./configs/drupal-sites.json
#     - /opt/logwatch-ai/drupal-sites.json
#
# Examples:
#   # List available sites
#   ./scripts/generate-drupal-watchdog.sh --list-sites
#
#   # Export from default site (requires default_site in drupal-sites.json)
#   ./scripts/generate-drupal-watchdog.sh
#
#   # Export from specific site
#   ./scripts/generate-drupal-watchdog.sh --site production
#
#   # Export last 500 error and warning entries
#   ./scripts/generate-drupal-watchdog.sh --site production -c 500 -s error,warning
#
#   # Override output path from site config
#   ./scripts/generate-drupal-watchdog.sh --site staging -o /var/log/logwatch-ai/staging-watchdog.json
#
#   # Export PHP errors only
#   ./scripts/generate-drupal-watchdog.sh --site production -t php -c 200
#
# Crontab example (export daily at 2:00 AM before analyzer runs):
#   0 2 * * * /opt/logwatch-ai/scripts/generate-drupal-watchdog.sh --site production
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

# Default configuration (will be overridden by site config and CLI args)
DRUPAL_ROOT=""
DRUPAL_SYSTEM_USER=""
OUTPUT_PATH=""
FORMAT=""
COUNT="10000"
LIMIT=""
SEVERITY=""
LOG_TYPE=""
MIN_SEVERITY=""
DRUPAL_EXPORT_TIMEOUT_SECONDS="${DRUPAL_EXPORT_TIMEOUT_SECONDS:-300}"
DRUPAL_MAX_OUTPUT_BYTES="${DRUPAL_MAX_OUTPUT_BYTES:-16777216}"
DRUPAL_OUTPUT_ROOTS="${DRUPAL_OUTPUT_ROOTS:-/var/log/logwatch-ai:/opt/logwatch-ai/logs}"

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

# Get site configuration field using jq.
# Passes site_id and field via --arg to avoid jq-filter injection from crafted
# CLI input or config keys.
get_site_config() {
    local config_file="$1"
    local site_id="$2"
    local field="$3"

    jq -r --arg site "$site_id" --arg field "$field" \
        '.sites[$site][$field] // empty' "$config_file" 2>/dev/null
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

    # Validate site exists (use --arg to avoid jq-filter injection)
    if ! jq -e --arg site "$site_id" '.sites[$site]' "$config_file" > /dev/null 2>&1; then
        log_error "Site '$site_id' not found in $config_file"
        log_error "Use --list-sites to see available sites"
        exit 1
    fi

    log "Using site configuration: $site_id from $config_file"

    # Get site-specific configuration
    local site_drupal_root site_system_user site_watchdog_path site_watchdog_format site_min_severity site_watchdog_limit

    site_drupal_root=$(get_site_config "$config_file" "$site_id" "drupal_root")
    site_system_user=$(get_site_config "$config_file" "$site_id" "system_user")
    site_watchdog_path=$(get_site_config "$config_file" "$site_id" "watchdog_path")
    site_watchdog_format=$(get_site_config "$config_file" "$site_id" "watchdog_format")
    site_min_severity=$(get_site_config "$config_file" "$site_id" "min_severity")
    site_watchdog_limit=$(get_site_config "$config_file" "$site_id" "watchdog_limit")

    # Apply site config (CLI > site config > .env > defaults)
    [ -z "$CLI_DRUPAL_ROOT" ] && [ -n "$site_drupal_root" ] && DRUPAL_ROOT="$site_drupal_root"
    [ -n "$site_system_user" ] && DRUPAL_SYSTEM_USER="$site_system_user"
    [ -z "$CLI_OUTPUT_PATH" ] && [ -n "$site_watchdog_path" ] && OUTPUT_PATH="$site_watchdog_path"
    [ -z "$CLI_FORMAT" ] && [ -n "$site_watchdog_format" ] && FORMAT="$site_watchdog_format"
    [ -z "$CLI_LIMIT" ] && [ -n "$site_watchdog_limit" ] && LIMIT="$site_watchdog_limit"
    [ -n "$site_min_severity" ] && MIN_SEVERITY="$site_min_severity"
    # With `set -e`, an omitted optional field must not make the function's
    # final conditional test become a false failure status.
    return 0
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

# Parse command line arguments
CLI_DRUPAL_ROOT=""
CLI_OUTPUT_PATH=""
CLI_FORMAT=""
CLI_LIMIT=""

while [[ $# -gt 0 ]]; do
    case $1 in
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

# Find drupal-sites.json (required)
FOUND_SITES_CONFIG=$(find_sites_config "$SITES_CONFIG_FILE") || {
    log_error "No drupal-sites.json configuration file found."
    echo ""
    echo "Search locations:"
    echo "  - ./drupal-sites.json"
    echo "  - ./configs/drupal-sites.json"
    echo "  - /opt/logwatch-ai/drupal-sites.json"
    echo ""
    echo "Use --sites-config to specify a custom path."
    echo "See configs/drupal-sites.json.example for format."
    exit 1
}

# Handle --list-sites flag
if [ "$LIST_SITES" = true ]; then
    list_drupal_sites "$FOUND_SITES_CONFIG"
fi

# Determine site to use (from CLI or default_site in config)
if [ -z "$DRUPAL_SITE" ]; then
    # Try to get default_site from config
    if command -v jq &> /dev/null; then
        DRUPAL_SITE=$(jq -r '.default_site // empty' "$FOUND_SITES_CONFIG" 2>/dev/null)
    fi
fi

if [ -z "$DRUPAL_SITE" ]; then
    log_error "No site specified. Use --site <site_id> or set default_site in drupal-sites.json"
    log_error "Use --list-sites to see available sites"
    exit 1
fi

# Apply site configuration
apply_site_config "$FOUND_SITES_CONFIG" "$DRUPAL_SITE"

# Apply configuration priority: CLI args > site config > defaults
DRUPAL_ROOT="${CLI_DRUPAL_ROOT:-${DRUPAL_ROOT}}"
OUTPUT_PATH="${CLI_OUTPUT_PATH:-${OUTPUT_PATH}}"
FORMAT="${CLI_FORMAT:-${FORMAT:-json}}"
LIMIT="${CLI_LIMIT:-${LIMIT:-100}}"

# Default min_severity: 4 (warning). Drupal records many authentication and
# access incidents as warnings, so the secure default must not turn those days
# into false "no entries" reports.
MIN_SEVERITY="${MIN_SEVERITY:-4}"

# Raw Drush tables cannot preserve multi-word types reliably and cannot be
# safely filtered by timestamp. JSON is the sole supported interchange format.
if [[ "$FORMAT" != "json" ]]; then
    log_error "Invalid format '$FORMAT'. Must be 'json'"
    exit 1
fi
DRUSH_OUTPUT_FORMAT="json"

# Validate count is a positive, bounded number.
if ! [[ "$COUNT" =~ ^[0-9]+$ ]] || (( ${#COUNT} > 6 )) || (( 10#$COUNT < 1 || 10#$COUNT > 100000 )); then
    log_error "Count must be between 1 and 100000 (got: $COUNT)"
    exit 1
fi

# Validate limit is a positive, bounded number.
if ! [[ "$LIMIT" =~ ^[0-9]+$ ]] || (( ${#LIMIT} > 6 )) || (( 10#$LIMIT < 1 || 10#$LIMIT > 100000 )); then
    log_error "Limit must be between 1 and 100000 (got: $LIMIT)"
    exit 1
fi

if ! [[ "$DRUPAL_EXPORT_TIMEOUT_SECONDS" =~ ^[0-9]+$ ]] \
    || (( ${#DRUPAL_EXPORT_TIMEOUT_SECONDS} > 4 )) \
    || (( 10#$DRUPAL_EXPORT_TIMEOUT_SECONDS < 1 || 10#$DRUPAL_EXPORT_TIMEOUT_SECONDS > 3600 )); then
    log_error "DRUPAL_EXPORT_TIMEOUT_SECONDS must be between 1 and 3600"
    exit 1
fi
if ! [[ "$DRUPAL_MAX_OUTPUT_BYTES" =~ ^[0-9]+$ ]] \
    || (( ${#DRUPAL_MAX_OUTPUT_BYTES} > 9 )) \
    || (( 10#$DRUPAL_MAX_OUTPUT_BYTES < 1024 || 10#$DRUPAL_MAX_OUTPUT_BYTES > 67108864 )); then
    log_error "DRUPAL_MAX_OUTPUT_BYTES must be between 1024 and 67108864"
    exit 1
fi

# Validate LOG_TYPE and SEVERITY shapes before forwarding to drush.
# Real Drupal watchdog type names can contain spaces (e.g. "page not found",
# "access denied"), and --severity accepts a comma-separated list (e.g.
# "error,warning"), so both allow those characters. Defense in depth:
#   1. The leading-character anchor blocks values that begin with '-', so
#      a crafted argument cannot be interpreted as an additional drush flag.
#   2. An explicit "--" check rejects values that contain a double-dash
#      anywhere, closing the theoretical argparse re-split path on
#      a value like "php --count=99" that some argparse implementations
#      could interpret as an extra flag inside the --type payload.
if [ -n "$LOG_TYPE" ]; then
    if ! [[ "$LOG_TYPE" =~ ^[A-Za-z0-9_][A-Za-z0-9\ ._-]*$ ]]; then
        log_error "Invalid --type '$LOG_TYPE'. Allowed: letters, digits, space, '_', '.', '-' (no leading '-')"
        exit 1
    fi
    if [[ "$LOG_TYPE" == *"--"* ]]; then
        log_error "Invalid --type '$LOG_TYPE'. Value must not contain '--' (would be re-interpreted as a drush flag)"
        exit 1
    fi
fi

if [ -n "$SEVERITY" ]; then
    if ! [[ "$SEVERITY" =~ ^[A-Za-z0-9_][A-Za-z0-9,_-]*$ ]]; then
        log_error "Invalid --severity '$SEVERITY'. Allowed: letters, digits, ',', '_', '-' (no leading '-')"
        exit 1
    fi
    if [[ "$SEVERITY" == *"--"* ]]; then
        log_error "Invalid --severity '$SEVERITY'. Value must not contain '--' (would be re-interpreted as a drush flag)"
        exit 1
    fi
fi

# Calculate yesterday's time boundaries (00:00:00 to 23:59:59)
if [[ "$OSTYPE" == "darwin"* ]]; then
    TODAY_START=$(date -j -f "%Y-%m-%d %H:%M:%S" "$(date +%Y-%m-%d) 00:00:00" +%s)
    YESTERDAY_START=$(date -j -v-1d -f "%Y-%m-%d %H:%M:%S" \
        "$(date +%Y-%m-%d) 00:00:00" +%s)
else
    TODAY_START=$(date -d "today 00:00:00" +%s)
    YESTERDAY_START=$(date -d "yesterday 00:00:00" +%s)
fi
YESTERDAY_END=$((TODAY_START - 1))

log "Starting Drupal watchdog export"
log "Configuration:"
log "  Site: $DRUPAL_SITE"
log "  Sites config: $FOUND_SITES_CONFIG"
log "  Drupal root: $DRUPAL_ROOT"
log "  Drupal system user: ${DRUPAL_SYSTEM_USER:-current user}"
log "  Output: $OUTPUT_PATH"
log "  Format: $FORMAT"
log "  Count: $COUNT (max entries to fetch)"
log "  Limit: $LIMIT (max entries in output)"
log "  Min severity: $MIN_SEVERITY (0=emergency to 7=debug)"
log "  Time filter: yesterday only"
[ -n "$LOG_TYPE" ] && log "  Type filter: $LOG_TYPE"

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

# Vendor Drush executes the Drupal application and therefore third-party PHP.
# Root cron must always drop privileges to the site's configured Unix account.
DRUSH_RUNNER=()
if [[ $EUID -eq 0 ]]; then
	if [[ -z $DRUPAL_SYSTEM_USER ]]; then
		if stat -c '%U' "$DRUPAL_ROOT" >/dev/null 2>&1; then
			DRUPAL_SYSTEM_USER=$(stat -c '%U' "$DRUPAL_ROOT")
		else
			DRUPAL_SYSTEM_USER=$(stat -f '%Su' "$DRUPAL_ROOT")
		fi
		log_warning "Site '$DRUPAL_SITE' has no system_user; using Drupal root owner '$DRUPAL_SYSTEM_USER'"
    fi
    if ! id "$DRUPAL_SYSTEM_USER" >/dev/null 2>&1; then
        log_error "Configured system_user does not exist: $DRUPAL_SYSTEM_USER"
        exit 1
    fi
    if [[ $(id -u "$DRUPAL_SYSTEM_USER") -eq 0 ]]; then
        log_error "system_user must not be root"
        exit 1
    fi
    if ! command -v runuser >/dev/null 2>&1; then
        log_error "runuser is required to execute Drush without root privileges"
        exit 1
    fi
    DRUSH_RUNNER=(runuser -u "$DRUPAL_SYSTEM_USER" --)
fi

# Resolve and constrain output to a dedicated analyzer-owned root. Requiring a
# watchdog-specific filename prevents a typo from replacing cron.log or another
# regular file inside the same root.
OUTPUT_DIR=$(dirname "$OUTPUT_PATH")
OUTPUT_NAME=$(basename "$OUTPUT_PATH")
if [[ $OUTPUT_PATH != /* || $OUTPUT_PATH == *'/../'* || $OUTPUT_PATH == */.. ]]; then
    log_error "Output path must be absolute and normalized"
    exit 1
fi
if [[ ! $OUTPUT_NAME =~ (^|-)watchdog\.json$ ]]; then
    log_error "Output filename must be watchdog.json or end in -watchdog.json"
    exit 1
fi
if [[ ! -d $OUTPUT_DIR || -L $OUTPUT_DIR ]]; then
    log_error "Output directory must already exist as a real directory: $OUTPUT_DIR"
    exit 1
fi
OUTPUT_DIR=$(cd "$OUTPUT_DIR" && pwd -P)
OUTPUT_PATH="$OUTPUT_DIR/$OUTPUT_NAME"
output_allowed=0
IFS=: read -r -a configured_output_roots <<< "$DRUPAL_OUTPUT_ROOTS"
for configured_root in "${configured_output_roots[@]}"; do
    [[ -n $configured_root && -d $configured_root && ! -L $configured_root ]] || continue
    resolved_root=$(cd "$configured_root" && pwd -P)
    case "$OUTPUT_PATH" in
        "$resolved_root"/*) output_allowed=1; break ;;
    esac
done
if [[ $output_allowed != 1 ]]; then
    log_error "Output path must be below an allowed root: $DRUPAL_OUTPUT_ROOTS"
    exit 1
fi
if [[ -L $OUTPUT_PATH || ( -e $OUTPUT_PATH && ! -f $OUTPUT_PATH ) ]]; then
    log_error "Output destination must be a regular file, not a symlink: $OUTPUT_PATH"
    exit 1
fi
if stat -c '%u %a' "$OUTPUT_DIR" >/dev/null 2>&1; then
    read -r output_owner output_mode < <(stat -c '%u %a' "$OUTPUT_DIR")
else
    read -r output_owner output_mode < <(stat -f '%u %Lp' "$OUTPUT_DIR")
fi
if [[ $output_owner != $(id -u) ]] || (( (8#$output_mode & 8#022) != 0 )); then
    log_error "Output directory must be owned by uid $(id -u) and not group/world-writable"
    exit 1
fi
if [[ -e $OUTPUT_PATH ]]; then
    if stat -c '%u' "$OUTPUT_PATH" >/dev/null 2>&1; then
        output_file_owner=$(stat -c '%u' "$OUTPUT_PATH")
    else
        output_file_owner=$(stat -f '%u' "$OUTPUT_PATH")
    fi
    if [[ $output_file_owner != $(id -u) ]]; then
        log_error "Existing output file must be owned by uid $(id -u): $OUTPUT_PATH"
        exit 1
    fi
fi
TEMP_OUTPUT=$(mktemp "$OUTPUT_DIR/.$(basename "$OUTPUT_PATH").tmp.XXXXXXXX")
DRUSH_OUTPUT_FILE=$(mktemp "$OUTPUT_DIR/.$(basename "$OUTPUT_PATH").drush.XXXXXXXX")
# shellcheck disable=SC2329 # invoked by the EXIT trap
cleanup_temp_output() {
    [[ -z $TEMP_OUTPUT ]] || rm -f -- "$TEMP_OUTPUT"
    [[ -z $DRUSH_OUTPUT_FILE ]] || rm -f -- "$DRUSH_OUTPUT_FILE"
}
trap cleanup_temp_output EXIT

# Build drush command. An explicit CLI severity is authoritative and is
# enforced by Drush. The configured numeric minimum is applied locally only
# when no explicit severity list was requested.
DRUSH_CMD=("$DRUSH_BIN" "-r" "$DRUPAL_ROOT" "watchdog:show" "--count=$COUNT" "--format=$DRUSH_OUTPUT_FORMAT")
APPLY_MIN_SEVERITY=true

# Add type filter if specified
if [ -n "$LOG_TYPE" ]; then
    DRUSH_CMD+=("--type=$LOG_TYPE")
fi
if [ -n "$SEVERITY" ]; then
    DRUSH_CMD+=("--severity=$SEVERITY")
    APPLY_MIN_SEVERITY=false
fi

log "Executing: ${DRUSH_CMD[*]}"

# Execute Drush with a hard deadline and an OS-enforced output file limit.
if command -v gtimeout >/dev/null 2>&1; then
    TIMEOUT_BIN=$(command -v gtimeout)
elif command -v timeout >/dev/null 2>&1; then
    TIMEOUT_BIN=$(command -v timeout)
else
    log_error "GNU timeout is required to bound Drush execution"
    exit 1
fi
# Bash applies ulimit -f in 1024-byte blocks when not running in POSIX mode.
# The exact byte check below remains authoritative for a non-aligned limit.
DRUSH_OUTPUT_BLOCKS=$(( (10#$DRUPAL_MAX_OUTPUT_BYTES + 1023) / 1024 ))
if (
    ulimit -f "$DRUSH_OUTPUT_BLOCKS"
    exec "$TIMEOUT_BIN" --kill-after=10s "${DRUPAL_EXPORT_TIMEOUT_SECONDS}s" \
        "${DRUSH_RUNNER[@]}" "${DRUSH_CMD[@]}"
) > "$DRUSH_OUTPUT_FILE" 2>&1; then
    if [[ $OSTYPE == darwin* ]]; then
        OUTPUT_SIZE=$(stat -f%z "$DRUSH_OUTPUT_FILE")
    else
        OUTPUT_SIZE=$(stat -c%s "$DRUSH_OUTPUT_FILE")
    fi
    if (( OUTPUT_SIZE > 10#$DRUPAL_MAX_OUTPUT_BYTES )); then
        log_error "Drush output exceeded $DRUPAL_MAX_OUTPUT_BYTES bytes"
        exit 1
    fi
    OUTPUT=$(<"$DRUSH_OUTPUT_FILE")
    log "Drush returned $OUTPUT_SIZE bytes"

    if [ "$OUTPUT_SIZE" -lt 10 ]; then
        log_warning "Drush output is very small or empty: '$OUTPUT'"
    fi
    # Filter by timestamp and apply the configured limit using jq.
    if command -v jq &> /dev/null; then
            # Check JSON validity first
            if ! echo "$OUTPUT" | jq empty 2>/dev/null; then
                log_error "Drush output is not valid JSON"
                exit 1
            fi

            RAW_COUNT=$(echo "$OUTPUT" | jq 'length' 2>/dev/null) || {
                log_error "Could not count Drush result rows"
                exit 1
            }
            # Drush returns the newest rows first, so a full result set covers
            # all of yesterday only when its oldest row predates yesterday.
            if (( RAW_COUNT >= 10#$COUNT )); then
                OLDEST_TIMESTAMP=$(echo "$OUTPUT" | jq '[.[] | .timestamp | tonumber] | min' 2>/dev/null) || OLDEST_TIMESTAMP=""
                if ! [[ $OLDEST_TIMESTAMP =~ ^[0-9]+$ ]] || (( 10#$OLDEST_TIMESTAMP >= YESTERDAY_START )); then
                    log_error "Drush returned the full --count=$COUNT result set without reaching back before yesterday; refusing an incomplete date range"
                    log_error "Increase --count (maximum 100000) or reduce Drupal log volume before retrying"
                    exit 1
                fi
            fi

            # Drush outputs object {wid: entry, ...} - convert to array and sort by wid (higher = newer)
            # Convert severity string to number and filter by severity and yesterday's time range
            if ! CONVERTED=$(echo "$OUTPUT" | jq --argjson minSev "$MIN_SEVERITY" \
                --argjson applyMinSev "$APPLY_MIN_SEVERITY" \
                --argjson yesterdayStart "$YESTERDAY_START" \
                --argjson yesterdayEnd "$YESTERDAY_END" '
                def is_security:
                    (((.type // "") | ascii_downcase | test("security|access denied|authentication|login"))
                     or ((.message // "") | ascii_downcase | test("failed login|login attempt failed|access denied|blocked ip|brute force|unauthorized")));
                [to_entries | .[].value] |
                map(
                    {
                        wid: (.wid | tonumber),
                        uid: ((.uid // "0") | tonumber),
                        type: .type,
                        message: .message,
                        severity: (
                            # Severity strings: English, Russian, German, Spanish, French
                            if .severity == "Emergency" or .severity == "Авария" or .severity == "Notfall" or .severity == "Emergencia" or .severity == "Urgence" then 0
                            elif .severity == "Alert" or .severity == "Тревога" or .severity == "Alarm" or .severity == "Alerta" or .severity == "Alerte" then 1
                            elif .severity == "Critical" or .severity == "Критический" or .severity == "Критическая" or .severity == "Kritisch" or .severity == "Crítico" or .severity == "Critique" then 2
                            elif .severity == "Error" or .severity == "Ошибка" or .severity == "Fehler" or .severity == "Erreur" then 3
                            elif .severity == "Warning" or .severity == "Предупреждение" or .severity == "Warnung" or .severity == "Advertencia" or .severity == "Aviso" or .severity == "Avertissement" then 4
                            elif .severity == "Notice" or .severity == "Уведомление" or .severity == "Hinweis" or .severity == "Notificación" or .severity == "Avis" then 5
                            elif .severity == "Info" or .severity == "Инфо" or .severity == "Информация" or .severity == "Información" or .severity == "Information" then 6
                            elif .severity == "Debug" or .severity == "Отладка" or .severity == "Depuración" or .severity == "Débogage" then 7
                            elif (.severity | type) == "number" and .severity >= 0 and .severity <= 7 then .severity
                            else error("unknown watchdog severity: \(.severity)") end
                        ),
                        severity_label: .severity,
                        location: .location,
                        hostname: .hostname,
                        timestamp: (.timestamp | tonumber),
                        date: .date
                    }
                ) |
                [.[] | select(
                    ($applyMinSev | not)
                    or .severity <= $minSev
                    or is_security
                )] |
                [.[] | select(.timestamp >= $yesterdayStart and .timestamp <= $yesterdayEnd)] |
                # Preserve severe and security-relevant evidence before the
                # configured output limit is applied. Recency breaks ties.
                sort_by(
                    (if .severity <= 2 then 0
                     elif is_security then 1
                     elif .severity == 3 then 2
                     else 3 + .severity end),
                    .severity,
                    -.wid
                )
            ' 2>/dev/null) || [ -z "$CONVERTED" ]; then
                log_error "Failed to convert drush output format"
                exit 1
            fi

            ORIGINAL_COUNT=$(echo "$CONVERTED" | jq 'length' 2>/dev/null || echo "0")
            log "Converted $ORIGINAL_COUNT entries from drush format"

            # Select the highest-value records, then restore chronological
            # order for the analyzer's human-readable report.
            FILTERED=$(echo "$CONVERTED" | jq --argjson limit "$LIMIT" '.[:$limit] | sort_by(-.wid)' 2>/dev/null)

            if [ -n "$FILTERED" ] && [ "$FILTERED" != "[]" ]; then
                printf '%s\n' "$FILTERED" > "$TEMP_OUTPUT"
                ENTRY_COUNT=$(echo "$FILTERED" | jq 'length' 2>/dev/null || echo "unknown")
                log "Exported $ENTRY_COUNT entries (limited from $ORIGINAL_COUNT total)"
            else
                log "No watchdog entries found for the specified criteria"
                printf '%s\n' "[]" > "$TEMP_OUTPUT"
            fi
    else
        log_error "jq is required to validate and filter the JSON watchdog export"
        exit 1
    fi

    # Set file permissions (readable by owner and group)
    chmod 640 "$TEMP_OUTPUT" 2>/dev/null || log_warning "Failed to set file permissions"
    mv -f -- "$TEMP_OUTPUT" "$OUTPUT_PATH"
    TEMP_OUTPUT=""
    rm -f -- "$DRUSH_OUTPUT_FILE"
    DRUSH_OUTPUT_FILE=""

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
    echo "To analyze with logwatch-ai-go:"
    echo "  ./bin/logwatch-analyzer -source-type drupal_watchdog -drupal-site $DRUPAL_SITE"
    echo ""

    exit 0
else
    drush_status=$?
    if [[ $drush_status == 124 || $drush_status == 137 ]]; then
        log_error "drush watchdog:show exceeded ${DRUPAL_EXPORT_TIMEOUT_SECONDS}s and was terminated"
    else
        log_error "drush watchdog:show failed with status $drush_status"
    fi

    # Check for common issues
    if grep -qi "database" "$DRUSH_OUTPUT_FILE"; then
        log_error "Database connection issue. Check Drupal database settings."
    elif grep -qi "permission" "$DRUSH_OUTPUT_FILE"; then
        log_error "Permission issue. Try running with appropriate user permissions."
    elif grep -qi "not found" "$DRUSH_OUTPUT_FILE"; then
        log_error "Command not found. Ensure drush is properly installed."
    fi

    exit 1
fi
