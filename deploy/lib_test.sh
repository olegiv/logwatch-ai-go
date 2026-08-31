#!/usr/bin/env bash
#
# Unit tests for the pure functions in deploy/lib.sh and deploy/remote-lib.sh.
#
#   bash deploy/lib_test.sh        (or: make test-sh)
#
# No framework, no network, no remote host — these are the parts of the deploy
# tooling that can be exercised in isolation. Everything else (ssh
# orchestration, atomic renames, flock contention, GOAMD64) needs a real host
# and is covered by `./deploy/preflight.sh` and `./deploy/deploy.sh
# --stage-only`.
#
# Every case here corresponds to a defect that actually shipped in this
# tooling and was found in review. They are regression tests, not coverage
# theatre.

set -uo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
pass=0 fail=0

ok() {   printf '  ok    %s\n' "$1"; pass=$((pass + 1)); }
bad() {  printf '  FAIL  %s\n     expected: [%s]\n     actual:   [%s]\n' "$1" "$2" "$3"; fail=$((fail + 1)); }

eq() {  # eq <label> <expected> <actual>
    if [[ $2 == "$3" ]]; then ok "$1"; else bad "$1" "$2" "$3"; fi
}
accepts() {  # accepts <label> <fn> <value>
    if "$2" "$3"; then ok "$1"; else bad "$1" "accept" "reject"; fi
}
rejects() {  # rejects <label> <fn> <value>
    # stderr is suppressed: a rejection printing its explanation is the
    # correct behaviour here, not test output.
    if "$2" "$3" 2>/dev/null; then bad "$1" "reject" "accept"; else ok "$1"; fi
}

# deploy.env would otherwise be sourced and override the host variables the
# resolve_host cases set.
_deploy_env_loaded=1
# shellcheck source=lib.sh source-path=SCRIPTDIR
source "$DIR/lib.sh"
# shellcheck source=remote-lib.sh source-path=SCRIPTDIR
source "$DIR/remote-lib.sh"

WORK=$(mktemp -d "${TMPDIR:-/tmp}/logwatch-libtest.XXXXXXXX")
trap 'rm -rf "$WORK"' EXIT

# --------------------------------------------------------------- read_env
# Regression: the original parser did `tr -d` on quotes and spaces, which
# corrupted quoted paths and mangled inline comments.
echo "read_env"
cd "$WORK" || exit 1
cat > .env <<'EOF'
ENABLE_DATABASE=true # enabled for prod
DATABASE_PATH="/opt/logwatch ai/data/summaries.db"
LOG_LEVEL=info
export CLAUDE_MODEL='claude-haiku-4-5-20251001'
DUPLICATE=first
DUPLICATE=second
EOF
eq "keeps a space inside a quoted value" "/opt/logwatch ai/data/summaries.db" "$(read_env DATABASE_PATH)"
eq "strips an inline comment from an unquoted value" "true" "$(read_env ENABLE_DATABASE)"
eq "reads a plain value" "info" "$(read_env LOG_LEVEL)"
eq "handles the export prefix and single quotes" "claude-haiku-4-5-20251001" "$(read_env CLAUDE_MODEL)"
eq "last assignment wins" "second" "$(read_env DUPLICATE)"
eq "returns the default for a missing key" "fallback" "$(read_env NOT_PRESENT fallback)"
eq "returns the default when .env is absent" "fallback" "$(cd "$WORK" && rm -f .env && read_env ANY fallback)"

# ------------------------------------------------------------- resolve_db
echo "resolve_db"
cd "$WORK" || exit 1
printf 'ENABLE_DATABASE=true\nDATABASE_PATH=./data/summaries.db\n' > .env
# shellcheck disable=SC2034 # read by the sourced resolve_* functions
INSTALL_DIR=/opt/logwatch-ai
eq "resolves a relative path against INSTALL_DIR" "/opt/logwatch-ai/data/summaries.db" "$(resolve_db)"

printf 'ENABLE_DATABASE=true\nDATABASE_PATH=/var/lib/lw/summaries.db\n' > .env
eq "keeps an absolute path as-is" "/var/lib/lw/summaries.db" "$(resolve_db)"

printf 'ENABLE_DATABASE=false\n' > .env
if resolve_db >/dev/null; then bad "returns nonzero when disabled" "nonzero" "zero"; else ok "returns nonzero when disabled"; fi

printf 'LOG_LEVEL=info\n' > .env
eq "defaults to enabled at the default path" "/opt/logwatch-ai/data/summaries.db" "$(resolve_db)"

# The application reads this key with viper.GetBool, which also accepts 1/t/T.
# Treating those as "disabled" would silently skip the database backup.
for v in TRUE True 1 t T; do
    printf 'ENABLE_DATABASE=%s\n' "$v" > .env
    if resolve_db >/dev/null; then ok "treats ENABLE_DATABASE=$v as enabled"
    else bad "treats ENABLE_DATABASE=$v as enabled" "enabled" "disabled"; fi
done
for v in false FALSE 0 f no; do
    printf 'ENABLE_DATABASE=%s\n' "$v" > .env
    if resolve_db >/dev/null; then bad "treats ENABLE_DATABASE=$v as disabled" "disabled" "enabled"
    else ok "treats ENABLE_DATABASE=$v as disabled"; fi
done

# ------------------------------------------------------ resolve_lock_file
# Regression: matched commented-out crontab lines, and truncated a quoted
# path at the first space.
echo "resolve_lock_file"
cd "$WORK" || exit 1
mkdir -p bin
# shellcheck disable=SC2034 # read by the sourced resolve_* functions
INSTALL_DIR="$WORK"
export PATH="$WORK/bin:$PATH"

stub_crontab() {  # stub_crontab <content>
    { echo '#!/bin/sh'; printf 'cat <<"CRONEOF"\n%s\nCRONEOF\n' "$1"; } > "$WORK/bin/crontab"
    chmod 755 "$WORK/bin/crontab"
}

# shellcheck disable=SC2016 # writing a literal shell default, not expanding it
printf 'LOCK_FILE="${LOCK_FILE:-/run/logwatch-ai-cron.lock}"\n' > run-cron.sh
stub_crontab '7 2 * * * /opt/logwatch-ai/run-cron.sh >> /x.log 2>&1'
eq "falls back to the deployed script's default" "/run/logwatch-ai-cron.lock" "$(LOCK_FILE='' resolve_lock_file)"

stub_crontab '7 2 * * * LOCK_FILE=/opt/logwatch-ai/.cron.lock /opt/logwatch-ai/run-cron.sh'
eq "honours a crontab-pinned lock path" "/opt/logwatch-ai/.cron.lock" "$(LOCK_FILE='' resolve_lock_file)"

stub_crontab '7 2 * * * LOCK_FILE=/opt/logwatch-ai/.cron.lock /opt/logwatch-ai/run-cron.sh
#OLD 7 2 * * * LOCK_FILE=/var/lock/DISABLED.lock /opt/logwatch-ai/run-cron.sh'
eq "ignores a commented-out crontab line" "/opt/logwatch-ai/.cron.lock" "$(LOCK_FILE='' resolve_lock_file)"

stub_crontab '7 2 * * * LOCK_FILE="/var/lock/quoted path.lock" /opt/logwatch-ai/run-cron.sh'
eq "keeps a space inside a quoted lock path" "/var/lock/quoted path.lock" "$(LOCK_FILE='' resolve_lock_file)"

eq "an explicit LOCK_FILE wins over everything" "/tmp/explicit.lock" "$(LOCK_FILE=/tmp/explicit.lock resolve_lock_file)"

# ----------------------------------------------------------- resolve_host
echo "resolve_host / require_root_target"
eq "prefixes root@ for a bare host"   "root@example.com"   "$(HOST='' DEPLOY_HOST=example.com resolve_host '')"
eq "keeps an explicit user@host"      "deploy@example.com" "$(HOST='' DEPLOY_HOST=deploy@example.com resolve_host '')"
eq "an argument beats HOST"           "root@arg.example"   "$(HOST=env.example resolve_host arg.example)"
eq "HOST beats DEPLOY_HOST"           "root@env.example"   "$(HOST=env.example DEPLOY_HOST=file.example resolve_host '')"
if HOST='' DEPLOY_HOST='' resolve_host '' >/dev/null 2>&1
then bad "fails with no host set" "nonzero" "zero"; else ok "fails with no host set"; fi

accepts "accepts a root target"       require_root_target root@example.com
rejects "rejects a non-root target"   require_root_target deploy@example.com
rejects "rejects another non-root"    require_root_target ubuntu@example.com

# --------------------------------------------------------- valid_version
# The version reaches remote command strings and filesystem paths as root.
echo "valid_version"
for v in v0.15.0 v0.15.0-1-gabc123 v1.0.0+build.5 v0.15.0-dirty 0.1.0; do
    accepts "accepts $v" valid_version "$v"
done
# shellcheck disable=SC2016 # these are hostile literals, expansion is the bug
for v in 'x;id' 'feature/foo' 'v1$(id)' 'a b' '-rf' '`id`' '../etc' '' 'v1|tee'; do
    rejects "rejects ${v:-<empty>}" valid_version "$v"
done

# ------------------------------------------------------- valid_stage_dir
# This value is handed to `rm -rf` running as root on the target.
echo "valid_stage_dir"
accepts "accepts a real mktemp result"  valid_stage_dir /tmp/logwatch-deploy.aB3xY9zQ1w
for v in '/tmp/logwatch-deploy.abc; rm -rf /' "/tmp/logwatch-deploy.a'b" '/tmp/other.abc123' \
         '/tmp/logwatch-deploy.' '/etc' '' 'banner text
/tmp/logwatch-deploy.abc123'; do
    rejects "rejects ${v:-<empty>}" valid_stage_dir "$v"
done

# ------------------------------------------------- backup snapshot selection
# Regression: the glob also matched -wal/-shm/-journal, and because `cp -p`
# preserves source mtimes a sidecar is routinely the newest match, so
# `ls -t | head -1` installed sidecar bytes as the database.
echo "backup selection"
cd "$WORK" || exit 1
rm -rf dbdir && mkdir dbdir && cd dbdir || exit 1
db=summaries.db
printf 'REAL-DATABASE-BYTES' > "$db.pre-v0.15.0"
printf 'WAL-SIDECAR-BYTES'   > "$db.pre-v0.15.0-wal"
printf 'SHM-SIDECAR-BYTES'   > "$db.pre-v0.15.0-shm"
touch -t 202608301000 "$db.pre-v0.15.0"
touch -t 202608301100 "$db.pre-v0.15.0-wal" "$db.pre-v0.15.0-shm"

# shellcheck disable=SC2010 # this mirrors rollback.sh's expression exactly
selected=$(ls -1t "$db".pre-* 2>/dev/null | grep -vE -- '-(wal|shm|journal)$' | head -1 || true)
eq "selects the database, not a newer sidecar" "$db.pre-v0.15.0" "$selected"
eq "the selected file holds database bytes" "REAL-DATABASE-BYTES" "$(cat "$selected")"

# With no snapshot at all the expression must yield an empty string rather
# than tripping errexit under pipefail (which made the guard unreachable).
rm -f -- *.pre-*
# shellcheck disable=SC2010 # this mirrors rollback.sh's expression exactly
missing=$(ls -1t "$db".pre-* 2>/dev/null | grep -vE -- '-(wal|shm|journal)$' | head -1 || true)
eq "yields empty when no snapshot exists" "" "$missing"

# ------------------------------------------------ in-flight process matching
# Regression: an unanchored `pgrep -f 'run-cron\.sh|logwatch-analyzer'` matched
# any command line merely mentioning those names, so an editor holding the file
# open blocked both deploy and rollback — worst on rollback, the safety net.
echo "in-flight process pattern"
INSTALL_DIR=/opt/logwatch-ai
pat="^(${INSTALL_DIR}/)?(run-cron\.sh|logwatch-analyzer)"
matches() { grep -qE "$pat" <<<"$1"; }

for cmd in "/opt/logwatch-ai/run-cron.sh" \
           "/opt/logwatch-ai/logwatch-analyzer -source-type ocms" \
           "run-cron.sh"; do
    if matches "$cmd"; then ok "matches a real job: $cmd"
    else bad "matches a real job: $cmd" "match" "no match"; fi
done
for cmd in "vim /opt/logwatch-ai/run-cron.sh" \
           "less /opt/logwatch-ai/logs/cron.log" \
           "grep logwatch-analyzer /var/log/syslog" \
           "tail -f /opt/logwatch-ai/logs/analyzer.log"; do
    if matches "$cmd"; then bad "ignores a bystander: $cmd" "no match" "match"
    else ok "ignores a bystander: $cmd"; fi
done

# ------------------------------------------------------------------ result
echo
if [[ $fail -eq 0 ]]; then
    echo "PASS — $pass assertions"
    exit 0
fi
echo "FAIL — $fail of $((pass + fail)) assertions failed"
exit 1
