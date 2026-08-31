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
    local depth="${3:-0}"
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

    # godotenv expands $VAR and ${VAR} against earlier assignments, so a valid
    # config like DATA_DIR=/var/lib/lw + DATABASE_PATH=${DATA_DIR}/x.db would
    # otherwise be looked for under a literal "${DATA_DIR}" and its backup
    # silently skipped. Substitution is by lookup, never eval; the depth guard
    # stops a self-referential .env from looping.
    if [ "$depth" -lt 5 ]; then
        local out="" rest="$val" name
        while [[ $rest =~ ^([^$]*)\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?(.*)$ ]]; do
            out+="${BASH_REMATCH[1]}"
            name="${BASH_REMATCH[2]}"
            out+="$(read_env "$name" "" $((depth + 1)))"
            rest="${BASH_REMATCH[3]}"
        done
        val="$out$rest"
    fi
    printf '%s' "$val"
}

# resolve_db
#
# Prints the absolute database path on stdout, or returns 1 when the
# database is disabled. Relative paths resolve against INSTALL_DIR because
# that is the working directory the analyzer runs from.
resolve_db() {
    local enabled dbrel
    enabled=$(read_env ENABLE_DATABASE true)
    # The application reads this with viper.GetBool, i.e. strconv.ParseBool,
    # which accepts 1/t/T/TRUE/true/True as true. Accepting only the literal
    # "true" here would report a live database as disabled and silently skip
    # its backup.
    case "$enabled" in
        1|t|T|true|TRUE|True) ;;
        *) return 1 ;;
    esac
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
# resolve_lock_file
#
# Prints the lock path the deployed runner will actually use. An explicit
# LOCK_FILE in the environment wins; otherwise the crontab entry is
# consulted, because pinning LOCK_FILE there is the documented workaround
# when /run is unsuitable (rollback.sh recommends exactly that), and a
# deploy that locked the default path would not exclude such a run. The
# deployed script's own default is the next source, then the built-in.
resolve_lock_file() {
    local from_cron from_script
    if [ -n "${LOCK_FILE:-}" ]; then printf '%s' "$LOCK_FILE"; return 0; fi

    # `grep -v '^[[:space:]]*#'` matters: an operator who pins LOCK_FILE and
    # leaves the previous entry commented out would otherwise have the dead
    # line win, and the deploy would lock a path cron never touches.
    # The sed alternation preserves a quoted path containing spaces, which a
    # `[^[:space:]]+` match truncates.
    from_cron=$(crontab -l -u root 2>/dev/null \
        | grep -v '^[[:space:]]*#' \
        | grep -F 'run-cron.sh' \
        | sed -nE 's/.*LOCK_FILE="([^"]*)".*/\1/p;
                   s/.*LOCK_FILE='"'"'([^'"'"']*)'"'"'.*/\1/p;
                   s/.*LOCK_FILE=([^"'"'"'[:space:]][^[:space:]]*).*/\1/p' \
        | tail -1)
    if [ -n "$from_cron" ]; then printf '%s' "$from_cron"; return 0; fi

    # shellcheck disable=SC2016 # the ${...} here is literal text being matched
    from_script=$(sed -n 's/^[[:space:]]*LOCK_FILE="\${LOCK_FILE:-\([^}]*\)}".*/\1/p' \
        "$INSTALL_DIR/run-cron.sh" 2>/dev/null | tail -1)
    if [ -n "$from_script" ]; then printf '%s' "$from_script"; return 0; fi

    printf '%s' /run/logwatch-ai-cron.lock
}

acquire_cron_lock() {
    local lock
    lock=$(resolve_lock_file)
    echo "  cron lock: $lock"
    if ! command -v flock >/dev/null 2>&1; then
        echo "WARN: flock(1) not on PATH — falling back to a pgrep check only" >&2
    else
        exec 9>"$lock" || { echo "ABORT: cannot open lock $lock" >&2; return 1; }
        if ! flock -n 9; then
            if [ "${FORCE:-0}" = 1 ]; then
                # The case that most needs a rollback is a wedged runner, and
                # a wedged runner is holding this lock — so FORCE has to cover
                # the lock as well as the process check, or it is inert
                # exactly when it is needed.
                echo "WARN: $lock is held; FORCE=1 given, proceeding without it" >&2
            else
                echo "ABORT: the cron runner holds $lock — a run is in progress" >&2
                echo "       (set FORCE=1 to override — needed to roll back a wedged run)" >&2
                return 1
            fi
        fi
    fi
    # Anchor on the install path so an editor or pager holding the file open
    # ("vim /opt/logwatch-ai/run-cron.sh") does not look like a running job:
    # pgrep -f matches the whole command line, so ^ requires the process to BE
    # the runner or the analyzer, not merely to mention it.
    # Accept the bare, absolute and ./-relative forms. An operator running a
    # manual analysis from the install directory types `./logwatch-analyzer
    # -source-type ocms`, which holds no cron lock — exactly the case this
    # check exists to catch — so omitting `./` would let a deploy proceed
    # alongside it and copy the database mid-write.
    local pat="^(${INSTALL_DIR}/|\./)?(run-cron\.sh|logwatch-analyzer)"
    if pgrep -u root -f "$pat" >/dev/null 2>&1; then
        if [ "${FORCE:-0}" = 1 ]; then
            echo "WARN: an analyzer process is running; FORCE=1 given, continuing anyway" >&2
            pgrep -u root -a -f "$pat" >&2
            return 0
        fi
        echo "ABORT: an analyzer process is running outside the lock" >&2
        pgrep -u root -a -f "$pat" >&2
        echo "       (set FORCE=1 to override — needed if a wedged run must be rolled back)" >&2
        return 1
    fi
    return 0
}
