#!/usr/bin/env bash
# Atomically restore the rollback target on the Linux deployment target.

set -euo pipefail
umask 027

: "${INSTALL_DIR:?INSTALL_DIR is required}"
: "${LOCK_FILE:?LOCK_FILE is required}"
: "${FORCE:?FORCE is required}"

[[ $FORCE == 0 || $FORCE == 1 ]] || {
    echo "error: FORCE must be 0 or 1" >&2
    exit 1
}

stat_owner_mode() {
    if stat -c '%u %a' "$1" >/dev/null 2>&1; then
        stat -c '%u %a' "$1"
    else
        stat -f '%u %Lp' "$1"
    fi
}

validate_trusted_directory_chain() {
    local path=$1 label=$2 component current='' owner mode mode_value
    local -a components

    [[ $path == /* ]] || { echo "error: $label must be absolute: $path" >&2; return 1; }
    IFS=/ read -r -a components <<< "${path#/}"
    for component in "${components[@]}"; do
        [[ -n $component ]] || continue
        current="${current%/}/$component"
        [[ -e $current || -L $current ]] || {
            echo "error: $label component does not exist: $current" >&2
            return 1
        }
        [[ ! -L $current && -d $current ]] || {
            echo "error: $label component must be a real directory: $current" >&2
            return 1
        }
        read -r owner mode < <(stat_owner_mode "$current") || return 1
        [[ $owner == 0 || $owner == $(id -u) ]] || {
            echo "error: $label has an ancestor owned by untrusted uid $owner: $current" >&2
            return 1
        }
        mode_value=$((8#$mode))
        if (( (mode_value & 8#022) != 0 && (mode_value & 8#1000) == 0 )); then
            echo "error: $label has a non-sticky writable ancestor: $current" >&2
            return 1
        fi
    done
}

validate_owned_path() {
    local path=$1
    local kind=$2
    local owner mode mode_value

    read -r owner mode < <(stat_owner_mode "$path") || return 1
    if [[ $owner != $(id -u) ]]; then
        echo "ABORT: $kind must be owned by uid $(id -u): $path" >&2
        return 1
    fi
    mode_value=$((8#$mode))
    if (( (mode_value & 8#022) != 0 )); then
        echo "ABORT: $kind must not be group/world-writable: $path" >&2
        return 1
    fi
}

validate_managed_artifact() {
    local path=$1
    local owner mode mode_value

    case "$path" in
        "$INSTALL_DIR"/logwatch-analyzer-*) ;;
        *) echo "error: rollback target is outside $INSTALL_DIR: $path" >&2; return 1 ;;
    esac
    [[ ! -L $path && -f $path && -x $path ]] || {
        echo "error: rollback target is not a regular executable: $path" >&2
        return 1
    }
    read -r owner mode < <(stat_owner_mode "$path") || return 1
    [[ $owner == $(id -u) ]] || {
        echo "error: rollback target is not owned by uid $(id -u): $path" >&2
        return 1
    }
    mode_value=$((8#$mode))
    (( (mode_value & 8#022) == 0 )) || {
        echo "error: rollback target is group/world-writable: $path" >&2
        return 1
    }
}

validate_trusted_directory_chain "$INSTALL_DIR" "install directory" || exit 1
cd "$INSTALL_DIR"
INSTALL_DIR=$(pwd -P)
validate_owned_path "$INSTALL_DIR" "install directory" || exit 1
lock_dir=${LOCK_FILE%/*}
lock_name=${LOCK_FILE##*/}
[[ -n $lock_dir ]] || lock_dir=/
[[ -n $lock_name ]] || { echo "error: LOCK_FILE must name a file" >&2; exit 1; }
validate_trusted_directory_chain "$lock_dir" "lock directory" || exit 1
lock_dir=$(cd "$lock_dir" && pwd -P) || {
    echo "error: lock directory does not exist: ${LOCK_FILE%/*}" >&2
    exit 1
}
LOCK_FILE="$lock_dir/$lock_name"
validate_owned_path "$lock_dir" "lock directory" || exit 1
if [[ -L $LOCK_FILE || ( -e $LOCK_FILE && ! -f $LOCK_FILE ) ]]; then
    echo "ABORT: lock path must be a regular file, not a symlink: $LOCK_FILE" >&2
    exit 1
fi
[[ ! -e $LOCK_FILE ]] || validate_owned_path "$LOCK_FILE" "lock file" || exit 1

if command -v flock >/dev/null 2>&1; then
    exec 9>>"$LOCK_FILE"
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
journal=./.logwatch-analyzer.deploy-transaction
if [[ -L $record || ( -e $record && ! -f $record ) ]]; then
    echo "error: rollback record must be a regular file" >&2
    exit 1
fi
[[ ! -e $record ]] || validate_owned_path "$record" "rollback record" || exit 1
if [[ -L $journal || ( -e $journal && ! -f $journal ) ]]; then
    echo "error: deployment transaction must be a regular file" >&2
    exit 1
fi
[[ ! -e $journal ]] || validate_owned_path "$journal" "deployment transaction" || exit 1

rollback_link=""
rollback_record_restore=""
cleanup_rollback_temps() {
    if [[ -n $rollback_link ]]; then
        rm -f -- "$rollback_link" || {
            echo "WARN: remove temporary rollback link manually: $rollback_link" >&2
        }
    fi
    if [[ -n $rollback_record_restore ]]; then
        rm -f -- "$rollback_record_restore" || {
            echo "WARN: remove temporary rollback-record file manually: $rollback_record_restore" >&2
        }
    fi
}
trap cleanup_rollback_temps EXIT

restore_transaction_record() {
    local present=$1
    local value=$2

    if [[ $present == 1 ]]; then
        rollback_record_restore="./.logwatch-analyzer.prev-target.restore.$$"
        if ! printf '%s\n' "$value" > "$rollback_record_restore" \
            || ! chmod 0600 "$rollback_record_restore"; then
            echo "error: could not prepare the previous rollback record" >&2
            return 1
        fi
        if ! mv -Tf "$rollback_record_restore" "$record"; then
            echo "error: could not restore the previous rollback record" >&2
            return 1
        fi
        rollback_record_restore=""
    elif ! rm -f -- "$record"; then
        echo "error: could not remove the newly published rollback record" >&2
        return 1
    fi
}

recover_deploy_transaction() {
    local tx_prev tx_new current recorded="" transaction_line tx_record_present="" tx_record_value=""
    local -a transaction

    [[ -f $journal ]] || return 0
    transaction=()
    while IFS= read -r transaction_line; do
        transaction+=("$transaction_line")
    done < "$journal"
    if [[ (${#transaction[@]} != 2 && ${#transaction[@]} != 4) \
        || ${transaction[0]} != prev=* || ${transaction[1]} != new=* ]]; then
        echo "error: invalid deployment transaction" >&2
        return 1
    fi
    tx_prev=${transaction[0]#prev=}
    tx_new=${transaction[1]#new=}
    if [[ ${#transaction[@]} == 4 ]]; then
        if [[ ${transaction[2]} != record_present=* || ${transaction[3]} != record_value=* ]]; then
            echo "error: invalid rollback metadata in deployment transaction" >&2
            return 1
        fi
        tx_record_present=${transaction[2]#record_present=}
        tx_record_value=${transaction[3]#record_value=}
        if [[ $tx_record_present != 0 && $tx_record_present != 1 ]]; then
            echo "error: invalid rollback-record state in deployment transaction" >&2
            return 1
        fi
        if [[ $tx_record_present == 1 ]]; then
            case "$tx_record_value" in
                "$INSTALL_DIR"/logwatch-analyzer-*) ;;
                *) echo "error: invalid previous rollback target in deployment transaction" >&2; return 1 ;;
            esac
        elif [[ -n $tx_record_value ]]; then
            echo "error: unexpected rollback value for an absent transaction record" >&2
            return 1
        fi
    fi
    validate_managed_artifact "$tx_prev" || return 1
    case "$tx_new" in
        "$INSTALL_DIR"/logwatch-analyzer-*) ;;
        *) echo "error: transaction candidate is outside $INSTALL_DIR: $tx_new" >&2; return 1 ;;
    esac

    current=$(readlink -f ./logwatch-analyzer 2>/dev/null || true)
    [[ -f $record ]] && recorded=$(<"$record")
    if [[ ${#transaction[@]} == 2 && $current == "$tx_new" && $recorded == "$tx_prev" ]]; then
        validate_managed_artifact "$tx_new" || return 1
        "$tx_new" -version >/dev/null 2>&1 || {
            echo "error: committed transaction candidate does not run: $tx_new" >&2
            return 1
        }
        if ! rm -f -- "$journal"; then
            echo "error: could not remove the completed deployment transaction" >&2
            return 1
        fi
        echo "  recovered completed deployment transaction"
        return 0
    fi
    if [[ $current == "$tx_new" ]]; then
        "$tx_prev" -version >/dev/null 2>&1 || {
            echo "error: transaction predecessor does not run: $tx_prev" >&2
            return 1
        }
        rollback_link="./logwatch-analyzer.rollback.$$"
        if ! ln -sfn "$tx_prev" "$rollback_link"; then
            echo "error: could not create the recovery link" >&2
            return 1
        fi
        if ! mv -Tf "$rollback_link" ./logwatch-analyzer; then
            echo "error: could not publish the recovery link; transaction retained" >&2
            rm -f -- "$rollback_link" || echo "WARN: remove temporary recovery link manually: $rollback_link" >&2
            rollback_link=""
            return 1
        fi
        rollback_link=""
        if ! ./logwatch-analyzer -version >/dev/null; then
            echo "error: recovered predecessor failed through the stable link; transaction retained" >&2
            return 1
        fi
        if [[ ${#transaction[@]} == 4 ]] \
            && ! restore_transaction_record "$tx_record_present" "$tx_record_value"; then
            echo "error: live binary was recovered, but rollback history was not" >&2
            return 1
        fi
        if ! rm -f -- "$journal"; then
            echo "error: recovery succeeded but the transaction could not be removed" >&2
            return 1
        fi
        echo "  recovered interrupted deployment by restoring $tx_prev"
        exit 0
    fi
    if [[ $current == "$tx_prev" ]]; then
        if [[ ${#transaction[@]} == 4 ]] \
            && ! restore_transaction_record "$tx_record_present" "$tx_record_value"; then
            echo "error: live binary was already reverted, but rollback history was not" >&2
            return 1
        fi
        if ! rm -f -- "$journal"; then
            echo "error: could not remove the reverted deployment transaction" >&2
            return 1
        fi
        echo "  cleared reverted deployment transaction"
        return 0
    fi

    echo "error: deployment transaction does not match the live target: $current" >&2
    return 1
}

recover_deploy_transaction || exit 1

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
prev=$canonical_prev
validate_managed_artifact "$prev" || exit 1
"$prev" -version >/dev/null 2>&1 || {
    echo "error: $prev does not run — refusing to switch to it." >&2
    echo "       The current binary is untouched." >&2
    exit 1
}

failed=$(readlink -f ./logwatch-analyzer 2>/dev/null || true)
if [[ -z $failed && -L ./logwatch-analyzer ]]; then
    failed=$(readlink ./logwatch-analyzer 2>/dev/null || true)
fi
[[ -n $failed ]] || failed="$INSTALL_DIR/logwatch-analyzer (unresolved)"

rollback_link="./logwatch-analyzer.rollback.$$"

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
        rollback_link="./logwatch-analyzer.restore.$$"
        if ln -sfn "$failed" "$rollback_link" && mv -Tf "$rollback_link" ./logwatch-analyzer; then
            rollback_link=""
            if ./logwatch-analyzer -version >&2; then
                echo "restored the original live target: $failed" >&2
            else
                echo "CRITICAL: the original target also failed through the stable symlink: $failed" >&2
            fi
        else
            echo "CRITICAL: could not restore the original live target: $failed" >&2
        fi
    fi
    exit 1
fi

record_consumed=1
if ! rm -f -- "$record"; then
    record_consumed=0
    echo "WARN: rollback succeeded, but its record could not be consumed; a repeated rollback is safe" >&2
fi
echo "  rolled away from $failed (kept when resolvable)"
if [[ $record_consumed == 1 ]]; then
    echo "  record consumed; a further rollback needs an explicit target"
else
    echo "  record retained; a repeated rollback is safe"
fi
