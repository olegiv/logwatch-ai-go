#!/usr/bin/env bash
#
# Lint the remote payloads.
#
# Most of this tooling's logic lives inside quoted heredocs (`<<'__REMOTE_X__'`)
# that are streamed to `ssh host bash -s`. shellcheck treats a quoted heredoc
# as data, so `shellcheck -x deploy/*.sh` reports clean while several hundred
# lines of root-executed code are never checked at all.
#
# That matters more than usual here: bash parses a streamed script
# incrementally, so a syntax error part-way down executes everything above it
# first. The payloads are brace-wrapped to make that fail closed, but a syntax
# error should be caught before it ever reaches the target.
#
# This extracts each payload, prepends remote-lib.sh (which every payload is
# shipped with, and which supplies the functions they call), and runs
# `bash -n` plus shellcheck over the result.
#
#   bash deploy/lint-payloads.sh      (or: make lint-payloads)

set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK=$(mktemp -d "${TMPDIR:-/tmp}/logwatch-payloads.XXXXXXXX")
trap 'rm -rf "$WORK"' EXIT

fail=0 checked=0

for src in "$DIR"/deploy.sh "$DIR"/rollback.sh "$DIR"/preflight.sh "$DIR"/status.sh; do
    # Every marker this file opens a heredoc with.
    while IFS= read -r marker; do
        [ -n "$marker" ] || continue
        out="$WORK/$(basename "$src" .sh).$marker.sh"
        {
            echo '#!/usr/bin/env bash'
            # The payload calls these; without them shellcheck reports every
            # one as an unknown command and bash -n cannot see the real shape.
            cat "$DIR/remote-lib.sh"
            # Stand-ins for the variables remote_env injects, so `set -u`
            # analysis is meaningful. Each payload uses only a subset, hence
            # the blanket SC2034 suppression on the block.
            cat <<'DECLS'
# shellcheck disable=SC2034
{
INSTALL_DIR=/opt/logwatch-ai; STAGE_DIR=/tmp/x; REMOTE_BIN=b; VERSION=v0.0.0
INSTALL_SCRIPTS=0; TRACKED="a.sh"; KEEP_VERSIONS=3; FORCE=0; BIN_SHA=x
DO_BIN=0; DO_RUNNER=0; DO_SCRIPTS=0; DO_DB=0; LENIENT=0; OLD_LOCK_FILE=/var/lock/x
}
redact_assignments() { cat; }
DECLS
            awk -v m="$marker" '
                $0 ~ "<<'"'"'"m"'"'"'$" { grab=1; next }
                grab && $0 == m         { grab=0; next }
                grab                    { print }
            ' "$src"
        } > "$out"

        checked=$((checked + 1))
        if ! bash -n "$out"; then
            echo "FAIL (syntax): $(basename "$src") payload $marker" >&2
            fail=1
            continue
        fi
        # SC2154: remote_env-injected vars are declared above, but any the
        # stub misses would be noise rather than a real finding.
        if ! shellcheck -S warning -e SC2154 "$out"; then
            echo "FAIL (shellcheck): $(basename "$src") payload $marker" >&2
            fail=1
        fi
    done < <(grep -oE "<<'(__REMOTE_[A-Z_]+__)'" "$src" | sed "s/<<'//; s/'$//" | sort -u)
done

if [ "$fail" -eq 0 ]; then
    echo "payload lint: $checked payload(s) clean"
else
    echo "payload lint: FAILURES above" >&2
fi
exit "$fail"
