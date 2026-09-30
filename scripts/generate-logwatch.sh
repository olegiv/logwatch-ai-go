#!/usr/bin/env bash
# Generate a Logwatch report safely for the analyzer.

set -euo pipefail
umask 027

OUTPUT_PATH="${1:-/var/log/logwatch-ai/logwatch-output.txt}"
DETAIL_LEVEL="${2:-0}"
RANGE="${3:-yesterday}"
SCRIPT_NAME="$(basename "$0")"
TEMP_OUTPUT=""
LOGWATCH_TIMEOUT_SECONDS="${LOGWATCH_TIMEOUT_SECONDS:-600}"
LOGWATCH_MAX_OUTPUT_BYTES="${LOGWATCH_MAX_OUTPUT_BYTES:-10485760}"
LOGWATCH_OUTPUT_ROOTS="${LOGWATCH_OUTPUT_ROOTS:-/var/log/logwatch-ai:/opt/logwatch-ai/logs}"

log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] $SCRIPT_NAME: $*"
    logger -t "$SCRIPT_NAME" "$*" 2>/dev/null || true
}
cleanup() {
    [[ -z $TEMP_OUTPUT ]] || rm -f -- "$TEMP_OUTPUT"
}
trap cleanup EXIT

if [[ $OUTPUT_PATH != /* || $OUTPUT_PATH == *'/../'* || $OUTPUT_PATH == */.. ]]; then
    log "ERROR: OUTPUT_PATH must be an absolute normalized path"
    exit 1
fi
if ! [[ $DETAIL_LEVEL =~ ^[0-9]+$ ]] || (( DETAIL_LEVEL < 0 || DETAIL_LEVEL > 10 )); then
    log "ERROR: detail level must be a number from 0 through 10"
    exit 1
fi
case "$RANGE" in
    yesterday|today|all|help) ;;
    *) log "ERROR: range must be yesterday, today, all, or help"; exit 1 ;;
esac
if ! [[ $LOGWATCH_TIMEOUT_SECONDS =~ ^[0-9]+$ ]] \
    || (( ${#LOGWATCH_TIMEOUT_SECONDS} > 4 )) \
    || (( 10#$LOGWATCH_TIMEOUT_SECONDS < 1 || 10#$LOGWATCH_TIMEOUT_SECONDS > 3600 )); then
    log "ERROR: LOGWATCH_TIMEOUT_SECONDS must be between 1 and 3600"
    exit 1
fi
if ! [[ $LOGWATCH_MAX_OUTPUT_BYTES =~ ^[0-9]+$ ]] \
    || (( ${#LOGWATCH_MAX_OUTPUT_BYTES} > 9 )) \
    || (( 10#$LOGWATCH_MAX_OUTPUT_BYTES < 1024 || 10#$LOGWATCH_MAX_OUTPUT_BYTES > 104857600 )); then
    log "ERROR: LOGWATCH_MAX_OUTPUT_BYTES must be between 1024 and 104857600"
    exit 1
fi

OUTPUT_DIR=${OUTPUT_PATH%/*}
OUTPUT_NAME=${OUTPUT_PATH##*/}
[[ -n $OUTPUT_NAME && $OUTPUT_NAME != . && $OUTPUT_NAME != .. ]] || {
    log "ERROR: OUTPUT_PATH must name a file"
    exit 1
}
[[ -d $OUTPUT_DIR && ! -L $OUTPUT_DIR ]] || {
    log "ERROR: output directory must already exist as a real directory: $OUTPUT_DIR"
    exit 1
}
OUTPUT_DIR=$(cd "$OUTPUT_DIR" && pwd -P)
OUTPUT_PATH="$OUTPUT_DIR/$OUTPUT_NAME"
output_allowed=0
IFS=: read -r -a configured_output_roots <<< "$LOGWATCH_OUTPUT_ROOTS"
for configured_root in "${configured_output_roots[@]}"; do
    [[ -n $configured_root && -d $configured_root && ! -L $configured_root ]] || continue
    resolved_root=$(cd "$configured_root" && pwd -P)
    case "$OUTPUT_PATH" in
        "$resolved_root"/*) output_allowed=1; break ;;
    esac
done
if [[ $output_allowed != 1 ]]; then
    log "ERROR: resolved output path is outside allowed roots: $LOGWATCH_OUTPUT_ROOTS"
    exit 1
fi

if [[ -L $OUTPUT_PATH || ( -e $OUTPUT_PATH && ! -f $OUTPUT_PATH ) ]]; then
    log "ERROR: output destination must be a regular file, not a symlink: $OUTPUT_PATH"
    exit 1
fi
if stat -c '%u %a' "$OUTPUT_DIR" >/dev/null 2>&1; then
    read -r dir_owner dir_mode < <(stat -c '%u %a' "$OUTPUT_DIR")
else
    read -r dir_owner dir_mode < <(stat -f '%u %Lp' "$OUTPUT_DIR")
fi
if [[ $dir_owner != $(id -u) ]] || (( (8#$dir_mode & 8#022) != 0 )); then
    log "ERROR: output directory must be owned by uid $(id -u) and not group/world-writable: $OUTPUT_DIR"
    exit 1
fi

if [[ -n ${LOGWATCH_BIN_OVERRIDE:-} ]]; then
    LOGWATCH_BIN=$LOGWATCH_BIN_OVERRIDE
elif [[ -x /opt/local/bin/logwatch ]]; then
    LOGWATCH_BIN=/opt/local/bin/logwatch
elif [[ -x /usr/sbin/logwatch ]]; then
    LOGWATCH_BIN=/usr/sbin/logwatch
else
    LOGWATCH_BIN=$(command -v logwatch 2>/dev/null || true)
fi
if [[ -z $LOGWATCH_BIN || ! -x $LOGWATCH_BIN ]]; then
    log "ERROR: logwatch is not installed or executable"
    exit 1
fi
if [[ $EUID -ne 0 ]]; then
    log "WARNING: not running as root; Logwatch may not read all system logs"
fi

TEMP_OUTPUT=$(mktemp "$OUTPUT_DIR/.${OUTPUT_NAME}.tmp.XXXXXXXX")
log "Generating report: output=$OUTPUT_PATH detail=$DETAIL_LEVEL range=$RANGE"
if command -v gtimeout >/dev/null 2>&1; then
    TIMEOUT_BIN=$(command -v gtimeout)
elif command -v timeout >/dev/null 2>&1; then
    TIMEOUT_BIN=$(command -v timeout)
else
    log "ERROR: GNU timeout is required to bound Logwatch execution"
    exit 1
fi
# Bash applies ulimit -f in 1024-byte blocks when not running in POSIX mode.
# The exact byte check below remains authoritative for a non-aligned limit.
LOGWATCH_OUTPUT_BLOCKS=$(( (10#$LOGWATCH_MAX_OUTPUT_BYTES + 1023) / 1024 ))
if ! (
    ulimit -f "$LOGWATCH_OUTPUT_BLOCKS"
    exec "$TIMEOUT_BIN" --kill-after=10s "${LOGWATCH_TIMEOUT_SECONDS}s" "$LOGWATCH_BIN" \
        --output file \
        --filename "$TEMP_OUTPUT" \
        --format text \
        --detail "$DETAIL_LEVEL" \
        --range "$RANGE"
); then
    log "ERROR: Logwatch generation failed, exceeded ${LOGWATCH_TIMEOUT_SECONDS}s, or exceeded $LOGWATCH_MAX_OUTPUT_BYTES bytes"
    exit 1
fi
[[ -f $TEMP_OUTPUT && ! -L $TEMP_OUTPUT ]] || {
    log "ERROR: Logwatch did not create a regular report file"
    exit 1
}
if [[ $OSTYPE == darwin* ]]; then
    TEMP_SIZE=$(stat -f%z "$TEMP_OUTPUT")
else
    TEMP_SIZE=$(stat -c%s "$TEMP_OUTPUT")
fi
if (( TEMP_SIZE > 10#$LOGWATCH_MAX_OUTPUT_BYTES )); then
    log "ERROR: Logwatch output exceeded $LOGWATCH_MAX_OUTPUT_BYTES bytes"
    exit 1
fi
chmod 0640 "$TEMP_OUTPUT"
if [[ -n ${OUTPUT_GROUP:-} ]]; then
    chgrp "$OUTPUT_GROUP" "$TEMP_OUTPUT"
fi
mv -f -- "$TEMP_OUTPUT" "$OUTPUT_PATH"
TEMP_OUTPUT=""

if [[ $OSTYPE == darwin* ]]; then
    FILE_SIZE=$(stat -f%z "$OUTPUT_PATH")
else
    FILE_SIZE=$(stat -c%s "$OUTPUT_PATH")
fi
log "Report generated successfully: $OUTPUT_PATH ($FILE_SIZE bytes)"
