#!/usr/bin/env bash
#
# Revert a deploy by swapping in the .prev backups left by deploy.sh.
# Reverting the binary and the cron runner are independent operations,
# because they are independent failure modes:
#
#   binary  — a bad binary shows up as -version failing, SIGILL, or
#             analyzer errors in the logs.
#   runner  — a bad runner shows up as the nightly jobs never running. Note
#             a failed `exec 9>"$LOCK_FILE"` does NOT kill the shell; the
#             runner carries on, `flock -n 9` fails on the unopened fd, and
#             it exits 0 claiming another run is in progress.
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
#   FORCE=1 ./deploy/rollback.sh            # proceed even if a run is active
#
# FORCE=1 exists because the case that most needs a rollback — a wedged or
# runaway analyzer — is also the case where the running-process check would
# otherwise refuse to act.

set -euo pipefail

# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
REMOTE_LIB="$(dirname "${BASH_SOURCE[0]}")/remote-lib.sh"
# A missing remote-lib.sh would otherwise ship a payload with no resolve_db
# or acquire_cron_lock. Process substitution hides `cat`'s failure, so in
# preflight that degrades silently all the way to "ALL GATES PASSED".
[[ -r $REMOTE_LIB ]] || { echo "error: $REMOTE_LIB is missing or unreadable" >&2; exit 1; }

DO_BIN=0; DO_RUNNER=0; DO_SCRIPTS=0; DO_DB=0; LENIENT=0; HOST_ARG=""
for arg in "$@"; do
  case "$arg" in
    --runner)  DO_RUNNER=1 ;;
    --scripts) DO_SCRIPTS=1 ;;
    --db)      DO_DB=1 ;;
    --all)     DO_BIN=1; DO_RUNNER=1; DO_SCRIPTS=1; LENIENT=1 ;;
    -*)        echo "error: unknown flag $arg" >&2; exit 2 ;;
    *)         if [[ -n $HOST_ARG ]]; then
                 echo "error: more than one host given ('$HOST_ARG' and '$arg')" >&2; exit 2
               fi
               HOST_ARG="$arg" ;;
  esac
done
# Default: binary only.
if [[ $DO_RUNNER == 0 && $DO_SCRIPTS == 0 && $DO_DB == 0 ]]; then DO_BIN=1; fi

HOST=$(resolve_host "$HOST_ARG") || exit 1
require_root_target "$HOST" || exit 1
INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"
valid_install_dir "$INSTALL_DIR" || {
  echo "error: refusing to use INSTALL_DIR='$INSTALL_DIR'" >&2; exit 1
}

echo "==> Rolling back on ${HOST}:${INSTALL_DIR}"
ssh "$HOST" 'bash -s' \
  < <(remote_env INSTALL_DIR "$INSTALL_DIR" DO_BIN "$DO_BIN" DO_RUNNER "$DO_RUNNER" \
                 DO_SCRIPTS "$DO_SCRIPTS" DO_DB "$DO_DB" LENIENT "$LENIENT" \
                 TRACKED "${TRACKED_SCRIPTS[*]}" FORCE "${FORCE:-0}"
      cat "$REMOTE_LIB"; cat <<'__REMOTE_ROLLBACK__'
set -euo pipefail
cd "$INSTALL_DIR"

acquire_cron_lock || exit 1

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
    # Executable by mode is not the same as runnable: a corrupt or
    # wrong-architecture binary passes -x and fails at exec. deploy.sh only
    # WARNs when the recorded target cannot run, so that state can reach us.
    # Discover it before repointing the symlink, or an emergency rollback
    # replaces a partly working release with an unusable one and then aborts.
    if ! "$prev_target" -version >/dev/null 2>&1; then
        echo "error: rollback target $prev_target does not run — refusing to switch to it." >&2
        echo "       The current binary is untouched. Rebuild the previous tag instead." >&2
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
        # deploy.sh only writes .prev for components it actually replaced, so
        # a missing backup means this deployment left the runner alone. Under
        # --all that must not abort the remaining components.
        if [ "$LENIENT" = 1 ]; then
            echo "  run-cron.sh unchanged by the deployment — skipping"
            DO_RUNNER=0
        else
            echo "error: no run-cron.sh.prev to roll back to." >&2
            exit 1
        fi
    fi
fi

if [ "$DO_RUNNER" = 1 ]; then
    # Check the backup BEFORE installing it. run-cron.sh is gitignored and
    # hand-maintained, so .prev is whatever the last deploy happened to copy
    # and has never been syntax-checked. As the left side of `&&` a failing
    # `bash -n` is exempt from errexit, so validating after the swap would
    # print the error and still report success.
    if ! bash -n ./run-cron.sh.prev; then
        echo "ABORT: run-cron.sh.prev has a syntax error — not restoring it." >&2
        echo "       The current runner is untouched. Fix the backup by hand." >&2
        exit 1
    fi
    # A failed deployment or manual recovery may have left no live runner —
    # precisely when --runner is needed — so an unconditional mv would abort
    # under errexit before the valid backup could be restored.
    [ -e ./run-cron.sh ] && mv ./run-cron.sh ./run-cron.sh.failed
    mv ./run-cron.sh.prev ./run-cron.sh
    echo "runner rolled back, syntax OK"
    grep -n 'LOCK_FILE=' ./run-cron.sh \
        || echo "WARN: restored runner pins no LOCK_FILE — it will use the built-in default"
fi

if [ "$DO_SCRIPTS" = 1 ]; then
    restored=0
    for f in $TRACKED; do
        [ -f "./scripts/$f.prev" ] || continue
        # .prev was made with `cp -p`, so it already carries the mode the file
        # had when the deploy replaced it. Re-applying the CURRENT file's mode
        # would faithfully restore an operator's later chmod — e.g. a
        # helper.sh loosened from 0640 to 0644 — which is the opposite of a
        # rollback. Guarding the move also matters: a missing live file must
        # not abort the loop half-way through the restore.
        [ -e "./scripts/$f" ] && mv "./scripts/$f" "./scripts/$f.failed"
        mv "./scripts/$f.prev" "./scripts/$f"
        echo "  restored scripts/$f (mode $(stat -c '%a' "./scripts/$f"))"
        restored=$((restored + 1))
    done
    if [ "$restored" -eq 0 ]; then
        echo "  no scripts/*.prev backups — helpers unchanged by the deployment"
    fi
fi

if [ "$DO_DB" = 1 ]; then
    # Resolve the same path deploy.sh backed up: .env may set an absolute or
    # non-default DATABASE_PATH, in which case the snapshot does not live in
    # ./data at all.
    if ! db=$(resolve_db); then
        echo "error: database disabled in .env — nothing to restore" >&2
        exit 1
    fi
    # Match only main snapshots: the glob also catches .pre-<v>-wal/-shm/
    # -journal, and because cp -p preserves source mtimes a WAL is often the
    # newest match — `ls -t | head -1` would then install WAL bytes as the
    # database.
    # `|| true` is required: with no match `ls` exits 2 and `grep -v` exits 1
    # on empty input, so under `pipefail` the assignment itself trips errexit
    # and the script dies here — before the guard below can explain why.
    # Prefer the snapshot deploy.sh recorded. Sorting by mtime is unreliable:
    # the cp -p fallback preserves the database's mtime, so two deploys with
    # no writes between them yield identically stamped snapshots and `ls -t`
    # can pick the older release's.
    backup=$(cat ./.logwatch-analyzer.db-snapshot 2>/dev/null || echo "")
    if [ -n "$backup" ] && [ ! -f "$backup" ]; then backup=""; fi
    if [ -z "$backup" ]; then
        backup=$(ls -1t "$db".pre-* 2>/dev/null | grep -vE -- '-(wal|shm|journal)$' | head -1 || true)
    fi
    if [ -z "$backup" ]; then
        echo "error: no $(basename "$db").pre-* backup found beside $db" >&2
        exit 1
    fi
    cd "$(dirname "$db")"
    db=$(basename "$db")
    backup=$(basename "$backup")
    echo "restoring from $backup"
    # Move the ENTIRE suspect state aside, sidecars included. Leaving a stale
    # -wal/-shm/-journal beside the restored snapshot lets SQLite replay those
    # pages into it on the next open, silently reconstructing data from the
    # database we are trying to abandon — or corrupting the restore outright.
    # The live file may be absent precisely because a failed release removed
    # it — that is a state where --db is most needed, so guard the move.
    [ -f "$db" ] && mv "$db" "$db.suspect"
    for ext in -wal -shm -journal; do
        [ -f "$db$ext" ] && mv "$db$ext" "$db.suspect$ext"
    done
    cp -p "$backup" "$db"
    # Restore the snapshot's own sidecars if the backup captured any; a
    # sqlite3 .backup produces a single consistent file and needs none.
    for ext in -wal -shm -journal; do
        [ -f "$backup$ext" ] && cp -p "$backup$ext" "$db$ext"
    done
    ls -l "$db"*
fi
__REMOTE_ROLLBACK__
)

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
