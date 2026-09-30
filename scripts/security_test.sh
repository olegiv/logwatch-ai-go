#!/usr/bin/env bash
# Hermetic security regression tests for cron and generator scripts.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
pass=0
fail=0
ok() { printf '  ok    %s\n' "$1"; pass=$((pass + 1)); }
bad() { printf '  FAIL  %s%s\n' "$1" "${2:+ — $2}"; fail=$((fail + 1)); }

TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/logwatch-script-test.XXXXXXXX")
case "$TEST_ROOT" in
    /tmp/logwatch-script-test.*|/private/tmp/logwatch-script-test.*|/var/folders/*/logwatch-script-test.*) ;;
    *) echo "unsafe temporary test directory: $TEST_ROOT" >&2; exit 1 ;;
esac
TEST_ROOT=$(cd "$TEST_ROOT" && pwd -P)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
mkdir "$TEST_ROOT/bin"
printf '%s\n' '#!/usr/bin/env bash' 'exit 0' > "$TEST_ROOT/bin/flock"
chmod 0755 "$TEST_ROOT/bin/flock"
TEST_PATH="$TEST_ROOT/bin:$PATH"
CRON_LOG="$TEST_ROOT/cron.log"

# shellcheck source=scripts/helper.sh
. "$SCRIPT_DIR/helper.sh"

echo "bootstrap helpers reject noncanonical roots and select host artifacts"
for invalid_root in /opt/logwatch-ai/ /opt//logwatch-ai /opt/./logwatch-ai /opt/x/../logwatch-ai /srv/logwatch-ai; do
    if validate_install_dir "$invalid_root"; then
        bad "noncanonical install root is rejected: $invalid_root"
    else
        ok "noncanonical install root is rejected: $invalid_root"
    fi
done
if validate_install_dir /opt/logwatch-ai && validate_install_dir /usr/local/logwatch-ai; then
    ok "canonical install roots are accepted"
else
    bad "canonical install roots are accepted"
fi
if [[ $(platform_binary_name Linux x86_64) == logwatch-analyzer-linux-amd64 \
    && $(platform_binary_name Darwin arm64) == logwatch-analyzer-darwin-arm64 ]]; then
    ok "platform-specific artifact names are selected"
else
    bad "platform-specific artifact names are selected"
fi

echo "cron template binds Logwatch producer and consumer to one gated path"
cron_template=$(<"$SCRIPT_DIR/run-cron.sh.example")
if [[ $cron_template == *"generate-logwatch.sh \"\$LOGWATCH_REPORT_PATH\""* \
    && $cron_template == *"-source-path \"\$LOGWATCH_REPORT_PATH\""* ]]; then
    ok "Logwatch producer and consumer share LOGWATCH_REPORT_PATH"
else
    bad "Logwatch producer and consumer share LOGWATCH_REPORT_PATH"
fi
if [[ $cron_template == *"generate-logwatch.sh \"\$LOGWATCH_REPORT_PATH\" 10 yesterday || exit"* \
    && $cron_template == *"-source-path \"\$LOGWATCH_REPORT_PATH\" || exit"* ]]; then
    ok "Logwatch producer and consumer are success-gated"
else
    bad "Logwatch producer and consumer are success-gated"
fi

echo "cron lock rejects symlinks without touching their target"
mkdir "$TEST_ROOT/install"
printf '%s\n' sentinel > "$TEST_ROOT/target"
ln -s "$TEST_ROOT/target" "$TEST_ROOT/cron.lock"
if output=$(INSTALL_DIR="$TEST_ROOT/install" LOCK_FILE="$TEST_ROOT/cron.lock" \
    CRON_LOG="$CRON_LOG" PATH="$TEST_PATH" \
    bash "$SCRIPT_DIR/run-cron.sh.example" 2>&1); then
    bad "symlink lock is rejected"
else
    ok "symlink lock is rejected"
fi
output+=$(<"$CRON_LOG")
if [[ $(<"$TEST_ROOT/target") == sentinel ]]; then
    ok "symlink target is unchanged"
else
    bad "symlink target is unchanged"
fi
if [[ $output == *"lock path must be"* ]]; then
    ok "lock rejection is diagnosed"
else
    bad "lock rejection is diagnosed" "$output"
fi

echo "cron log rejects symlinks in root-controlled output path"
printf '%s\n' cron-sentinel > "$TEST_ROOT/cron-target"
ln -s "$TEST_ROOT/cron-target" "$TEST_ROOT/cron-symlink.log"
if output=$(INSTALL_DIR="$TEST_ROOT/install" LOCK_FILE="$TEST_ROOT/safe.lock" \
    CRON_LOG="$TEST_ROOT/cron-symlink.log" PATH="$TEST_PATH" \
    bash "$SCRIPT_DIR/run-cron.sh.example" 2>&1); then
    bad "symlink cron log is rejected"
else
    ok "symlink cron log is rejected"
fi
if [[ $(<"$TEST_ROOT/cron-target") == cron-sentinel ]]; then
    ok "cron log symlink target is unchanged"
else
    bad "cron log symlink target is unchanged"
fi

echo "cron runner fails closed without flock"
mkdir "$TEST_ROOT/no-flock-bin"
for command_name in date id stat; do
    ln -s "$(command -v "$command_name")" "$TEST_ROOT/no-flock-bin/$command_name"
done
: > "$CRON_LOG"
if output=$(INSTALL_DIR="$TEST_ROOT/install" LOCK_FILE="$TEST_ROOT/safe.lock" \
    CRON_LOG="$CRON_LOG" PATH="$TEST_ROOT/no-flock-bin" \
    /bin/bash "$SCRIPT_DIR/run-cron.sh.example" 2>&1); then
    bad "missing flock aborts cron runner"
else
    ok "missing flock aborts cron runner"
fi
output+=$(<"$CRON_LOG")
if [[ $output == *"refusing to run without overlap protection"* ]]; then
    ok "missing flock is diagnosed"
else
    bad "missing flock is diagnosed" "$output"
fi

echo "cron lock rejects writable parent directories"
mkdir "$TEST_ROOT/unsafe-locks"
chmod 0777 "$TEST_ROOT/unsafe-locks"
: > "$CRON_LOG"
if output=$(INSTALL_DIR="$TEST_ROOT/install" LOCK_FILE="$TEST_ROOT/unsafe-locks/cron.lock" \
    CRON_LOG="$CRON_LOG" PATH="$TEST_PATH" \
    bash "$SCRIPT_DIR/run-cron.sh.example" 2>&1); then
    bad "writable lock directory is rejected"
else
    ok "writable lock directory is rejected"
fi
output+=$(<"$CRON_LOG")
if [[ $output == *"non-sticky writable ancestor"* ]]; then
    ok "unsafe lock directory is diagnosed"
else
    bad "unsafe lock directory is diagnosed" "$output"
fi

echo "cron lock rejects a writable ancestor above a safe lock directory"
mkdir -p "$TEST_ROOT/unsafe-parent/safe-locks"
chmod 0777 "$TEST_ROOT/unsafe-parent"
chmod 0700 "$TEST_ROOT/unsafe-parent/safe-locks"
: > "$CRON_LOG"
if output=$(INSTALL_DIR="$TEST_ROOT/install" LOCK_FILE="$TEST_ROOT/unsafe-parent/safe-locks/cron.lock" \
    CRON_LOG="$CRON_LOG" PATH="$TEST_PATH" \
    bash "$SCRIPT_DIR/run-cron.sh.example" 2>&1); then
    bad "writable lock ancestor is rejected"
else
    ok "writable lock ancestor is rejected"
fi
output+=$(<"$CRON_LOG")
if [[ $output == *"non-sticky writable ancestor"* ]]; then
    ok "unsafe lock ancestor is diagnosed"
else
    bad "unsafe lock ancestor is diagnosed" "$output"
fi

echo "Logwatch generator rejects the shared temporary directory"
if output=$(bash "$SCRIPT_DIR/generate-logwatch.sh" "$TEST_ROOT/report.txt" 0 yesterday 2>&1); then
    bad "temporary output root is rejected"
else
    ok "temporary output root is rejected"
fi
if [[ ! -e $TEST_ROOT/report.txt ]]; then
    ok "rejected Logwatch destination is untouched"
else
    bad "rejected Logwatch destination is untouched"
fi

echo "Logwatch producer is terminated at its deadline"
logwatch_output_dir="$TEST_ROOT/logwatch-output"
fake_logwatch="$TEST_ROOT/bin/fake-logwatch"
mkdir "$logwatch_output_dir"
cat > "$fake_logwatch" <<'EOF'
#!/usr/bin/env bash
sleep 30 &
wait
EOF
chmod 0755 "$fake_logwatch"
started_at=$SECONDS
if output=$(LOGWATCH_BIN_OVERRIDE="$fake_logwatch" \
    LOGWATCH_OUTPUT_ROOTS="$logwatch_output_dir" LOGWATCH_TIMEOUT_SECONDS=1 \
    bash "$SCRIPT_DIR/generate-logwatch.sh" "$logwatch_output_dir/report.txt" 0 yesterday 2>&1); then
    bad "Logwatch timeout returns failure" "$output"
else
    ok "Logwatch timeout returns failure"
fi
if (( SECONDS - started_at <= 5 )); then
    ok "Logwatch timeout terminates promptly"
else
    bad "Logwatch timeout terminates promptly" "$output"
fi
if [[ ! -e $logwatch_output_dir/report.txt ]]; then
    ok "timed-out Logwatch output is not published"
else
    bad "timed-out Logwatch output is not published"
fi

echo "Logwatch producer enforces the configured output-size limit"
large_logwatch="$TEST_ROOT/bin/large-logwatch"
cat > "$large_logwatch" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
filename=''
while (($# > 0)); do
    case "$1" in
        --filename) filename=$2; shift 2 ;;
        *) shift ;;
    esac
done
[[ -n $filename ]]
dd if=/dev/zero of="$filename" bs=2048 count=1 2>/dev/null
EOF
chmod 0755 "$large_logwatch"
printf '%s\n' previous-report > "$logwatch_output_dir/report.txt"
if output=$(LOGWATCH_BIN_OVERRIDE="$large_logwatch" \
    LOGWATCH_OUTPUT_ROOTS="$logwatch_output_dir" LOGWATCH_MAX_OUTPUT_BYTES=1024 \
    bash "$SCRIPT_DIR/generate-logwatch.sh" "$logwatch_output_dir/report.txt" 0 yesterday 2>&1); then
    bad "oversized Logwatch output returns failure" "$output"
else
    ok "oversized Logwatch output returns failure"
fi
if [[ $(<"$logwatch_output_dir/report.txt") == previous-report ]]; then
    ok "oversized Logwatch output preserves the previous report"
else
    bad "oversized Logwatch output preserves the previous report"
fi

echo "Drupal generator forwards severity to Drush"
if ! command -v jq >/dev/null 2>&1; then
    bad "jq is available for the Drupal generator test"
else
    drupal_root="$TEST_ROOT/drupal"
    output_dir="$TEST_ROOT/output"
    output_path="$output_dir/watchdog.json"
    args_file="$TEST_ROOT/drush-args"
    config_file="$TEST_ROOT/drupal-sites.json"
    mkdir -p "$drupal_root/vendor/bin" "$output_dir"
    export DRUPAL_OUTPUT_ROOTS="$output_dir"
    jq -n --arg root "$drupal_root" --arg output "$output_path" \
        --arg user "$(id -un)" '{
            version: "1.0",
            default_site: "test",
            sites: {test: {
                name: "Test",
                drupal_root: $root,
                system_user: $user,
                watchdog_path: $output,
                watchdog_format: "json",
                min_severity: 3,
                watchdog_limit: 10
            }}
        }' > "$config_file"
    {
        printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail'
        # shellcheck disable=SC2016 # literal lines for the generated Drush stub
        printf '%s\n' \
            'printf "%s\n" "$*" > "$DRUSH_ARGS_FILE"' \
            'if [[ -n ${DRUSH_SLEEP_SECONDS:-} ]]; then sleep "$DRUSH_SLEEP_SECONDS"; fi' \
            'if [[ ${DRUSH_USE_TODAY:-0} == 1 ]]; then timestamp=$(date +%s)' \
            'elif [[ $OSTYPE == darwin* ]]; then' \
            '  timestamp=$(date -v-1d -v12H -v0M -v0S +%s)' \
            'else' \
            '  timestamp=$(date -d "yesterday 12:00:00" +%s)' \
            'fi' \
            'severity=${DRUSH_SEVERITY_LABEL:-Error}' \
            'type=${DRUSH_TYPE:-php}' \
            'message=${DRUSH_MESSAGE:-error}' \
            'if [[ -n ${DRUSH_PAYLOAD_FILE:-} ]]; then cat "$DRUSH_PAYLOAD_FILE"; exit 0; fi' \
            'jq -nc --arg severity "$severity" --arg type "$type" --arg message "$message" --argjson timestamp "$timestamp" '\''{"1":{wid:1,uid:0,type:$type,message:$message,severity:$severity,timestamp:$timestamp},"2":{wid:2,uid:0,type:"system",message:"routine warning",severity:"Warning",timestamp:$timestamp}}'\'''
    } > "$drupal_root/vendor/bin/drush"
    chmod 0755 "$drupal_root/vendor/bin/drush"

    if output=$(DRUSH_ARGS_FILE="$args_file" bash "$SCRIPT_DIR/generate-drupal-watchdog.sh" \
        --sites-config "$config_file" --site test --severity error,warning 2>&1); then
        ok "Drupal generator succeeds with explicit severity"
    else
        bad "Drupal generator succeeds with explicit severity" "$output"
    fi
    if [[ -f $args_file && $(<"$args_file") == *"--severity=error,warning"* ]]; then
        ok "explicit severity reaches the primary Drush command"
    else
        bad "explicit severity reaches the primary Drush command"
    fi
    if [[ -f $output_path && $(jq 'length' "$output_path") == 2 ]]; then
        ok "explicit severity bypasses the configured numeric threshold"
    else
        bad "explicit severity bypasses the configured numeric threshold" "$output"
    fi

    if output=$(DRUSH_ARGS_FILE="$args_file" DRUSH_SEVERITY_LABEL=Warning \
        DRUSH_TYPE=user DRUSH_MESSAGE='Login attempt failed from 203.0.113.44' \
        bash "$SCRIPT_DIR/generate-drupal-watchdog.sh" \
        --sites-config "$config_file" --site test 2>&1); then
        ok "security warnings survive a stricter configured severity threshold"
    else
        bad "security warnings survive a stricter configured severity threshold" "$output"
    fi
    if [[ -f $output_path && $(jq '[.[] | select(.type == "user")] | length' "$output_path") == 1 ]]; then
        ok "authentication warning evidence is published"
    else
        bad "authentication warning evidence is published" "$output"
    fi

    default_config_file="$TEST_ROOT/drupal-sites-default-severity.json"
    jq 'del(.sites.test.min_severity)' "$config_file" > "$default_config_file"
    if output=$(DRUSH_ARGS_FILE="$args_file" DRUSH_SEVERITY_LABEL=Warning \
        DRUSH_TYPE=system DRUSH_MESSAGE='routine warning' \
        bash "$SCRIPT_DIR/generate-drupal-watchdog.sh" \
        --sites-config "$default_config_file" --site test 2>&1); then
        ok "default Drupal severity includes warnings"
    else
        bad "default Drupal severity includes warnings" "$output"
    fi
    if [[ -f $output_path && $(jq 'length' "$output_path") == 2 ]]; then
        ok "warning-level entries are present under the secure default"
    else
        bad "warning-level entries are present under the secure default" "$output"
    fi

    priority_payload="$TEST_ROOT/drush-priority-payload.json"
    if [[ $OSTYPE == darwin* ]]; then
        priority_timestamp=$(date -v-1d -v12H -v0M -v0S +%s)
    else
        priority_timestamp=$(date -d "yesterday 12:00:00" +%s)
    fi
    jq -n --argjson timestamp "$priority_timestamp" '
        reduce range(1; 12) as $wid ({};
            .[($wid | tostring)] = {
                wid: $wid, uid: 0, type: "php", message: "newer error",
                severity: "Error", timestamp: ($timestamp + $wid)
            }
        ) |
        .emergency = {
            wid: 99, uid: 0, type: "system", message: "older unique emergency",
            severity: "Emergency", timestamp: ($timestamp - 1)
        } |
        .authentication = {
            wid: 98, uid: 0, type: "user", message: "login attempt failed from 203.0.113.55",
            severity: "Warning", timestamp: ($timestamp - 2)
        }
    ' > "$priority_payload"
    if output=$(DRUSH_ARGS_FILE="$args_file" DRUSH_PAYLOAD_FILE="$priority_payload" \
        bash "$SCRIPT_DIR/generate-drupal-watchdog.sh" \
        --sites-config "$config_file" --site test 2>&1); then
        ok "Drupal output limit succeeds with mixed critical severities"
    else
        bad "Drupal output limit succeeds with mixed critical severities" "$output"
    fi
    if [[ -f $output_path && $(jq 'length' "$output_path") == 10 \
        && $(jq '[.[] | select(.message == "older unique emergency")] | length' "$output_path") == 1 \
        && $(jq '[.[] | select(.type == "user")] | length' "$output_path") == 1 ]]; then
        ok "Drupal output limit retains older emergency and authentication evidence"
    else
        bad "Drupal output limit retains older emergency and authentication evidence" "$output"
    fi
    if ! compgen -G "$output_dir/.watchdog.json.tmp.*" >/dev/null; then
        ok "Drupal output leaves no temporary file"
    else
        bad "Drupal output leaves no temporary file"
    fi

    sentinel_dir="$TEST_ROOT/not-allowed"
    sentinel_path="$sentinel_dir/production-watchdog.json"
    mkdir "$sentinel_dir"
    printf '%s\n' sentinel > "$sentinel_path"
    if output=$(DRUSH_ARGS_FILE="$args_file" bash "$SCRIPT_DIR/generate-drupal-watchdog.sh" \
        --sites-config "$config_file" --site test --output "$sentinel_path" 2>&1); then
        bad "Drupal output outside dedicated roots is rejected" "$output"
    else
        ok "Drupal output outside dedicated roots is rejected"
    fi
    if [[ $(<"$sentinel_path") == sentinel ]]; then
        ok "rejected Drupal destination is preserved"
    else
        bad "rejected Drupal destination is preserved"
    fi

    published_before=$(<"$output_path")
    if output=$(DRUSH_ARGS_FILE="$args_file" DRUSH_USE_TODAY=1 \
        bash "$SCRIPT_DIR/generate-drupal-watchdog.sh" \
        --sites-config "$config_file" --site test --count 1 2>&1); then
        bad "saturated Drush result is rejected as incomplete" "$output"
    else
        ok "saturated Drush result is rejected as incomplete"
    fi
    if [[ $(<"$output_path") == "$published_before" ]]; then
        ok "incomplete Drush result preserves the previous report"
    else
        bad "incomplete Drush result preserves the previous report"
    fi

    started_at=$SECONDS
    if output=$(DRUSH_ARGS_FILE="$args_file" DRUSH_SLEEP_SECONDS=30 \
        DRUPAL_EXPORT_TIMEOUT_SECONDS=1 bash "$SCRIPT_DIR/generate-drupal-watchdog.sh" \
        --sites-config "$config_file" --site test 2>&1); then
        bad "Drush timeout returns failure" "$output"
    else
        ok "Drush timeout returns failure"
    fi
    if (( SECONDS - started_at <= 5 )); then
        ok "Drush timeout terminates promptly"
    else
        bad "Drush timeout terminates promptly" "$output"
    fi
    if [[ $(<"$output_path") == "$published_before" ]]; then
        ok "timed-out Drush preserves the previous report"
    else
        bad "timed-out Drush preserves the previous report"
    fi

    full_payload="$TEST_ROOT/drush-full-payload.json"
    jq -n --argjson timestamp "$priority_timestamp" '{
        "2": {wid: 2, uid: 0, type: "php", message: "yesterday error",
              severity: "Error", timestamp: $timestamp},
        "1": {wid: 1, uid: 0, type: "php", message: "older error",
              severity: "Error", timestamp: ($timestamp - 86400)}
    }' > "$full_payload"
    if output=$(DRUSH_ARGS_FILE="$args_file" DRUSH_PAYLOAD_FILE="$full_payload" \
        bash "$SCRIPT_DIR/generate-drupal-watchdog.sh" \
        --sites-config "$config_file" --site test --count 2 2>&1); then
        ok "saturated Drush result reaching back before yesterday is accepted"
    else
        bad "saturated Drush result reaching back before yesterday is accepted" "$output"
    fi
    if [[ -f $output_path && $(jq -c '[.[].message]' "$output_path") == '["yesterday error"]' ]]; then
        ok "saturated Drush result publishes only yesterday's entries"
    else
        bad "saturated Drush result publishes only yesterday's entries" "$output"
    fi

    if output=$(DRUSH_ARGS_FILE="$args_file" bash "$SCRIPT_DIR/generate-drupal-watchdog.sh" \
        --sites-config "$config_file" --site test --format drush 2>&1); then
        bad "unsafe raw Drush table format is rejected" "$output"
    else
        ok "unsafe raw Drush table format is rejected"
    fi

    if output=$(DRUSH_ARGS_FILE="$args_file" bash "$SCRIPT_DIR/generate-drupal-watchdog.sh" \
        --sites-config "$config_file" --site test --count 0 2>&1); then
        bad "zero Drupal fetch count is rejected" "$output"
    else
        ok "zero Drupal fetch count is rejected"
    fi

    if output=$(DRUSH_ARGS_FILE="$args_file" bash "$SCRIPT_DIR/generate-drupal-watchdog.sh" \
        --sites-config "$config_file" --site test --limit 0 2>&1); then
        bad "zero Drupal output limit is rejected" "$output"
    else
        ok "zero Drupal output limit is rejected"
    fi

    if output=$(DRUSH_ARGS_FILE="$args_file" DRUSH_SEVERITY_LABEL=Unexpected \
        bash "$SCRIPT_DIR/generate-drupal-watchdog.sh" \
        --sites-config "$config_file" --site test --format json 2>&1); then
        bad "unknown watchdog severity is rejected"
    else
        ok "unknown watchdog severity is rejected"
    fi
fi

echo
if [[ $fail -eq 0 ]]; then
    echo "PASS — $pass assertions"
    exit 0
fi
echo "FAIL — $fail of $((pass + fail))"
exit 1
