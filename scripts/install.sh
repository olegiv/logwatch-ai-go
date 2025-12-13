#!/bin/bash
# Installation script for Logwatch AI Analyzer

set -e

# Set restrictive umask for security (owner: rwx, group: rx, others: none)
umask 027

# Source helper functions
. "$(dirname "$0")/helper.sh"

# Configuration
INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"
BINARY_NAME="logwatch-analyzer"
SERVICE_USER="${SERVICE_USER:-$(whoami)}"

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo_error "This script must be run as root (use sudo)"
    exit 1
fi

# Check for jq (required for Drupal watchdog multi-site support)
if ! command -v jq &> /dev/null; then
    echo_warn "jq is not installed. Required for Drupal watchdog multi-site support."
    echo_warn "Install with: apt-get install jq (Debian/Ubuntu) or brew install jq / port install jq (macOS)"
fi

echo_info "Installing Logwatch AI Analyzer to $INSTALL_DIR"

# Create installation directory with secure permissions
echo_info "Creating installation directory..."
mkdir -p "$INSTALL_DIR" "$INSTALL_DIR/data" "$INSTALL_DIR/logs"
chmod 750 "$INSTALL_DIR" "$INSTALL_DIR/data" "$INSTALL_DIR/logs"

# Copy binary
if [ -f "bin/$BINARY_NAME" ]; then
    echo_info "Copying binary..."
    cp "bin/$BINARY_NAME" "$INSTALL_DIR/"
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
else
    echo_error "Binary not found at bin/$BINARY_NAME"
    echo_error "Please run 'make build' first"
    exit 1
fi

# Copy scripts
echo_info "Copying scripts..."
cp -r scripts "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/scripts"/*.sh

# Create .env file with restrictive permissions
if [ ! -f "$INSTALL_DIR/.env" ]; then
    echo_info "Creating .env configuration file..."
    cp "configs/.env.example" "$INSTALL_DIR/.env"
    chmod 600 "$INSTALL_DIR/.env"
else
    echo_info ".env file already exists, skipping"
    # Ensure existing .env has secure permissions
    chmod 600 "$INSTALL_DIR/.env"
fi

# Create drupal-sites.json for Drupal watchdog configuration
if [ ! -f "$INSTALL_DIR/drupal-sites.json" ]; then
    echo_info "Creating drupal-sites.json configuration file..."
    cp "configs/drupal-sites.json.example" "$INSTALL_DIR/drupal-sites.json"
    chmod 640 "$INSTALL_DIR/drupal-sites.json"
else
    echo_info "drupal-sites.json file already exists, skipping"
fi

# Set ownership
echo_info "Setting ownership to $SERVICE_USER..."
chown -R "$SERVICE_USER:$(id -gn "$SERVICE_USER")" "$INSTALL_DIR"

echo_info ""
echo_info "========================================"
echo_info "Installation completed successfully!"
echo_info "========================================"
echo_info ""
echo_info "Next steps:"
echo_info "1. Configure $INSTALL_DIR/.env with your API credentials"
echo_info "2. For Drupal: Edit $INSTALL_DIR/drupal-sites.json with your sites"
echo_info "3. Test the analyzer: $BINARY_NAME"
echo_info "4. Set up cron jobs (see docs/CRON_SETUP.md)"
echo_info ""
echo_info "Cron setup for Logwatch (as root):"
echo_info "  0 2 * * * $INSTALL_DIR/scripts/generate-logwatch.sh"
echo_info "  15 2 * * * cd $INSTALL_DIR && ./$BINARY_NAME >> logs/cron.log 2>&1"
echo_info ""
echo_info "Cron setup for Drupal (as $SERVICE_USER):"
echo_info "  0 2 * * * $INSTALL_DIR/scripts/generate-drupal-watchdog.sh --site production"
echo_info "  15 2 * * * cd $INSTALL_DIR && ./$BINARY_NAME -source-type drupal_watchdog -drupal-site production >> logs/cron.log 2>&1"
echo_info ""
