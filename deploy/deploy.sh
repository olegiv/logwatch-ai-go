#!/usr/bin/env bash
#
# Build and deploy logwatch-analyzer to the production host.
#
# Usage:
#   ./deploy/deploy.sh                     # uses DEPLOY_HOST from deploy.env
#   ./deploy/deploy.sh <host>              # explicit (overrides env)
#   REF=v0.15.0 ./deploy/deploy.sh         # build that git ref, not the worktree
#   ./deploy/deploy.sh --stage-only        # build + upload, touch nothing in /opt
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
# Tracked scripts are taken from that same worktree so a REF deploy is
# internally consistent; only the gitignored, host-specific run-cron.sh
# necessarily comes from the real working tree.
#
# Why not scripts/install.sh: that is a first-time installer, not an upgrader.
# It looks for bin/logwatch-analyzer (the host-arch name, so it cannot place a
# cross-built Linux binary), replaces scripts/ wholesale, and finishes with a
# recursive `chown -R $(whoami)` across .env and data/summaries.db.
#
# Why staging + mv rather than scp onto the live path: scp truncates and then
# streams, so a cron fire mid-transfer would exec a partial ELF, and writing
# the running inode in place risks ETXTBSY. Staging and finishing with a
# same-filesystem `mv` makes the swap atomic.
#
# Staging uses a root-owned mktemp directory created on the target rather than
# a predictable /tmp path: /tmp is world-writable, and scp follows and
# truncates an existing symlink at the destination, so a predictable name lets
# any local user turn a root deploy into an arbitrary-file overwrite.
#
# The install lays out the binary as a versioned regular file with a stable
# `logwatch-analyzer` symlink pointing at it. The symlink is re-pointed, never
# replaced.

set -euo pipefail

# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_LIB="$(dirname "${BASH_SOURCE[0]}")/remote-lib.sh"

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
require_root_target "$HOST" || exit 1
INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"
REF="${REF:-}"
WITH_SCRIPTS=1
[[ ${SKIP_SCRIPTS:-0} == 1 ]] && WITH_SCRIPTS=0

WORKTREE=""
STAGE_DIR=""
cleanup() {
  [[ -n $WORKTREE ]] && git -C "$REPO_ROOT" worktree remove --force "$WORKTREE" 2>/dev/null
  # shellcheck disable=SC2029 # STAGE_DIR is deliberately expanded client-side;
  # it is validated against a strict pattern before ever being used here.
  [[ -n $STAGE_DIR ]] && ssh "$HOST" "rm -rf -- '$STAGE_DIR'" 2>/dev/null
  return 0
}
trap cleanup EXIT

# --------------------------------------------------------- 1. build source
if [[ -n $REF ]]; then
  # GNU mktemp requires at least three X's in a template and rejects a
  # bare -t prefix, so the documented REF workflow must not rely on the
  # BSD/macOS form. This spelling works on both.
  WORKTREE=$(mktemp -d "${TMPDIR:-/tmp}/logwatch-deploy.XXXXXXXXXX")
  rmdir "$WORKTREE"                       # git worktree add wants a fresh path
  echo "==> Building from a clean worktree at ${REF}"
  git -C "$REPO_ROOT" worktree add --detach --quiet "$WORKTREE" "$REF"
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

# VERSION reaches remote shell commands and filesystem paths. git permits tags
# containing shell metacharacters and slashes, so anything outside a strict
# filename-safe set is rejected rather than quoted around.
if [[ ! $VERSION =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]]; then
  echo "error: refusing to deploy version string '$VERSION'" >&2
  echo "       Only [A-Za-z0-9._+-] are allowed (no slashes or metacharacters)." >&2
  exit 1
fi

LOCAL_BIN="$SRC/bin/logwatch-analyzer-linux-amd64"
# Tracked helpers follow the built ref; the gitignored runner cannot.
LOCAL_RUNNER="$REPO_ROOT/scripts/run-cron.sh"
TRACKED_SCRIPTS=(generate-logwatch.sh generate-drupal-watchdog.sh helper.sh)
REMOTE_BIN="logwatch-analyzer-${VERSION}"

echo "==> Deploying ${VERSION} to ${HOST}:${INSTALL_DIR}"

if [[ $WITH_SCRIPTS == 1 && ! -f $LOCAL_RUNNER ]]; then
  echo "error: $LOCAL_RUNNER missing. It is gitignored and host-specific;" >&2
  echo "       this machine holds the only copy of the production job list." >&2
  echo "       Use SKIP_SCRIPTS=1 to deploy the binary alone." >&2
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
# release. This is the exact failure the REF mechanism exists to prevent.
if go version -m "$LOCAL_BIN" | grep -q 'vcs.modified=true'; then
  echo "error: binary stamped vcs.modified=true — not a clean release build" >&2
  exit 1
fi

BIN_SHA=$(shasum -a 256 "$LOCAL_BIN" | awk '{print $1}')
echo "    binary sha256: $BIN_SHA"

# --------------------------------------------------------------- 3. stage
echo "==> Creating a root-owned staging directory on ${HOST}"
STAGE_DIR=$(ssh "$HOST" 'd=$(mktemp -d /tmp/logwatch-deploy.XXXXXXXXXX) && chmod 700 "$d" && printf %s "$d"')
if [[ ! $STAGE_DIR =~ ^/tmp/logwatch-deploy\.[A-Za-z0-9]+$ ]]; then
  echo "error: unexpected staging directory from target: '$STAGE_DIR'" >&2
  exit 1
fi
echo "    ${STAGE_DIR} (0700, root-owned)"

echo "==> Staging (nothing under ${INSTALL_DIR} touched yet)"
scp -q "$LOCAL_BIN" "${HOST}:${STAGE_DIR}/logwatch-analyzer"
if [[ $WITH_SCRIPTS == 1 ]]; then
  scp -q "$LOCAL_RUNNER" "${HOST}:${STAGE_DIR}/run-cron.sh"
  for s in "${TRACKED_SCRIPTS[@]}"; do
    [[ -f "$SRC/scripts/$s" ]] && scp -q "$SRC/scripts/$s" "${HOST}:${STAGE_DIR}/$s"
  done
fi

echo "==> Verifying transfer + proving this CPU can execute the binary"
ssh "$HOST" BIN_SHA="$BIN_SHA" STAGE_DIR="$STAGE_DIR" 'bash -s' <<'__REMOTE_STAGE__'
set -euo pipefail
got=$(sha256sum "$STAGE_DIR/logwatch-analyzer" | awk '{print $1}')
[ "$got" = "$BIN_SHA" ] || { echo "error: binary checksum mismatch" >&2; exit 1; }
echo "  checksum matches"
file "$STAGE_DIR/logwatch-analyzer"
chmod 0755 "$STAGE_DIR/logwatch-analyzer"
echo "  --- executing the STAGED binary (catches GOAMD64/SIGILL, /opt untouched) ---"
"$STAGE_DIR/logwatch-analyzer" -version
for f in "$STAGE_DIR"/*.sh; do [ -e "$f" ] || continue; bash -n "$f" || exit 1; done
echo "  staged scripts pass syntax check"
__REMOTE_STAGE__

if [[ $STAGE_ONLY == 1 ]]; then
  echo
  echo "--stage-only: stopped before touching ${INSTALL_DIR}."
  echo "Staged artifacts are removed on exit; re-run without --stage-only to install."
  exit 0
fi

# ------------------------------- 4. lock + db backup + binary swap (atomic)
# The cron lock, the database backup and the binary swap run in ONE remote
# session so the lock is held across the entire critical section.
echo "==> Installing (cron lock, database backup, binary swap)"
ssh "$HOST" INSTALL_DIR="$INSTALL_DIR" STAGE_DIR="$STAGE_DIR" \
            REMOTE_BIN="$REMOTE_BIN" VERSION="$VERSION" 'bash -s' \
  < <(cat "$REMOTE_LIB"; cat <<'__REMOTE_INSTALL__'
set -euo pipefail
cd "$INSTALL_DIR"

acquire_cron_lock || exit 1

# --- clear stale component backups -----------------------------------------
# rollback --all uses .prev existence as proof that THIS deployment replaced a
# component. Backups left by an earlier deployment would otherwise make --all
# roll an unchanged component back an extra release, so they are cleared up
# front; the stages below recreate .prev only for what they actually replace.
rm -f ./run-cron.sh.prev ./scripts/*.prev

# --- database backup -------------------------------------------------------
if ! db=$(resolve_db); then
    echo "  database disabled in .env — skipping backup"
elif [ ! -f "$db" ]; then
    echo "  no database at $db — skipping backup"
else
    dst="$db.pre-$VERSION"
    if command -v sqlite3 >/dev/null 2>&1; then
        sqlite3 "$db" ".backup '$dst'"        # WAL-aware, consistent
    else
        # No sqlite3: a plain copy is only safe because we hold the lock.
        # Copy any sidecars too, so the snapshot is self-consistent.
        # Redeploying the same version reuses $dst. Clear any sidecars left
        # by an earlier snapshot first, or rollback --db would copy a stale
        # -wal beside a fresh snapshot and replay it into the restore.
        rm -f "$dst" "$dst"-wal "$dst"-shm "$dst"-journal
        cp -p "$db" "$dst"
        for ext in -wal -shm -journal; do
            [ -f "$db$ext" ] && cp -p "$db$ext" "$dst$ext"
        done
    fi
    ls -l "$dst"
fi

# --- binary swap -----------------------------------------------------------
if [ ! -e ./logwatch-analyzer ]; then
    echo "ABORT: nothing installed at $INSTALL_DIR/logwatch-analyzer." >&2
    echo "       This script upgrades an existing install; use scripts/install.sh" >&2
    echo "       for a first-time bootstrap." >&2
    exit 1
fi

stamp=$(date -u +%Y%m%dT%H%M%SZ)

if [ -L ./logwatch-analyzer ]; then
    prev_target=$(readlink -f ./logwatch-analyzer)
else
    # A regular file here means the install predates this tooling. Hard-link
    # it to a versioned name rather than moving it: the stable path keeps
    # working right up to the final atomic rename, so a cron launch in the
    # interval cannot hit ENOENT and a failure below leaves production intact.
    legacy="logwatch-analyzer-legacy-$stamp"
    ln ./logwatch-analyzer "./$legacy" 2>/dev/null || cp -p ./logwatch-analyzer "./$legacy"
    prev_target="$INSTALL_DIR/$legacy"
    echo "  legacy regular-file install preserved as $legacy"
fi

# Redeploying a version that is already live would otherwise write straight
# through to the running inode (ETXTBSY) and leave prev-target aimed at the
# artifact we just overwrote, making rollback a no-op. Preserve it first.
if [ "$prev_target" = "$INSTALL_DIR/$REMOTE_BIN" ]; then
    preserved="$REMOTE_BIN.prev-$stamp"
    mv "./$REMOTE_BIN" "./$preserved"
    prev_target="$INSTALL_DIR/$preserved"
    echo "  same version already live — previous artifact kept as $preserved"
fi

echo "  rollback target: $prev_target"
"$prev_target" -version 2>&1 | head -1 || true
printf '%s\n' "$prev_target" > ./.logwatch-analyzer.prev-target

# Install under a unique name, then rename: never write into a live inode.
install -m 0755 -o root -g root "$STAGE_DIR/logwatch-analyzer" "./$REMOTE_BIN.incoming.$$"
mv -Tf "./$REMOTE_BIN.incoming.$$" "./$REMOTE_BIN"

ln -sfn "$INSTALL_DIR/$REMOTE_BIN" ./logwatch-analyzer.new
mv -Tf ./logwatch-analyzer.new ./logwatch-analyzer

ls -la ./logwatch-analyzer ./"$REMOTE_BIN"
./logwatch-analyzer -version
__REMOTE_INSTALL__
)

# ------------------------------------------------------ 5. scripts + runner
if [[ $WITH_SCRIPTS == 0 ]]; then
  echo "==> SKIP_SCRIPTS=1 — leaving scripts/ and run-cron.sh as deployed"
else
  echo "==> Comparing deployed helper scripts against the built ref"
  # shellcheck disable=SC2029 # INSTALL_DIR is deliberately expanded client-side
  deployed_sums=$(ssh "$HOST" "cd ${INSTALL_DIR} && sha256sum run-cron.sh scripts/*.sh 2>/dev/null" || true)
  changed=0
  check_one() {  # $1 = remote path, $2 = local path
    [[ -f $2 ]] || return 0
    local lsum rsum
    lsum=$(shasum -a 256 "$2" | awk '{print $1}')
    rsum=$(awk -v p="$1" '$2 == p {print $1}' <<<"$deployed_sums")
    if [[ $lsum == "$rsum" ]]; then printf '  same      %s\n' "$1"
    else printf '  DIFFERENT %s\n' "$1"; changed=1; fi
  }
  check_one run-cron.sh "$LOCAL_RUNNER"
  for s in "${TRACKED_SCRIPTS[@]}"; do check_one "scripts/$s" "$SRC/scripts/$s"; done

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
      ssh "$HOST" INSTALL_DIR="$INSTALL_DIR" STAGE_DIR="$STAGE_DIR" 'bash -s' \
        < <(cat "$REMOTE_LIB"; cat <<'__REMOTE_SCRIPTS__'
set -euo pipefail
cd "$INSTALL_DIR"
acquire_cron_lock || exit 1
mkdir -p ./scripts

# run-cron.sh lives at the top level: that is the path cron invokes.
if [ -f "$STAGE_DIR/run-cron.sh" ] && ! cmp -s "$STAGE_DIR/run-cron.sh" ./run-cron.sh; then
    cp -p ./run-cron.sh ./run-cron.sh.prev
    install -m 0755 -o root -g root "$STAGE_DIR/run-cron.sh" ./run-cron.sh.new
    mv -f ./run-cron.sh.new ./run-cron.sh
    bash -n ./run-cron.sh && echo "  run-cron.sh updated, syntax OK"
    grep -n 'LOCK_FILE=' ./run-cron.sh
fi

# Preserve each file's existing mode rather than flattening them all to 0755
# (generate-logwatch.sh is 0750, helper.sh is 0640).
for f in generate-logwatch.sh generate-drupal-watchdog.sh helper.sh; do
    src="$STAGE_DIR/$f"
    dst="./scripts/$f"
    [ -f "$src" ] || continue
    cmp -s "$src" "$dst" && continue
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
)
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
  ./deploy/rollback.sh            # binary
  ./deploy/rollback.sh --all      # binary + runner + helper scripts
EOF
