#!/usr/bin/env bash
# Hermetic state-machine tests for remote-install.sh and remote-rollback.sh.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REMOTE_INSTALL="$SCRIPT_DIR/remote-install.sh"
REMOTE_ROLLBACK="$SCRIPT_DIR/remote-rollback.sh"
REMOTE_VERIFY="$SCRIPT_DIR/remote-verify.sh"
ROLLBACK_SCRIPT="$SCRIPT_DIR/rollback.sh"

pass=0
fail=0
ok() { printf '  ok    %s\n' "$1"; pass=$((pass + 1)); }
bad() { printf '  FAIL  %s%s\n' "$1" "${2:+ — $2}"; fail=$((fail + 1)); }
contains() {
    if [[ $2 == *"$3"* ]]; then ok "$1"; else bad "$1" "missing '$3'"; fi
}
same_file() {
    if [[ -e $2 && -e $3 && $2 -ef $3 ]]; then ok "$1"; else bad "$1" "$2 != $3"; fi
}
no_install_temps() {
    if compgen -G "$1/*.incoming.*" >/dev/null \
        || compgen -G "$1/logwatch-analyzer.new.*" >/dev/null \
        || compgen -G "$1/logwatch-analyzer.revert.*" >/dev/null \
        || compgen -G "$1/logwatch-analyzer.recover.*" >/dev/null \
        || compgen -G "$1/.logwatch-analyzer.prev-target.new.*" >/dev/null \
        || compgen -G "$1/.logwatch-analyzer.prev-target.restore.*" >/dev/null \
        || compgen -G "$1/.logwatch-analyzer.deploy-transaction.new.*" >/dev/null; then
        bad "$2" "temporary deployment files remain"
    else
        ok "$2"
    fi
}
no_rollback_temps() {
    if compgen -G "$1/logwatch-analyzer.rollback.*" >/dev/null \
        || compgen -G "$1/logwatch-analyzer.restore.*" >/dev/null \
        || compgen -G "$1/.logwatch-analyzer.prev-target.consumed.*" >/dev/null; then
        bad "$2" "temporary rollback files remain"
    else
        ok "$2"
    fi
}

TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/logwatch-remote-test.XXXXXXXXXX")
case "$TEST_ROOT" in
    /tmp/logwatch-remote-test.*|/private/tmp/logwatch-remote-test.*|/var/folders/*/logwatch-remote-test.*) ;;
    *) echo "unsafe temporary test directory: $TEST_ROOT" >&2; exit 1 ;;
esac
TEST_ROOT=$(cd "$TEST_ROOT" && pwd -P)
trap 'rm -rf -- "$TEST_ROOT"' EXIT

REAL_BASH=$(command -v bash)
REAL_INSTALL=$(command -v install)
REAL_LN=$(command -v ln)
REAL_MV=$(command -v mv)
REAL_RM=$(command -v rm)
export REAL_INSTALL REAL_LN REAL_MV REAL_RM

SHIM_DIR="$TEST_ROOT/bin"
NO_FLOCK_DIR="$TEST_ROOT/no-flock-bin"
mkdir "$SHIM_DIR" "$NO_FLOCK_DIR"
ln -s "$(command -v stat)" "$NO_FLOCK_DIR/stat"
ln -s "$(command -v id)" "$NO_FLOCK_DIR/id"

make_script() {
    local path=$1
    shift
    printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail' "$@" > "$path"
    chmod 0755 "$path"
}

# shellcheck disable=SC2016 # these are literal lines for the generated shim
make_script "$SHIM_DIR/install" \
    'mode=0755' \
    'while (($# > 0)); do' \
    '  case "$1" in' \
    '    -m) mode=$2; shift 2 ;;' \
    '    -o|-g) shift 2 ;;' \
    '    --) shift; break ;;' \
    '    *) break ;;' \
    '  esac' \
    'done' \
    'exec "$REAL_INSTALL" -m "$mode" "$1" "$2"'

# shellcheck disable=SC2016 # literal line for the generated shim
make_script "$SHIM_DIR/flock" 'exit "${TEST_FLOCK_RC:-0}"'

# shellcheck disable=SC2016 # these are literal lines for the generated shim
make_script "$SHIM_DIR/ln" \
    'last=${!#}' \
    'if [[ ${TEST_FAIL_REVERT_LINK:-0} == 1 && $last == *logwatch-analyzer.revert.* ]]; then exit 73; fi' \
    'if [[ ${TEST_FAIL_NEXT_LINK:-0} == 1 && $last == *logwatch-analyzer.new.* ]]; then exit 75; fi' \
    'exec "$REAL_LN" "$@"'

# shellcheck disable=SC2016 # these are literal lines for the generated shim
make_script "$SHIM_DIR/mv" \
    'last=${!#}' \
    'if [[ ${TEST_FAIL_RECORD_PUBLISH:-0} == 1 && $last == ./.logwatch-analyzer.prev-target ]]; then exit 74; fi' \
    'if [[ ${TEST_FAIL_LIVE_PUBLISH:-0} == 1 && $last == ./logwatch-analyzer ]]; then exit 77; fi' \
    'if [[ ${TEST_FAIL_RECOVERY_PUBLISH:-0} == 1 && $last == ./logwatch-analyzer && $* == *logwatch-analyzer.recover.* ]]; then exit 79; fi' \
    'if [[ ${TEST_FAIL_RECOVERY_PUBLISH:-0} == 1 && $last == ./logwatch-analyzer && $* == *logwatch-analyzer.rollback.* ]]; then exit 79; fi' \
    'args=()' \
    'treat_dest_as_file=0' \
    'for arg in "$@"; do' \
    '  if [[ $(uname -s) == Darwin ]]; then' \
    '    case "$arg" in' \
    '      -Tf|-fT) args+=(-f); treat_dest_as_file=1 ;;' \
    '      -T) treat_dest_as_file=1; continue ;;' \
    '      *) args+=("$arg") ;;' \
    '    esac' \
    '  else' \
    '    args+=("$arg")' \
    '  fi' \
    'done' \
    'if [[ $treat_dest_as_file == 1 && -d $last ]]; then exit 76; fi' \
    'if [[ ${TEST_SIGNAL_AFTER_LIVE_PUBLISH:-0} == 1 && $last == ./logwatch-analyzer && $* == *logwatch-analyzer.new.* ]]; then' \
    '  "$REAL_MV" "${args[@]}"' \
    '  kill -TERM "$PPID"' \
    '  exit 0' \
    'fi' \
    'if [[ ${TEST_SIGNAL_AFTER_RECORD_PUBLISH:-0} == 1 && $last == ./.logwatch-analyzer.prev-target && $* == *.prev-target.new.* ]]; then' \
    '  "$REAL_MV" "${args[@]}"' \
    '  kill -TERM "$PPID"' \
    '  exit 0' \
    'fi' \
    'if [[ ${TEST_SIGNAL_AFTER_ROLLBACK_PUBLISH:-0} == 1 && $last == ./logwatch-analyzer && $* == *logwatch-analyzer.rollback.* ]]; then' \
    '  "$REAL_MV" "${args[@]}"' \
    '  kill -TERM "$PPID"' \
    '  exit 0' \
    'fi' \
    'exec "$REAL_MV" "${args[@]}"'

# shellcheck disable=SC2016 # these are literal lines for the generated shim
make_script "$SHIM_DIR/ssh" \
    'printf "host=%s\ncommand=%s\n" "$1" "$2"' \
    'cat >/dev/null'

# shellcheck disable=SC2016 # these are literal lines for the generated shim
make_script "$SHIM_DIR/rm" \
    'last=${!#}' \
    'if [[ ${TEST_FAIL_RECORD_CONSUME:-0} == 1 && $last == ./.logwatch-analyzer.prev-target ]]; then exit 78; fi' \
    'if [[ ${TEST_FAIL_JOURNAL_REMOVE:-0} == 1 && $last == ./.logwatch-analyzer.deploy-transaction ]]; then exit 80; fi' \
    'exec "$REAL_RM" "$@"'

TEST_PATH="$SHIM_DIR:$PATH"

make_analyzer() {
    local path=$1
    local label=${2:-analyzer}
    local version_rc=${3:-0}
    local runtime_rc=${4:-0}
    {
        printf '%s\n' '#!/usr/bin/env bash'
        printf 'label=%q\n' "$label"
        printf 'version_rc=%q\n' "$version_rc"
        printf 'runtime_rc=%q\n' "$runtime_rc"
        # shellcheck disable=SC2016 # literal lines for the generated analyzer
        printf '%s\n' \
            'if [[ ${1:-} == -version ]]; then' \
            '  printf "%s\n" "$label"' \
            '  exit "$version_rc"' \
            'fi' \
            'if [[ ${1:-} == -check-runtime ]]; then' \
            '  exit "$runtime_rc"' \
            'fi' \
            'exit 2'
    } > "$path"
    chmod 0755 "$path"
}

make_symlink_sensitive_analyzer() {
    local path=$1
    local label=$2
    {
        printf '%s\n' '#!/usr/bin/env bash'
        printf 'label=%q\n' "$label"
        # shellcheck disable=SC2016 # literal lines for the generated analyzer
        printf '%s\n' \
            'if [[ ${1:-} == -version ]]; then' \
            '  [[ $0 != */logwatch-analyzer ]] || exit 1' \
            '  printf "%s\n" "$label"' \
            '  exit 0' \
            'fi' \
            'exit 2'
    } > "$path"
    chmod 0755 "$path"
}

new_case() {
    local name=$1
    CASE_ROOT="$TEST_ROOT/$name"
    mkdir "$CASE_ROOT" "$CASE_ROOT/install" "$CASE_ROOT/stage"
    INSTALL_PATH=$(cd "$CASE_ROOT/install" && pwd -P)
    STAGE_PATH=$(cd "$CASE_ROOT/stage" && pwd -P)
    LOCK_PATH="$CASE_ROOT/cron.lock"
    make_analyzer "$STAGE_PATH/logwatch-analyzer"
}

seed_live() {
    local version=$1
    make_analyzer "$INSTALL_PATH/logwatch-analyzer-$version" "$version"
    ln -s "$INSTALL_PATH/logwatch-analyzer-$version" "$INSTALL_PATH/logwatch-analyzer"
}

run_install() {
    local remote_bin=$1
    local force=${2:-0}
    TEST_FLOCK_RC="${TEST_FLOCK_RC:-0}" \
    TEST_FAIL_REVERT_LINK="${TEST_FAIL_REVERT_LINK:-0}" \
    TEST_FAIL_NEXT_LINK="${TEST_FAIL_NEXT_LINK:-0}" \
    TEST_FAIL_LIVE_PUBLISH="${TEST_FAIL_LIVE_PUBLISH:-0}" \
    TEST_SIGNAL_AFTER_LIVE_PUBLISH="${TEST_SIGNAL_AFTER_LIVE_PUBLISH:-0}" \
    TEST_SIGNAL_AFTER_RECORD_PUBLISH="${TEST_SIGNAL_AFTER_RECORD_PUBLISH:-0}" \
    TEST_FAIL_RECORD_PUBLISH="${TEST_FAIL_RECORD_PUBLISH:-0}" \
    TEST_FAIL_JOURNAL_REMOVE="${TEST_FAIL_JOURNAL_REMOVE:-0}" \
    TEST_FAIL_RECOVERY_PUBLISH="${TEST_FAIL_RECOVERY_PUBLISH:-0}" \
    INSTALL_DIR="${INSTALL_OVERRIDE:-$INSTALL_PATH}" STAGE_DIR="$STAGE_PATH" \
    REMOTE_BIN="$remote_bin" LOCK_FILE="$LOCK_PATH" FORCE="$force" \
    PATH="$TEST_PATH" "$REAL_BASH" "$REMOTE_INSTALL"
}

run_rollback() {
    local force=$1
    INSTALL_DIR="$INSTALL_PATH" LOCK_FILE="$LOCK_PATH" FORCE="$force" \
    TEST_FLOCK_RC="${TEST_FLOCK_RC:-0}" \
    TEST_FAIL_RECORD_CONSUME="${TEST_FAIL_RECORD_CONSUME:-0}" \
    TEST_SIGNAL_AFTER_ROLLBACK_PUBLISH="${TEST_SIGNAL_AFTER_ROLLBACK_PUBLISH:-0}" \
    TEST_FAIL_RECOVERY_PUBLISH="${TEST_FAIL_RECOVERY_PUBLISH:-0}" \
    PATH="$TEST_PATH" \
    "$REAL_BASH" "$REMOTE_ROLLBACK"
}

echo "rollback CLI preserves explicit target settings"
if output=$(_deploy_env_loaded=1 INSTALL_DIR=/wrong LOCK_FILE=/wrong PATH="$TEST_PATH" \
    "$REAL_BASH" "$ROLLBACK_SCRIPT" \
    --install-dir /srv/logwatch --lock-file /run/custom.lock root@override.example 2>&1); then
    ok "rollback CLI accepts explicit target settings"
else
    bad "rollback CLI accepts explicit target settings" "$output"
fi
contains "rollback CLI keeps the resolved host" "$output" "host=root@override.example"
contains "rollback CLI keeps the install root" "$output" "INSTALL_DIR=/srv/logwatch"
contains "rollback CLI keeps the lock path" "$output" "LOCK_FILE=/run/custom.lock"

echo "same-version redeploy preserves the outgoing inode"
new_case same-version
seed_live v1
INSTALL_OVERRIDE="$INSTALL_PATH/"
if output=$(run_install logwatch-analyzer-v1 2>&1); then
    ok "same-version deployment succeeds with a trailing slash"
else
    bad "same-version deployment succeeds with a trailing slash" "$output"
fi
unset INSTALL_OVERRIDE
recorded=$(<"$INSTALL_PATH/.logwatch-analyzer.prev-target")
if [[ $recorded == "$INSTALL_PATH/logwatch-analyzer-v1" ]]; then
    ok "rollback record keeps the immutable outgoing artifact"
else
    bad "rollback record keeps the immutable outgoing artifact" "$recorded"
fi
same_file "outgoing artifact is not overwritten" "$recorded" "$INSTALL_PATH/logwatch-analyzer-v1"
if [[ ! "$INSTALL_PATH/logwatch-analyzer" -ef "$INSTALL_PATH/logwatch-analyzer-v1" ]]; then
    ok "live symlink points at a unique redeploy artifact"
else
    bad "live symlink points at a unique redeploy artifact"
fi
no_install_temps "$INSTALL_PATH" "same-version deployment cleans temporary files"

echo "external predecessors are never advertised as rollback targets"
new_case external-predecessor
mkdir "$CASE_ROOT/external"
TEST_EXTERNAL_MARKER="$CASE_ROOT/external-was-executed"
export TEST_EXTERNAL_MARKER
# shellcheck disable=SC2016 # literal lines for the generated analyzer
make_script "$CASE_ROOT/external/operator-analyzer" \
    'if [[ ${1:-} == -version ]]; then : > "$TEST_EXTERNAL_MARKER"; printf "%s\n" operator; exit 0; fi' \
    'exit 2'
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v0" v0
ln -s "$CASE_ROOT/external/operator-analyzer" "$INSTALL_PATH/logwatch-analyzer"
printf '%s\n' "$INSTALL_PATH/logwatch-analyzer-v0" > "$INSTALL_PATH/.logwatch-analyzer.prev-target"
if output=$(run_install logwatch-analyzer-v2 2>&1); then
    bad "external predecessor aborts without FORCE=1"
else
    ok "external predecessor aborts without FORCE=1"
fi
same_file "rejected external predecessor stays live" "$INSTALL_PATH/logwatch-analyzer" \
    "$CASE_ROOT/external/operator-analyzer"
contains "external predecessor is diagnosed" "$output" "rollback target is outside"
if [[ ! -e $TEST_EXTERNAL_MARKER ]]; then
    ok "rejected external predecessor is not executed"
else
    bad "rejected external predecessor is not executed"
fi
if output=$(run_install logwatch-analyzer-v2 1 2>&1); then
    ok "FORCE=1 deploys without recording the external target"
else
    bad "FORCE=1 deploys without recording the external target" "$output"
fi
same_file "forced external deployment publishes v2" "$INSTALL_PATH/logwatch-analyzer" \
    "$INSTALL_PATH/logwatch-analyzer-v2"
recorded=$(<"$INSTALL_PATH/.logwatch-analyzer.prev-target")
if [[ $recorded == "$INSTALL_PATH/logwatch-analyzer-v0" ]]; then
    ok "forced external deployment preserves the valid rollback record"
else
    bad "forced external deployment preserves the valid rollback record" "$recorded"
fi
contains "external-target override is reported" "$output" "FORCE=1"
if [[ ! -e $TEST_EXTERNAL_MARKER ]]; then
    ok "forced external predecessor is not executed"
else
    bad "forced external predecessor is not executed"
fi
unset TEST_EXTERNAL_MARKER

echo "failure before the symlink swap leaves the live artifact untouched"
new_case pre-swap-failure
seed_live v1
TEST_FAIL_NEXT_LINK=1
if output=$(run_install logwatch-analyzer-v1 2>&1); then
    bad "pre-swap link failure aborts deployment"
else
    ok "pre-swap link failure aborts deployment"
fi
unset TEST_FAIL_NEXT_LINK
same_file "pre-swap failure leaves v1 live" "$INSTALL_PATH/logwatch-analyzer" "$INSTALL_PATH/logwatch-analyzer-v1"
if compgen -G "$INSTALL_PATH/logwatch-analyzer-v1.redeploy-*" >/dev/null; then
    bad "pre-swap failure removes the unpublished artifact"
else
    ok "pre-swap failure removes the unpublished artifact"
fi
contains "pre-swap failure is visible" "$output" "could not create the candidate live symlink"

echo "failure to publish the symlink leaves the live artifact untouched"
new_case publish-failure
seed_live v1
TEST_FAIL_LIVE_PUBLISH=1
if output=$(run_install logwatch-analyzer-v1 2>&1); then
    bad "live-link publication failure aborts deployment"
else
    ok "live-link publication failure aborts deployment"
fi
unset TEST_FAIL_LIVE_PUBLISH
same_file "publication failure leaves v1 live" "$INSTALL_PATH/logwatch-analyzer" "$INSTALL_PATH/logwatch-analyzer-v1"
if compgen -G "$INSTALL_PATH/logwatch-analyzer-v1.redeploy-*" >/dev/null; then
    bad "publication failure removes the unpublished artifact"
else
    ok "publication failure removes the unpublished artifact"
fi
contains "publication failure is visible" "$output" "could not publish the candidate live symlink"
no_install_temps "$INSTALL_PATH" "publication failure cleans temporary files"

echo "runtime validation fails before the symlink swap"
new_case runtime-preflight-failure
seed_live v1
make_analyzer "$STAGE_PATH/logwatch-analyzer" candidate 0 1
if output=$(run_install logwatch-analyzer-v2 2>&1); then
    bad "runtime validation failure aborts deployment"
else
    ok "runtime validation failure aborts deployment"
fi
same_file "runtime validation failure leaves v1 live" \
    "$INSTALL_PATH/logwatch-analyzer" "$INSTALL_PATH/logwatch-analyzer-v1"
contains "runtime validation failure is diagnosed" "$output" "failed runtime validation"
if [[ ! -e $INSTALL_PATH/logwatch-analyzer-v2 ]]; then
    ok "runtime validation failure publishes no artifact"
else
    bad "runtime validation failure publishes no artifact"
fi

echo "an interrupt after publication leaves a recoverable transaction"
new_case publication-signal
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v0" v0
seed_live v1
printf '%s\n' "$INSTALL_PATH/logwatch-analyzer-v0" > "$INSTALL_PATH/.logwatch-analyzer.prev-target"
TEST_SIGNAL_AFTER_LIVE_PUBLISH=1
if output=$(run_install logwatch-analyzer-v1 2>&1); then
    bad "post-publication signal interrupts deployment"
else
    ok "post-publication signal interrupts deployment"
fi
unset TEST_SIGNAL_AFTER_LIVE_PUBLISH
if [[ -f $INSTALL_PATH/.logwatch-analyzer.deploy-transaction ]]; then
    ok "post-publication signal leaves a durable transaction"
else
    bad "post-publication signal leaves a durable transaction"
fi
if output=$(run_rollback 0 2>&1); then
    ok "rollback command recovers the interrupted deployment"
else
    bad "rollback command recovers the interrupted deployment" "$output"
fi
same_file "post-publication signal restores v1" "$INSTALL_PATH/logwatch-analyzer" \
    "$INSTALL_PATH/logwatch-analyzer-v1"
recorded=$(<"$INSTALL_PATH/.logwatch-analyzer.prev-target")
if [[ $recorded == "$INSTALL_PATH/logwatch-analyzer-v0" ]]; then
    ok "interrupted deployment preserves the prior rollback record"
else
    bad "interrupted deployment preserves the prior rollback record" "$recorded"
fi
no_install_temps "$INSTALL_PATH" "post-publication signal cleans temporary files"
if [[ ! -e $INSTALL_PATH/.logwatch-analyzer.deploy-transaction ]]; then
    ok "post-publication signal clears the recovered transaction"
else
    bad "post-publication signal clears the recovered transaction"
fi

echo "an interrupt after rollback-record publication restores older history"
new_case record-publication-signal
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v0" v0
seed_live v1
printf '%s\n' "$INSTALL_PATH/logwatch-analyzer-v0" > "$INSTALL_PATH/.logwatch-analyzer.prev-target"
TEST_SIGNAL_AFTER_RECORD_PUBLISH=1
if output=$(run_install logwatch-analyzer-v2 2>&1); then
    bad "post-record-publication signal interrupts deployment"
else
    ok "post-record-publication signal interrupts deployment"
fi
unset TEST_SIGNAL_AFTER_RECORD_PUBLISH
if [[ ! "$INSTALL_PATH/logwatch-analyzer" -ef "$INSTALL_PATH/logwatch-analyzer-v1" \
    || ! -f $INSTALL_PATH/.logwatch-analyzer.prev-target \
    || $(<"$INSTALL_PATH/.logwatch-analyzer.prev-target") != "$INSTALL_PATH/logwatch-analyzer-v0" ]]; then
    if output=$(run_rollback 0 2>&1); then
        ok "rollback command recovers the post-record-publication interrupt"
    else
        bad "rollback command recovers the post-record-publication interrupt" "$output"
    fi
else
    ok "EXIT cleanup recovers the post-record-publication interrupt"
fi
same_file "record-publication signal restores v1" "$INSTALL_PATH/logwatch-analyzer" \
    "$INSTALL_PATH/logwatch-analyzer-v1"
recorded=$(<"$INSTALL_PATH/.logwatch-analyzer.prev-target")
if [[ $recorded == "$INSTALL_PATH/logwatch-analyzer-v0" ]]; then
    ok "record-publication signal restores the older rollback target"
else
    bad "record-publication signal restores the older rollback target" "$recorded"
fi
if [[ ! -e $INSTALL_PATH/.logwatch-analyzer.deploy-transaction ]]; then
    ok "record-publication signal clears the recovered transaction"
else
    bad "record-publication signal clears the recovered transaction"
fi
no_install_temps "$INSTALL_PATH" "record-publication signal cleans temporary files"

echo "failed recovery publication is never reported as success"
new_case recovery-publish-failure
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v0" v0
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v1" v1
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v2" v2
ln -s "$INSTALL_PATH/logwatch-analyzer-v2" "$INSTALL_PATH/logwatch-analyzer"
printf '%s\n' "$INSTALL_PATH/logwatch-analyzer-v0" > "$INSTALL_PATH/.logwatch-analyzer.prev-target"
printf 'prev=%s\nnew=%s\n' "$INSTALL_PATH/logwatch-analyzer-v1" \
    "$INSTALL_PATH/logwatch-analyzer-v2" > "$INSTALL_PATH/.logwatch-analyzer.deploy-transaction"
TEST_FAIL_RECOVERY_PUBLISH=1
if output=$(run_rollback 0 2>&1); then
    bad "rollback recovery publish failure returns nonzero"
else
    ok "rollback recovery publish failure returns nonzero"
fi
same_file "failed rollback recovery leaves the candidate live" \
    "$INSTALL_PATH/logwatch-analyzer" "$INSTALL_PATH/logwatch-analyzer-v2"
if [[ -f $INSTALL_PATH/.logwatch-analyzer.deploy-transaction ]]; then
    ok "failed rollback recovery retains its transaction"
else
    bad "failed rollback recovery retains its transaction"
fi
if [[ $output != *"recovered interrupted deployment"* ]]; then
    ok "failed rollback recovery emits no success message"
else
    bad "failed rollback recovery emits no success message" "$output"
fi
if output=$(run_install logwatch-analyzer-v3 2>&1); then
    bad "install recovery publish failure returns nonzero"
else
    ok "install recovery publish failure returns nonzero"
fi
same_file "failed install recovery leaves the candidate live" \
    "$INSTALL_PATH/logwatch-analyzer" "$INSTALL_PATH/logwatch-analyzer-v2"
if [[ -f $INSTALL_PATH/.logwatch-analyzer.deploy-transaction ]]; then
    ok "failed install recovery retains its transaction"
else
    bad "failed install recovery retains its transaction"
fi
if [[ $output != *"reverted incomplete deployment"* ]]; then
    ok "failed install recovery emits no success message"
else
    bad "failed install recovery emits no success message" "$output"
fi
unset TEST_FAIL_RECOVERY_PUBLISH
no_install_temps "$INSTALL_PATH" "failed recovery cleans temporary links"

echo "a directory cannot absorb the rollback record"
new_case record-directory
seed_live v1
mkdir "$INSTALL_PATH/.logwatch-analyzer.prev-target"
if output=$(run_install logwatch-analyzer-v2 2>&1); then
    bad "record directory aborts deployment"
else
    ok "record directory aborts deployment"
fi
same_file "record directory leaves v1 live" "$INSTALL_PATH/logwatch-analyzer" "$INSTALL_PATH/logwatch-analyzer-v1"
contains "record directory is diagnosed" "$output" "deployment state path must be a regular file"
no_install_temps "$INSTALL_PATH" "record-directory failure creates no temporary files"

echo "failed smoke test restores both live state and rollback history"
new_case auto-revert
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v0"
seed_live v1
make_analyzer "$STAGE_PATH/logwatch-analyzer" broken 1
printf '%s\n' "$INSTALL_PATH/logwatch-analyzer-v0" > "$INSTALL_PATH/.logwatch-analyzer.prev-target"
if output=$(run_install logwatch-analyzer-broken 2>&1); then
    bad "broken deployment fails"
else
    ok "broken deployment fails"
fi
same_file "auto-revert restores v1" "$INSTALL_PATH/logwatch-analyzer" "$INSTALL_PATH/logwatch-analyzer-v1"
recorded=$(<"$INSTALL_PATH/.logwatch-analyzer.prev-target")
if [[ $recorded == "$INSTALL_PATH/logwatch-analyzer-v0" ]]; then
    ok "auto-revert preserves the prior rollback record"
else
    bad "auto-revert preserves the prior rollback record" "$recorded"
fi
contains "auto-revert reports success" "$output" "reverted to"
no_install_temps "$INSTALL_PATH" "auto-revert cleans temporary files"

echo "record publication failure also restores live state"
new_case record-failure
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v0"
seed_live v1
printf '%s\n' "$INSTALL_PATH/logwatch-analyzer-v0" > "$INSTALL_PATH/.logwatch-analyzer.prev-target"
TEST_FAIL_RECORD_PUBLISH=1
if output=$(run_install logwatch-analyzer-v2 2>&1); then
    bad "record publication failure aborts deployment"
else
    ok "record publication failure aborts deployment"
fi
unset TEST_FAIL_RECORD_PUBLISH
same_file "record failure restores v1" "$INSTALL_PATH/logwatch-analyzer" "$INSTALL_PATH/logwatch-analyzer-v1"
recorded=$(<"$INSTALL_PATH/.logwatch-analyzer.prev-target")
if [[ $recorded == "$INSTALL_PATH/logwatch-analyzer-v0" ]]; then
    ok "record failure preserves the prior rollback record"
else
    bad "record failure preserves the prior rollback record" "$recorded"
fi
contains "record failure reports its reason" "$output" "rollback record could not be published"

echo "transaction cleanup failure restores the previous deployment"
new_case journal-cleanup-failure
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v0" v0
seed_live v1
printf '%s\n' "$INSTALL_PATH/logwatch-analyzer-v0" > "$INSTALL_PATH/.logwatch-analyzer.prev-target"
TEST_FAIL_JOURNAL_REMOVE=1
if output=$(run_install logwatch-analyzer-v2 2>&1); then
    bad "journal cleanup failure aborts deployment"
else
    ok "journal cleanup failure aborts deployment"
fi
unset TEST_FAIL_JOURNAL_REMOVE
same_file "journal cleanup failure restores v1" "$INSTALL_PATH/logwatch-analyzer" \
    "$INSTALL_PATH/logwatch-analyzer-v1"
recorded=$(<"$INSTALL_PATH/.logwatch-analyzer.prev-target")
if [[ $recorded == "$INSTALL_PATH/logwatch-analyzer-v0" ]]; then
    ok "journal cleanup failure restores the prior rollback record"
else
    bad "journal cleanup failure restores the prior rollback record" "$recorded"
fi
if [[ -f $INSTALL_PATH/.logwatch-analyzer.deploy-transaction ]]; then
    ok "journal cleanup failure retains a recoverable transaction"
else
    bad "journal cleanup failure retains a recoverable transaction"
fi
contains "journal cleanup failure reports abort" "$output" \
    "deployment transaction could not be finalized"
if output=$(run_install logwatch-analyzer-v3 2>&1); then
    ok "next deployment recovers the retained transaction"
else
    bad "next deployment recovers the retained transaction" "$output"
fi
same_file "recovered deployment publishes v3" "$INSTALL_PATH/logwatch-analyzer" \
    "$INSTALL_PATH/logwatch-analyzer-v3"
if [[ ! -e $INSTALL_PATH/.logwatch-analyzer.deploy-transaction ]]; then
    ok "recovered deployment clears the transaction"
else
    bad "recovered deployment clears the transaction"
fi
no_install_temps "$INSTALL_PATH" "journal cleanup failure cleans temporary files"

echo "emergency revert failures are explicit"
new_case revert-failure
seed_live v1
make_analyzer "$STAGE_PATH/logwatch-analyzer" broken 1
TEST_FAIL_REVERT_LINK=1
if output=$(run_install logwatch-analyzer-broken 2>&1); then
    bad "failed emergency revert returns nonzero"
else
    ok "failed emergency revert returns nonzero"
fi
unset TEST_FAIL_REVERT_LINK
contains "failed emergency revert emits CRITICAL" "$output" "CRITICAL: could not create the emergency revert link"

echo "flock protection fails closed and reports overrides"
new_case no-flock
seed_live v1
if output=$(INSTALL_DIR="$INSTALL_PATH" STAGE_DIR="$STAGE_PATH" \
    REMOTE_BIN=logwatch-analyzer-v2 LOCK_FILE="$LOCK_PATH" FORCE=0 \
    PATH="$NO_FLOCK_DIR" "$REAL_BASH" "$REMOTE_INSTALL" 2>&1); then
    bad "missing flock aborts by default"
else
    ok "missing flock aborts by default"
fi
contains "missing flock explains remediation" "$output" "flock(1) is unavailable"

new_case held-lock
seed_live v1
TEST_FLOCK_RC=1
if output=$(run_install logwatch-analyzer-v2 2>&1); then
    bad "held lock aborts by default"
else
    ok "held lock aborts by default"
fi
contains "held lock reports the conflict" "$output" "is held"
if output=$(run_install logwatch-analyzer-v2 1 2>&1); then
    ok "FORCE=1 overrides a held lock"
else
    bad "FORCE=1 overrides a held lock" "$output"
fi
unset TEST_FLOCK_RC
contains "forced lock override is reported" "$output" "FORCE=1"

echo "deployment rejects writable ancestors above safe target directories"
new_case writable-install-ancestor
seed_live v1
chmod 0777 "$CASE_ROOT"
if output=$(run_install logwatch-analyzer-v2 2>&1); then
    bad "install with writable target ancestor aborts"
else
    ok "install with writable target ancestor aborts"
fi
chmod 0700 "$CASE_ROOT"
contains "writable install ancestor is diagnosed" "$output" "non-sticky writable ancestor"
same_file "writable install ancestor leaves v1 live" \
    "$INSTALL_PATH/logwatch-analyzer" "$INSTALL_PATH/logwatch-analyzer-v1"

new_case writable-rollback-ancestor
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v1" v1
seed_live v2
printf '%s\n' "$INSTALL_PATH/logwatch-analyzer-v1" > "$INSTALL_PATH/.logwatch-analyzer.prev-target"
chmod 0777 "$CASE_ROOT"
if output=$(run_rollback 0 2>&1); then
    bad "rollback with writable target ancestor aborts"
else
    ok "rollback with writable target ancestor aborts"
fi
chmod 0700 "$CASE_ROOT"
contains "writable rollback ancestor is diagnosed" "$output" "non-sticky writable ancestor"
same_file "writable rollback ancestor leaves v2 live" \
    "$INSTALL_PATH/logwatch-analyzer" "$INSTALL_PATH/logwatch-analyzer-v2"

if [[ $(uname -s) == Linux ]]; then
    verify_stage=$(mktemp -d /tmp/logwatch-deploy.XXXXXXXXXX)
    verify_parent="$TEST_ROOT/writable-verify-parent"
    verify_install="$verify_parent/install"
    mkdir -p "$verify_install"
    make_analyzer "$verify_stage/logwatch-analyzer" candidate
    chmod 0777 "$verify_parent"
    if output=$(BIN_SHA=$(printf '0%.0s' {1..64}) STAGE_DIR="$verify_stage" \
        INSTALL_DIR="$verify_install" "$REAL_BASH" "$REMOTE_VERIFY" 2>&1); then
        bad "verification with writable install ancestor aborts"
    else
        ok "verification with writable install ancestor aborts"
    fi
    contains "writable verification ancestor is diagnosed" "$output" "non-sticky writable ancestor"
    rm -rf -- "$verify_stage"
else
    ok "remote verification ancestor regression is exercised in Linux CI"
fi

new_case lock-symlink
seed_live v1
printf '%s\n' sentinel > "$CASE_ROOT/lock-target"
ln -s "$CASE_ROOT/lock-target" "$LOCK_PATH"
if output=$(run_install logwatch-analyzer-v2 2>&1); then
    bad "symlink lock aborts deployment"
else
    ok "symlink lock aborts deployment"
fi
contains "symlink lock is diagnosed" "$output" "lock file must not be a symlink"
if [[ $(<"$CASE_ROOT/lock-target") == sentinel ]]; then
    ok "symlink lock target is untouched"
else
    bad "symlink lock target is untouched"
fi

echo "a dangling live symlink is a recoverable state"
new_case dangling
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v0"
printf '%s\n' "$INSTALL_PATH/logwatch-analyzer-v0" > "$INSTALL_PATH/.logwatch-analyzer.prev-target"
ln -s "$INSTALL_PATH/missing-binary" "$INSTALL_PATH/logwatch-analyzer"
if output=$(run_install logwatch-analyzer-v2 2>&1); then
    bad "dangling predecessor requires an explicit override"
else
    ok "dangling predecessor requires an explicit override"
fi
contains "dangling predecessor is diagnosed accurately" "$output" "not a managed analyzer artifact"
if output=$(run_install logwatch-analyzer-v2 1 2>&1); then
    ok "FORCE=1 can repair a dangling live symlink"
else
    bad "FORCE=1 can repair a dangling live symlink" "$output"
fi
same_file "forced repair publishes v2" "$INSTALL_PATH/logwatch-analyzer" "$INSTALL_PATH/logwatch-analyzer-v2"
recorded=$(<"$INSTALL_PATH/.logwatch-analyzer.prev-target")
if [[ $recorded == "$INSTALL_PATH/logwatch-analyzer-v0" ]]; then
    ok "forced repair leaves the last valid rollback record unchanged"
else
    bad "forced repair leaves the last valid rollback record unchanged" "$recorded"
fi

echo "rollback diagnostics and record consumption"
new_case missing-record
ln -s /usr/bin/true "$INSTALL_PATH/logwatch-analyzer"
if output=$(run_rollback 0 2>&1); then
    bad "rollback without a record fails"
else
    ok "rollback without a record fails"
fi
contains "missing-record output includes manual recovery" "$output" "Re-point by hand"
contains "missing-record output handles an empty artifact list" "$output" "(none)"

new_case rollback-success
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v1" v1
seed_live v2
printf '%s\n' "$INSTALL_PATH/logwatch-analyzer-v1" > "$INSTALL_PATH/.logwatch-analyzer.prev-target"
if output=$(run_rollback 0 2>&1); then
    ok "rollback succeeds"
else
    bad "rollback succeeds" "$output"
fi
same_file "rollback publishes v1" "$INSTALL_PATH/logwatch-analyzer" "$INSTALL_PATH/logwatch-analyzer-v1"
if [[ ! -e $INSTALL_PATH/.logwatch-analyzer.prev-target ]]; then
    ok "rollback consumes its record"
else
    bad "rollback consumes its record"
fi

echo "rollback interruption retains a retryable record"
new_case rollback-record-signal
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v1" v1
seed_live v2
printf '%s\n' "$INSTALL_PATH/logwatch-analyzer-v1" > "$INSTALL_PATH/.logwatch-analyzer.prev-target"
TEST_SIGNAL_AFTER_ROLLBACK_PUBLISH=1
if output=$(run_rollback 0 2>&1); then
    bad "post-publication signal interrupts rollback"
else
    ok "post-publication signal interrupts rollback"
fi
unset TEST_SIGNAL_AFTER_ROLLBACK_PUBLISH
same_file "post-publication signal leaves v1 live" "$INSTALL_PATH/logwatch-analyzer" \
    "$INSTALL_PATH/logwatch-analyzer-v1"
recorded=$(<"$INSTALL_PATH/.logwatch-analyzer.prev-target")
if [[ $recorded == "$INSTALL_PATH/logwatch-analyzer-v1" ]]; then
    ok "post-publication signal retains the rollback record"
else
    bad "post-publication signal retains the rollback record" "$recorded"
fi
if output=$(run_rollback 0 2>&1); then
    ok "interrupted rollback can be retried safely"
else
    bad "interrupted rollback can be retried safely" "$output"
fi
if [[ ! -e $INSTALL_PATH/.logwatch-analyzer.prev-target ]]; then
    ok "retry consumes the retained rollback record"
else
    bad "retry consumes the retained rollback record"
fi
no_rollback_temps "$INSTALL_PATH" "post-publication signal cleans rollback temporaries"

echo "failed rollback smoke restores the original live target"
new_case rollback-smoke-failure
make_symlink_sensitive_analyzer "$INSTALL_PATH/logwatch-analyzer-v1" v1
seed_live v2
printf '%s\n' "$INSTALL_PATH/logwatch-analyzer-v1" > "$INSTALL_PATH/.logwatch-analyzer.prev-target"
if output=$(run_rollback 0 2>&1); then
    bad "rollback smoke failure returns nonzero"
else
    ok "rollback smoke failure returns nonzero"
fi
same_file "rollback smoke failure restores v2" "$INSTALL_PATH/logwatch-analyzer" \
    "$INSTALL_PATH/logwatch-analyzer-v2"
recorded=$(<"$INSTALL_PATH/.logwatch-analyzer.prev-target")
if [[ $recorded == "$INSTALL_PATH/logwatch-analyzer-v1" ]]; then
    ok "rollback smoke failure restores the rollback record"
else
    bad "rollback smoke failure restores the rollback record" "$recorded"
fi
contains "rollback smoke recovery is reported" "$output" "restored the original live target"
no_rollback_temps "$INSTALL_PATH" "rollback smoke recovery cleans temporaries"

echo "failed record consumption restores an honest retry state"
new_case rollback-consume-failure
make_analyzer "$INSTALL_PATH/logwatch-analyzer-v1" v1
seed_live v2
printf '%s\n' "$INSTALL_PATH/logwatch-analyzer-v1" > "$INSTALL_PATH/.logwatch-analyzer.prev-target"
TEST_FAIL_RECORD_CONSUME=1
if output=$(run_rollback 0 2>&1); then
    ok "record-consumption failure keeps the successful rollback"
else
    bad "record-consumption failure keeps the successful rollback" "$output"
fi
unset TEST_FAIL_RECORD_CONSUME
same_file "record-consumption failure leaves v1 live" "$INSTALL_PATH/logwatch-analyzer" \
    "$INSTALL_PATH/logwatch-analyzer-v1"
recorded=$(<"$INSTALL_PATH/.logwatch-analyzer.prev-target")
if [[ $recorded == "$INSTALL_PATH/logwatch-analyzer-v1" ]]; then
    ok "record-consumption failure restores the retry record"
else
    bad "record-consumption failure restores the retry record" "$recorded"
fi
contains "retained record is reported honestly" "$output" "record retained"
no_rollback_temps "$INSTALL_PATH" "record-consumption failure cleans temporaries"

echo
if [[ $fail -eq 0 ]]; then
    echo "PASS — $pass assertions"
    exit 0
fi
echo "FAIL — $fail of $((pass + fail))"
exit 1
