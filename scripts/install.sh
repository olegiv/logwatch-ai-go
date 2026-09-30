#!/usr/bin/env bash
# Bootstrap Logwatch AI Analyzer on a new host.

set -euo pipefail
umask 027

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd -P)"
# shellcheck source=scripts/helper.sh
. "$SCRIPT_DIR/helper.sh"

INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"
BINARY_NAME="logwatch-analyzer"
SERVICE_USER="${SERVICE_USER:-${SUDO_USER:-root}}"
BINARY_PATH="${BINARY_PATH:-}"

if ! validate_install_dir "$INSTALL_DIR"; then
    echo_error "INSTALL_DIR must be a normalized child path below /opt or /usr/local"
    exit 1
fi
if [[ $EUID -ne 0 ]]; then
    echo_error "This script must be run as root (use sudo)"
    exit 1
fi
if ! validate_root_owned_ancestors "$INSTALL_DIR"; then
    echo_error "INSTALL_DIR ancestors must be real root-owned directories not writable by other accounts"
    exit 1
fi
if [[ -L $INSTALL_DIR ]]; then
    echo_error "INSTALL_DIR must not be a symlink: $INSTALL_DIR"
    exit 1
fi
if ! id "$SERVICE_USER" >/dev/null 2>&1; then
    echo_error "SERVICE_USER does not exist: $SERVICE_USER"
    exit 1
fi
SERVICE_GROUP=$(id -gn "$SERVICE_USER")
CRON_LOG_DIR=/var/log/logwatch-ai

if [[ -z $BINARY_PATH ]]; then
    if platform_binary=$(platform_binary_name "$(uname -s)" "$(uname -m)") \
        && [[ -f $REPO_ROOT/bin/$platform_binary ]]; then
        BINARY_PATH="$REPO_ROOT/bin/$platform_binary"
    elif [[ -f $REPO_ROOT/bin/$BINARY_NAME ]]; then
        BINARY_PATH="$REPO_ROOT/bin/$BINARY_NAME"
    fi
fi
if [[ -z $BINARY_PATH || ! -f $BINARY_PATH || ! -x $BINARY_PATH || -L $BINARY_PATH ]]; then
    echo_error "Binary not found or not a regular file"
    echo_error "Build it first, or set BINARY_PATH=/absolute/path/to/$BINARY_NAME"
    exit 1
fi
if ! "$BINARY_PATH" -version >/dev/null 2>&1; then
    echo_error "Selected binary does not run on this host: $BINARY_PATH"
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    echo_warn "jq is not installed. Drupal watchdog multi-site support requires it."
fi

echo_info "Installing Logwatch AI Analyzer to $INSTALL_DIR"

# Executable code, configuration, and the database directory stay root-owned.
# The service account is used only for source-log access and cannot replace
# files later opened by the root cron process.
install -d -o root -g "$SERVICE_GROUP" -m 0750 "$INSTALL_DIR"
for runtime_dir in scripts data logs; do
    if [[ -L $INSTALL_DIR/$runtime_dir || ( -e $INSTALL_DIR/$runtime_dir && ! -d $INSTALL_DIR/$runtime_dir ) ]]; then
        echo_error "$INSTALL_DIR/$runtime_dir must be a real directory"
        exit 1
    fi
done
install -d -o root -g root -m 0755 "$INSTALL_DIR/scripts"
install -d -o root -g root -m 0700 "$INSTALL_DIR/data"
install -d -o root -g "$SERVICE_GROUP" -m 0750 "$INSTALL_DIR/logs"
if [[ -L $CRON_LOG_DIR || ( -e $CRON_LOG_DIR && ! -d $CRON_LOG_DIR ) ]]; then
    echo_error "$CRON_LOG_DIR must be a real directory"
    exit 1
fi
install -d -o root -g "$SERVICE_GROUP" -m 0750 "$CRON_LOG_DIR"
for log_name in cron.log analyzer.log; do
    log_path="$CRON_LOG_DIR/$log_name"
    if [[ -L $log_path || ( -e $log_path && ! -f $log_path ) ]]; then
        echo_error "$log_path must be a regular file"
        exit 1
    fi
    if [[ ! -e $log_path ]]; then
        install -o root -g "$SERVICE_GROUP" -m 0640 /dev/null "$log_path"
    else
        chown root:"$SERVICE_GROUP" "$log_path"
        chmod 0640 "$log_path"
    fi
done

echo_info "Installing root-owned binary and scripts..."
install_code_file() {
    local source=$1 destination=$2 mode=$3

    if [[ -L $destination || ( -e $destination && ! -f $destination ) ]]; then
        echo_error "Code destination must be a regular file: $destination"
        exit 1
    fi
    install -o root -g root -m "$mode" "$source" "$destination"
}

install_code_file "$BINARY_PATH" "$INSTALL_DIR/$BINARY_NAME" 0755
for source_script in "$REPO_ROOT"/scripts/*.sh "$REPO_ROOT"/scripts/*.sh.example; do
    [[ -f $source_script ]] || continue
    [[ $source_script != *_test.sh ]] || continue
    install_code_file "$source_script" \
        "$INSTALL_DIR/scripts/$(basename "$source_script")" 0755
done

install_config() {
    local source=$1 destination=$2 mode=$3

    if [[ -L $destination || ( -e $destination && ! -f $destination ) ]]; then
        echo_error "Configuration destination must be a regular file: $destination"
        exit 1
    fi
    if [[ ! -e $destination ]]; then
        install -o root -g "$SERVICE_GROUP" -m "$mode" "$source" "$destination"
    else
        chown root:"$SERVICE_GROUP" "$destination"
        chmod "$mode" "$destination"
    fi
}

echo_info "Installing configuration templates..."
install_config "$REPO_ROOT/configs/.env.example" "$INSTALL_DIR/.env" 0640
install_config "$REPO_ROOT/configs/drupal-sites.json.example" \
    "$INSTALL_DIR/drupal-sites.json" 0640
install_config "$REPO_ROOT/configs/ocms-sites.json.example" \
    "$INSTALL_DIR/ocms-sites.json" 0640
install -o root -g root -m 0644 "$REPO_ROOT/configs/exclusions.json.example" \
    "$INSTALL_DIR/exclusions.json.example"

echo_info "Installation completed successfully"
echo_info "Configure $INSTALL_DIR/.env and the required site files."
echo_info "Then copy and customize $INSTALL_DIR/scripts/run-cron.sh.example"
echo_info "as root-owned $INSTALL_DIR/run-cron.sh (mode 0755)."
