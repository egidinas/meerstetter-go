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
TEC_NODES="${TEC_NODES:-30-39}"

# ------------------------------------------------------------------ deps
missing_can_tools=()
for tool in candump cansend; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        missing_can_tools+=("$tool")
    fi
done

if ((${#missing_can_tools[@]} == 0)); then
    echo "==> can-utils already available."
elif [[ "${SKIP_APT:-0}" == "1" ]]; then
    echo "WARN: SKIP_APT=1 and missing CAN tools: ${missing_can_tools[*]}"
elif command -v apt-get >/dev/null 2>&1; then
    echo "==> Installing can-utils..."
    if apt-get update -qq && apt-get install -y --no-install-recommends can-utils; then
        echo "    can-utils installed."
    else
        echo "WARN: can-utils install failed; continuing with existing tools."
    fi
else
    echo "WARN: apt-get not available; missing CAN tools: ${missing_can_tools[*]}"
fi

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
ARCH="$(uname -m)"
PREBUILT=""
GOARCH_TARGET=""
GOARM_TARGET=""
case "$ARCH" in
    aarch64|arm64)
        PREBUILT="$SCRIPT_DIR/bin/teccanprobe_linux_arm64"
        GOARCH_TARGET="arm64"
        ;;
    armv7l|armv6l|armhf)
        PREBUILT="$SCRIPT_DIR/bin/teccanprobe_linux_arm7"
        GOARCH_TARGET="arm"
        GOARM_TARGET="7"
        ;;
    *)
        GOARCH_TARGET="$(go env GOARCH 2>/dev/null || true)"
        ;;
esac

install_prebuilt() {
    if [[ -n "$PREBUILT" && -x "$PREBUILT" ]]; then
        echo "==> Installing prebuilt teccanprobe for $ARCH..."
        install -m 755 "$PREBUILT" /usr/local/bin/teccanprobe
        return 0
    fi
    return 1
}

if [[ "${BUILD_FROM_SOURCE:-0}" != "1" ]] && install_prebuilt; then
    echo "    Installed: /usr/local/bin/teccanprobe"
elif command -v go >/dev/null 2>&1; then
    [[ -z "$GOARCH_TARGET" ]] && GOARCH_TARGET="$(go env GOARCH)"
    build_env=(GOOS=linux GOARCH="$GOARCH_TARGET")
    [[ -n "$GOARM_TARGET" ]] && build_env+=(GOARM="$GOARM_TARGET")
    GO_VERSION=$(go version | awk '{print $3}')
    echo "==> Found Go $GO_VERSION — building teccanprobe for linux/$GOARCH_TARGET${GOARM_TARGET:+ GOARM=$GOARM_TARGET}..."
    (cd "$REPO_ROOT" && env "${build_env[@]}" go build -o /usr/local/bin/teccanprobe ./cmd/teccanprobe)
    echo "    Installed: /usr/local/bin/teccanprobe"
else
    echo "WARN: teccanprobe was not installed. Copy deploy/rpi/bin/teccanprobe_linux_arm64 or _arm7 to /usr/local/bin/teccanprobe."
fi

# ------------------------------------------------------------------ summary
cat <<DONE

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

  6. ACTIVE PROBE (nodes ${TEC_NODES}):
       teccanprobe -if can0 -profile pixtend-v2l -active -nodes ${TEC_NODES}

  Full guide: docs/rpi_pixtend_bootstrap.md
DONE
