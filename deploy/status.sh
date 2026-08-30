#!/usr/bin/env bash
#
# One-call status snapshot of the production install: deployed version,
# the crontab entry that drives it, lock file state, and recent run output.
#
# Read-only.
#
# Usage:
#   ./deploy/status.sh                  # uses DEPLOY_HOST from deploy.env
#   ./deploy/status.sh <host>           # explicit (overrides env)

set -euo pipefail

# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
HOST=$(resolve_host "${1:-}") || exit 1

INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"

ssh "$HOST" INSTALL_DIR="$INSTALL_DIR" 'bash -s' <<'__REMOTE_STATUS__'
set -u
echo "==> Version"
"$INSTALL_DIR/logwatch-analyzer" -version 2>&1 || echo "!! -version exited $?"

echo
echo "==> Binary layout and rollback targets"
ls -la "$INSTALL_DIR"/logwatch-analyzer "$INSTALL_DIR"/logwatch-analyzer-* 2>/dev/null
if [ -f "$INSTALL_DIR/.logwatch-analyzer.prev-target" ]; then
    echo "  rollback target: $(cat "$INSTALL_DIR/.logwatch-analyzer.prev-target")"
else
    echo "  rollback target: (none recorded)"
fi
ls -l "$INSTALL_DIR"/run-cron.sh.prev "$INSTALL_DIR"/data/summaries.db.pre-* 2>/dev/null \
      || echo "  (no runner/db backups)"

echo
echo "==> Schedule"
crontab -l -u root 2>/dev/null | grep -iE 'logwatch|@desc' || echo "(no logwatch crontab line)"

echo
echo "==> Lock file"
grep -n 'LOCK_FILE=' "$INSTALL_DIR/run-cron.sh" 2>/dev/null || echo "(run-cron.sh unreadable)"
ls -la /run/logwatch-ai-cron.lock /var/lock/logwatch-ai-cron.lock 2>&1 | grep -v 'No such file' \
      || echo "(no lock file present — normal between runs)"

echo
echo "==> In flight?"
pgrep -a -f 'run-cron\.sh|logwatch-analyzer' 2>/dev/null || echo "(idle)"

echo
echo "==> Last runs (job status lines only; cron.log also holds full analyzer output)"
grep -aE '^\[?[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9:]+ (\[|done|begin)' "$INSTALL_DIR/logs/cron.log" 2>/dev/null \
    | grep -aE '\] (ok|FAILED)|done|begin' | tail -n 22 || tail -n 10 "$INSTALL_DIR/logs/cron.log"

echo
echo "==> Database"
ls -l "$INSTALL_DIR/data/summaries.db" 2>&1
__REMOTE_STATUS__
