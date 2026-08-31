#!/usr/bin/env bash
#
# Revert a deploy. The binary is reverted by re-pointing the stable symlink
# at the target recorded in .logwatch-analyzer.prev-target; the runner and
# helper scripts are reverted from their .prev copies; the database is
# restored from the snapshot named in .logwatch-analyzer.db-snapshot.
# Reverting the binary, the cron runner, the helper scripts and the database
# are independent operations,
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
# Nothing is deleted. What the replaced artifact becomes depends on which:
#   binary  — keeps its own versioned name; only the symlink moves
#   runner  — run-cron.sh.failed
#   scripts — scripts/<name>.failed
#   database— <db>.suspect-<timestamp>  (never pruned; clean up by hand)
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
# Shared environment, read by every deploy/ script:
#   DEPLOY_HOST  target host (deploy/deploy.env); HOST overrides it,
#                and a positional argument overrides both
#   INSTALL_DIR  remote install root (default /opt/logwatch-ai)
#   LOCK_FILE    target-side only — no script forwards it; the lock is
#                discovered from the crontab and the deployed runner
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
  < <(echo '{'
       remote_env INSTALL_DIR "$INSTALL_DIR" DO_BIN "$DO_BIN" DO_RUNNER "$DO_RUNNER" \
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
        echo "  Either no deploy has run since these scripts were installed, or a" >&2
        echo "  rollback has already consumed the record. Available artifacts:" >&2
        ls -1t ./logwatch-analyzer-* 2>/dev/null | sed 's|^|    |' >&2
        echo "  Re-point by hand: ln -sfn $INSTALL_DIR/<artifact> ./logwatch-analyzer.rb \\" >&2
        echo "                    && mv -Tf ./logwatch-analyzer.rb ./logwatch-analyzer" >&2
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
    # Consume the record. Leaving it in place made a second rollback a silent
    # no-op: it re-pointed the symlink at the target it already had and still
    # printed "rolled back". We do not know this target's own predecessor, so
    # the honest state is "no recorded target" — the guard above then tells
    # the operator to choose an artifact explicitly.
    rm -f ./.logwatch-analyzer.prev-target
    echo "binary rolled back:"
    echo "  symlink now -> $prev_target"
    echo "  rolled away from $failed_target (kept for inspection)"
    echo "  rollback record consumed; a further rollback needs an explicit target"
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
    # The restored runner may pin a different lock than the one currently
    # deployed; cron could enter through it the moment it lands — worse with
    # --runner --db, where the database is being swapped at the same time.
    incoming_lock=$(lock_path_of ./run-cron.sh.prev)
    if [ -n "$incoming_lock" ] && [ "$incoming_lock" != "$(resolve_lock_file)" ]; then
        acquire_extra_lock "$incoming_lock" || exit 1
    fi
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
    # Stage, then rename. Two moves left run-cron.sh absent in between, and
    # every cron invocation in that window fails until someone repairs it.
    # A failed deployment may also have left no live runner — precisely when
    # --runner is needed — so keeping the .failed copy is conditional.
    [ -e ./run-cron.sh ] && cp -p ./run-cron.sh ./run-cron.sh.failed
    cp -p ./run-cron.sh.prev ./run-cron.sh.restoring.$$
    mv -f ./run-cron.sh.restoring.$$ ./run-cron.sh
    rm -f ./run-cron.sh.prev
    echo "runner rolled back, syntax OK"
    grep -n 'LOCK_FILE=' ./run-cron.sh \
        || echo "WARN: restored runner pins no LOCK_FILE — it will use the built-in default"
fi

if [ "$DO_SCRIPTS" = 1 ]; then
    restored=0
    for f in $TRACKED; do
        [ -f "./scripts/$f.prev" ] || continue
        # Stage, then rename — the same shape the runner restore uses. Two
        # moves left the helper ABSENT between them, so a failure on the
        # second (EACCES, ENOSPC, EIO) deleted it outright and errexit then
        # aborted the loop, leaving the remaining helpers unrestored too.
        #
        # .prev was made with `cp -p`, so it already carries the mode the file
        # had when the deploy replaced it. Re-applying the CURRENT file's mode
        # would faithfully restore an operator's later chmod — e.g. a
        # helper.sh loosened from 0640 to 0644 — which is the opposite of a
        # rollback.
        if ! cp -p "./scripts/$f.prev" "./scripts/$f.restoring.$$"; then
            echo "  WARN: could not stage scripts/$f — left as deployed" >&2
            rm -f "./scripts/$f.restoring.$$"
            continue
        fi
        [ -e "./scripts/$f" ] && cp -p "./scripts/$f" "./scripts/$f.failed"
        mv -f "./scripts/$f.restoring.$$" "./scripts/$f"
        rm -f "./scripts/$f.prev"
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
    db_rc=0; db=$(resolve_db) || db_rc=$?
    if [ "$db_rc" -eq 2 ]; then
        # resolve_db has already explained the runtime override. Restoring the
        # .env database would put back one the analyzer does not read.
        exit 1
    elif [ "$db_rc" -ne 0 ]; then
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
        # shellcheck disable=SC2010 # mtime order is the point; the names are ours
        backup=$(ls -1t "$db".pre-* 2>/dev/null | grep -vE -- '-(wal|shm|journal)$' | head -1 || true)
    fi
    if [ -z "$backup" ]; then
        echo "error: no $(basename "$db").pre-* backup found beside $db" >&2
        exit 1
    fi
    # A symlinked DATABASE_PATH would be REPLACED by the rename below, so the
    # analyzer would then write beside the link instead of to its target,
    # silently splitting future data from the configured storage. Refuse
    # rather than guess which the operator meant.
    if [ -L "$db" ]; then
        echo "error: $db is a symlink to $(readlink -f "$db")." >&2
        echo "       Restoring would replace the link with a regular file." >&2
        echo "       Restore to the link's target by hand, or point" >&2
        echo "       DATABASE_PATH at the real path." >&2
        exit 1
    fi

    # Keep absolute paths. Changing into the database directory and reducing
    # both to basenames breaks when DATABASE_PATH has moved since the
    # snapshot was recorded: the backup would be looked for beside the new
    # database, aborting a valid rollback or restoring a same-named stranger.
    if [ "$(dirname "$backup")" != "$(dirname "$db")" ]; then
        echo "error: recorded snapshot $backup is not beside the configured" >&2
        echo "       database $db — DATABASE_PATH appears to have changed." >&2
        echo "       Restore it by hand, or point DATABASE_PATH back." >&2
        exit 1
    fi
    # Refuse to discard data the snapshot predates. Re-running --db days later
    # ("did that actually take?") would otherwise revert the live database a
    # second time and drop every row written since, reporting success.
    if [ -f "$db" ] && [ "$db" -nt "$backup" ] && [ "${FORCE:-0}" != 1 ]; then
        echo "error: the live database is NEWER than $backup." >&2
        echo "       Restoring would discard everything written since that" >&2
        echo "       snapshot. If that is genuinely what you want, re-run with" >&2
        echo "       FORCE=1. Nothing has been changed." >&2
        exit 1
    fi
    echo "restoring from $backup"
    # Move the ENTIRE suspect state aside, sidecars included. Leaving a stale
    # -wal/-shm/-journal beside the restored snapshot lets SQLite replay those
    # pages into it on the next open, silently reconstructing data from the
    # database we are trying to abandon — or corrupting the restore outright.
    # Stage the restore FIRST. Moving the live database aside before the copy
    # means a full filesystem or an unreadable backup leaves no database at
    # the configured path at all, and the next analyzer run would create an
    # empty one — losing the records this rollback was meant to protect.
    staged="$db.restoring.$$"
    rm -f "$staged" "$staged"-wal "$staged"-shm "$staged"-journal
    if ! cp -p "$backup" "$staged"; then
        echo "error: could not stage $backup — the live database is untouched." >&2
        rm -f "$staged"
        exit 1
    fi
    for ext in -wal -shm -journal; do
        [ -f "$backup$ext" ] && cp -p "$backup$ext" "$staged$ext"
    done
    if command -v sqlite3 >/dev/null 2>&1; then
        if ! sqlite3 -readonly "$staged" 'select count(*) from summaries;' >/dev/null 2>&1; then
            echo "error: staged restore is not a readable database — aborting." >&2
            echo "       The live database is untouched." >&2
            rm -f "$staged" "$staged"-wal "$staged"-shm "$staged"-journal
            exit 1
        fi
        # Fold any WAL into the main file so the publish below is a single
        # rename. Otherwise the main file becomes live before its WAL is
        # moved, and an interruption in between drops rows that exist only in
        # that WAL — rows the validation above just confirmed were there.
        # wal_checkpoint reports completion in its result row (busy, log,
        # checkpointed); a failure that is discarded here would let the WAL be
        # deleted while its frames are still missing from the main file. The
        # validation below cannot catch that, because it reads main+WAL
        # together and would succeed either way.
        ck=$(sqlite3 "$staged" 'PRAGMA wal_checkpoint(TRUNCATE);' 2>&1) || {
            echo "error: WAL checkpoint failed on the staged restore: $ck" >&2
            echo "       The live database is untouched." >&2
            rm -f "$staged" "$staged"-wal "$staged"-shm "$staged"-journal
            exit 1
        }
        if [ "${ck%%|*}" != "0" ]; then
            echo "error: WAL checkpoint did not complete (result: $ck)." >&2
            echo "       The live database is untouched." >&2
            rm -f "$staged" "$staged"-wal "$staged"-shm "$staged"-journal
            exit 1
        fi
        if [ -s "$staged-wal" ]; then
            echo "error: WAL still holds frames after a TRUNCATE checkpoint." >&2
            rm -f "$staged" "$staged"-wal "$staged"-shm "$staged"-journal
            exit 1
        fi
        if ! sqlite3 -readonly "$staged" 'select count(*) from summaries;' >/dev/null 2>&1; then
            echo "error: staged restore became unreadable after checkpoint — aborting." >&2
            rm -f "$staged" "$staged"-wal "$staged"-shm "$staged"-journal
            exit 1
        fi
        rm -f "$staged"-wal "$staged"-shm "$staged"-journal
    elif [ -f "$backup-wal" ] || [ -f "$backup-journal" ]; then
        # Without sqlite3 the WAL cannot be folded in, and publishing a
        # main/sidecar pair in two steps can leave a mismatched set that
        # SQLite may replay or reject. Refuse rather than risk it. Note the
        # LIVE database's own WAL is handled separately below, where sqlite3
        # is likewise required.
        echo "error: $backup carries a WAL/journal and sqlite3 is not available" >&2
        echo "       to consolidate it. Install sqlite3 and retry, or restore" >&2
        echo "       the snapshot set by hand." >&2
        rm -f "$staged" "$staged"-wal "$staged"-shm "$staged"-journal
        exit 1
    fi

    # A fixed .suspect name would destroy the evidence of an earlier failed
    # rollback, which this script promises to retain.
    suspect="$db.suspect-$(date -u +%Y%m%dT%H%M%SZ)"
    # Hard-link the MAIN file aside rather than moving it: a move empties the
    # live path, and an interruption before the rename below would leave no
    # database at all, so the next analyzer run would create an empty one. A
    # link keeps both names on the same inode until the rename replaces the
    # live one. The sidecars ARE moved — safe only because the checkpoint
    # above folded them into the main file, so they hold nothing unique.
    # Fold the LIVE database's own WAL in before touching anything, so the
    # sidecars can be removed without the old main file ever being left
    # incomplete. Removing them first would mean an interruption before the
    # rename releases the lock with a live database missing rows that existed
    # only in its WAL.
    if [ -f "$db-wal" ] || [ -f "$db-journal" ]; then
        # As with the staged copy: sqlite3 can exit 0 while reporting a busy
        # checkpoint in the first result column. Unlinking the live WAL on
        # that basis would leave the configured database missing committed
        # frames if the rename below never happened.
        ck=$(sqlite3 "$db" 'PRAGMA wal_checkpoint(TRUNCATE);' 2>&1) || {
            echo "error: could not checkpoint the live database before replacing it: $ck" >&2
            echo "       Nothing has been changed." >&2
            rm -f "$staged"; exit 1
        }
        if [ "${ck%%|*}" != "0" ]; then
            echo "error: live checkpoint did not complete (result: $ck)." >&2
            echo "       Another connection may hold the database. Nothing changed." >&2
            rm -f "$staged"; exit 1
        fi
        if [ -s "$db-wal" ]; then
            echo "error: live WAL still holds frames after a TRUNCATE checkpoint." >&2
            echo "       Nothing has been changed." >&2
            rm -f "$staged"; exit 1
        fi
    fi
    [ -f "$db" ] && ln -f "$db" "$suspect"
    for ext in -wal -shm -journal; do
        [ -f "$db$ext" ] && ln -f "$db$ext" "$suspect$ext" && rm -f "$db$ext"
    done
    # Both sides are now single consolidated files, so the publish is one
    # atomic rename over a live path that never went missing.
    mv -f "$staged" "$db"
    # Consume the marker: it names a snapshot that has now been restored, and
    # leaving it would let a repeat run pick the same one again.
    rm -f "$INSTALL_DIR/.logwatch-analyzer.db-snapshot"
    echo "  restored; previous state kept as $(basename "$suspect")"
    ls -l "$db"*
fi
__REMOTE_ROLLBACK__
      echo '}'
)

cat <<EOF

==> Rollback complete on ${HOST}.

Nothing was deleted: the binary you rolled away from is still on disk
under its own version name, the runner is kept as run-cron.sh.failed, and
a replaced database is kept as <db>.suspect-<timestamp>. Fix the source, then
re-run ./deploy/deploy.sh.

If the runner was rolled back because /run turned out not to be writable,
prefer keeping the /run lock path and pinning it in the crontab line
instead of reverting the script:

  7 2 * * * LOCK_FILE=${INSTALL_DIR}/.cron.lock ${INSTALL_DIR}/run-cron.sh >> ${INSTALL_DIR}/logs/cron.log 2>&1

${INSTALL_DIR} is root-owned 0750, so that path is still not world-writable.
EOF
