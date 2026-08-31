#!/usr/bin/env bash
#
# Build the analyzer and install it on the production host.
#
#   ./deploy/deploy.sh                 # build the current worktree (must be clean)
#   REF=v0.15.0 ./deploy/deploy.sh     # build that tag from a throwaway worktree
#   ./deploy/deploy.sh --stage-only    # build and verify on the host, install nothing
#   ./deploy/deploy.sh <host>          # override DEPLOY_HOST
#   SKIP_TESTS=1 …                     # skip `make check`
#   FORCE=1 …                          # proceed even if the cron lock is held
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
# the host-arch binary name, replaces scripts/ wholesale, and finishes with a
# recursive chown across .env and data/summaries.db.

set -euo pipefail

# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

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

WORKTREE=""; STAGE_DIR=""
cleanup() {
  local rc=$?
  [[ -n $WORKTREE ]] && { git -C "$REPO_ROOT" worktree remove --force "$WORKTREE" 2>/dev/null \
      || echo "WARN: remove worktree $WORKTREE by hand" >&2; }
  if [[ -n $STAGE_DIR ]] && valid_stage_dir "$STAGE_DIR"; then
    # shellcheck disable=SC2029 # expanded client-side, validated above
    ssh -n "$HOST" "rm -rf -- '$STAGE_DIR'" 2>/dev/null \
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
  [[ -z "$(git -C "$SRC" status --porcelain)" ]] || {
    echo "error: worktree is dirty; commit, or build a tag with REF=" >&2; exit 1; }
fi

VERSION=$(git -C "$SRC" describe --tags --always --dirty)
valid_version "$VERSION" || { echo "error: unsafe version '$VERSION'" >&2; exit 1; }
LOCAL_BIN="$SRC/bin/logwatch-analyzer-linux-amd64"
REMOTE_BIN="logwatch-analyzer-$VERSION"

echo "==> Deploying $VERSION to $HOST:$INSTALL_DIR"
[[ ${SKIP_TESTS:-0} == 1 ]] || make -C "$SRC" check
make -C "$SRC" build-linux-amd64

# Read the build info once and fail closed: inside an `if` condition a failing
# pipeline is not an error, which would silently disarm this check.
buildinfo=$(go version -m "$LOCAL_BIN") || {
  echo "error: cannot read build info from $LOCAL_BIN" >&2; exit 1; }
grep -q 'vcs.modified=true' <<<"$buildinfo" && {
  echo "error: binary stamped vcs.modified=true — not a clean release build" >&2; exit 1; }
BIN_SHA=$(shasum -a 256 "$LOCAL_BIN" | awk '{print $1}')
echo "    $(grep -m1 '	mod	' <<<"$buildinfo" | awk '{print $2, $3}')  sha256 ${BIN_SHA:0:16}…"

# ---------------------------------------------------------------- 2. stage
# A root-owned mktemp directory, not a predictable name: /tmp is world
# writable, and scp follows and truncates a symlink at the destination.
STAGE_DIR=$(ssh "$HOST" 'd=$(mktemp -d /tmp/logwatch-deploy.XXXXXXXXXX) && chmod 700 "$d" && printf %s "$d"')
valid_stage_dir "$STAGE_DIR" || { echo "error: bad staging path from host: '$STAGE_DIR'" >&2; exit 1; }
scp -q "$LOCAL_BIN" "$HOST:$STAGE_DIR/logwatch-analyzer"

echo "==> Verifying on the host"
ssh "$HOST" BIN_SHA="$BIN_SHA" STAGE_DIR="$STAGE_DIR" bash <<'__VERIFY__'
set -euo pipefail
[ "$(sha256sum "$STAGE_DIR/logwatch-analyzer" | awk '{print $1}')" = "$BIN_SHA" ] \
  || { echo "error: checksum mismatch after transfer" >&2; exit 1; }
chmod 0755 "$STAGE_DIR/logwatch-analyzer"
# Run it here: this catches a GOAMD64/SIGILL mismatch while /opt is untouched.
"$STAGE_DIR/logwatch-analyzer" -version | head -1
__VERIFY__

[[ $STAGE_ONLY == 1 ]] && { echo "--stage-only: nothing installed."; exit 0; }

# ----------------------------------------------------------------- 3. swap
echo "==> Installing"
ssh "$HOST" INSTALL_DIR="$INSTALL_DIR" STAGE_DIR="$STAGE_DIR" REMOTE_BIN="$REMOTE_BIN" \
            LOCK_FILE="$LOCK_FILE" FORCE="${FORCE:-0}" bash <<'__INSTALL__'
set -euo pipefail
cd "$INSTALL_DIR"

# Hold the runner's own lock for the whole swap, so cron cannot fire into it.
if command -v flock >/dev/null 2>&1; then
    exec 9>"$LOCK_FILE"
    flock -n 9 || [ "$FORCE" = 1 ] || {
        echo "ABORT: $LOCK_FILE is held — a run is in progress (FORCE=1 overrides)" >&2; exit 1; }
fi

[ -e ./logwatch-analyzer ] || {
    echo "ABORT: no install at $INSTALL_DIR — use scripts/install.sh to bootstrap" >&2; exit 1; }

stamp=$(date -u +%Y%m%dT%H%M%SZ)
install -m 0755 -o root -g root "$STAGE_DIR/logwatch-analyzer" "./$REMOTE_BIN.incoming.$$"

if [ -L ./logwatch-analyzer ]; then
    prev=$(readlink -f ./logwatch-analyzer)
else
    # Pre-dates this tooling. Hard-link rather than move, so the stable path
    # keeps resolving right up to the rename below.
    ln ./logwatch-analyzer "./logwatch-analyzer-legacy-$stamp"
    prev="$INSTALL_DIR/logwatch-analyzer-legacy-$stamp"
fi
# Redeploying the live version: preserve its inode under another name, or
# rollback would point at the artifact this deploy overwrites.
if [ "$prev" = "$INSTALL_DIR/$REMOTE_BIN" ]; then
    ln "./$REMOTE_BIN" "./$REMOTE_BIN.prev-$stamp"
    prev="$INSTALL_DIR/$REMOTE_BIN.prev-$stamp"
fi
"$prev" -version >/dev/null 2>&1 || {
    echo "ABORT: the current rollback target does not run: $prev" >&2
    echo "       Deploying would leave no way back (FORCE=1 overrides)." >&2
    [ "$FORCE" = 1 ] || exit 1; }

mv -Tf "./$REMOTE_BIN.incoming.$$" "./$REMOTE_BIN"
ln -sfn "$INSTALL_DIR/$REMOTE_BIN" ./logwatch-analyzer.new
mv -Tf ./logwatch-analyzer.new ./logwatch-analyzer

# Only now is $prev actually "previous"; writing it earlier would leave the
# record naming the live binary if anything above failed.
printf '%s\n' "$prev" > ./.prev-target.new
mv -f ./.prev-target.new ./.logwatch-analyzer.prev-target

# The one point where a failure leaves production broken, so revert here
# rather than exiting and leaving it to the operator.
if ! ./logwatch-analyzer -version; then
    echo "ABORT: the new binary failed -version after the swap. Reverting." >&2
    ln -sfn "$prev" ./logwatch-analyzer.rb && mv -Tf ./logwatch-analyzer.rb ./logwatch-analyzer
    ./logwatch-analyzer -version >&2 && echo "reverted to $prev" >&2
    exit 1
fi
echo "  rollback target: $prev"
__INSTALL__

cat <<EOF

==> Deployed $VERSION to $HOST
    Roll back:  ./deploy/rollback.sh
EOF
