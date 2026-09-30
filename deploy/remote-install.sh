#!/usr/bin/env bash
# Atomically install a staged analyzer on the Linux deployment target.

set -euo pipefail
umask 027

: "${INSTALL_DIR:?INSTALL_DIR is required}"
: "${STAGE_DIR:?STAGE_DIR is required}"
: "${REMOTE_BIN:?REMOTE_BIN is required}"
: "${LOCK_FILE:?LOCK_FILE is required}"
: "${FORCE:?FORCE is required}"

[[ $FORCE == 0 || $FORCE == 1 ]] || {
    echo "ABORT: FORCE must be 0 or 1" >&2
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

    [[ $path == /* ]] || { echo "ABORT: $label must be absolute: $path" >&2; return 1; }
    IFS=/ read -r -a components <<< "${path#/}"
    for component in "${components[@]}"; do
        [[ -n $component ]] || continue
        current="${current%/}/$component"
        [[ -e $current || -L $current ]] || {
            echo "ABORT: $label component does not exist: $current" >&2
            return 1
        }
        [[ ! -L $current && -d $current ]] || {
            echo "ABORT: $label component must be a real directory: $current" >&2
            return 1
        }
        read -r owner mode < <(stat_owner_mode "$current") || return 1
        [[ $owner == 0 || $owner == $(id -u) ]] || {
            echo "ABORT: $label has an ancestor owned by untrusted uid $owner: $current" >&2
            return 1
        }
        mode_value=$((8#$mode))
        if (( (mode_value & 8#022) != 0 && (mode_value & 8#1000) == 0 )); then
            echo "ABORT: $label has a non-sticky writable ancestor: $current" >&2
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

validate_lock_path() {
    validate_owned_path "$lock_dir" "lock directory" || return 1
    if [[ -L $LOCK_FILE ]]; then
        echo "ABORT: lock file must not be a symlink: $LOCK_FILE" >&2
        return 1
    fi
    if [[ -e $LOCK_FILE ]]; then
        [[ -f $LOCK_FILE ]] || {
            echo "ABORT: lock path must be a regular file: $LOCK_FILE" >&2
            return 1
        }
        validate_owned_path "$LOCK_FILE" "lock file" || return 1
    fi
}

artifact_problem=""
validate_managed_artifact() {
    local path=$1
    local owner mode mode_value

    case "$path" in
        "$INSTALL_DIR"/logwatch-analyzer-*) ;;
        "$INSTALL_DIR"/*)
            artifact_problem="target is not a managed analyzer artifact: $path"
            return 1
            ;;
        *)
            artifact_problem="target is outside $INSTALL_DIR: $path"
            return 1
            ;;
    esac
    if [[ -L $path || ! -f $path || ! -x $path ]]; then
        artifact_problem="target is not a regular executable: $path"
        return 1
    fi
    if ! read -r owner mode < <(stat_owner_mode "$path"); then
        artifact_problem="cannot inspect target ownership: $path"
        return 1
    fi
    if [[ $owner != $(id -u) ]]; then
        artifact_problem="target is not owned by uid $(id -u): $path"
        return 1
    fi
    mode_value=$((8#$mode))
    if (( (mode_value & 8#022) != 0 )); then
        artifact_problem="target is group/world-writable: $path"
        return 1
    fi
}

[[ $REMOTE_BIN =~ ^logwatch-analyzer-[A-Za-z0-9][A-Za-z0-9._+-]*$ ]] || {
    echo "ABORT: REMOTE_BIN is not a safe analyzer artifact name: $REMOTE_BIN" >&2
    exit 1
}
validate_trusted_directory_chain "$STAGE_DIR" "staging directory" || exit 1
STAGE_DIR=$(cd "$STAGE_DIR" && pwd -P) || {
    echo "ABORT: staging directory does not exist: $STAGE_DIR" >&2
    exit 1
}
validate_owned_path "$STAGE_DIR" "staging directory" || exit 1
staged_binary="$STAGE_DIR/logwatch-analyzer"
if [[ -L $staged_binary || ! -f $staged_binary || ! -x $staged_binary ]]; then
    echo "ABORT: staged binary must be a regular executable: $staged_binary" >&2
    exit 1
fi
validate_owned_path "$staged_binary" "staged binary" || exit 1

validate_trusted_directory_chain "$INSTALL_DIR" "install directory" || exit 1
cd "$INSTALL_DIR"
INSTALL_DIR=$(pwd -P)
validate_owned_path "$INSTALL_DIR" "install directory" || exit 1
lock_dir=${LOCK_FILE%/*}
lock_name=${LOCK_FILE##*/}
[[ -n $lock_dir ]] || lock_dir=/
[[ -n $lock_name ]] || { echo "ABORT: LOCK_FILE must name a file" >&2; exit 1; }
validate_trusted_directory_chain "$lock_dir" "lock directory" || exit 1
lock_dir=$(cd "$lock_dir" && pwd -P) || {
    echo "ABORT: lock directory does not exist: ${LOCK_FILE%/*}" >&2
    exit 1
}
LOCK_FILE="$lock_dir/$lock_name"
validate_lock_path || exit 1

if command -v flock >/dev/null 2>&1; then
    exec 9>>"$LOCK_FILE"
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
journal=./.logwatch-analyzer.deploy-transaction
for state_file in "$record" "$journal"; do
    if [[ -L $state_file || ( -e $state_file && ! -f $state_file ) ]]; then
        echo "ABORT: deployment state path must be a regular file: $INSTALL_DIR/${state_file#./}" >&2
        exit 1
    fi
    [[ ! -e $state_file ]] || validate_owned_path "$state_file" "deployment state file" || exit 1
done

recover_link=""
record_restore=""
restore_record_value() {
    local present=$1
    local value=$2

    if [[ $present == 1 ]]; then
        record_restore="./.logwatch-analyzer.prev-target.restore.$$"
        if ! printf '%s\n' "$value" > "$record_restore" || ! chmod 0600 "$record_restore"; then
            echo "CRITICAL: could not prepare the previous rollback record" >&2
            return 1
        fi
        if ! mv -Tf "$record_restore" "$record"; then
            echo "CRITICAL: could not restore the previous rollback record" >&2
            return 1
        fi
        record_restore=""
    elif ! rm -f -- "$record"; then
        echo "CRITICAL: could not remove the newly published rollback record" >&2
        return 1
    fi
}

recover_deploy_transaction() {
    local tx_prev tx_new current recorded="" tx_record_present="" tx_record_value=""
    local -a transaction

    [[ -f $journal ]] || return 0
    validate_owned_path "$journal" "deployment transaction" || return 1
    transaction=()
    while IFS= read -r transaction_line; do
        transaction+=("$transaction_line")
    done < "$journal"
    if [[ (${#transaction[@]} != 2 && ${#transaction[@]} != 4) \
        || ${transaction[0]} != prev=* || ${transaction[1]} != new=* ]]; then
        echo "ABORT: invalid deployment transaction: $INSTALL_DIR/${journal#./}" >&2
        return 1
    fi
    tx_prev=${transaction[0]#prev=}
    tx_new=${transaction[1]#new=}
    if [[ ${#transaction[@]} == 4 ]]; then
        if [[ ${transaction[2]} != record_present=* || ${transaction[3]} != record_value=* ]]; then
            echo "ABORT: invalid rollback metadata in deployment transaction" >&2
            return 1
        fi
        tx_record_present=${transaction[2]#record_present=}
        tx_record_value=${transaction[3]#record_value=}
        if [[ $tx_record_present != 0 && $tx_record_present != 1 ]]; then
            echo "ABORT: invalid rollback-record state in deployment transaction" >&2
            return 1
        fi
        if [[ $tx_record_present == 1 ]]; then
            case "$tx_record_value" in
                "$INSTALL_DIR"/logwatch-analyzer-*) ;;
                *)
                    echo "ABORT: invalid previous rollback target in deployment transaction" >&2
                    return 1
                    ;;
            esac
        elif [[ -n $tx_record_value ]]; then
            echo "ABORT: unexpected rollback value for an absent transaction record" >&2
            return 1
        fi
    fi
    if ! validate_managed_artifact "$tx_prev"; then
        echo "ABORT: invalid transaction predecessor: $artifact_problem" >&2
        return 1
    fi
    case "$tx_new" in
        "$INSTALL_DIR"/logwatch-analyzer-*) ;;
        *)
            echo "ABORT: invalid transaction candidate outside $INSTALL_DIR: $tx_new" >&2
            return 1
            ;;
    esac
    current=$(readlink -f ./logwatch-analyzer 2>/dev/null || true)
    [[ -f $record ]] && recorded=$(<"$record")

    # Legacy two-line journals could only infer commitment from the live link
    # and rollback record. Four-line journals use journal removal as the commit
    # marker, so a surviving journal is always recovered as incomplete.
    if [[ ${#transaction[@]} == 2 && $current == "$tx_new" && $recorded == "$tx_prev" ]]; then
        if ! validate_managed_artifact "$tx_new" || ! "$tx_new" -version >/dev/null 2>&1; then
            echo "ABORT: committed transaction candidate is invalid: $tx_new" >&2
            return 1
        fi
        if ! rm -f -- "$journal"; then
            echo "ABORT: could not remove the completed deployment transaction" >&2
            return 1
        fi
        echo "  recovered completed deployment transaction"
        return 0
    fi
    if [[ $current == "$tx_new" ]]; then
        if ! "$tx_prev" -version >/dev/null 2>&1; then
            echo "ABORT: transaction predecessor does not run: $tx_prev" >&2
            return 1
        fi
        recover_link="./logwatch-analyzer.recover.$$"
        if ! ln -sfn "$tx_prev" "$recover_link"; then
            echo "ABORT: could not create the recovery link" >&2
            return 1
        fi
        if ! mv -Tf "$recover_link" ./logwatch-analyzer; then
            echo "ABORT: could not publish the recovery link; transaction retained" >&2
            rm -f -- "$recover_link" || echo "WARN: remove temporary recovery link manually: $recover_link" >&2
            recover_link=""
            return 1
        fi
        recover_link=""
        if ! ./logwatch-analyzer -version >/dev/null; then
            echo "ABORT: recovered predecessor failed through the stable link; transaction retained" >&2
            return 1
        fi
        if [[ ${#transaction[@]} == 4 ]] \
            && ! restore_record_value "$tx_record_present" "$tx_record_value"; then
            echo "ABORT: live binary was recovered, but rollback history was not" >&2
            return 1
        fi
        if ! rm -f -- "$journal"; then
            echo "ABORT: recovery succeeded but the transaction could not be removed" >&2
            return 1
        fi
        echo "  reverted incomplete deployment transaction to $tx_prev"
        return 0
    fi
    if [[ $current == "$tx_prev" ]]; then
        if [[ ${#transaction[@]} == 4 ]] \
            && ! restore_record_value "$tx_record_present" "$tx_record_value"; then
            echo "ABORT: live binary was already reverted, but rollback history was not" >&2
            return 1
        fi
        if ! rm -f -- "$journal"; then
            echo "ABORT: could not remove the reverted deployment transaction" >&2
            return 1
        fi
        echo "  cleared reverted deployment transaction"
        return 0
    fi

    echo "ABORT: transaction does not match the current live target: $current" >&2
    return 1
}

recover_deploy_transaction || exit 1

if ! "$staged_binary" -check-runtime; then
    echo "ABORT: staged binary failed runtime validation; live binary is untouched" >&2
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
next_journal="./.logwatch-analyzer.deploy-transaction.new.$$"
published_artifact=""
published_new_path=""
prev=""
prev_valid=0
live_published=0
deployment_committed=0
prior_record_present=0
prior_record_value=""

if [[ -f $record ]]; then
    record_lines=()
    while IFS= read -r record_line; do
        record_lines+=("$record_line")
    done < "$record"
    if [[ ${#record_lines[@]} != 1 ]]; then
        echo "ABORT: rollback record must contain exactly one non-empty line" >&2
        exit 1
    fi
    if [[ -z ${record_lines[0]} ]]; then
        echo "ABORT: rollback record must contain exactly one non-empty line" >&2
        exit 1
    fi
    prior_record_present=1
    prior_record_value=${record_lines[0]}
    case "$prior_record_value" in
        "$INSTALL_DIR"/logwatch-analyzer-*) ;;
        *)
            echo "ABORT: rollback record points outside managed analyzer artifacts: $prior_record_value" >&2
            exit 1
            ;;
    esac
fi

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
    live_published=0
    if ! restore_record_value "$prior_record_present" "$prior_record_value"; then
        echo "CRITICAL: live binary was restored, but rollback history was not; transaction retained" >&2
        return 1
    fi
    if ! rm -f -- "$journal"; then
        echo "CRITICAL: live binary was restored, but the recovery transaction remains" >&2
        return 1
    fi
    echo "reverted to $prev" >&2
    return 0
}

cleanup_install_temps() {
    local exit_status=$?
    local current temp

    if [[ $exit_status != 0 && $live_published == 1 && $deployment_committed == 0 && $prev_valid == 1 ]]; then
        current=$(readlink -f ./logwatch-analyzer 2>/dev/null || true)
        if [[ $current == "$published_new_path" ]]; then
            revert_live_binary "deployment was interrupted before commit." || true
        fi
    fi
    if [[ -n $published_artifact && -e $published_artifact \
        && -e ./logwatch-analyzer && ./logwatch-analyzer -ef $published_artifact ]]; then
        published_artifact=""
    fi
    for temp in "$incoming" "$next_link" "$revert_link" "$recover_link" "$next_record" \
        "$next_journal" "$record_restore" "$published_artifact"; do
        [[ -n $temp ]] || continue
        rm -f -- "$temp" || echo "WARN: remove temporary deployment file manually: $temp" >&2
    done
}
trap cleanup_install_temps EXIT

install -m 0755 -o root -g root "$staged_binary" "$incoming"

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
    legacy="./logwatch-analyzer-legacy-$stamp-$$"
    ln ./logwatch-analyzer "$legacy"
    prev="$INSTALL_DIR/${legacy#./}"
fi

prev_valid=1
prev_problem=""
if ! validate_managed_artifact "$prev"; then
    prev_valid=0
    prev_problem="the current rollback $artifact_problem"
elif ! "$prev" -version >/dev/null 2>&1; then
    prev_valid=0
    prev_problem="the current rollback target does not run: $prev"
fi
if [[ $prev_valid == 0 ]]; then
    if [[ $FORCE == 1 ]]; then
        echo "WARN: FORCE=1 — $prev_problem" >&2
        echo "      The previous rollback record will be left unchanged." >&2
    else
        echo "ABORT: $prev_problem" >&2
        echo "       Fix it, run rollback.sh, or use FORCE=1 to deploy without this safeguard." >&2
        exit 1
    fi
fi

if [[ $prev_valid == 1 ]]; then
    printf '%s\n' "$prev" > "$next_record"
fi

mv -Tf "$incoming" "./$install_name"
incoming=""
published_artifact="./$install_name"
published_new_path="$INSTALL_DIR/$install_name"
if ! ln -sfn "$published_new_path" "$next_link"; then
    echo "ABORT: could not create the candidate live symlink; current binary is untouched" >&2
    exit 1
fi

if [[ $prev_valid == 1 ]]; then
    printf 'prev=%s\nnew=%s\nrecord_present=%s\nrecord_value=%s\n' \
        "$prev" "$published_new_path" "$prior_record_present" "$prior_record_value" > "$next_journal"
    chmod 0600 "$next_journal"
    mv -Tf "$next_journal" "$journal"
    next_journal=""
fi

live_published=1
if ! mv -Tf "$next_link" ./logwatch-analyzer; then
    live_published=0
    [[ $prev_valid == 0 ]] || rm -f -- "$journal"
    echo "ABORT: could not publish the candidate live symlink; current binary is untouched" >&2
    exit 1
fi
next_link=""
published_artifact=""

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
    if ! rm -f -- "$journal"; then
        revert_live_binary "the deployment transaction could not be finalized." || true
        exit 1
    fi
    deployment_committed=1
    echo "  rollback target: $prev"
else
    deployment_committed=1
    echo "  rollback target unchanged: no runnable current binary was available"
fi
