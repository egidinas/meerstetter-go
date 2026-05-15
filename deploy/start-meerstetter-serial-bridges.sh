#!/usr/bin/env bash
# Starts socat TCP→serial bridges for all 4 Meerstetter TEC controllers.
# Run this from a login shell that has dialout group access.
# Bridges stay alive; kill with: pkill -f meerstetter-serial-bridges
set -euo pipefail

LOGDIR=${LOGDIR:-/tmp}
BASE_PORT=${BASE_PORT:-50002}

declare -A FTDI_MAP=(
  ["usb-FTDI_FT230X_Basic_UART_DQ01FKRM-if00-port0"]="$((BASE_PORT + 0))"
  ["usb-FTDI_FT230X_Basic_UART_DQ01FSW6-if00-port0"]="$((BASE_PORT + 1))"
  ["usb-FTDI_FT230X_Basic_UART_DQ01GVYX-if00-port0"]="$((BASE_PORT + 2))"
  ["usb-FTDI_FT230X_Basic_UART_DQ01GW7L-if00-port0"]="$((BASE_PORT + 3))"
)

for byid in "${!FTDI_MAP[@]}"; do
  port="${FTDI_MAP[$byid]}"
  dev="/dev/serial/by-id/$byid"
  log="$LOGDIR/meerstetter-serial-${port}.log"
  if [[ ! -e "$dev" ]]; then
    echo "skip $byid (not present)"
    continue
  fi
  # Kill any existing bridge on this port
  fuser -k "${port}/tcp" 2>/dev/null || true
  socat -d \
    "TCP-LISTEN:${port},bind=0.0.0.0,fork,reuseaddr,keepalive" \
    "FILE:${dev},raw,echo=0,b57600,cs8" \
    > "$log" 2>&1 &
  echo "started port=${port} dev=${dev} pid=$! log=${log}"
done

wait
