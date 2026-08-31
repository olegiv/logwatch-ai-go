#!/usr/bin/env bash
#
# One-call status snapshot of the production install: deployed version and
# binary layout, available rollback targets, the crontab entry that drives it,
# the effective lock path and its state, and recent job results.
#
# Read-only. Resolves the database and lock paths through remote-lib.sh, the
# same way deploy.sh and rollback.sh do, so a host with a configured
# DATABASE_PATH or a crontab-pinned LOCK_FILE is reported accurately rather
# than against the built-in defaults.
#
# Shared environment, read by every deploy/ script:
#   DEPLOY_HOST  target host (deploy/deploy.env); HOST overrides it,
#                and a positional argument overrides both
#   INSTALL_DIR  remote install root (default /opt/logwatch-ai)
#   LOCK_FILE    target-side only — no script forwards it; the lock is
#                discovered from the crontab and the deployed runner
#
# Usage:
#   ./deploy/status.sh                  # uses DEPLOY_HOST from deploy.env
#   ./deploy/status.sh <host>           # explicit (overrides env)

set -euo pipefail

# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
REMOTE_LIB="$(dirname "${BASH_SOURCE[0]}")/remote-lib.sh"
[[ -r $REMOTE_LIB ]] || { echo "error: $REMOTE_LIB is missing or unreadable" >&2; exit 1; }

HOST=$(resolve_host "${1:-}") || exit 1
require_root_target "$HOST" || exit 1

INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"
valid_install_dir "$INSTALL_DIR" || {
  echo "error: refusing to use INSTALL_DIR='$INSTALL_DIR'" >&2; exit 1
}

# No -n here: it would redirect stdin from /dev/null and discard the payload
# fed in below. -n belongs only on ssh calls that do NOT read a script.
ssh "$HOST" 'bash -s' \
  < <(echo '{'
       remote_env INSTALL_DIR "$INSTALL_DIR"
      declare -f redact_assignments
      cat "$REMOTE_LIB"; cat <<'__REMOTE_STATUS__'
set -u
cd "$INSTALL_DIR" || { echo "FATAL: cannot cd to $INSTALL_DIR"; exit 1; }

echo "==> Version"
./logwatch-analyzer -version 2>&1 || echo "!! -version exited $?"

echo
echo "==> Binary layout and rollback targets"
ls -la ./logwatch-analyzer ./logwatch-analyzer-* 2>/dev/null
if [ -f ./.logwatch-analyzer.prev-target ]; then
    target=$(cat ./.logwatch-analyzer.prev-target)
    if [ -x "$target" ]; then
        echo "  rollback target: $target"
    else
        echo "  rollback target: $target  !! MISSING OR NOT EXECUTABLE"
    fi
else
    echo "  rollback target: (none recorded)"
fi
for f in ./run-cron.sh.prev ./scripts/*.prev; do
    [ -e "$f" ] && echo "  component backup: $f"
done

echo
echo "==> Schedule"
# docs/CRON_SETUP.md documents /etc/cron.d/logwatch-ai as a supported
# location, so reporting only root's personal crontab could claim there is no
# schedule while the lock section below shows that same entry's lock path.
sched=$( { # @desc lines are the marker convention scripts/run-cron.sh documents for
           # identifying this job among a host's other cron entries.
           crontab -l -u root 2>/dev/null | sed 's/^/[crontab] /';
           grep -H -iE 'logwatch|@desc' /etc/cron.d/* 2>/dev/null | sed 's/^/[cron.d] /'; } \
         | grep -iE 'logwatch|@desc' )
if [ -n "$sched" ]; then
    # Cron lines can carry inline credentials and this output gets pasted
    # into tickets and CI logs — the same reason .env is only read for keys.
    printf '%s\n' "$sched" | redact_assignments
else
    echo "(no logwatch schedule found)"
fi

echo
echo "==> Lock"
lock=$(resolve_lock_file)
echo "  effective path: $lock"
if [ -e "$lock" ]; then
    stat -c '  %n  mode=%a  owner=%U:%G  mtime=%y' "$lock"
else
    echo "  (not present — normal between runs)"
fi

echo
echo "==> In flight?"
# The shared predicate, so status cannot report "(idle)" during a run the
# other scripts would have detected.
pgrep -u root -a -f "$(inflight_pattern)" 2>/dev/null || echo "(idle)"

echo
echo "==> Last runs"
# Match the runner's own status lines, including the "already in progress"
# line that signals a lock the runner could not open — the symptom an
# unwritable lock directory produces, and one that no ok/FAILED filter shows.
if [ -r ./logs/cron.log ]; then
    matches=$(grep -aE '\] (ok|FAILED)|done —|done -|begin run|already in progress' \
                ./logs/cron.log | tail -n 22)
    if [ -n "$matches" ]; then
        printf '%s\n' "$matches"
    else
        echo "(no job status lines found; last 10 raw lines follow)"
        tail -n 10 ./logs/cron.log
    fi
else
    echo "(logs/cron.log unreadable)"
fi

echo
echo "==> Database"
db_rc=0; db=$(resolve_db 2>/dev/null) || db_rc=$?
if [ "$db_rc" -eq 2 ]; then
    echo "  !! DATABASE_PATH is overridden outside .env — this tooling and the"
    echo "     analyzer disagree about which database is live. Snapshot and"
    echo "     rollback are unsafe until that is resolved."
elif [ "$db_rc" -ne 0 ]; then
    echo "  disabled in .env"
elif [ -f "$db" ]; then
    ls -l "$db"
    # The pointer rollback --db actually prefers, which was never shown.
    if [ -f "$INSTALL_DIR/.logwatch-analyzer.db-snapshot" ]; then
        echo "  recorded snapshot: $(cat "$INSTALL_DIR/.logwatch-analyzer.db-snapshot")"
    else
        echo "  recorded snapshot: (none — rollback --db would fall back to mtime order)"
    fi
    # Filter sidecars: listing them as separate "snapshots" is the same
    # confusion rollback.sh and deploy.sh already filter out.
    for f in "$db".pre-*; do
        case "$f" in *-wal|*-shm|*-journal) continue ;; esac
        [ -e "$f" ] && echo "  snapshot: $(ls -l "$f" | awk '{print $5, $6, $7, $8, $9}')"
    done
    for f in "$db".suspect-*; do
        [ -e "$f" ] && echo "  suspect (from a rollback; prune does not reach these): $f"
    done
else
    echo "  configured at $db but not present"
fi
__REMOTE_STATUS__
      echo '}'
)
