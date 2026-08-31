#!/usr/bin/env bash
# Atomically restore the rollback target on the Linux deployment target.

set -euo pipefail

: "${INSTALL_DIR:?INSTALL_DIR is required}"
: "${LOCK_FILE:?LOCK_FILE is required}"
: "${FORCE:?FORCE is required}"

[[ $FORCE == 0 || $FORCE == 1 ]] || {
    echo "error: FORCE must be 0 or 1" >&2
    exit 1
}

cd "$INSTALL_DIR"
INSTALL_DIR=$(pwd -P)
lock_dir=${LOCK_FILE%/*}
lock_name=${LOCK_FILE##*/}
[[ -n $lock_dir ]] || lock_dir=/
[[ -n $lock_name ]] || { echo "error: LOCK_FILE must name a file" >&2; exit 1; }
lock_dir=$(cd "$lock_dir" && pwd -P) || {
    echo "error: lock directory does not exist: ${LOCK_FILE%/*}" >&2
    exit 1
}
LOCK_FILE="$lock_dir/$lock_name"

if command -v flock >/dev/null 2>&1; then
    exec 9>"$LOCK_FILE"
    if ! flock -n 9; then
        if [[ $FORCE == 1 ]]; then
            echo "WARN: FORCE=1 — rolling back while $LOCK_FILE is held" >&2
        else
            echo "ABORT: $LOCK_FILE is held — a run is in progress" >&2
            echo "       Re-run with FORCE=1 only for an intentionally forced recovery." >&2
            exit 1
        fi
    fi
elif [[ $FORCE == 1 ]]; then
    echo "WARN: FORCE=1 — flock(1) is unavailable; rolling back without overlap protection" >&2
else
    echo "ABORT: flock(1) is unavailable; refusing an unlocked rollback" >&2
    echo "       Install util-linux, or re-run with FORCE=1 to accept the risk." >&2
    exit 1
fi

record=./.logwatch-analyzer.prev-target
if [[ ! -f $record ]]; then
    echo "error: no recorded rollback target." >&2
    echo "  Either no deploy has run since these scripts were installed, or a" >&2
    echo "  rollback already consumed the record. Available artifacts:" >&2
    shopt -s nullglob
    artifacts=(./logwatch-analyzer-*)
    shown=0
    for artifact in "${artifacts[@]}"; do
        [[ $artifact == *.incoming.* ]] && continue
        printf '    %s\n' "$artifact" >&2
        shown=1
    done
    [[ $shown == 1 ]] || echo "    (none)" >&2
    echo "  Re-point by hand:" >&2
    echo "    ln -sfn $INSTALL_DIR/<artifact> ./logwatch-analyzer.revert &&" >&2
    echo "    mv -Tf ./logwatch-analyzer.revert ./logwatch-analyzer" >&2
    exit 1
fi

prev=$(<"$record")
if ! canonical_prev=$(readlink -f "$prev"); then
    echo "error: cannot resolve recorded rollback target: $prev" >&2
    exit 1
fi
case "$canonical_prev" in
    "$INSTALL_DIR"/logwatch-analyzer-*) ;;
    *)
        echo "error: recorded rollback target is outside $INSTALL_DIR: $prev" >&2
        exit 1
        ;;
esac
prev=$canonical_prev
"$prev" -version >/dev/null 2>&1 || {
    echo "error: $prev does not run — refusing to switch to it." >&2
    echo "       The current binary is untouched." >&2
    exit 1
}

# This value is diagnostic and, on failure, an optional restoration target. A
# broken live link must never prevent rollback to the already-validated prev.
failed=$(readlink -f ./logwatch-analyzer 2>/dev/null || true)
if [[ -z $failed && -L ./logwatch-analyzer ]]; then
    failed=$(readlink ./logwatch-analyzer 2>/dev/null || true)
fi
[[ -n $failed ]] || failed="$INSTALL_DIR/logwatch-analyzer (unresolved)"

rollback_link="./logwatch-analyzer.rollback.$$"
consumed_record="./.logwatch-analyzer.prev-target.consumed.$$"
restore_record=0
cleanup_rollback_temps() {
    if [[ -n $rollback_link ]]; then
        rm -f -- "$rollback_link" || {
            echo "WARN: remove temporary rollback link manually: $rollback_link" >&2
        }
    fi
    if [[ $restore_record == 1 && -f $consumed_record && ! -e $record ]]; then
        mv -f "$consumed_record" "$record" || {
            echo "CRITICAL: restore the rollback record manually: $consumed_record" >&2
        }
    fi
}
trap cleanup_rollback_temps EXIT

# Hide the record atomically before the swap. Any failure below restores it;
# success consumes the hidden copy so a second rollback cannot silently no-op.
mv -f "$record" "$consumed_record"
restore_record=1
if ! ln -sfn "$prev" "$rollback_link"; then
    echo "error: could not create the rollback link; current binary is untouched" >&2
    exit 1
fi
if ! mv -Tf "$rollback_link" ./logwatch-analyzer; then
    echo "error: could not publish the rollback link; current binary is untouched" >&2
    exit 1
fi
rollback_link=""

if ! ./logwatch-analyzer -version; then
    echo "CRITICAL: rollback target failed through the stable symlink: $prev" >&2
    if [[ $failed == /* && -x $failed ]] && "$failed" -version >/dev/null 2>&1; then
        if ln -sfn "$failed" "$rollback_link" && mv -Tf "$rollback_link" ./logwatch-analyzer; then
            rollback_link=""
            echo "restored the original live target: $failed" >&2
        else
            echo "CRITICAL: could not restore the original live target: $failed" >&2
        fi
    fi
    exit 1
fi

restore_record=0
if ! rm -f -- "$consumed_record"; then
    echo "WARN: rollback succeeded, but remove the consumed record manually: $consumed_record" >&2
fi
echo "  rolled away from $failed (kept when resolvable)"
echo "  record consumed; a further rollback needs an explicit target"
