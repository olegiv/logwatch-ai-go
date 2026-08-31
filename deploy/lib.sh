# shellcheck shell=bash
#
# Common helpers sourced by the deploy/* scripts. Auto-loads host-specific
# settings from deploy/deploy.env if that file exists (gitignored; copy
# deploy.env.example to deploy.env and edit). Idempotent — safe to source
# multiple times.

_lib_sh_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -z ${_deploy_env_loaded:-} && -f "$_lib_sh_dir/deploy.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$_lib_sh_dir/deploy.env"
  set +a
  _deploy_env_loaded=1
fi

# resolve_host [arg]
#
# Prints the SSH target on stdout, prefixing with "root@" if the input
# has no user part. Precedence:
#   1. explicit argument (passed as $1)
#   2. HOST env var
#   3. DEPLOY_HOST env var (from deploy.env or shell)
#
# Returns nonzero with a hint-printing error if no source produces a host.
resolve_host() {
  local host="${1:-${HOST:-${DEPLOY_HOST:-}}}"
  if [[ -z $host ]]; then
    cat >&2 <<EOF
error: no target host set. Options:
  - copy deploy/deploy.env.example → deploy/deploy.env and set DEPLOY_HOST
  - pass via env: DEPLOY_HOST=server.example.com $0
  - pass via Makefile: make <target> HOST=server.example.com
EOF
    return 1
  fi
  [[ $host == *@* ]] || host="root@$host"
  printf '%s\n' "$host"
}

# require_root_target HOST
#
# The remote workflow is root-only: it writes under /opt, uses
# `install -o root -g root`, reads a 0600 .env and takes a lock in /run,
# and it never invokes sudo. Accepting a non-root target would let a
# deploy build and upload before failing at install time, with a staging
# directory that is not root-owned despite deploy.sh's guarantee.
require_root_target() {
  local host="$1"
  if [[ ${host%%@*} != root ]]; then
    cat >&2 <<EOF
error: target '$host' is not root.

  These scripts perform root-only operations on the remote host and do
  not elevate. Deploy as root, or add an elevation mechanism first.
EOF
    return 1
  fi
  return 0
}

# valid_version VERSION
#
# True when VERSION is safe to use as both a filename component and an
# interpolated word in a remote command. git permits tags containing shell
# metacharacters and slashes, and the version reaches both, so anything
# outside a strict filename-safe set is rejected rather than quoted around.
valid_version() {
  [[ $1 =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]]
}

# valid_stage_dir PATH
#
# True when PATH is a staging directory this tooling created. The value comes
# from the target's stdout, which can carry shell-profile banners, and it is
# handed to `rm -rf` running as root — so it is matched against the exact
# shape `mktemp -d /tmp/logwatch-deploy.XXXXXXXXXX` produces.
valid_stage_dir() {
  [[ $1 =~ ^/tmp/logwatch-deploy\.[A-Za-z0-9]+$ ]]
}

# Tracked helper scripts deployed into $INSTALL_DIR/scripts/. Defined once:
# deploy.sh uploads and diffs them and rollback.sh restores them, and a list
# that disagreed between the two would upload a script, report it changed,
# then never install or restore it.
# shellcheck disable=SC2034 # consumed by deploy.sh and rollback.sh
TRACKED_SCRIPTS=(generate-logwatch.sh generate-drupal-watchdog.sh helper.sh)

# valid_install_dir PATH
#
# True when PATH is safe to interpolate into a remote command. INSTALL_DIR is
# passed through `ssh host VAR=... bash -s`, where the remote login shell
# re-parses the argument, so a space or a metacharacter would break or inject.
# VERSION and STAGE_DIR are already validated; this closes the same hole.
valid_install_dir() {
  [[ $1 =~ ^/[A-Za-z0-9._/-]+$ && $1 != *//* && $1 != */ ]]
}

# remote_env NAME VALUE [NAME VALUE ...]
#
# Emits shell-quoted assignments to prepend to a remote payload.
#
# Passing them as `ssh host NAME=value 'bash -s'` does NOT work: ssh joins its
# arguments with spaces and hands the result to the remote *shell*, so a value
# containing a space splits — `TRACKED="a b c" bash -s` arrives as
# `TRACKED=a b c bash -s`, and the remote shell runs `b` as the command while
# the payload is never read. It exits 0, so the failure is silent.
remote_env() {
  while [[ $# -gt 0 ]]; do
    printf '%s=%q\n' "$1" "$2"
    shift 2
  done
}
