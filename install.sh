#!/usr/bin/env bash
set -euo pipefail

REPO="yamacasis/gomon"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/gomon"
LOG_DIR="/var/log/gomon"
SERVICE_FILE="/etc/systemd/system/gomon.service"
SERVICE_USER="gomon"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()    { echo -e "${GREEN}[+]${NC} $*"; }
warn()    { echo -e "${YELLOW}[!]${NC} $*"; }
error()   { echo -e "${RED}[x]${NC} $*" >&2; exit 1; }

need_root() {
  if [ "$(id -u)" -ne 0 ]; then
    error "This script must be run as root. Try: sudo bash install.sh"
  fi
}

detect_arch() {
  case "$(uname -m)" in
    x86_64)  echo "amd64" ;;
    aarch64) echo "arm64" ;;
    *)       error "Unsupported architecture: $(uname -m)" ;;
  esac
}

fetch_latest_version() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | head -1 \
    | cut -d'"' -f4
}

uninstall() {
  info "Stopping and disabling service..."
  systemctl stop gomon  2>/dev/null || true
  systemctl disable gomon 2>/dev/null || true
  rm -f "$SERVICE_FILE"
  systemctl daemon-reload

  info "Removing binary..."
  rm -f "$INSTALL_DIR/gomon"

  warn "Config and logs kept at $CONFIG_DIR and $LOG_DIR"
  warn "Remove them manually if no longer needed:"
  warn "  sudo rm -rf $CONFIG_DIR $LOG_DIR"

  info "Uninstall complete."
  exit 0
}

# ── flags ─────────────────────────────────────────────────────────────────────

UNINSTALL=false
for arg in "$@"; do
  case "$arg" in
    --uninstall|-u) UNINSTALL=true ;;
    --help|-h)
      echo "Usage: sudo bash install.sh [--uninstall]"
      exit 0
      ;;
  esac
done

need_root

if $UNINSTALL; then
  uninstall
fi

# ── install ────────────────────────────────────────────────────────────────────

ARCH=$(detect_arch)
ASSET="gomon-linux-${ARCH}"

info "Fetching latest release from github.com/${REPO}..."
VERSION=$(fetch_latest_version)
[ -z "$VERSION" ] && error "Could not determine latest release. Check your internet connection."

info "Latest version: $VERSION"

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"

# Download binary to temp file
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT

info "Downloading ${ASSET}..."
curl -fsSL --progress-bar "$DOWNLOAD_URL" -o "$TMP" \
  || error "Download failed: $DOWNLOAD_URL"
chmod +x "$TMP"

# Install binary
info "Installing binary to ${INSTALL_DIR}/gomon..."
mv "$TMP" "${INSTALL_DIR}/gomon"

# Create dedicated system user
if ! id -u "$SERVICE_USER" &>/dev/null; then
  info "Creating system user '${SERVICE_USER}'..."
  useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
fi

# Create directories
info "Creating config and log directories..."
mkdir -p "$CONFIG_DIR" "$LOG_DIR"
chown "$SERVICE_USER:$SERVICE_USER" "$LOG_DIR"

# Download example config if no config exists yet
if [ ! -f "${CONFIG_DIR}/config.yaml" ]; then
  info "Installing example config to ${CONFIG_DIR}/config.yaml..."
  curl -fsSL \
    "https://raw.githubusercontent.com/${REPO}/main/config.example.yaml" \
    -o "${CONFIG_DIR}/config.yaml" \
    || warn "Could not download example config — create ${CONFIG_DIR}/config.yaml manually."

  # Set the log file to the system log path
  sed -i 's|^log_file:.*|log_file: "/var/log/gomon/monitor.log"|' "${CONFIG_DIR}/config.yaml"

  warn "Config file created at ${CONFIG_DIR}/config.yaml"
  warn "Edit it and set your Telegram token / chat ID before starting."
else
  info "Config already exists at ${CONFIG_DIR}/config.yaml — skipping."
fi

# Install systemd service
info "Installing systemd service..."
curl -fsSL \
  "https://raw.githubusercontent.com/${REPO}/main/gomon.service" \
  -o "$SERVICE_FILE" \
  || error "Could not download service file."

systemctl daemon-reload
systemctl enable gomon

# Only start if config looks configured (token placeholder not present)
if grep -q "YOUR_BOT_TOKEN_HERE" "${CONFIG_DIR}/config.yaml" 2>/dev/null; then
  warn "Service installed but NOT started — edit ${CONFIG_DIR}/config.yaml first."
  warn "Then run: sudo systemctl start gomon"
else
  info "Starting gomon service..."
  systemctl start gomon
  sleep 1
  systemctl status gomon --no-pager -l || true
fi

echo ""
echo -e "${GREEN}Gomon ${VERSION} installed successfully.${NC}"
echo ""
echo "  Config:       ${CONFIG_DIR}/config.yaml"
echo "  Log file:     ${LOG_DIR}/monitor.log"
echo "  Service:      sudo systemctl {start|stop|restart|status} gomon"
echo "  Live logs:    sudo journalctl -u gomon -f"
echo "  Uninstall:    sudo bash install.sh --uninstall"
