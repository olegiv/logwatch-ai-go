#!/usr/bin/env bash
#
# Read-only pre-deployment inspection of the logwatch-ai production host.
#
# Read-only, with one exception: GATE 2 creates and immediately removes a
# probe file to prove the lock directory is writable. Nothing else is written.
#
# The remote payload runs with `set -u` only — deliberately no `-e` and no
# `pipefail` — so that every gate runs and their failures aggregate into
# gate_fail rather than aborting at the first one.
# Prints a captured snapshot interleaved with the gates below. Any of them
# failing sets gate_fail and the script exits non-zero; deploy.sh should not
# be run until they all pass. Note the missing-binary check (section 2) and
# the flock check (section 10) are gates too, despite sitting outside the
# numbered GATE blocks.
#
#   GATE 1  CPU supports GOAMD64=v3 (build-linux-amd64 targets v3; a v3
#           binary on a pre-Haswell CPU dies at exec).
#   GATE 2  The directory of the runner's EFFECTIVE lock path (a crontab or
#           /etc/cron.d override is honoured) is root-owned, not writable by
#           group or other, and writable by us. Root can write anywhere, so
#           writability alone proves nothing: any other principal that can
#           write there may plant the fixed lock name as a symlink. If it is not, run-cron.sh's
#           `exec 9>"$LOCK_FILE"` fails but the shell KEEPS GOING (verified:
#           a failed exec redirection does not terminate non-interactive
#           bash). `flock -n 9` then fails on the unopened fd, the runner
#           treats that as "another run holds the lock" and exits 0. So cron
#           reports success every night while nothing is analyzed, and
#           cron.log gains only a plausible "already in progress" line.
#   GATE 3  No analyzer/cron run currently in flight.
#   GATE 4  Enough free space, summed PER FILESYSTEM: ~25 MiB for the binary
#           in INSTALL_DIR, plus the database's own size +10% +1 MiB beside
#           the database. Those can be different filesystems when
#           DATABASE_PATH is configured, so each is checked against its own.
#   GATE 5  Not inside the nightly run window (derived from the crontab).
#
# Secrets: .env is read for KEY NAMES ONLY. Values are never printed except
# for an explicit allowlist of non-secret tuning settings.
#
# Shared environment, read by every deploy/ script:
#   DEPLOY_HOST  target host (deploy/deploy.env); HOST overrides it,
#                and a positional argument overrides both
#   INSTALL_DIR  remote install root (default /opt/logwatch-ai)
#   LOCK_FILE    target-side only — no script forwards it; the lock is
#                discovered from the crontab and the deployed runner
#
# Usage:
#   ./deploy/preflight.sh                # uses DEPLOY_HOST from deploy.env
#   ./deploy/preflight.sh <host>         # explicit (overrides env)

set -euo pipefail

# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
REMOTE_LIB="$(dirname "${BASH_SOURCE[0]}")/remote-lib.sh"
# A missing remote-lib.sh would otherwise ship a payload with no resolve_db
# or acquire_cron_lock. Process substitution hides `cat`'s failure, so in
# preflight that degrades silently all the way to "ALL GATES PASSED".
[[ -r $REMOTE_LIB ]] || { echo "error: $REMOTE_LIB is missing or unreadable" >&2; exit 1; }
HOST=$(resolve_host "${1:-}") || exit 1
require_root_target "$HOST" || exit 1

INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"
valid_install_dir "$INSTALL_DIR" || {
  echo "error: refusing to use INSTALL_DIR='$INSTALL_DIR'" >&2; exit 1
}
# The pre-move /var/lock location. Reported alongside the effective lock so
# can see a leftover there; overridable only for hosts that used another path.
OLD_LOCK_FILE="${OLD_LOCK_FILE:-/var/lock/logwatch-ai-cron.lock}"

echo "==> Pre-flight inspection of ${HOST} (read-only)"
echo "    install dir: ${INSTALL_DIR}"
echo

# rc is captured via `|| rc=$?` because `set -e` would otherwise abort the
# script the moment a gate fails, before we could print the summary.
rc=0
# LOCK_FILE is deliberately NOT forwarded: resolve_lock_file short-circuits on
# a set value, which would make the gate test the local default again instead
# of discovering what the deployed runner uses.
ssh "$HOST" 'bash -s' \
  < <(echo '{'
       remote_env INSTALL_DIR "$INSTALL_DIR" OLD_LOCK_FILE "$OLD_LOCK_FILE"
      declare -f redact_assignments
      cat "$REMOTE_LIB"; cat <<'__REMOTE_PREFLIGHT__'
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
    # An upgrade has nothing to upgrade. Without failing here, preflight says
    # ALL GATES PASSED and the deploy builds, uploads and snapshots before
    # hitting its own "nothing installed" guard.
    echo "  FAILED   no executable at $INSTALL_DIR/logwatch-analyzer"
    echo "           use scripts/install.sh to bootstrap before deploying"
    gate_fail=1
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
crontab -l -u root 2>&1 | grep -n -iE 'logwatch|^SHELL|^PATH|^MAILTO' \
    | redact_assignments || echo "(no logwatch line in root crontab)"
# shellcheck disable=SC2010 # a human-readable listing, not a name list
ls -la /etc/cron.d/ 2>/dev/null | grep -i logwatch || echo "(nothing logwatch-related in /etc/cron.d)"

echo
echo "===== 5. LOCK_FILE default in deployed runners ====="
echo "--- top-level runner (THIS is the file cron executes) ---"
grep -Hn 'LOCK_FILE' "$INSTALL_DIR/run-cron.sh" 2>&1 || echo "!! no run-cron.sh at top level"
# deploy.sh installs the runner only at the top level, which is the path cron
# invokes. A scripts/run-cron.sh here is a leftover from an old `cp -r
# scripts`: it is never executed and never updated, so it can only drift and
# mislead whoever reads it next.
if [ -e "$INSTALL_DIR/scripts/run-cron.sh" ]; then
    echo "--- scripts/run-cron.sh: STALE COPY PRESENT (never executed; safe to delete) ---"
    grep -Hn 'LOCK_FILE' "$INSTALL_DIR/scripts/run-cron.sh" 2>&1
else
    echo "--- scripts/run-cron.sh: absent (correct — the runner lives at the top level) ---"
fi
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
# Probe the path the runner will actually use, not the built-in default: an
# operator who pins LOCK_FILE in the crontab (which rollback.sh recommends
# when /run is unsuitable) would otherwise have this gate vouch for a
# directory nothing touches.
effective_lock=$(resolve_lock_file)
echo "  effective lock path: $effective_lock"
lock_dir=$(dirname "$effective_lock")
stat -c '%n  mode=%a  owner=%U:%G' "$lock_dir" 2>&1
findmnt -no FSTYPE "$lock_dir" 2>&1 || true
# Root can write anywhere, so a successful probe says nothing about safety.
# Any principal that can write the directory can plant a symlink at the fixed
# lock name and have the root runner's `exec 9>"$LOCK_FILE"` truncate its
# target — the reason this path was moved off /var/lock. Checking only
# the other-write bit is not enough: a directory owned by another account
# (0700) or writable by an untrusted group (0770) is equally exploitable, so
# require root ownership and no group or other write.
lock_perm=$(stat -c '%a' "$lock_dir" 2>/dev/null || echo "")
lock_owner=$(stat -c '%U' "$lock_dir" 2>/dev/null || echo "")
if [ -z "$lock_perm" ]; then
    echo "  FAILED   cannot stat $lock_dir"
    gate_fail=1
else
    if [ "$lock_owner" != "root" ]; then
        echo "  FAILED   $lock_dir is owned by '$lock_owner', not root"
        gate_fail=1
    fi
    if [ "$((8#$lock_perm & 0022))" -ne 0 ]; then
        echo "  FAILED   $lock_dir is group- or world-writable (mode $lock_perm)"
        gate_fail=1
    fi
    if [ "$lock_owner" = "root" ] && [ "$((8#$lock_perm & 0022))" -eq 0 ]; then
        echo "  ok       $lock_dir is root-owned, not writable by others (mode $lock_perm)"
    else
        echo "           point LOCK_FILE at a root-owned directory such as /run"
    fi
fi

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
# shellcheck disable=SC2010 # filtering ls's stderr, not parsing names
ls -la "$effective_lock" "$OLD_LOCK_FILE" 2>&1 | grep -v 'No such file' || echo "(neither present)"

echo
echo "===== GATE 3: run in flight? ====="
# Exactly the predicate acquire_cron_lock uses — shared, because these two
# have drifted apart twice, each time leaving this gate blind to a real run
# (most recently an interpreter-prefixed `/bin/bash .../run-cron.sh`).
if pgrep -u root -a -f "$(inflight_pattern)" 2>/dev/null; then
    echo "  FAILED   a run is in flight — do not deploy now"
    gate_fail=1
else
    echo "  ok       nothing running"
fi
# Derived from the crontab rather than hardcoded, and treated as a gate.
# Deploying into the window means cron fires against the lock the deploy
# holds, and run-cron.sh then exits 0 logging "already in progress" — a whole
# night of analysis skipped, indistinguishable from a genuine overlap.
cron_hm=$(crontab -l -u root 2>/dev/null | grep -v '^[[:space:]]*#' \
          | grep -F "$INSTALL_DIR/run-cron.sh" \
          | awk '{printf "%02d:%02d", $2, $1}' | head -1)
if [ -n "$cron_hm" ]; then
    now_min=$(( 10#$(date +%H) * 60 + 10#$(date +%M) ))
    cron_min=$(( 10#${cron_hm%%:*} * 60 + 10#${cron_hm##*:} ))
    lo=$(( cron_min - 20 )); hi=$(( cron_min + 50 ))
    echo "  cron runs at $cron_hm; now $(date +%H:%M) $(date +%Z)"
    if [ "$now_min" -ge "$lo" ] && [ "$now_min" -le "$hi" ]; then
        echo "  FAILED   inside the nightly run window — deploying now can make"
        echo "           cron skip tonight's run entirely (it exits 0 quietly)"
        gate_fail=1
    else
        echo "  ok       outside the nightly run window"
    fi
else
    echo "  (no cron entry found for $INSTALL_DIR/run-cron.sh — window not checked)"
fi

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
# Runs on the TARGET, inside the payload — not a probe from the client.
has_sqlite3() { command -v sqlite3 >/dev/null 2>&1; }

check_free_dev() {   # $1 = device, $2 = KiB required, $3 = label
    local dev="$1" need="$2" label="$3" out line avail
    if ! out=$(df -Pk 2>/dev/null | awk -v d="$dev" '$1 == d'); then
        echo "  FAILED   df could not inspect $dev"; gate_fail=1; return
    fi
    line=$(printf '%s\n' "$out" | tail -1)
    if [ -z "$line" ]; then
        echo "  FAILED   device $dev not found in df output"; gate_fail=1; return
    fi
    avail=$(printf '%s\n' "$line" | awk '{print $4}')
    if ! [ "$avail" -ge 0 ] 2>/dev/null; then
        echo "  FAILED   could not parse df output for $dev: $line"; gate_fail=1; return
    fi
    printf '  %s KiB free on %s (%s), combined need %s KiB for: %s\n' \
        "$avail" "$(printf '%s\n' "$line" | awk '{print $6}')" "$dev" "$need" "$label"
    if [ "$avail" -lt "$need" ]; then
        echo "  FAILED   insufficient space for $label"; gate_fail=1
    else
        echo "  ok       $label"
    fi
}

# Requirements are summed PER FILESYSTEM. With the default layout the binary
# and the database snapshot land on the same device, so checking each against
# the same free-space figure independently would pass a host that cannot fit
# both together.
# Binary plus headroom: ~25 MiB covers the incoming ~12 MiB artifact
# alongside the one it replaces. Older artifacts are already on disk and are
# pruned to KEEP_VERSIONS (default 3) AFTER the new one lands, so peak usage
# is higher than this gate checks; it covers the transient pair, not the
# whole retention set.
declare -A need_by_dev=() label_by_dev=()
add_need() {  # $1 = path, $2 = KiB, $3 = label
    local dev
    dev=$(df -Pk "$1" 2>/dev/null | tail -1 | awk '{print $1}')
    if [ -z "$dev" ]; then
        echo "  FAILED   df could not inspect $1"; gate_fail=1; return
    fi
    need_by_dev[$dev]=$(( ${need_by_dev[$dev]:-0} + $2 ))
    label_by_dev[$dev]="${label_by_dev[$dev]:+${label_by_dev[$dev]} + }$3"
}

add_need "$INSTALL_DIR" 25600 "binary+headroom"
db_rc=0; db=$(resolve_db) || db_rc=$?
if [ "$db_rc" -eq 2 ]; then
    # A runtime override means deploy.sh will refuse, so the gate must too —
    # otherwise preflight says ALL GATES PASSED for a host that cannot deploy.
    echo "  FAILED   DATABASE_PATH is overridden outside .env (see above)"
    gate_fail=1
elif [ "$db_rc" -eq 0 ] && [ -f "$db" ]; then
    db_kib=$(du -k "$db" | awk '{print $1}')
    # The WAL counts either way. Without sqlite3 the deploy copies the
    # sidecars verbatim; WITH sqlite3, .backup folds those pages into the
    # destination, so a 4 KiB main file with a 20 MiB WAL still allocates
    # ~20 MiB. Sizing only the main file let Gate 4 pass and the snapshot
    # then fill the filesystem.
    for ext in -wal -journal; do
        if [ -f "$db$ext" ]; then
            db_kib=$(( db_kib + $(du -k "$db$ext" | awk '{print $1}') ))
        fi
    done
    # -shm is shared memory, only copied by the raw fallback.
    if ! has_sqlite3 && [ -f "$db-shm" ]; then
        db_kib=$(( db_kib + $(du -k "$db-shm" | awk '{print $1}') ))
    fi
    # The snapshot is a full copy, so require its size again plus 10% slack.
    echo "  database: $db (${db_kib} KiB incl. sidecars where they are copied)"
    add_need "$(dirname "$db")" $(( db_kib + db_kib / 10 + 1024 )) "db snapshot"
else
    echo "  database: disabled or absent — no snapshot space needed"
fi
for dev in "${!need_by_dev[@]}"; do
    check_free_dev "$dev" "${need_by_dev[$dev]}" "${label_by_dev[$dev]}"
done
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
db_rc=0; db=$(resolve_db 2>/dev/null) || db_rc=$?
if [ "$db_rc" -eq 2 ]; then
    echo "  DATABASE_PATH overridden outside .env — not inspected"
elif [ "$db_rc" -ne 0 ]; then
    echo "  database disabled in .env — skipped"
elif [ ! -f "$db" ]; then
    echo "  no database at $db — skipped (not creating one)"
else
    ls -l "$db"* 2>&1
    # Opening a WAL-mode database can create its -shm even with -readonly,
    # when the WAL exists but the shared-memory file does not. That would
    # make this read-only inspection mutate the installation, so skip the
    # query in exactly that state rather than risk it.
    if [ -f "$db-wal" ] && [ ! -f "$db-shm" ]; then
        echo "  (WAL present without -shm; skipping the query so nothing is created)"
    elif command -v sqlite3 >/dev/null 2>&1; then
        # Only -readonly is used. The `file:...?mode=ro` URI form was the
        # fallback, but a sqlite3 built without URI filename support treats it
        # as a literal name and CREATES it in the cwd — which would break the
        # read-only guarantee this section exists to keep.
        if ! sqlite3 -readonly "$db" 'select 1;' >/dev/null 2>&1; then
            echo "  (sqlite3 here has no -readonly; skipping rather than risk a write)"
            SQ=()
        else
            SQ=(sqlite3 -readonly "$db")
        fi
        [ ${#SQ[@]} -gt 0 ] && "${SQ[@]}" \
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
# flock is not optional: without it nothing can hold the runner's lock across
# the deploy's critical section, so deploy.sh refuses to run.
if ! command -v flock >/dev/null 2>&1; then
    echo "  FAILED   flock is required to hold the cron lock during a deploy"
    gate_fail=1
fi

echo
if [ "$gate_fail" -eq 0 ]; then
    echo "########## ALL GATES PASSED ##########"
else
    echo "########## ONE OR MORE GATES FAILED — DO NOT DEPLOY ##########"
fi
exit "$gate_fail"
__REMOTE_PREFLIGHT__
      echo '}'
) || rc=$?

echo
if [ "$rc" -eq 0 ]; then
    echo "==> Pre-flight OK. Next: review the .env key diff below, then ./deploy/deploy.sh"
elif [ "$rc" -eq 255 ]; then
    echo "==> Could not connect to ${HOST} (ssh rc=255). No gates were evaluated." >&2
else
    echo "==> Pre-flight FAILED (rc=$rc). Resolve the gate above before deploying." >&2
fi
exit "$rc"
