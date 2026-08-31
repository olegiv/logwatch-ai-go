#!/usr/bin/env bash
#
# Revert the binary to the target recorded by the last deploy.
#
#   ./deploy/rollback.sh          # uses DEPLOY_HOST from deploy/deploy.env
#   ./deploy/rollback.sh <host>   # override it
#   FORCE=1 ./deploy/rollback.sh  # proceed even if the cron lock is held
#
# FORCE matters here: the case that most needs a rollback is a wedged run,
# and a wedged run is holding that lock.
#
# The symlink is re-pointed at the recorded target; nothing is deleted. The
# binary rolled away from keeps its own versioned name.

set -euo pipefail

# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

HOST=$(resolve_host "${1:-}") || exit 1
require_root_target "$HOST" || exit 1
INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"
LOCK_FILE="${LOCK_FILE:-/run/logwatch-ai-cron.lock}"

echo "==> Rolling back on $HOST:$INSTALL_DIR"
ssh "$HOST" INSTALL_DIR="$INSTALL_DIR" LOCK_FILE="$LOCK_FILE" FORCE="${FORCE:-0}" bash <<'__ROLLBACK__'
set -euo pipefail
cd "$INSTALL_DIR"

if command -v flock >/dev/null 2>&1; then
    exec 9>"$LOCK_FILE"
    flock -n 9 || [ "$FORCE" = 1 ] || {
        echo "ABORT: $LOCK_FILE is held — a run is in progress (FORCE=1 overrides)" >&2; exit 1; }
fi

if [ ! -f ./.logwatch-analyzer.prev-target ]; then
    echo "error: no recorded rollback target." >&2
    echo "  Either no deploy has run since these scripts were installed, or a" >&2
    echo "  rollback already consumed the record. Available artifacts:" >&2
    ls -1t ./logwatch-analyzer-* 2>/dev/null | sed 's|^|    |' >&2
    echo "  Re-point by hand:" >&2
    echo "    ln -sfn $INSTALL_DIR/<artifact> ./logwatch-analyzer.rb &&" >&2
    echo "    mv -Tf ./logwatch-analyzer.rb ./logwatch-analyzer" >&2
    exit 1
fi

prev=$(cat ./.logwatch-analyzer.prev-target)
# Executable by mode is not the same as runnable: a corrupt or
# wrong-architecture binary passes -x and fails at exec. Find out before
# repointing, not after.
"$prev" -version >/dev/null 2>&1 || {
    echo "error: $prev does not run — refusing to switch to it." >&2
    echo "       The current binary is untouched." >&2; exit 1; }

failed=$(readlink -f ./logwatch-analyzer)
ln -sfn "$prev" ./logwatch-analyzer.rb
mv -Tf ./logwatch-analyzer.rb ./logwatch-analyzer

# Consume the record. Leaving it made a second rollback a silent no-op: it
# re-pointed at the target it already had and still reported success. We do
# not know this target's own predecessor, so "none recorded" is the truth.
rm -f ./.logwatch-analyzer.prev-target

./logwatch-analyzer -version
echo "  rolled away from $failed (kept)"
echo "  record consumed; a further rollback needs an explicit target"
__ROLLBACK__
