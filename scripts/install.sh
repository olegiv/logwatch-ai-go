#!/bin/bash
# Installation script for Logwatch AI Analyzer

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
INSTALL_DIR="${INSTALL_DIR:-/opt/logwatch-ai}"
BINARY_NAME="logwatch-analyzer"
SERVICE_USER="${SERVICE_USER:-$(whoami)}"

# Log functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    log_error "This script must be run as root (use sudo)"
    exit 1
fi

log_info "Installing Logwatch AI Analyzer to $INSTALL_DIR"

# Create installation directory
log_info "Creating installation directory..."
mkdir -p "$INSTALL_DIR"
mkdir -p "$INSTALL_DIR/data"
mkdir -p "$INSTALL_DIR/logs"

# Copy binary
if [ -f "bin/$BINARY_NAME" ]; then
    log_info "Copying binary..."
    cp "bin/$BINARY_NAME" "$INSTALL_DIR/"
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
else
    log_error "Binary not found at bin/$BINARY_NAME"
    log_error "Please run 'make build' first"
    exit 1
fi

# Copy scripts
log_info "Copying scripts..."
cp -r scripts "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/scripts"/*.sh

# Copy or create .env file
if [ ! -f "$INSTALL_DIR/.env" ]; then
    log_info "Creating .env configuration file..."
    if [ -f "configs/.env.example" ]; then
        cp "configs/.env.example" "$INSTALL_DIR/.env"
    else
        log_warn ".env.example not found, creating minimal config"
        cat > "$INSTALL_DIR/.env" << 'EOF'
# AI Provider Configuration
ANTHROPIC_API_KEY=
CLAUDE_MODEL=claude-sonnet-4-5-20250929

# Telegram Notifications
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHANNEL_ARCHIVE_ID=
TELEGRAM_CHANNEL_ALERTS_ID=

# Paths
LOGWATCH_OUTPUT_PATH=/tmp/logwatch-output.txt

# Application Settings
LOG_LEVEL=info
ENABLE_DATABASE=true
DATABASE_PATH=./data/summaries.db
EOF
    fi
    log_warn "Please configure $INSTALL_DIR/.env with your API keys and settings"
else
    log_info ".env file already exists, skipping"
fi

# Set ownership
log_info "Setting ownership to $SERVICE_USER..."
chown -R "$SERVICE_USER:$(id -gn $SERVICE_USER)" "$INSTALL_DIR"

# Create symlink
log_info "Creating symlink in /usr/local/bin..."
ln -sf "$INSTALL_DIR/$BINARY_NAME" "/usr/local/bin/$BINARY_NAME"

log_info ""
log_info "========================================"
log_info "Installation completed successfully!"
log_info "========================================"
log_info ""
log_info "Next steps:"
log_info "1. Configure $INSTALL_DIR/.env with your credentials"
log_info "2. Test the analyzer: $BINARY_NAME"
log_info "3. Set up cron jobs (see docs/CRON_SETUP.md)"
log_info ""
log_info "Cron setup (as root):"
log_info "  # Generate logwatch report at 2:00 AM"
log_info "  0 2 * * * $INSTALL_DIR/scripts/generate-logwatch.sh"
log_info ""
log_info "Cron setup (as $SERVICE_USER):"
log_info "  # Run analyzer at 2:15 AM"
log_info "  15 2 * * * cd $INSTALL_DIR && ./$BINARY_NAME >> logs/cron.log 2>&1"
log_info ""
