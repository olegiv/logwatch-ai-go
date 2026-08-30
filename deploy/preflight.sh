#!/usr/bin/env bash
#
# Read-only pre-deployment inspection of the logwatch-ai production host.
#
# Touches nothing: every remote command reads state or probes a permission.
# Prints a captured snapshot, then evaluates four hard gates that must pass
# before deploy.sh is allowed to mutate /opt/logwatch-ai.
#
#   GATE 1  CPU supports GOAMD64=v3 (build-linux-amd64 targets v3; a v3
#           binary on a pre-Haswell CPU dies at exec).
#   GATE 2  $LOCK_FILE's directory is writable. If it is not, run-cron.sh's
#           `exec 9>"$LOCK_FILE"` fails but the shell KEEPS GOING (verified:
#           a failed exec redirection does not terminate non-interactive
#           bash). `flock -n 9` then fails on the unopened fd, the runner
#           treats that as "another run holds the lock" and exits 0. So cron
#           reports success every night while nothing is analyzed, and
#           cron.log gains only a plausible "already in progress" line.
#   GATE 3  No analyzer/cron run currently in flight.
#   GATE 4  At least 100 MB free on the install filesystem.
#
# Secrets: .env is read for KEY NAMES ONLY. Values are never printed except
# for an explicit allowlist of non-secret tuning settings.
#
# Usage:
#   ./deploy/preflight.sh                # uses DEPLOY_HOST from deploy.env
#   ./deploy/preflight.sh <host>         # explicit (overrides env)

set -euo pipefail

# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
REMOTE_LIB="$(dirname "${BASH_SOURCE[0]}")/remote-lib.sh"
HOST=$(resolve_host "${1:-}") || exit 1
require_root_target "$HOST" || exit 1

INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"
LOCK_FILE="${LOCK_FILE:-/run/logwatch-ai-cron.lock}"
OLD_LOCK_FILE="${OLD_LOCK_FILE:-/var/lock/logwatch-ai-cron.lock}"

echo "==> Pre-flight inspection of ${HOST} (read-only)"
echo "    install dir: ${INSTALL_DIR}"
echo "    lock file:   ${LOCK_FILE}"
echo

# rc is captured via `|| rc=$?` because `set -e` would otherwise abort the
# script the moment a gate fails, before we could print the summary.
rc=0
ssh "$HOST" INSTALL_DIR="$INSTALL_DIR" LOCK_FILE="$LOCK_FILE" \
            OLD_LOCK_FILE="$OLD_LOCK_FILE" 'bash -s' \
  < <(cat "$REMOTE_LIB"; cat <<'__REMOTE_PREFLIGHT__'
set -u
gate_fail=0

# remote-lib.sh's read_env greps a RELATIVE .env, so every resolve_db call
# below depends on this cd. Without it an `ssh host bash -s` payload runs in
# /root, silently falls back to the built-in defaults, and GATE 4 measures the
# wrong filesystem while reporting success.
if ! cd "$INSTALL_DIR"; then
    echo "FATAL: cannot cd to $INSTALL_DIR — is logwatch-ai installed on this host?"
    exit 1
fi

echo "===== 1. host ====="
hostname -f; uname -srm; date; uptime

echo
echo "===== 2. installed binary ====="
if [ -x "$INSTALL_DIR/logwatch-analyzer" ]; then
    "$INSTALL_DIR/logwatch-analyzer" -version 2>&1 || echo "!! -version exited $?"
else
    echo "!! no executable at $INSTALL_DIR/logwatch-analyzer"
fi

echo
echo "===== 3. install tree ====="
ls -la "$INSTALL_DIR/" "$INSTALL_DIR/scripts/" "$INSTALL_DIR/data/" "$INSTALL_DIR/logs/" 2>&1
echo "--- protected files (must survive the deploy untouched) ---"
for f in .env drupal-sites.json ocms-sites.json exclusions.json data/summaries.db; do
    if [ -e "$INSTALL_DIR/$f" ]; then
        stat -c '%n  mode=%a  owner=%U:%G  size=%s  mtime=%y' "$INSTALL_DIR/$f"
    else
        echo "$INSTALL_DIR/$f  ABSENT"
    fi
done

echo
echo "===== 4. crontab ====="
crontab -l -u root 2>&1 | grep -n -iE 'logwatch|^SHELL|^PATH|^MAILTO' || echo "(no logwatch line in root crontab)"
ls -la /etc/cron.d/ 2>/dev/null | grep -i logwatch || echo "(nothing logwatch-related in /etc/cron.d)"

echo
echo "===== 5. L-06 drift: LOCK_FILE default in deployed runners ====="
echo "--- top-level runner (THIS is the file cron executes) ---"
grep -Hn 'LOCK_FILE' "$INSTALL_DIR/run-cron.sh" 2>&1 || echo "!! no run-cron.sh at top level"
echo "--- scripts/ copy (never executed; kept consistent only to avoid misleading auditors) ---"
grep -Hn 'LOCK_FILE' "$INSTALL_DIR/scripts/run-cron.sh" 2>&1 || echo "(absent)"
echo "--- checksums (compare against local) ---"
sha256sum "$INSTALL_DIR/logwatch-analyzer" "$INSTALL_DIR/run-cron.sh" "$INSTALL_DIR"/scripts/*.sh 2>&1

echo
echo "===== 6. .env KEY NAMES ONLY (no values) ====="
grep -oE '^[[:space:]]*(export[[:space:]]+)?[A-Za-z_][A-Za-z0-9_]*[[:space:]]*=' "$INSTALL_DIR/.env" 2>/dev/null \
    | sed -E 's/^[[:space:]]*(export[[:space:]]+)?//; s/[[:space:]]*=$//' | sort
echo "--- non-secret settings (explicit allowlist) ---"
grep -E '^(LLM_PROVIDER|CLAUDE_MODEL|LOG_SOURCE_TYPE|LOG_LEVEL|ENABLE_DATABASE|DATABASE_PATH|ENABLE_PREPROCESSING|MAX_PREPROCESSING_TOKENS|AI_MAX_TOKENS|AI_TIMEOUT_SECONDS|MAX_LOG_SIZE_MB)=' \
    "$INSTALL_DIR/.env" 2>/dev/null || echo "(none matched)"

echo
echo "===== 7. site configuration ====="
cat "$INSTALL_DIR/ocms-sites.json" 2>&1
echo "--- drupal site ids ---"
if command -v jq >/dev/null 2>&1; then
    jq -r '.sites | keys[]' "$INSTALL_DIR/drupal-sites.json" 2>&1
else
    echo "(jq not installed — drupal reader requires it!)"
fi
echo "--- OCMS registry /etc/ocms/sites.conf (id + dir only) ---"
[ -r /etc/ocms/sites.conf ] && awk '!/^#/ && NF {print "  " $1, $2}' /etc/ocms/sites.conf || echo "(unreadable)"

echo
echo "===== GATE 1: CPU flags for GOAMD64=v3 ====="
# osxsave is deliberately NOT checked: it is part of the x86-64-v3 spec, but
# Linux does not always surface it in /proc/cpuinfo as a separate flag (it
# reports xsave/xsavec/xsaveopt/xsaves instead), which produced a false
# negative on this very host. The authoritative test is executing the staged
# binary before the swap — deploy.sh does exactly that.
grep -m1 '^model name' /proc/cpuinfo
for f in avx avx2 bmi1 bmi2 f16c fma movbe abm xsave; do
    if grep -qw "$f" /proc/cpuinfo; then
        echo "  ok       $f"
    else
        echo "  MISSING  $f   <-- GOAMD64=v3 UNSAFE"
        gate_fail=1
    fi
done

echo
echo "===== GATE 2: lock directory writable ====="
lock_dir=$(dirname "$LOCK_FILE")
stat -c '%n  mode=%a  owner=%U:%G' "$lock_dir" 2>&1
findmnt -no FSTYPE "$lock_dir" 2>&1 || true
probe="$lock_dir/.logwatch-preflight-probe.$$"
if touch "$probe" 2>/dev/null; then
    echo "  ok       $lock_dir is writable"
    rm -f "$probe"
else
    echo "  FAILED   $lock_dir NOT writable — run-cron.sh would skip every run,"
    echo "           exiting 0 with a false 'already in progress' line"
    gate_fail=1
fi
echo "--- existing lock files ---"
ls -la "$LOCK_FILE" "$OLD_LOCK_FILE" 2>&1 | grep -v 'No such file' || echo "(neither present)"

echo
echo "===== GATE 3: run in flight? ====="
if pgrep -a -f 'run-cron\.sh|logwatch-analyzer' 2>/dev/null; then
    echo "  FAILED   a run is in flight — do not deploy now"
    gate_fail=1
else
    echo "  ok       nothing running"
fi
echo "--- current time vs 02:07 cron (avoid 01:50-03:00) ---"
date '+  now: %H:%M %Z'

echo
echo "===== GATE 4: disk ====="
# The gate must cover what the deploy actually allocates: a new binary in
# INSTALL_DIR plus a full-size .pre-<version> database snapshot beside the
# database. With a configured DATABASE_PATH those can be different
# filesystems, so each is checked against its own.
#
# df is captured and tested explicitly rather than piped into awk: this
# script runs without `pipefail`, so a df failure would otherwise be masked
# by awk exiting cleanly on zero records and the gate would report success.
check_free() {   # $1 = path, $2 = KiB required, $3 = label
    local path="$1" need="$2" label="$3" out line avail
    if ! out=$(df -Pk "$path" 2>&1); then
        echo "  FAILED   df could not inspect $path: $out"; gate_fail=1; return
    fi
    line=$(printf '%s\n' "$out" | tail -1)
    avail=$(printf '%s\n' "$line" | awk '{print $4}')
    if ! [ "$avail" -ge 0 ] 2>/dev/null; then
        echo "  FAILED   could not parse df output for $path: $line"; gate_fail=1; return
    fi
    printf '  %s: %s KiB free on %s, needs %s KiB\n' \
        "$label" "$avail" "$(printf '%s\n' "$line" | awk '{print $6}')" "$need"
    if [ "$avail" -lt "$need" ]; then
        echo "  FAILED   insufficient space for $label"; gate_fail=1
    else
        echo "  ok       $label"
    fi
}

# Binary plus headroom: ~25 MiB covers a 12 MiB artifact kept alongside its
# predecessor, with room for logs written during the run.
check_free "$INSTALL_DIR" 25600 "install dir (binary + headroom)"

if db=$(resolve_db) && [ -f "$db" ]; then
    db_kib=$(du -k "$db" | awk '{print $1}')
    # The snapshot is a full copy, so require its size again plus 10% slack.
    need=$(( db_kib + db_kib / 10 + 1024 ))
    echo "  database: $db (${db_kib} KiB)"
    check_free "$(dirname "$db")" "$need" "database backup"
else
    echo "  database: disabled or absent — no snapshot space needed"
fi
du -sh "$INSTALL_DIR" "$INSTALL_DIR/logs" "$INSTALL_DIR/data" 2>&1

echo
echo "===== 8. recent cron activity ====="
tail -n 40 "$INSTALL_DIR/logs/cron.log" 2>&1
echo "--- last run summaries ---"
grep -aE 'done|FAILED' "$INSTALL_DIR/logs/cron.log" 2>/dev/null | tail -n 8 || echo "(none)"

echo
echo "===== 9. database ====="
# Resolve the configured path and never touch a database that is disabled or
# absent: sqlite3 CREATES an empty file when handed a missing path, which
# would make this supposedly read-only preflight mutate the installation.
if ! db=$(resolve_db); then
    echo "  database disabled in .env — skipped"
elif [ ! -f "$db" ]; then
    echo "  no database at $db — skipped (not creating one)"
else
    ls -l "$db"* 2>&1
    if command -v sqlite3 >/dev/null 2>&1; then
        # -readonly where supported; the file: URI is the portable fallback.
        sqlite3 -readonly "$db" 'select 1;' >/dev/null 2>&1 \
            && SQ=(sqlite3 -readonly "$db") \
            || SQ=(sqlite3 "file:$db?mode=ro")
        "${SQ[@]}" \
          "select 'rows=' || count(*) from summaries;
           select log_source_type || ' / ' || site_name || ' : ' || count(*) || '  last=' || max(timestamp)
             from summaries group by log_source_type, site_name;" 2>&1
    else
        echo "  (sqlite3 CLI absent — fine, the binary bundles modernc.org/sqlite)"
    fi
fi

echo
echo "===== 10. dependencies ====="
for c in flock jq logwatch drush sqlite3; do
    printf '  %-10s %s\n' "$c" "$(command -v "$c" 2>/dev/null || echo 'NOT FOUND')"
done

echo
if [ "$gate_fail" -eq 0 ]; then
    echo "########## ALL GATES PASSED ##########"
else
    echo "########## ONE OR MORE GATES FAILED — DO NOT DEPLOY ##########"
fi
exit "$gate_fail"
__REMOTE_PREFLIGHT__
) || rc=$?

echo
if [ "$rc" -eq 0 ]; then
    echo "==> Pre-flight OK. Next: review the .env key diff below, then ./deploy/deploy.sh"
else
    echo "==> Pre-flight FAILED (rc=$rc). Resolve the gate above before deploying." >&2
fi
exit "$rc"
