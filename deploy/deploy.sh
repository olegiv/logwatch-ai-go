#!/usr/bin/env bash
#
# Build and deploy logwatch-analyzer to the production host.
#
# Usage:
#   ./deploy/deploy.sh                     # uses DEPLOY_HOST from deploy.env
#   ./deploy/deploy.sh <host>              # explicit (overrides env)
#   REF=v0.15.0 ./deploy/deploy.sh         # build that git ref, not the worktree
#   ./deploy/deploy.sh --stage-only        # build + verify on the target, then
#                                          # clean up; nothing in /opt is touched
#   SKIP_TESTS=1 ./deploy/deploy.sh        # emergency bypass of make check
#   SKIP_SCRIPTS=1 ./deploy/deploy.sh      # binary only; leave scripts/ and run-cron.sh
#   ASSUME_YES=1 ./deploy/deploy.sh        # answer the scripts prompt automatically
#   KEEP_VERSIONS=5 ./deploy/deploy.sh     # retain more old artifacts (default 3, 0 = keep all)
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
# A missing remote-lib.sh would otherwise ship a payload with no resolve_db
# or acquire_cron_lock. Process substitution hides `cat`'s failure, so in
# preflight that degrades silently all the way to "ALL GATES PASSED".
[[ -r $REMOTE_LIB ]] || { echo "error: $REMOTE_LIB is missing or unreadable" >&2; exit 1; }

STAGE_ONLY=0
HOST_ARG=""
for arg in "$@"; do
  case "$arg" in
    --stage-only) STAGE_ONLY=1 ;;
    -*) echo "error: unknown flag $arg" >&2; exit 2 ;;
    *)  if [[ -n $HOST_ARG ]]; then
          echo "error: more than one host given ('$HOST_ARG' and '$arg')" >&2; exit 2
        fi
        HOST_ARG="$arg" ;;
  esac
done

HOST=$(resolve_host "$HOST_ARG") || exit 1
require_root_target "$HOST" || exit 1
INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"
valid_install_dir "$INSTALL_DIR" || {
  echo "error: refusing to use INSTALL_DIR='$INSTALL_DIR'" >&2
  echo "       Expected an absolute path of [A-Za-z0-9._/-] with no trailing slash." >&2
  exit 1
}
REF="${REF:-}"
KEEP_VERSIONS="${KEEP_VERSIONS:-3}"
[[ $KEEP_VERSIONS =~ ^[0-9]+$ ]] || { echo "error: KEEP_VERSIONS must be a number" >&2; exit 1; }
WITH_SCRIPTS=1
[[ ${SKIP_SCRIPTS:-0} == 1 ]] && WITH_SCRIPTS=0

WORKTREE=""
STAGE_DIR=""
cleanup() {
  # Capture and re-return the real exit status first. Each step is guarded
  # independently and reports failure: a bare `cmd1 && cmd2` here would abort
  # the function on cmd1's failure (errexit DOES apply to the final command of
  # an && list inside a function), skipping the remote cleanup and replacing
  # the script's status with the trap's — a successful deploy exiting 128.
  local rc=$?

  if [[ -n $WORKTREE ]]; then
    git -C "$REPO_ROOT" worktree remove --force "$WORKTREE" 2>/dev/null \
      || echo "WARN: could not remove worktree $WORKTREE — remove it by hand" >&2
  fi

  if [[ -n $STAGE_DIR ]]; then
    # STAGE_DIR is validated at assignment, but the validation-failure path
    # exits through this trap, so re-check before handing it to a root rm.
    if valid_stage_dir "$STAGE_DIR"; then
      # shellcheck disable=SC2029 # deliberately expanded client-side
      ssh -n "$HOST" "rm -rf -- '$STAGE_DIR'" 2>/dev/null \
        || echo "WARN: staging dir $STAGE_DIR left on $HOST — remove it by hand" >&2
    else
      echo "WARN: not removing unvalidated staging path: '$STAGE_DIR'" >&2
    fi
  fi

  return "$rc"
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
if ! valid_version "$VERSION"; then
  echo "error: refusing to deploy version string '$VERSION'" >&2
  echo "       Only [A-Za-z0-9._+-] are allowed (no slashes or metacharacters)." >&2
  exit 1
fi

LOCAL_BIN="$SRC/bin/logwatch-analyzer-linux-amd64"
# Tracked helpers follow the built ref; the gitignored runner cannot.
LOCAL_RUNNER="$REPO_ROOT/scripts/run-cron.sh"
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

# Read the build info once and fail closed. Inside an `if` condition a failing
# pipeline is not an error, so testing `go version -m ... | grep -q` directly
# would treat an unreadable binary as "clean" — silently disarming the one
# check that stops a non-release artifact from shipping.
if ! buildinfo=$(go version -m "$LOCAL_BIN"); then
  echo "error: could not read build info from $LOCAL_BIN — refusing to deploy" >&2
  exit 1
fi
printf '%s\n' "$buildinfo" \
  | grep -E '^[[:space:]]+(mod|build[[:space:]]+(GOOS|GOARCH|GOAMD64|CGO_ENABLED|vcs))' || true

# A dirty stamp means we are about to ship something that is not the tagged
# release. This is the exact failure the REF mechanism exists to prevent.
if printf '%s\n' "$buildinfo" | grep -q 'vcs.modified=true'; then
  echo "error: binary stamped vcs.modified=true — not a clean release build" >&2
  exit 1
fi

BIN_SHA=$(shasum -a 256 "$LOCAL_BIN" | awk '{print $1}')
echo "    binary sha256: $BIN_SHA"

# --------------------------------------------------------------- 3. stage
echo "==> Creating a root-owned staging directory on ${HOST}"
STAGE_DIR=$(ssh "$HOST" 'd=$(mktemp -d /tmp/logwatch-deploy.XXXXXXXXXX) && chmod 700 "$d" && printf %s "$d"')
if ! valid_stage_dir "$STAGE_DIR"; then
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
# ------------------------------------------ 4. compare scripts + confirm
# The comparison and the prompt happen BEFORE the lock is taken, so a human
# is never the reason the lock is held: stage 5 then performs the binary and
# script swaps in one locked session. Asking after the binary swap would
# leave cron free to fire mid-deploy and run a mixed tree.
INSTALL_SCRIPTS=0
if [[ $WITH_SCRIPTS == 0 ]]; then
  echo "==> SKIP_SCRIPTS=1 — leaving scripts/ and run-cron.sh as deployed"
else
  echo "==> Comparing deployed helper scripts against the built ref"
  # -n is essential: without it ssh forwards this script's stdin to the remote
  # command and consumes it, leaving the prompt below at EOF.
  # shellcheck disable=SC2029 # INSTALL_DIR is deliberately expanded client-side
  if ! deployed_sums=$(ssh -n "$HOST" "cd ${INSTALL_DIR} && sha256sum run-cron.sh scripts/*.sh 2>/dev/null"); then
    echo "WARN: could not read deployed checksums (ssh rc=$?) — treating every" >&2
    echo "      script as changed; the comparison below is not trustworthy." >&2
    deployed_sums=""
  fi
  changed=0
  check_one() {  # $1 = remote path, $2 = local path
    if [[ ! -f $2 ]]; then
      printf '  MISSING   %s (not in the source tree — will not be deployed)\n' "$1"
      return 0
    fi
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
  elif [[ ${ASSUME_YES:-0} == 1 ]]; then
    INSTALL_SCRIPTS=1
    echo "  ASSUME_YES=1 — the updated scripts will be installed"
  elif [[ ! -t 0 ]]; then
    # `read` returns 1 at EOF, which errexit turns into a silent abort.
    # Refuse before anything is mutated rather than dying without a message.
    cat >&2 <<EOF
error: stdin is not a terminal, so the scripts prompt cannot be answered.
       Nothing has been changed on the target. Re-run with ASSUME_YES=1 to
       install the scripts, or SKIP_SCRIPTS=1 to leave them alone.
EOF
    exit 1
  elif ! read -r -p "Install the updated scripts too? [y/N] " reply; then
    echo "error: could not read a reply — nothing was changed." >&2
    exit 1
  elif [[ ${reply:-n} =~ ^[Yy]$ ]]; then
    INSTALL_SCRIPTS=1
  else
    echo "  scripts will be left as deployed"
  fi
fi

# --------------------------- 5. one locked session: backup, binary, scripts
echo "==> Installing (cron lock, database backup, binary swap)"
ssh "$HOST" INSTALL_DIR="$INSTALL_DIR" STAGE_DIR="$STAGE_DIR" \
            REMOTE_BIN="$REMOTE_BIN" VERSION="$VERSION" \
            INSTALL_SCRIPTS="$INSTALL_SCRIPTS" TRACKED="${TRACKED_SCRIPTS[*]}" \
            KEEP_VERSIONS="$KEEP_VERSIONS" 'bash -s' \
  < <(cat "$REMOTE_LIB"; cat <<'__REMOTE_INSTALL__'
set -euo pipefail
cd "$INSTALL_DIR"

acquire_cron_lock || exit 1

# Record the protected files so stage 6 can prove they were not touched,
# rather than printing state a human is asked to eyeball.
stat_protected() {
    for f in .env drupal-sites.json ocms-sites.json exclusions.json data/summaries.db; do
        if [ -e "$f" ]; then stat -c '%n mode=%a owner=%U:%G size=%s mtime=%Y' "$f"
        else echo "$f ABSENT"; fi
    done
}
stat_protected > "$STAGE_DIR/protected.before"

# --- clear stale component backups -----------------------------------------
# A .prev file must mean "this deployment replaced it", or rollback --all
# rolls an unchanged component back an extra release. Only clear backups for
# the components this run will actually replace: clearing them under
# SKIP_SCRIPTS=1 would destroy the previous deployment's rollback material
# for files this run does not touch.
if [ "$INSTALL_SCRIPTS" = 1 ]; then
    for p in ./run-cron.sh.prev ./scripts/*.prev; do
        [ -e "$p" ] && echo "  clearing stale backup $p" && rm -f "$p"
    done
fi

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
        # Redeploying the same version reuses $dst, so clear any sidecars an
        # earlier snapshot left or rollback --db would replay stale pages.
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

# Stage the incoming artifact under a unique name FIRST. Nothing below
# unlinks or renames anything the live symlink resolves through, so the
# stable path stays valid for the whole sequence and the final rename is the
# only visible transition.
install -m 0755 -o root -g root "$STAGE_DIR/logwatch-analyzer" "./$REMOTE_BIN.incoming.$$"

if [ -L ./logwatch-analyzer ]; then
    prev_target=$(readlink -f ./logwatch-analyzer)
else
    # A regular file here means the install predates this tooling. Hard-link
    # it to a versioned name rather than moving it, so the stable path keeps
    # working right up to the final rename.
    legacy="logwatch-analyzer-legacy-$stamp"
    ln ./logwatch-analyzer "./$legacy" 2>/dev/null || cp -p ./logwatch-analyzer "./$legacy"
    prev_target="$INSTALL_DIR/$legacy"
    echo "  legacy regular-file install preserved as $legacy"
fi

# Redeploying a version that is already live would otherwise leave
# prev-target aimed at the artifact this deploy replaces, making rollback a
# no-op. Hard-link (never move) the live artifact aside: a rename here would
# leave ./logwatch-analyzer dangling until the swap below completes.
if [ "$prev_target" = "$INSTALL_DIR/$REMOTE_BIN" ]; then
    preserved="$REMOTE_BIN.prev-$stamp"
    ln "./$REMOTE_BIN" "./$preserved" 2>/dev/null || cp -p "./$REMOTE_BIN" "./$preserved"
    prev_target="$INSTALL_DIR/$preserved"
    echo "  same version already live — previous artifact kept as $preserved"
fi

echo "  rollback target: $prev_target"
if ! "$prev_target" -version >/dev/null 2>&1; then
    echo "  WARN: $prev_target does not execute — rollback.sh would refuse it." >&2
else
    "$prev_target" -version 2>&1 | head -1
fi
printf '%s\n' "$prev_target" > ./.logwatch-analyzer.prev-target

mv -Tf "./$REMOTE_BIN.incoming.$$" "./$REMOTE_BIN"
ln -sfn "$INSTALL_DIR/$REMOTE_BIN" ./logwatch-analyzer.new
mv -Tf ./logwatch-analyzer.new ./logwatch-analyzer

ls -la ./logwatch-analyzer ./"$REMOTE_BIN"

# Smoke-test the live path. On failure revert immediately: this is the only
# point where aborting would leave production running a broken binary, and
# the caller's rollback instructions are printed after this session ends.
if ! ./logwatch-analyzer -version; then
    echo "ABORT: the new binary failed -version AFTER the swap. Reverting." >&2
    ln -sfn "$prev_target" ./logwatch-analyzer.rb
    mv -Tf ./logwatch-analyzer.rb ./logwatch-analyzer
    if ./logwatch-analyzer -version >&2; then
        echo "reverted to $prev_target" >&2
    else
        echo "REVERT ALSO FAILED — $INSTALL_DIR needs manual attention" >&2
    fi
    exit 1
fi

# --- scripts + runner (same lock) ------------------------------------------
if [ "$INSTALL_SCRIPTS" = 1 ]; then
    mkdir -p ./scripts
    if [ -f "$STAGE_DIR/run-cron.sh" ] && ! cmp -s "$STAGE_DIR/run-cron.sh" ./run-cron.sh; then
        cp -p ./run-cron.sh ./run-cron.sh.prev
        install -m 0755 -o root -g root "$STAGE_DIR/run-cron.sh" ./run-cron.sh.new
        mv -f ./run-cron.sh.new ./run-cron.sh
        bash -n ./run-cron.sh || { echo "ABORT: installed runner has a syntax error" >&2; exit 1; }
        echo "  run-cron.sh updated, syntax OK"
        grep -n 'LOCK_FILE=' ./run-cron.sh \
            || echo "  WARN: runner pins no LOCK_FILE — it will use the built-in default"
    fi

    # Preserve each file's existing mode rather than flattening them to 0755
    # (generate-logwatch.sh is 0750, helper.sh is 0640).
    for f in $TRACKED; do
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
fi

# --- prune old artifacts ---------------------------------------------------
# Versioned binaries, legacy copies and database snapshots otherwise
# accumulate forever (~12 MiB per redeploy) while GATE 4 only demands 25 MiB,
# on a box shared with other tenants. Deletion is deliberately conservative:
# only names this tooling generates, never the live target, and never the
# recorded rollback target.
if [ "$KEEP_VERSIONS" -gt 0 ]; then
    live=$(readlink -f ./logwatch-analyzer)
    keep_target=$(cat ./.logwatch-analyzer.prev-target 2>/dev/null || echo "")
    echo "  pruning old artifacts (keeping $KEEP_VERSIONS, plus live and rollback target)"

    # Binaries: newest first, skip the two that must survive, drop the rest
    # past the keep count.
    n=0
    for f in $(ls -1t ./logwatch-analyzer-* 2>/dev/null); do
        abs="$INSTALL_DIR/${f#./}"
        [ "$abs" = "$live" ] && continue
        [ "$abs" = "$keep_target" ] && continue
        n=$((n + 1))
        if [ "$n" -gt "$KEEP_VERSIONS" ]; then
            rm -f "$f" && echo "    removed $f"
        fi
    done

    # Database snapshots: prune the main files and take their sidecars along,
    # so a restore can never pick up a WAL whose snapshot is gone.
    if db=$(resolve_db); then
        n=0
        for f in $(ls -1t "$db".pre-* 2>/dev/null | grep -vE -- '-(wal|shm|journal)$'); do
            n=$((n + 1))
            if [ "$n" -gt "$KEEP_VERSIONS" ]; then
                rm -f "$f" "$f"-wal "$f"-shm "$f"-journal && echo "    removed $f"
            fi
        done
    fi
fi

# --- prove the protected files are unchanged -------------------------------
stat_protected > "$STAGE_DIR/protected.after"
if diff -u "$STAGE_DIR/protected.before" "$STAGE_DIR/protected.after"; then
    echo "  protected files unchanged (verified, not just displayed)"
else
    echo "ABORT: a protected file changed during the deploy — see the diff above." >&2
    exit 1
fi
__REMOTE_INSTALL__
)

cat <<EOF

==> Deploy complete: ${VERSION} on ${HOST}

Verify:
  ./deploy/status.sh
  ssh ${HOST} 'cd ${INSTALL_DIR} && ./logwatch-analyzer -list-ocms-sites'

Roll back (~15s):
  ./deploy/rollback.sh            # binary
  ./deploy/rollback.sh --all      # binary + runner + helper scripts
EOF
