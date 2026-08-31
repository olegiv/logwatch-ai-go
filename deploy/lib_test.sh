#!/usr/bin/env bash
#
# Unit tests for the pure helpers in lib.sh. Small on purpose: these three
# functions guard values that reach a remote command line and a root `rm -rf`,
# so they are the part worth pinning. Everything else needs a real host and is
# covered by `./deploy/deploy.sh --stage-only`.
#
#   bash deploy/lib_test.sh    (or: make test-sh)

set -uo pipefail
_deploy_env_loaded=1   # do not let deploy.env override the host cases below
# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

pass=0 fail=0
ok()  { printf '  ok    %s\n' "$1"; pass=$((pass+1)); }
bad() { printf '  FAIL  %s (expected %s)\n' "$1" "$2"; fail=$((fail+1)); }
eq()  { if [[ $2 == "$3" ]]; then ok "$1"; else bad "$1" "$2 got $3"; fi; }
yes() { if "$2" "$3" 2>/dev/null; then ok "$1"; else bad "$1" accept; fi; }
no()  { if "$2" "$3" 2>/dev/null; then bad "$1" reject; else ok "$1"; fi; }

echo "resolve_host / require_root_target"
eq "bare host gets root@"   "root@example.com"   "$(HOST='' DEPLOY_HOST=example.com resolve_host '')"
eq "explicit user kept"     "deploy@example.com" "$(HOST='' DEPLOY_HOST=deploy@example.com resolve_host '')"
eq "argument beats HOST"    "root@arg.example"   "$(HOST=env.example resolve_host arg.example)"
if HOST='' DEPLOY_HOST='' resolve_host '' >/dev/null 2>&1
then bad "fails with no host" nonzero; else ok "fails with no host"; fi
yes "accepts root@"  require_root_target root@example.com
no  "rejects deploy@" require_root_target deploy@example.com

echo "valid_version   (reaches a remote command line as root)"
for v in v0.15.0 v0.15.0-1-gabc123 v1.0.0+build.5 0.1.0; do yes "accepts $v" valid_version "$v"; done
# shellcheck disable=SC2016 # hostile literals; expansion is the bug
for v in 'x;id' 'feature/foo' 'v1$(id)' 'a b' '-rf' '`id`' '../etc' ''; do
  no "rejects ${v:-<empty>}" valid_version "$v"; done

echo "valid_stage_dir (reaches rm -rf as root)"
yes "accepts a real mktemp result" valid_stage_dir /tmp/logwatch-deploy.aB3xY9zQ1w
for v in "/tmp/logwatch-deploy.a; rm -rf /" "/tmp/logwatch-deploy.a'b" /tmp/other.abc \
         /tmp/logwatch-deploy. /etc '' 'banner
/tmp/logwatch-deploy.abc'; do
  no "rejects ${v:-<empty>}" valid_stage_dir "$v"; done

echo
[[ $fail -eq 0 ]] && { echo "PASS — $pass assertions"; exit 0; }
echo "FAIL — $fail of $((pass+fail))"; exit 1
