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
  < <(remote_env INSTALL_DIR "$INSTALL_DIR"
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
crontab -l -u root 2>/dev/null | grep -iE 'logwatch|@desc' || echo "(no logwatch crontab line)"

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
pat="^(${INSTALL_DIR}/)?(run-cron\.sh|logwatch-analyzer)"
pgrep -u root -a -f "$pat" 2>/dev/null || echo "(idle)"

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
if ! db=$(resolve_db); then
    echo "  disabled in .env"
elif [ -f "$db" ]; then
    ls -l "$db"
    for f in "$db".pre-*; do
        [ -e "$f" ] && echo "  snapshot: $(ls -l "$f" | awk '{print $5, $6, $7, $8, $9}')"
    done
else
    echo "  configured at $db but not present"
fi
__REMOTE_STATUS__
)
