#!/usr/bin/env bash
# Helper for shell scripts

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Log functions

echo_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

echo_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

echo_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

validate_install_dir() {
    local path=$1

    [[ $path == /opt/?* || $path == /usr/local/?* ]] || return 1
    [[ $path != */ && $path != *//* && $path != */./* && $path != */. \
        && $path != */../* && $path != */.. ]] || return 1
}

validate_root_owned_ancestors() {
    local path=$1 current='' component owner mode
    local -a components

    IFS=/ read -r -a components <<< "${path#/}"
    for component in "${components[@]}"; do
        [[ -n $component ]] || continue
        current="${current%/}/$component"
        if [[ ! -e $current && ! -L $current ]]; then
            break
        fi
        [[ ! -L $current && -d $current ]] || return 1
        if [[ $(uname -s) == Darwin ]]; then
            owner=$(stat -f '%u' "$current") || return 1
            mode=$(stat -f '%Lp' "$current") || return 1
        else
            owner=$(stat -c '%u' "$current") || return 1
            mode=$(stat -c '%a' "$current") || return 1
        fi
        [[ $owner == 0 ]] || return 1
        (( (8#$mode & 8#022) == 0 )) || return 1
    done
}

platform_binary_name() {
    local operating_system=$1 architecture=$2

    case "$operating_system:$architecture" in
        Linux:x86_64|Linux:amd64) printf '%s\n' logwatch-analyzer-linux-amd64 ;;
        Darwin:arm64|Darwin:aarch64) printf '%s\n' logwatch-analyzer-darwin-arm64 ;;
        *) return 1 ;;
    esac
}
