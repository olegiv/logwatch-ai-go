# shellcheck shell=bash
#
# Helpers prepended to every remote payload sent by the deploy/* scripts.
# Kept in one file so deploy.sh and rollback.sh cannot drift apart on how
# they read configuration or take the cron lock. Runs on the TARGET host.
#
# Expects INSTALL_DIR in the environment and the caller to have cd'd there.

# read_env KEY [DEFAULT]
#
# Reads a value from .env the way the application's godotenv loader does,
# rather than by deleting characters: takes the last assignment, strips a
# surrounding quote pair, and drops an inline `# comment` only from an
# unquoted value. Deleting quotes and spaces instead would corrupt a
# legitimate path such as DATABASE_PATH="/opt/logwatch ai/data/x.db".
read_env() {
    local key="$1" def="${2-}" line val
    line=$(grep -E "^[[:space:]]*(export[[:space:]]+)?${key}[[:space:]]*=" .env 2>/dev/null | tail -1) || true
    if [ -z "$line" ]; then printf '%s' "$def"; return 0; fi
    val=${line#*=}
    val=${val#"${val%%[![:space:]]*}"}          # trim leading whitespace
    case "$val" in
        '"'*) val=${val#\"}; val=${val%%\"*} ;;
        "'"*) val=${val#\'}; val=${val%%\'*} ;;
        *)    val=${val%%#*}
              val=${val%"${val##*[![:space:]]}"} ;;   # trim trailing whitespace
    esac
    printf '%s' "$val"
}

# resolve_db
#
# Prints the absolute database path on stdout, or returns 1 when the
# database is disabled. Relative paths resolve against INSTALL_DIR because
# that is the working directory the analyzer runs from.
resolve_db() {
    local enabled dbrel
    enabled=$(read_env ENABLE_DATABASE true | tr '[:upper:]' '[:lower:]')
    [ "$enabled" = "true" ] || return 1
    dbrel=$(read_env DATABASE_PATH ./data/summaries.db)
    case "$dbrel" in
        /*) printf '%s' "$dbrel" ;;
        *)  printf '%s/%s' "$INSTALL_DIR" "${dbrel#./}" ;;
    esac
}

# acquire_cron_lock
#
# Takes the same exclusive flock the cron runner uses, held for the whole
# remote critical section. A pgrep snapshot is only instantaneous: cron
# could start immediately after the probe and overlap a database copy or a
# script swap. pgrep is still checked afterwards to catch a manual run
# started outside run-cron.sh, which holds no lock.
acquire_cron_lock() {
    local lock="${LOCK_FILE:-/run/logwatch-ai-cron.lock}"
    if ! command -v flock >/dev/null 2>&1; then
        echo "WARN: flock(1) not on PATH — falling back to a pgrep check only" >&2
    else
        exec 9>"$lock" || { echo "ABORT: cannot open lock $lock" >&2; return 1; }
        if ! flock -n 9; then
            echo "ABORT: the cron runner holds $lock — a run is in progress" >&2
            return 1
        fi
    fi
    if pgrep -f 'run-cron\.sh|logwatch-analyzer' >/dev/null 2>&1; then
        echo "ABORT: an analyzer process is running outside the lock" >&2
        pgrep -a -f 'run-cron\.sh|logwatch-analyzer' >&2
        return 1
    fi
    return 0
}
