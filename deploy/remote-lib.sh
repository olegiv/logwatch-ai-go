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
    local key="$1" def="${2-}" line val numbered
    local depth="${3:-0}" maxline="${4:-0}"
    # godotenv assigns top to bottom, so a reference only sees assignments
    # ABOVE it. Bounding the lookup by line number keeps a forward reference
    # unresolved here exactly as it is for the analyzer, instead of quietly
    # resolving to a different database.
    numbered=$(grep -nE "^[[:space:]]*(export[[:space:]]+)?${key}[[:space:]]*=" .env 2>/dev/null) || true
    line=$(printf '%s\n' "$numbered" \
           | awk -F: -v m="$maxline" 'NF && (m==0 || $1+0 < m+0){last=$0} END{print last}')
    local lineno="${line%%:*}"
    line="${line#*:}"
    if [ -z "$lineno" ]; then printf '%s' "$def"; return 0; fi
    val=${line#*=}
    val=${val#"${val%%[![:space:]]*}"}          # trim leading whitespace
    # godotenv suppresses variable expansion inside SINGLE quotes, so track
    # which quoting was used and skip substitution for that case.
    local literal=0
    case "$val" in
        '"'*) val=${val#\"}; val=${val%%\"*} ;;
        "'"*) val=${val#\'}; val=${val%%\'*}; literal=1 ;;
        *)    # godotenv only treats # as a comment when whitespace precedes
              # it, so a path like /srv/db#blue/x.db keeps its hash.
              # Two rules, matching godotenv: a # preceded by whitespace
              # starts a comment; a value that is ONLY a comment is empty.
              if [[ $val =~ ^(.*[^[:space:]])[[:space:]]+#.*$ ]]; then
                  val="${BASH_REMATCH[1]}"
              elif [[ $val =~ ^[[:space:]]*#.*$ ]]; then
                  val=""
              fi
              val=${val%"${val##*[![:space:]]}"} ;;   # trim trailing whitespace
    esac

    # godotenv expands $VAR and ${VAR} against earlier assignments, so a valid
    # config like DATA_DIR=/var/lib/lw + DATABASE_PATH=${DATA_DIR}/x.db would
    # otherwise be looked for under a literal "${DATA_DIR}" and its backup
    # silently skipped. Substitution is by lookup, never eval; the depth guard
    # stops a self-referential .env from looping.
    #
    # UPPERCASE ONLY, matching godotenv v1.5.1's expandVarRegex
    # `(\\)?(\$)(\()?\{?([A-Z0-9_]+)?\}?`. Accepting lowercase here resolved
    # ${data_dir}/x.db to a real path while the analyzer opened a file
    # literally named "${data_dir}/x.db" — so the deploy would snapshot, prune
    # and restore a database the analyzer never touches.
    if [ "$depth" -lt 5 ] && [ "$literal" -eq 0 ]; then
        local out="" rest="$val" name pre
        while [[ $rest =~ ^([^$]*)\$\{?([A-Z0-9_]+)\}?(.*)$ ]]; do
            pre="${BASH_REMATCH[1]}"
            name="${BASH_REMATCH[2]}"
            if [[ $pre == *\\ ]]; then
                # godotenv drops the backslash and leaves the reference
                # literal, so expanding here would resolve a path the
                # analyzer never uses.
                out+="${pre%\\}"
                out+="\${${name}}"
            else
                out+="$pre"
                out+="$(read_env "$name" "" $((depth + 1)) "$lineno")"
            fi
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
    # godotenv.Load does not override an already-set variable and viper reads
    # the environment first, so an exported DATABASE_PATH beats the file. The
    # deploy session cannot see cron's environment, but it can see whether the
    # crontab line or the runner exports one — and silently backing up a
    # different database than the analyzer writes is the failure to avoid.
    local override
    override=$( { crontab -l -u root 2>/dev/null; cat "$INSTALL_DIR/run-cron.sh" 2>/dev/null; } \
                | grep -v '^[[:space:]]*#' \
                | grep -oE '(export[[:space:]]+)?DATABASE_PATH=[^[:space:]]+' | tail -1 )
    if [ -n "$override" ]; then
        # Warning was not enough: resolve_db still returned the .env value, so
        # the deploy snapshotted one database while the analyzer wrote
        # another, and rollback --db could not recover the real one. Refuse.
        echo "ABORT: DATABASE_PATH is set outside .env ($override)." >&2
        echo "       godotenv does not override an existing environment" >&2
        echo "       variable and viper reads the environment first, so the" >&2
        echo "       analyzer uses that value while this tooling reads .env." >&2
        echo "       Backing up or restoring the wrong database is worse than" >&2
        echo "       not doing it. Remove the override, or move it into .env." >&2
        return 2
    fi
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
    # docs/CRON_SETUP.md documents /etc/cron.d/logwatch-ai as a supported
    # location, so an override there must be honoured too — otherwise the
    # documented cron job can start while the deploy holds a different lock.
    # Filter on the configured runner path, not the bare basename: a second
    # installation or a stale cron.d entry would otherwise contribute its
    # LOCK_FILE and leave this deploy holding an unrelated lock while the real
    # runner starts during a snapshot or swap.
    from_cron=$( { crontab -l -u root 2>/dev/null; cat /etc/cron.d/* 2>/dev/null; } \
        | grep -v '^[[:space:]]*#' \
        | grep -F "$INSTALL_DIR/run-cron.sh" \
        | sed -nE 's/.*LOCK_FILE="([^"]*)".*/\1/p;
                   s/.*LOCK_FILE='"'"'([^'"'"']*)'"'"'.*/\1/p;
                   s/.*LOCK_FILE=([^"'"'"'[:space:]][^[:space:]]*).*/\1/p' \
        | tail -1)
    if [ -n "$from_cron" ]; then printf '%s' "$from_cron"; return 0; fi

    # Reuse lock_path_of rather than a second, narrower parser: keeping a
    # template-only copy here meant a runner that assigns LOCK_FILE directly
    # fell back to the built-in path, and a binary-only deploy or a default
    # rollback then held a lock cron does not use.
    from_script=$(lock_path_of "$INSTALL_DIR/run-cron.sh")
    if [ -n "$from_script" ]; then printf '%s' "$from_script"; return 0; fi

    printf '%s' /run/logwatch-ai-cron.lock
}

# acquire_cron_lock
#
# Takes the same exclusive flock the cron runner uses, held for the whole
# remote critical section. A pgrep snapshot is only instantaneous: cron
# could start immediately after the probe and overlap a database copy or a
# script swap. pgrep is still checked afterwards to catch a manual run
# started outside run-cron.sh, which holds no lock.
acquire_cron_lock() {
    local lock
    lock=$(resolve_lock_file)
    echo "  cron lock: $lock"
    if ! command -v flock >/dev/null 2>&1; then
        # A pgrep snapshot cannot hold anything: cron could start immediately
        # after it and overlap the snapshot, the swap or the helper
        # replacement. Without flock there is no way to keep the
        # single-instance guarantee, so refuse rather than pretend.
        echo "ABORT: flock(1) is not available on this host." >&2
        echo "       It is required to hold the runner's lock across the" >&2
        echo "       critical section. Install util-linux, or set FORCE=1 to" >&2
        echo "       proceed with only an instantaneous process check." >&2
        [ "${FORCE:-0}" = 1 ] || return 1
        echo "WARN: FORCE=1 — proceeding with a pgrep check only" >&2
    else
        validate_lock_dir "$lock" || return 1
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
    local pat
    pat=$(inflight_pattern)
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

# inflight_pattern
#
# The predicate for "an analyzer or runner process is running". Defined once:
# preflight's gate and acquire_cron_lock both use it, and they have already
# drifted apart twice. Allows an optional interpreter word, because a shebang
# launch can appear as `/bin/bash /opt/logwatch-ai/run-cron.sh`, and the ./
# form an operator types from the install directory. Anchored, so a command
# that merely mentions the path — an editor, a pager — does not match.
inflight_pattern() {
    printf '^((/usr)?/bin/(ba)?sh[[:space:]]+)?(%s/|\./)?(run-cron\.sh|logwatch-analyzer)' \
        "$INSTALL_DIR"
}

# validate_lock_dir PATH
#
# A lock is opened by root with `exec N>`, which follows symlinks. Any
# principal that can write the directory can plant the fixed lock name and
# have root truncate the target, so the directory must be root-owned and not
# writable by group or other. Applied to every lock this tooling opens, not
# just the one preflight happens to gate.
validate_lock_dir() {
    local lock="$1" dir perm owner
    # A relative LOCK_FILE is opened by the runner before it cd's to
    # INSTALL_DIR, so cron resolves it against ITS starting directory while
    # this tooling — already cd'd — would resolve it somewhere else. The two
    # would flock different files and could overlap.
    case "$lock" in
        /*) ;;
        *)  echo "ABORT: LOCK_FILE '$lock' is relative." >&2
            echo "       cron and this tooling would resolve it to different" >&2
            echo "       files and could run concurrently. Use an absolute path." >&2
            return 1 ;;
    esac
    dir=$(dirname "$lock")
    perm=$(stat -c '%a' "$dir" 2>/dev/null) || { echo "ABORT: cannot stat $dir" >&2; return 1; }
    owner=$(stat -c '%U' "$dir" 2>/dev/null)
    if [ "$owner" != "root" ]; then
        echo "ABORT: lock directory $dir is owned by '$owner', not root" >&2
        return 1
    fi
    if [ "$((8#$perm & 0022))" -ne 0 ]; then
        echo "ABORT: lock directory $dir is group- or world-writable (mode $perm)" >&2
        return 1
    fi
    return 0
}

# acquire_extra_lock PATH
#
# Takes a second exclusive lock on fd 8. Replacing the runner can change the
# lock path it will use, and cron starting the NEW runner would then take a
# different lock than the deploy holds — so both must be held across the
# swap to keep the single-instance invariant.
acquire_extra_lock() {
    local lock="$1"
    [ -n "$lock" ] || return 0
    command -v flock >/dev/null 2>&1 || return 0
    validate_lock_dir "$lock" || return 1
    exec 8>"$lock" || { echo "ABORT: cannot open incoming lock $lock" >&2; return 1; }
    if ! flock -n 8; then
        if [ "${FORCE:-0}" = 1 ]; then
            echo "WARN: incoming lock $lock is held; FORCE=1 given, proceeding" >&2
            return 0
        fi
        echo "ABORT: the incoming runner's lock $lock is held" >&2
        return 1
    fi
    echo "  also holding incoming runner lock: $lock"
    return 0
}

# lock_path_of FILE — the LOCK_FILE default a run-cron.sh script would use.
lock_path_of() {
    # Both the template default and a plain assignment are valid in a
    # host-specific runner. Recognising only the template meant a runner
    # pinning `LOCK_FILE=/opt/logwatch-ai/.cron.lock` silently fell back to
    # the built-in path, and the deploy then held the wrong lock.
    # shellcheck disable=SC2016 # the ${...} here is literal text being matched
    sed -nE 's/^[[:space:]]*(export[[:space:]]+)?LOCK_FILE="\$\{LOCK_FILE:-([^}]*)\}".*/\2/p;
             s/^[[:space:]]*(export[[:space:]]+)?LOCK_FILE="([^"$]*)".*/\2/p;
             s/^[[:space:]]*(export[[:space:]]+)?LOCK_FILE='"'"'([^'"'"']*)'"'"'.*/\2/p;
             s/^[[:space:]]*(export[[:space:]]+)?LOCK_FILE=([^"'"'"'$[:space:]][^[:space:]]*).*/\2/p' \
        "$1" 2>/dev/null | tail -1
}
