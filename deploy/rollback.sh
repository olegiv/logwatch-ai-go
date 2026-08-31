#!/usr/bin/env bash
#
# Revert the binary to the target recorded by the last deploy.
#
#   ./deploy/rollback.sh                         # uses deploy.env/defaults
#   ./deploy/rollback.sh <host>                  # override the host
#   ./deploy/rollback.sh --install-dir <path>    # override the install root
#   ./deploy/rollback.sh --lock-file <path>      # override the cron lock
#   FORCE=1 ./deploy/rollback.sh                 # override lock protection
#
# FORCE matters here: the case that most needs a rollback is a wedged run,
# and a wedged run is holding that lock. Its use is always reported.
#
# The symlink is re-pointed at the recorded target; nothing is deleted. The
# binary rolled away from keeps its own versioned name.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$SCRIPT_DIR/lib.sh"

HOST_ARG=""
INSTALL_DIR_ARG=""
LOCK_FILE_ARG=""
while (($# > 0)); do
  case "$1" in
    --install-dir)
      (($# >= 2)) || { echo "error: --install-dir requires a path" >&2; exit 2; }
      INSTALL_DIR_ARG=$2
      shift 2
      ;;
    --lock-file)
      (($# >= 2)) || { echo "error: --lock-file requires a path" >&2; exit 2; }
      LOCK_FILE_ARG=$2
      shift 2
      ;;
    -*)
      echo "error: unknown flag $1" >&2
      exit 2
      ;;
    *)
      [[ -z $HOST_ARG ]] || { echo "error: two hosts given" >&2; exit 2; }
      HOST_ARG=$1
      shift
      ;;
  esac
done

HOST=$(resolve_host "$HOST_ARG") || exit 1
require_root_target "$HOST" || exit 1
INSTALL_DIR="${INSTALL_DIR_ARG:-${INSTALL_DIR:-/opt/logwatch-ai}}"
LOCK_FILE="${LOCK_FILE_ARG:-${LOCK_FILE:-/run/logwatch-ai-cron.lock}}"
FORCE_VALUE="${FORCE:-0}"
valid_absolute_path "$INSTALL_DIR" || {
  echo "error: INSTALL_DIR must be an absolute shell-safe path: '$INSTALL_DIR'" >&2; exit 1; }
valid_absolute_path "$LOCK_FILE" || {
  echo "error: LOCK_FILE must be an absolute shell-safe path: '$LOCK_FILE'" >&2; exit 1; }
[[ $LOCK_FILE != */ ]] || { echo "error: LOCK_FILE must name a file" >&2; exit 1; }
valid_boolean "$FORCE_VALUE" || { echo "error: FORCE must be 0 or 1" >&2; exit 1; }

echo "==> Rolling back on $HOST:$INSTALL_DIR"
# Values in this command are constrained to the same grammar used by deploy.sh.
# shellcheck disable=SC2029 # client-side expansion is allowlisted above
ssh "$HOST" \
  "INSTALL_DIR=$INSTALL_DIR LOCK_FILE=$LOCK_FILE FORCE=$FORCE_VALUE bash -s" \
  < "$SCRIPT_DIR/remote-rollback.sh"
