#!/usr/bin/env bash
# Verify a staged analyzer on the Linux deployment target.

set -euo pipefail

: "${BIN_SHA:?BIN_SHA is required}"
: "${STAGE_DIR:?STAGE_DIR is required}"

[[ $BIN_SHA =~ ^[A-Fa-f0-9]{64}$ ]] || {
    echo "error: invalid expected SHA-256 digest" >&2
    exit 1
}
[[ $STAGE_DIR =~ ^/tmp/logwatch-deploy\.[A-Za-z0-9]+$ ]] || {
    echo "error: invalid staging directory: $STAGE_DIR" >&2
    exit 1
}

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
