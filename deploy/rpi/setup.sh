#!/usr/bin/env bash
# setup.sh — PiXtend V2-L CAN bootstrap for Raspberry Pi OS
#
# Usage (run as root or with sudo):
#   sudo bash setup.sh
#
# What this script does:
#   1. Installs can-utils and build dependencies.
#   2. Applies the boot config overlay snippet.
#   3. Installs the systemd-networkd CAN unit for auto bring-up.
#   4. Enables systemd-networkd if not already active.
#   5. Builds teccanprobe from source (requires Go 1.21+).
#   6. Prints next steps.
#
# What this script does NOT do:
#   - It does not move the CAN/analog jumper on the PiXtend V2-L board.
#     You must do that manually before rebooting.
#   - It does not reboot. Reboot manually after the script completes.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# ------------------------------------------------------------------ deps
echo "==> Installing can-utils..."
apt-get update -qq
apt-get install -y --no-install-recommends can-utils

# ------------------------------------------------------------------ boot config
BOOT_CONFIG=""
for candidate in /boot/firmware/config.txt /boot/config.txt; do
    if [[ -f "$candidate" ]]; then
        BOOT_CONFIG="$candidate"
        break
    fi
done

if [[ -z "$BOOT_CONFIG" ]]; then
    echo "ERROR: could not find boot config (expected /boot/firmware/config.txt or /boot/config.txt)"
    exit 1
fi

if grep -q "dtoverlay=pixtendv2l" "$BOOT_CONFIG" 2>/dev/null; then
    echo "==> Boot config already contains pixtendv2l overlay — skipping."
elif grep -q "mcp2515-can0" "$BOOT_CONFIG" 2>/dev/null; then
    echo "==> Boot config already contains mcp2515-can0 overlay — skipping."
else
    echo "==> Appending PiXtend V2-L overlay to $BOOT_CONFIG..."
    cat >> "$BOOT_CONFIG" <<'EOF'

# PiXtend V2-L CAN — added by meerstetter-go/deploy/rpi/setup.sh
dtparam=spi=on
dtoverlay=pixtendv2l
EOF
    echo "    Done. (Verify $BOOT_CONFIG before rebooting.)"
fi

# ------------------------------------------------------------------ systemd-networkd
echo "==> Installing systemd-networkd CAN units..."
install -m 644 "$SCRIPT_DIR/can0.network" /etc/systemd/network/80-can0.network
install -m 644 "$SCRIPT_DIR/can1.network" /etc/systemd/network/80-can1.network

systemctl enable systemd-networkd
systemctl start systemd-networkd || true
echo "    can0.network and can1.network installed."

# ------------------------------------------------------------------ teccanprobe
if command -v go &>/dev/null; then
    GO_VERSION=$(go version | awk '{print $3}')
    echo "==> Found Go $GO_VERSION — building teccanprobe..."
    (cd "$REPO_ROOT" && GOOS=linux GOARCH=arm64 go build -o /usr/local/bin/teccanprobe ./cmd/teccanprobe)
    echo "    Installed: /usr/local/bin/teccanprobe"
else
    echo "==> Go not found. Install Go 1.21+ then run:"
    echo "      cd $REPO_ROOT && go build -o /usr/local/bin/teccanprobe ./cmd/teccanprobe"
fi

# ------------------------------------------------------------------ summary
cat <<'DONE'

==> Setup complete. Next steps:

  1. HARDWARE: Confirm the CAN/analog jumper on the PiXtend V2-L is in
     the CAN position (see the hardware manual, jumper block near the CAN
     terminal).

  2. REBOOT:
       sudo reboot

  3. VERIFY after reboot:
       dmesg | grep -i mcp251x
       ip -details link show can0

  4. PASSIVE BUS CHECK (TEC controller powered and wired):
       candump -n 20 can0

  5. PREFLIGHT CHECKLIST:
       teccanprobe -if can0 -profile pixtend-v2l -checklist

  6. ACTIVE PROBE (nodes 1–16):
       teccanprobe -if can0 -profile pixtend-v2l -active -nodes 1-16

  Full guide: docs/rpi_pixtend_bootstrap.md
DONE
