#!/usr/bin/env bash
# Verify a staged analyzer on the Linux deployment target.

set -euo pipefail

: "${BIN_SHA:?BIN_SHA is required}"
: "${STAGE_DIR:?STAGE_DIR is required}"
: "${INSTALL_DIR:?INSTALL_DIR is required}"

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

[[ $BIN_SHA =~ ^[A-Fa-f0-9]{64}$ ]] || {
    echo "error: invalid expected SHA-256 digest" >&2
    exit 1
}
[[ $STAGE_DIR =~ ^/tmp/logwatch-deploy\.[A-Za-z0-9]+$ ]] || {
    echo "error: invalid staging directory: $STAGE_DIR" >&2
    exit 1
}
[[ $INSTALL_DIR =~ ^/[A-Za-z0-9._+/-]+$ ]] || {
    echo "error: invalid install directory: $INSTALL_DIR" >&2
    exit 1
}
[[ -d $INSTALL_DIR && ! -L $INSTALL_DIR ]] || {
    echo "error: install directory is not a real directory: $INSTALL_DIR" >&2
    exit 1
}
validate_trusted_directory_chain "$INSTALL_DIR" "install directory" || exit 1
validate_trusted_directory_chain "$STAGE_DIR" "staging directory" || exit 1

if ! remote_sum=$(sha256sum -- "$STAGE_DIR/logwatch-analyzer"); then
    echo "error: cannot calculate the staged binary checksum" >&2
    exit 1
fi
remote_sha=${remote_sum%% *}
[[ $remote_sha == "$BIN_SHA" ]] || {
    echo "error: checksum mismatch after transfer" >&2
    exit 1
}

chmod 0755 "$STAGE_DIR/logwatch-analyzer"
if ! version_output=$("$STAGE_DIR/logwatch-analyzer" -version); then
    echo "error: staged binary failed -version" >&2
    exit 1
fi
printf '%s\n' "${version_output%%$'\n'*}"
if ! (cd "$INSTALL_DIR" && "$STAGE_DIR/logwatch-analyzer" -check-runtime); then
    echo "error: staged binary failed runtime validation; production was not changed" >&2
    exit 1
fi
