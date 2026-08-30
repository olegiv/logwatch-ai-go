#!/usr/bin/env bash
#
# Revert a deploy by swapping in the .prev backups left by deploy.sh.
# Reverting the binary and the cron runner are independent operations,
# because they are independent failure modes:
#
#   binary  — a bad binary shows up as -version failing, SIGILL, or
#             analyzer errors in the logs.
#   runner  — a bad runner shows up as cron.log being EMPTY, because a
#             failed `exec 9>"$LOCK_FILE"` kills the shell before it can
#             log anything.
#   scripts — a bad generate-* script shows up as that job alone failing
#             while the rest of the run succeeds.
#
# After a rollback the broken artifact is kept as .failed for inspection
# rather than deleted.
#
# Usage:
#   ./deploy/rollback.sh                    # binary only (the common case)
#   ./deploy/rollback.sh --runner           # cron runner only
#   ./deploy/rollback.sh --scripts          # generate-*/helper scripts only
#   ./deploy/rollback.sh --all              # binary + runner + helper scripts
#   ./deploy/rollback.sh --db               # restore the pre-deploy database
#   ./deploy/rollback.sh <host> [--flags]

set -euo pipefail

# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

DO_BIN=0; DO_RUNNER=0; DO_SCRIPTS=0; DO_DB=0; HOST_ARG=""
for arg in "$@"; do
  case "$arg" in
    --runner)  DO_RUNNER=1 ;;
    --scripts) DO_SCRIPTS=1 ;;
    --db)      DO_DB=1 ;;
    --all)     DO_BIN=1; DO_RUNNER=1; DO_SCRIPTS=1 ;;
    -*)        echo "error: unknown flag $arg" >&2; exit 2 ;;
    *)         HOST_ARG="$arg" ;;
  esac
done
# Default: binary only.
if [[ $DO_RUNNER == 0 && $DO_SCRIPTS == 0 && $DO_DB == 0 ]]; then DO_BIN=1; fi

HOST=$(resolve_host "$HOST_ARG") || exit 1
INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"

echo "==> Rolling back on ${HOST}:${INSTALL_DIR}"
ssh "$HOST" INSTALL_DIR="$INSTALL_DIR" DO_BIN="$DO_BIN" DO_RUNNER="$DO_RUNNER" \
            DO_SCRIPTS="$DO_SCRIPTS" DO_DB="$DO_DB" 'bash -s' <<'__REMOTE_ROLLBACK__'
set -euo pipefail
cd "$INSTALL_DIR"

if pgrep -f 'run-cron\.sh|logwatch-analyzer' >/dev/null 2>&1; then
    echo "ABORT: a run is in flight — wait for it to finish" >&2
    pgrep -a -f 'run-cron\.sh|logwatch-analyzer' >&2
    exit 1
fi

if [ "$DO_BIN" = 1 ]; then
    # logwatch-analyzer is a symlink to a versioned regular file. Rolling back
    # means re-pointing the symlink at the previous target, recorded by
    # deploy.sh — never replacing the symlink with a regular file, and never
    # deleting the binary we are rolling away from.
    if [ ! -f ./.logwatch-analyzer.prev-target ]; then
        echo "error: no .logwatch-analyzer.prev-target — nothing to roll back to." >&2
        echo "  deploy.sh writes it on every install. If this is the first deploy" >&2
        echo "  with these scripts, re-point the symlink by hand:" >&2
        ls -la ./logwatch-analyzer* >&2
        exit 1
    fi
    prev_target=$(cat ./.logwatch-analyzer.prev-target)
    if [ ! -x "$prev_target" ]; then
        echo "error: recorded rollback target $prev_target is missing or not executable" >&2
        exit 1
    fi
    failed_target=$(readlink -f ./logwatch-analyzer)
    ln -sfn "$prev_target" ./logwatch-analyzer.rb
    mv -Tf ./logwatch-analyzer.rb ./logwatch-analyzer
    echo "binary rolled back:"
    echo "  symlink now -> $prev_target"
    echo "  rolled away from $failed_target (kept for inspection)"
    ./logwatch-analyzer -version
    ls -la ./logwatch-analyzer
fi

if [ "$DO_RUNNER" = 1 ]; then
    if [ ! -f ./run-cron.sh.prev ]; then
        echo "error: no run-cron.sh.prev to roll back to." >&2
        exit 1
    fi
    mv ./run-cron.sh ./run-cron.sh.failed
    mv ./run-cron.sh.prev ./run-cron.sh
    bash -n ./run-cron.sh && echo "runner rolled back, syntax OK"
    grep -n 'LOCK_FILE=' ./run-cron.sh
fi

if [ "$DO_SCRIPTS" = 1 ]; then
    restored=0
    for f in generate-logwatch.sh generate-drupal-watchdog.sh helper.sh; do
        [ -f "./scripts/$f.prev" ] || continue
        mode=$(stat -c '%a' "./scripts/$f")
        mv "./scripts/$f" "./scripts/$f.failed"
        mv "./scripts/$f.prev" "./scripts/$f"
        chmod "$mode" "./scripts/$f"
        echo "  restored scripts/$f (mode $mode)"
        restored=$((restored + 1))
    done
    [ "$restored" -eq 0 ] && echo "  no scripts/*.prev backups to restore"
fi

if [ "$DO_DB" = 1 ]; then
    cd ./data
    backup=$(ls -1t summaries.db.pre-* 2>/dev/null | head -1)
    if [ -z "$backup" ]; then
        echo "error: no summaries.db.pre-* backup found" >&2
        exit 1
    fi
    echo "restoring from $backup"
    # Move the ENTIRE suspect state aside, sidecars included. Leaving a stale
    # -wal/-shm/-journal beside the restored snapshot lets SQLite replay those
    # pages into it on the next open, silently reconstructing data from the
    # database we are trying to abandon — or corrupting the restore outright.
    mv summaries.db summaries.db.suspect
    for ext in -wal -shm -journal; do
        [ -f "summaries.db$ext" ] && mv "summaries.db$ext" "summaries.db.suspect$ext"
    done
    cp -p "$backup" summaries.db
    # Restore the snapshot's own sidecars if the backup captured any; a
    # sqlite3 .backup produces a single consistent file and needs none.
    for ext in -wal -shm -journal; do
        [ -f "$backup$ext" ] && cp -p "$backup$ext" "summaries.db$ext"
    done
    ls -l
fi
__REMOTE_ROLLBACK__

cat <<EOF

==> Rollback complete on ${HOST}.

Nothing was deleted: the binary you rolled away from is still on disk
under its own version name, the runner is kept as run-cron.sh.failed, and
a replaced database is kept as summaries.db.suspect. Fix the source, then
re-run ./deploy/deploy.sh.

If the runner was rolled back because /run turned out not to be writable,
prefer keeping the L-06 fix and pinning the lock path in the crontab line
instead of reverting the script:

  7 2 * * * LOCK_FILE=${INSTALL_DIR}/.cron.lock ${INSTALL_DIR}/run-cron.sh >> ${INSTALL_DIR}/logs/cron.log 2>&1

${INSTALL_DIR} is root-owned 0750, so that path is still not world-writable.
EOF
