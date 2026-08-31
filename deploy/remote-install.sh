#!/usr/bin/env bash
# Atomically install a staged analyzer on the Linux deployment target.

set -euo pipefail

: "${INSTALL_DIR:?INSTALL_DIR is required}"
: "${STAGE_DIR:?STAGE_DIR is required}"
: "${REMOTE_BIN:?REMOTE_BIN is required}"
: "${LOCK_FILE:?LOCK_FILE is required}"
: "${FORCE:?FORCE is required}"

[[ $FORCE == 0 || $FORCE == 1 ]] || {
    echo "ABORT: FORCE must be 0 or 1" >&2
    exit 1
}

cd "$INSTALL_DIR"
INSTALL_DIR=$(pwd -P)
lock_dir=${LOCK_FILE%/*}
lock_name=${LOCK_FILE##*/}
[[ -n $lock_dir ]] || lock_dir=/
[[ -n $lock_name ]] || { echo "ABORT: LOCK_FILE must name a file" >&2; exit 1; }
lock_dir=$(cd "$lock_dir" && pwd -P) || {
    echo "ABORT: lock directory does not exist: ${LOCK_FILE%/*}" >&2
    exit 1
}
LOCK_FILE="$lock_dir/$lock_name"

if command -v flock >/dev/null 2>&1; then
    exec 9>"$LOCK_FILE"
    if ! flock -n 9; then
        if [[ $FORCE == 1 ]]; then
            echo "WARN: FORCE=1 — deploying while $LOCK_FILE is held" >&2
        else
            echo "ABORT: $LOCK_FILE is held — a run is in progress" >&2
            echo "       Re-run with FORCE=1 only if overriding that protection is intentional." >&2
            exit 1
        fi
    fi
elif [[ $FORCE == 1 ]]; then
    echo "WARN: FORCE=1 — flock(1) is unavailable; deploying without overlap protection" >&2
else
    echo "ABORT: flock(1) is unavailable; refusing an unlocked deployment" >&2
    echo "       Install util-linux, or re-run with FORCE=1 to accept the risk." >&2
    exit 1
fi

if [[ ! -e ./logwatch-analyzer && ! -L ./logwatch-analyzer ]]; then
    echo "ABORT: no install at $INSTALL_DIR — use scripts/install.sh to bootstrap" >&2
    exit 1
fi

record=./.logwatch-analyzer.prev-target
if [[ -d $record ]]; then
    echo "ABORT: rollback-record path is a directory: $INSTALL_DIR/${record#./}" >&2
    echo "       Move or remove that directory before deploying." >&2
    exit 1
fi

stamp=$(date -u +%Y%m%dT%H%M%SZ)
install_name=$REMOTE_BIN
if [[ -e ./$install_name || -L ./$install_name ]]; then
    install_name="$REMOTE_BIN.redeploy-$stamp-$$"
    if [[ -e ./$install_name || -L ./$install_name ]]; then
        echo "ABORT: immutable deployment artifact already exists: $INSTALL_DIR/$install_name" >&2
        exit 1
    fi
fi
incoming="./$install_name.incoming.$$"
next_link="./logwatch-analyzer.new.$$"
revert_link="./logwatch-analyzer.revert.$$"
next_record="./.logwatch-analyzer.prev-target.new.$$"
published_artifact=""

cleanup_install_temps() {
    local temp
    for temp in "$incoming" "$next_link" "$revert_link" "$next_record" "$published_artifact"; do
        [[ -n $temp ]] || continue
        rm -f -- "$temp" || echo "WARN: remove temporary deployment file manually: $temp" >&2
    done
}
trap cleanup_install_temps EXIT

install -m 0755 -o root -g root "$STAGE_DIR/logwatch-analyzer" "$incoming"

if [[ -L ./logwatch-analyzer ]]; then
    if ! prev=$(readlink -f ./logwatch-analyzer); then
        raw_prev=$(readlink ./logwatch-analyzer)
        if [[ $raw_prev == /* ]]; then
            prev=$raw_prev
        else
            prev="$INSTALL_DIR/$raw_prev"
        fi
    fi
else
    # Pre-dates this tooling. Hard-link rather than move, so the stable path
    # keeps resolving right up to the rename below.
    legacy="./logwatch-analyzer-legacy-$stamp-$$"
    ln ./logwatch-analyzer "$legacy"
    prev="$INSTALL_DIR/${legacy#./}"
fi

prev_valid=1
if ! "$prev" -version >/dev/null 2>&1; then
    prev_valid=0
    if [[ $FORCE == 1 ]]; then
        echo "WARN: FORCE=1 — current rollback target does not run: $prev" >&2
        echo "      The previous rollback record will be left unchanged." >&2
    else
        echo "ABORT: the current rollback target does not run: $prev" >&2
        echo "       Fix it, run rollback.sh, or use FORCE=1 to deploy without this safeguard." >&2
        exit 1
    fi
fi

# Prepare the record before touching the live link, but do not publish it until
# both the new binary and the stable symlink have passed their smoke test.
if [[ $prev_valid == 1 ]]; then
    printf '%s\n' "$prev" > "$next_record"
fi

mv -Tf "$incoming" "./$install_name"
incoming=""
published_artifact="./$install_name"
if ! ln -sfn "$INSTALL_DIR/$install_name" "$next_link"; then
    echo "ABORT: could not create the candidate live symlink; current binary is untouched" >&2
    exit 1
fi
if ! mv -Tf "$next_link" ./logwatch-analyzer; then
    echo "ABORT: could not publish the candidate live symlink; current binary is untouched" >&2
    exit 1
fi
next_link=""
published_artifact=""

revert_live_binary() {
    local reason=$1
    echo "ABORT: $reason Reverting to $prev." >&2
    if ! ln -sfn "$prev" "$revert_link"; then
        echo "CRITICAL: could not create the emergency revert link; production is still on the new binary" >&2
        return 1
    fi
    if ! mv -Tf "$revert_link" ./logwatch-analyzer; then
        echo "CRITICAL: could not publish the emergency revert link; production may still be on the new binary" >&2
        return 1
    fi
    revert_link=""
    if ! ./logwatch-analyzer -version >&2; then
        echo "CRITICAL: the reverted binary also failed -version: $prev" >&2
        return 1
    fi
    echo "reverted to $prev" >&2
}

if ! ./logwatch-analyzer -version; then
    if [[ $prev_valid == 1 ]]; then
        revert_live_binary "the new binary failed -version after the swap." || true
    else
        echo "CRITICAL: the new binary failed and FORCE=1 disabled the runnable-predecessor safeguard" >&2
    fi
    exit 1
fi

if [[ $prev_valid == 1 ]]; then
    if ! mv -Tf "$next_record" "$record"; then
        revert_live_binary "the rollback record could not be published." || true
        exit 1
    fi
    next_record=""
    echo "  rollback target: $prev"
else
    echo "  rollback target unchanged: no runnable current binary was available"
fi
