# shellcheck shell=bash
#
# Shared helpers. Sourced by deploy.sh and rollback.sh, and auto-loads
# deploy/deploy.env if present (gitignored; copy deploy.env.example).

_lib_sh_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -z ${_deploy_env_loaded:-} && -f "$_lib_sh_dir/deploy.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$_lib_sh_dir/deploy.env"
  set +a
  _deploy_env_loaded=1
fi

# resolve_host [arg] — precedence: argument > $HOST > $DEPLOY_HOST.
# Prefixes root@ when the value has no user part.
resolve_host() {
  local host="${1:-${HOST:-${DEPLOY_HOST:-}}}"
  if [[ -z $host ]]; then
    cat >&2 <<EOF
error: no target host. Set DEPLOY_HOST in deploy/deploy.env, pass HOST=…,
       or give the host as an argument.
EOF
    return 1
  fi
  [[ $host == *@* ]] || host="root@$host"
  printf '%s\n' "$host"
}

# require_root_target HOST
#
# The remote side writes under /opt, uses `install -o root -g root` and takes
# a lock in /run, and never elevates. A non-root target would fail only after
# a full build and upload.
require_root_target() {
  if [[ ${1%%@*} != root ]]; then
    echo "error: target '$1' is not root; these scripts do not elevate." >&2
    return 1
  fi
}

# valid_version VERSION
#
# git permits tags containing shell metacharacters and slashes, and the
# version reaches both a remote command line and a filename, so anything
# outside a filename-safe set is rejected rather than quoted around.
valid_version() {
  [[ $1 =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]]
}

# valid_stage_dir PATH
#
# Comes from the target's stdout (which can carry shell-profile banners) and
# is handed to `rm -rf` as root, so it must match exactly what our own
# `mktemp -d` produces.
valid_stage_dir() {
  [[ $1 =~ ^/tmp/logwatch-deploy\.[A-Za-z0-9]+$ ]]
}
