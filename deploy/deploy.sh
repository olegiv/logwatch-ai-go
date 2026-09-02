#!/usr/bin/env bash
#
# Build the analyzer and install it on the production host.
#
#   ./deploy/deploy.sh                 # build the current worktree (must be clean)
#   REF=v0.15.0 ./deploy/deploy.sh     # build that tag from a throwaway worktree
#   ./deploy/deploy.sh --stage-only    # build and verify on the host, install nothing
#   ./deploy/deploy.sh <host>          # override DEPLOY_HOST
#   SKIP_TESTS=1 …                     # skip `make check` (reported prominently)
#   FORCE=1 …                          # override lock/predecessor safeguards
#
# Target comes from deploy/deploy.env (DEPLOY_HOST); INSTALL_DIR overrides the
# install root, default /opt/logwatch-ai.
#
# Scope: the binary only. Helper scripts, run-cron.sh, the database and the
# crontab are the operator's; copy them with scp when they change.
#
# Layout on the host: `logwatch-analyzer` is a symlink to a versioned regular
# file. Deploys re-point the symlink; they never replace it.
#
# Why REF: building the worktree stamps the artifact with whatever it
# contains. REF checks the tag out separately so the version ldflags and Go's
# embedded vcs stamp describe the release.
#
# Why not scripts/install.sh: that is a first-time bootstrapper. It looks for
# the host-arch binary name, overwrites repository-managed scripts, and
# finishes with a recursive chown across .env and data/summaries.db.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$SCRIPT_DIR/lib.sh"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

STAGE_ONLY=0; HOST_ARG=""
for arg in "$@"; do
  case "$arg" in
    --stage-only) STAGE_ONLY=1 ;;
    -*) echo "error: unknown flag $arg" >&2; exit 2 ;;
    *)  [[ -n $HOST_ARG ]] && { echo "error: two hosts given" >&2; exit 2; }
        HOST_ARG="$arg" ;;
  esac
done

HOST=$(resolve_host "$HOST_ARG") || exit 1
require_root_target "$HOST" || exit 1
INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"
LOCK_FILE="${LOCK_FILE:-/run/logwatch-ai-cron.lock}"
FORCE_VALUE="${FORCE:-0}"
SKIP_TESTS_VALUE="${SKIP_TESTS:-0}"
valid_absolute_path "$INSTALL_DIR" || {
  echo "error: INSTALL_DIR must be an absolute shell-safe path: '$INSTALL_DIR'" >&2; exit 1; }
valid_absolute_path "$LOCK_FILE" || {
  echo "error: LOCK_FILE must be an absolute shell-safe path: '$LOCK_FILE'" >&2; exit 1; }
[[ $LOCK_FILE != */ ]] || { echo "error: LOCK_FILE must name a file" >&2; exit 1; }
valid_boolean "$FORCE_VALUE" || { echo "error: FORCE must be 0 or 1" >&2; exit 1; }
valid_boolean "$SKIP_TESTS_VALUE" || { echo "error: SKIP_TESTS must be 0 or 1" >&2; exit 1; }

WORKTREE=""; STAGE_DIR=""
cleanup() {
  local rc=$?
  [[ -n $WORKTREE ]] && { git -C "$REPO_ROOT" worktree remove --force "$WORKTREE" \
      || echo "WARN: remove worktree $WORKTREE by hand" >&2; }
  if [[ -n $STAGE_DIR ]] && valid_stage_dir "$STAGE_DIR"; then
    # shellcheck disable=SC2029 # expanded client-side, validated above
    ssh -n -o BatchMode=yes -o ConnectTimeout=10 "$HOST" "rm -rf -- '$STAGE_DIR'" \
      || echo "WARN: remove $STAGE_DIR on $HOST by hand" >&2
  fi
  return "$rc"
}
trap cleanup EXIT

# ---------------------------------------------------------------- 1. build
if [[ -n ${REF:-} ]]; then
  # GNU mktemp needs >=3 X's and rejects a bare -t prefix; this spelling
  # works on both BSD and GNU.
  WORKTREE=$(mktemp -d "${TMPDIR:-/tmp}/logwatch-deploy.XXXXXXXXXX"); rmdir "$WORKTREE"
  git -C "$REPO_ROOT" worktree add --detach --quiet "$WORKTREE" "$REF"
  SRC="$WORKTREE"
else
  SRC="$REPO_ROOT"
  worktree_status=$(git -C "$SRC" status --porcelain) || {
    echo "error: cannot determine whether the worktree is clean" >&2; exit 1; }
  [[ -z $worktree_status ]] || {
    echo "error: worktree is dirty; commit, or build a tag with REF=" >&2; exit 1; }
fi

VERSION=$(git -C "$SRC" describe --tags --always --dirty)
valid_version "$VERSION" || { echo "error: unsafe version '$VERSION'" >&2; exit 1; }
LOCAL_BIN="$SRC/bin/logwatch-analyzer-linux-amd64"
REMOTE_BIN="logwatch-analyzer-$VERSION"

echo "==> Deploying $VERSION to $HOST:$INSTALL_DIR"
if [[ $SKIP_TESTS_VALUE == 1 ]]; then
  echo "WARN: SKIP_TESTS=1 — the local quality gate is disabled for this deployment" >&2
else
  make -C "$SRC" check
fi
make -C "$SRC" build-linux-amd64

# Read the build info once and fail closed: inside an `if` condition a failing
# pipeline is not an error, which would silently disarm this check.
buildinfo=$(go version -m "$LOCAL_BIN") || {
  echo "error: cannot read build info from $LOCAL_BIN" >&2; exit 1; }
grep -q $'\tbuild\tvcs.modified=true$' <<<"$buildinfo" && {
  echo "error: binary stamped vcs.modified=true — not a clean release build" >&2; exit 1; }
grep -q $'\tbuild\tvcs.modified=false$' <<<"$buildinfo" || {
  echo "error: binary has no explicit vcs.modified=false stamp" >&2
  echo "       Refusing a build made with VCS stamping disabled or unavailable." >&2
  exit 1
}
if ! sha_line=$(shasum -a 256 "$LOCAL_BIN"); then
  echo "error: cannot calculate SHA-256 for $LOCAL_BIN" >&2
  exit 1
fi
BIN_SHA=${sha_line%% *}
valid_sha256 "$BIN_SHA" || { echo "error: cannot calculate SHA-256 for $LOCAL_BIN" >&2; exit 1; }
echo "    $(grep -m1 '	mod	' <<<"$buildinfo" | awk '{print $2, $3}')  sha256 ${BIN_SHA:0:16}…"

# ---------------------------------------------------------------- 2. stage
# A root-owned mktemp directory, not a predictable name: /tmp is world
# writable, and scp follows and truncates a symlink at the destination.
STAGE_DIR=$(ssh "$HOST" 'd=$(mktemp -d /tmp/logwatch-deploy.XXXXXXXXXX) && chmod 700 "$d" && printf %s "$d"')
valid_stage_dir "$STAGE_DIR" || { echo "error: bad staging path from host: '$STAGE_DIR'" >&2; exit 1; }
scp -q "$LOCAL_BIN" "$HOST:$STAGE_DIR/logwatch-analyzer"

echo "==> Verifying on the host"
# Values in this remote command are allowlisted above; the staged path and
# digest are additionally validated by remote-verify.sh before use.
# shellcheck disable=SC2029 # client-side expansion is allowlisted above
ssh "$HOST" "BIN_SHA=$BIN_SHA STAGE_DIR=$STAGE_DIR bash -s" < "$SCRIPT_DIR/remote-verify.sh"

[[ $STAGE_ONLY == 1 ]] && { echo "--stage-only: nothing installed."; exit 0; }

# ----------------------------------------------------------------- 3. swap
echo "==> Installing"
# INSTALL_DIR and LOCK_FILE use a strict path grammar, REMOTE_BIN is derived
# from a validated version, STAGE_DIR is validated, and FORCE is 0 or 1.
# shellcheck disable=SC2029 # client-side expansion is allowlisted above
ssh "$HOST" \
  "INSTALL_DIR=$INSTALL_DIR STAGE_DIR=$STAGE_DIR REMOTE_BIN=$REMOTE_BIN LOCK_FILE=$LOCK_FILE FORCE=$FORCE_VALUE bash -s" \
  < "$SCRIPT_DIR/remote-install.sh"

echo
echo "==> Deployed $VERSION to $HOST"
printf '    Roll back:  %s\n' "$(format_rollback_command "$HOST")"
