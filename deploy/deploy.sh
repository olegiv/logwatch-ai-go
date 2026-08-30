#!/usr/bin/env bash
#
# Build and deploy logwatch-analyzer to the production host.
#
# Usage:
#   ./deploy/deploy.sh                     # uses DEPLOY_HOST from deploy.env
#   ./deploy/deploy.sh <host>              # explicit (overrides env)
#   REF=v0.15.0 ./deploy/deploy.sh         # build that git ref, not the worktree
#   ./deploy/deploy.sh --stage-only        # build + upload to /tmp, touch nothing in /opt
#   SKIP_TESTS=1 ./deploy/deploy.sh        # emergency bypass of make check
#   SKIP_SCRIPTS=1 ./deploy/deploy.sh      # binary only; leave scripts/ and run-cron.sh
#   ASSUME_YES=1 ./deploy/deploy.sh        # answer the scripts prompt automatically
#
# Run ./deploy/preflight.sh FIRST — this script assumes its gates passed.
#
# REF builds from a throwaway git worktree checked out at that ref, so the
# version ldflags and Go's embedded vcs stamp describe the *release*, not
# whatever this working tree happens to contain. That is what lets deploy
# tooling be committed on top of a tag without contaminating the artifact.
#
# Why not scripts/install.sh: that is a first-time installer, not an upgrader.
# It looks for bin/logwatch-analyzer (the host-arch name, so it cannot place a
# cross-built Linux binary), replaces scripts/ wholesale, and finishes with a
# recursive `chown -R $(whoami)` across .env and data/summaries.db.
#
# Why staging + mv rather than scp onto the live path: scp truncates and then
# streams, so a cron fire mid-transfer would exec a partial ELF, and writing
# the running inode in place risks ETXTBSY. Staging to /tmp and finishing with
# a same-filesystem `mv` makes the swap atomic.
#
# The install lays out the binary as a versioned regular file with a stable
# `logwatch-analyzer` symlink pointing at it. The symlink is never replaced,
# only re-pointed, so anything referencing the stable name keeps working.

set -euo pipefail

# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

STAGE_ONLY=0
HOST_ARG=""
for arg in "$@"; do
  case "$arg" in
    --stage-only) STAGE_ONLY=1 ;;
    -*) echo "error: unknown flag $arg" >&2; exit 2 ;;
    *)  HOST_ARG="$arg" ;;
  esac
done

HOST=$(resolve_host "$HOST_ARG") || exit 1
INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"
REF="${REF:-}"

# --------------------------------------------------------- 1. build source
cleanup_worktree() { :; }
if [[ -n $REF ]]; then
  WORKTREE=$(mktemp -d -t logwatch-deploy)
  rmdir "$WORKTREE"                       # git worktree add wants a fresh path
  echo "==> Building from a clean worktree at ${REF}"
  git -C "$REPO_ROOT" worktree add --detach --quiet "$WORKTREE" "$REF"
  cleanup_worktree() {
    git -C "$REPO_ROOT" worktree remove --force "$WORKTREE" 2>/dev/null || true
  }
  trap cleanup_worktree EXIT
  SRC="$WORKTREE"
else
  SRC="$REPO_ROOT"
  if [[ -n "$(git -C "$SRC" status --porcelain)" ]]; then
    echo "error: working tree is dirty — the build would be stamped -dirty." >&2
    echo "       Commit first, or build a tag: REF=v0.15.0 $0" >&2
    git -C "$SRC" status --short >&2
    exit 1
  fi
fi

VERSION="$(git -C "$SRC" describe --tags --always --dirty)"
LOCAL_BIN="$SRC/bin/logwatch-analyzer-linux-amd64"
# The cron runner and the generate-* scripts are host-specific; run-cron.sh is
# gitignored and exists only in the real working tree, never in a worktree
# checked out from a tag. Always take them from the repo root.
LOCAL_RUNNER="$REPO_ROOT/scripts/run-cron.sh"
REMOTE_BIN="logwatch-analyzer-${VERSION}"
STAGE_BIN="/tmp/logwatch-analyzer.${VERSION}"
STAGE_SCRIPTS="/tmp/logwatch-scripts.${VERSION}"

echo "==> Deploying ${VERSION} to ${HOST}:${INSTALL_DIR}"

if [[ ! -f $LOCAL_RUNNER ]]; then
  echo "error: $LOCAL_RUNNER missing. It is gitignored and host-specific;" >&2
  echo "       this machine holds the only copy of the production job list." >&2
  exit 1
fi

# --------------------------------------------------------------- 2. build
if [[ ${SKIP_TESTS:-0} == 1 ]]; then
  echo "==> SKIP_TESTS=1 — skipping make check"
else
  echo "==> make check (fmt-check + vet + lint + test)"
  make -C "$SRC" check
fi

echo "==> make build-linux-amd64"
make -C "$SRC" build-linux-amd64

echo "==> Verifying the artifact"
file "$LOCAL_BIN"
go version -m "$LOCAL_BIN" | grep -E '^[[:space:]]+(mod|build[[:space:]]+(GOOS|GOARCH|GOAMD64|CGO_ENABLED|vcs))' || true

# A dirty stamp means we are about to ship something that is not the tagged
# release. This is the exact failure the whole REF mechanism exists to prevent.
if go version -m "$LOCAL_BIN" | grep -q 'vcs.modified=true'; then
  echo "error: binary stamped vcs.modified=true — not a clean release build" >&2
  exit 1
fi

BIN_SHA=$(shasum -a 256 "$LOCAL_BIN" | awk '{print $1}')
echo "    binary sha256: $BIN_SHA"

# --------------------------------------------------------------- 3. stage
echo "==> Staging to ${HOST}:/tmp (nothing under ${INSTALL_DIR} touched yet)"
scp -q "$LOCAL_BIN" "${HOST}:${STAGE_BIN}"
# shellcheck disable=SC2029 # STAGE_SCRIPTS is deliberately expanded client-side
ssh "$HOST" "rm -rf ${STAGE_SCRIPTS} && mkdir -p ${STAGE_SCRIPTS}"
scp -q "$LOCAL_RUNNER" "$REPO_ROOT/scripts/generate-logwatch.sh" \
       "$REPO_ROOT/scripts/generate-drupal-watchdog.sh" \
       "$REPO_ROOT/scripts/helper.sh" "${HOST}:${STAGE_SCRIPTS}/"

echo "==> Verifying transfer + proving this CPU can execute the binary"
ssh "$HOST" BIN_SHA="$BIN_SHA" STAGE_BIN="$STAGE_BIN" STAGE_SCRIPTS="$STAGE_SCRIPTS" \
    'bash -s' <<'__REMOTE_STAGE__'
set -euo pipefail
got=$(sha256sum "$STAGE_BIN" | awk '{print $1}')
[ "$got" = "$BIN_SHA" ] || { echo "error: binary checksum mismatch" >&2; exit 1; }
echo "  checksum matches"
file "$STAGE_BIN"
chmod 0755 "$STAGE_BIN"
echo "  --- executing the STAGED binary (catches GOAMD64/SIGILL, /opt untouched) ---"
"$STAGE_BIN" -version
for f in "$STAGE_SCRIPTS"/*.sh; do bash -n "$f" || exit 1; done
echo "  all staged scripts pass syntax check"
__REMOTE_STAGE__

if [[ $STAGE_ONLY == 1 ]]; then
  echo
  echo "--stage-only: stopped before touching ${INSTALL_DIR}."
  echo "Staged on ${HOST}: ${STAGE_BIN}, ${STAGE_SCRIPTS}/"
  exit 0
fi

# ------------------------------------------------- 4. db backup + binary swap
echo "==> Backing up the database"
ssh "$HOST" INSTALL_DIR="$INSTALL_DIR" VERSION="$VERSION" 'bash -s' <<'__REMOTE_DBBACKUP__'
set -euo pipefail
db="$INSTALL_DIR/data/summaries.db"
dst="$db.pre-$VERSION"
if command -v sqlite3 >/dev/null 2>&1; then
    sqlite3 "$db" ".backup '$dst'"   # WAL-aware, consistent
else
    cp -p "$db" "$dst"
fi
ls -l "$dst"
__REMOTE_DBBACKUP__

echo "==> Installing the binary"
ssh "$HOST" INSTALL_DIR="$INSTALL_DIR" STAGE_BIN="$STAGE_BIN" REMOTE_BIN="$REMOTE_BIN" \
    'bash -s' <<'__REMOTE_INSTALL__'
set -euo pipefail
cd "$INSTALL_DIR"

# A run may have started while we were staging.
if pgrep -f 'run-cron\.sh|logwatch-analyzer' >/dev/null 2>&1; then
    echo "ABORT: a run is in flight" >&2
    pgrep -a -f 'run-cron\.sh|logwatch-analyzer' >&2
    exit 1
fi

if [ ! -e ./logwatch-analyzer ]; then
    echo "ABORT: nothing installed at $INSTALL_DIR/logwatch-analyzer." >&2
    echo "       This script upgrades an existing install; use scripts/install.sh" >&2
    echo "       for a first-time bootstrap." >&2
    exit 1
fi

prev_target=$(readlink -f ./logwatch-analyzer)
echo "  current: $prev_target"
./logwatch-analyzer -version 2>&1 | head -1 || true

# Record what to roll back to. rollback.sh reads this file rather than
# guessing, so it works no matter how many versions accumulate here.
printf '%s\n' "$prev_target" > ./.logwatch-analyzer.prev-target

# install(1) writes a fresh inode; the symlink swap below is a rename, so
# readers see either the old target or the new one, never a partial file.
install -m 0755 -o root -g root "$STAGE_BIN" "./$REMOTE_BIN"

ln -sfn "$INSTALL_DIR/$REMOTE_BIN" ./logwatch-analyzer.new
mv -Tf ./logwatch-analyzer.new ./logwatch-analyzer

ls -la ./logwatch-analyzer ./"$REMOTE_BIN"
./logwatch-analyzer -version
__REMOTE_INSTALL__

# ------------------------------------------------------ 5. scripts + runner
if [[ ${SKIP_SCRIPTS:-0} == 1 ]]; then
  echo "==> SKIP_SCRIPTS=1 — leaving scripts/ and run-cron.sh as deployed"
else
  echo "==> Comparing deployed helper scripts against local"
  # shellcheck disable=SC2029 # INSTALL_DIR is deliberately expanded client-side
  ssh "$HOST" "cd ${INSTALL_DIR} && sha256sum run-cron.sh scripts/*.sh 2>/dev/null" \
    > /tmp/leon-deployed-sums.$$ || true
  changed=0
  for f in run-cron.sh scripts/generate-logwatch.sh scripts/generate-drupal-watchdog.sh scripts/helper.sh; do
    local_path="$REPO_ROOT/scripts/$(basename "$f")"
    [[ -f $local_path ]] || continue
    lsum=$(shasum -a 256 "$local_path" | awk '{print $1}')
    rsum=$(awk -v p="$f" '$2 == p || $2 == "./" p {print $1}' /tmp/leon-deployed-sums.$$)
    if [[ $lsum == "$rsum" ]]; then
      printf '  same      %s\n' "$f"
    else
      printf '  DIFFERENT %s\n' "$f"
      changed=1
    fi
  done
  rm -f /tmp/leon-deployed-sums.$$

  if [[ $changed == 0 ]]; then
    echo "  nothing to update"
  else
    if [[ ${ASSUME_YES:-0} == 1 ]]; then
      reply=y
      echo "  ASSUME_YES=1 — installing the updated scripts"
    else
      read -r -p "Install the updated scripts? [y/N] " reply
    fi
    if [[ ${reply:-n} =~ ^[Yy]$ ]]; then
      ssh "$HOST" INSTALL_DIR="$INSTALL_DIR" STAGE_SCRIPTS="$STAGE_SCRIPTS" 'bash -s' <<'__REMOTE_SCRIPTS__'
set -euo pipefail
cd "$INSTALL_DIR"
pgrep -f 'run-cron\.sh|logwatch-analyzer' >/dev/null 2>&1 && { echo "ABORT: run in flight" >&2; exit 1; }
mkdir -p ./scripts

# run-cron.sh lives at the top level: that is the path cron invokes.
if ! cmp -s "$STAGE_SCRIPTS/run-cron.sh" ./run-cron.sh; then
    cp -p ./run-cron.sh ./run-cron.sh.prev
    install -m 0755 -o root -g root "$STAGE_SCRIPTS/run-cron.sh" ./run-cron.sh.new
    mv -f ./run-cron.sh.new ./run-cron.sh
    bash -n ./run-cron.sh && echo "  run-cron.sh updated, syntax OK"
    grep -n 'LOCK_FILE=' ./run-cron.sh
fi

# generate-logwatch.sh is 0750 on the server (it runs logwatch as root);
# preserve each file's existing mode rather than flattening them all to 0755.
for f in generate-logwatch.sh generate-drupal-watchdog.sh helper.sh; do
    src="$STAGE_SCRIPTS/$f"
    dst="./scripts/$f"
    [ -f "$src" ] || continue
    if [ -e "$dst" ]; then
        mode=$(stat -c '%a' "$dst")
        cp -p "$dst" "$dst.prev"
    else
        mode=0755
    fi
    install -m "$mode" -o root -g root "$src" "$dst.new"
    mv -f "$dst.new" "$dst"
    echo "  updated $dst (mode $mode)"
done
ls -la ./run-cron.sh ./scripts/
__REMOTE_SCRIPTS__
    else
      echo "  skipped — scripts left as deployed"
    fi
  fi
fi

# ---------------------------------------------------------- 6. blast radius
echo "==> Confirming the protected files are untouched"
ssh "$HOST" INSTALL_DIR="$INSTALL_DIR" 'bash -s' <<'__REMOTE_CONFIRM__'
set -u
for f in .env drupal-sites.json ocms-sites.json exclusions.json data/summaries.db; do
    [ -e "$INSTALL_DIR/$f" ] \
      && stat -c '%n  mode=%a  owner=%U:%G  size=%s  mtime=%y' "$INSTALL_DIR/$f" \
      || echo "$INSTALL_DIR/$f  ABSENT"
done
__REMOTE_CONFIRM__

cat <<EOF

==> Deploy complete: ${VERSION} on ${HOST}

Verify:
  ./deploy/status.sh
  ssh ${HOST} 'cd ${INSTALL_DIR} && ./logwatch-analyzer -list-ocms-sites'

Roll back (~15s):
  ./deploy/rollback.sh

Remove the staging copies when satisfied:
  ssh ${HOST} 'rm -rf ${STAGE_BIN} ${STAGE_SCRIPTS}'
EOF
