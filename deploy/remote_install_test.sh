#!/usr/bin/env bash
# Hermetic state-machine tests for remote-install.sh and remote-rollback.sh.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REMOTE_INSTALL="$SCRIPT_DIR/remote-install.sh"
REMOTE_ROLLBACK="$SCRIPT_DIR/remote-rollback.sh"

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
        || compgen -G "$1/.logwatch-analyzer.prev-target.new.*" >/dev/null; then
        bad "$2" "temporary deployment files remain"
    else
        ok "$2"
    fi
}

TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/logwatch-remote-test.XXXXXXXXXX")
case "$TEST_ROOT" in
    /tmp/logwatch-remote-test.*|/private/tmp/logwatch-remote-test.*|/var/folders/*/logwatch-remote-test.*) ;;
    *) echo "unsafe temporary test directory: $TEST_ROOT" >&2; exit 1 ;;
esac
trap 'rm -rf -- "$TEST_ROOT"' EXIT

REAL_BASH=$(command -v bash)
REAL_INSTALL=$(command -v install)
REAL_LN=$(command -v ln)
REAL_MV=$(command -v mv)
export REAL_INSTALL REAL_LN REAL_MV

SHIM_DIR="$TEST_ROOT/bin"
NO_FLOCK_DIR="$TEST_ROOT/no-flock-bin"
mkdir "$SHIM_DIR" "$NO_FLOCK_DIR"

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
    'exec "$REAL_LN" "$@"'

# shellcheck disable=SC2016 # these are literal lines for the generated shim
make_script "$SHIM_DIR/mv" \
    'last=${!#}' \
    'if [[ ${TEST_FAIL_RECORD_PUBLISH:-0} == 1 && $last == ./.logwatch-analyzer.prev-target ]]; then exit 74; fi' \
    'args=()' \
    'for arg in "$@"; do' \
    '  if [[ $(uname -s) == Darwin ]]; then' \
    '    case "$arg" in -Tf|-fT) args+=(-f) ;; -T) continue ;; *) args+=("$arg") ;; esac' \
    '  else' \
    '    args+=("$arg")' \
    '  fi' \
    'done' \
    'exec "$REAL_MV" "${args[@]}"'

TEST_PATH="$SHIM_DIR:$PATH"

make_analyzer() {
    local path=$1
    local label=${2:-analyzer}
    local version_rc=${3:-0}
    {
        printf '%s\n' '#!/usr/bin/env bash'
        printf 'label=%q\n' "$label"
        printf 'version_rc=%q\n' "$version_rc"
        # shellcheck disable=SC2016 # literal lines for the generated analyzer
        printf '%s\n' \
            'if [[ ${1:-} == -version ]]; then' \
            '  printf "%s\n" "$label"' \
            '  exit "$version_rc"' \
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
    TEST_FAIL_RECORD_PUBLISH="${TEST_FAIL_RECORD_PUBLISH:-0}" \
    INSTALL_DIR="${INSTALL_OVERRIDE:-$INSTALL_PATH}" STAGE_DIR="$STAGE_PATH" \
    REMOTE_BIN="$remote_bin" LOCK_FILE="$LOCK_PATH" FORCE="$force" \
    PATH="$TEST_PATH" "$REAL_BASH" "$REMOTE_INSTALL"
}

run_rollback() {
    local force=$1
    INSTALL_DIR="$INSTALL_PATH" LOCK_FILE="$LOCK_PATH" FORCE="$force" \
    TEST_FLOCK_RC="${TEST_FLOCK_RC:-0}" PATH="$TEST_PATH" \
    "$REAL_BASH" "$REMOTE_ROLLBACK"
}

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
if [[ $recorded != "$INSTALL_PATH/logwatch-analyzer-v1" ]]; then
    ok "rollback record names the preserved inode"
else
    bad "rollback record names the preserved inode" "record points at the overwritten path"
fi
if [[ -e $recorded && ! $recorded -ef "$INSTALL_PATH/logwatch-analyzer-v1" ]]; then
    ok "preserved inode differs from the replacement"
else
    bad "preserved inode differs from the replacement"
fi
no_install_temps "$INSTALL_PATH" "same-version deployment cleans temporary files"

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
contains "dangling predecessor is diagnosed accurately" "$output" "rollback target does not run"
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

echo
if [[ $fail -eq 0 ]]; then
    echo "PASS — $pass assertions"
    exit 0
fi
echo "FAIL — $fail of $((pass + fail))"
exit 1
