#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
wrapper="$repo_root/deploy/start-meerstetter-serial-bridges.sh"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

config="$tmpdir/serial-bridges.env"
cat >"$config" <<'CONFIG'
BIND_ADDR=127.0.0.1
MODE=vserial
LISTEN_PORT=15000
CAN_LISTEN_PORT=15010
MECOM_ADDRESS_ZERO=auto-first
CAN_MECOM_ADDRESS_ZERO=auto-first
MECOM_ROUTE_POLICY=dynamic
ENABLE_SERIAL_ROUTES=true
ENABLE_CAN_ROUTES=auto
ENABLE_CAN_TCP=auto
ENABLE_PI_CAN_ROUTES=true
CAN_IFACE=definitely_missing_can0
PI_CAN_HOST=127.0.0.1
PI_CAN_PORT=15010
MECOM_ROUTE_COUNT=6
MECOM_ROUTE_1=90:CONTROLLER_A:0x5a:15002
MECOM_ROUTE_2=91:-:0x5b:-
MECOM_ROUTE_3=92:-:-:-
MECOM_ROUTE_4=addr=93,serial=CONTROLLER_D,can=0x5d,remote=off,legacy=15004
MECOM_ROUTE_5=addr=94,serial=-,can=-,remote=tcp:127.0.0.2:25010
MECOM_ROUTE_6=addr=95,serial=-,can=-,endpoint=127.0.0.3:25110
CONFIG

plan="$(
  MEERSTETTER_SERIAL_BRIDGES_CONFIG="$config" \
    "$wrapper" plan
)"

grep -Fq "LISTENER tcp://127.0.0.1:15000 address-zero=90 route-policy=dynamic" <<<"$plan"
grep -Fq "CAN_LISTENER tcp://127.0.0.1:15010 address-zero=90 route-policy=dynamic local-can=0 remote-can=1" <<<"$plan"
grep -Fq "ROUTE 90=serial:/dev/serial/by-id/usb-FTDI_FT230X_Basic_UART_CONTROLLER_A-if00-port0@57600" <<<"$plan"
grep -Fq "ROUTE 90=tcp:127.0.0.1:15010" <<<"$plan"
grep -Fq "CAN_ROUTE 90=tcp:127.0.0.1:15010" <<<"$plan"
grep -Fq "ROUTE 91=tcp:127.0.0.1:15010" <<<"$plan"
grep -Fq "CAN_ROUTE 91=tcp:127.0.0.1:15010" <<<"$plan"
grep -Fq "ROUTE 92=tcp:127.0.0.1:15010" <<<"$plan"
grep -Fq "CAN_ROUTE 92=tcp:127.0.0.1:15010" <<<"$plan"
grep -Fq "ROUTE 94=tcp:127.0.0.2:25010" <<<"$plan"
grep -Fq "CAN_ROUTE 94=tcp:127.0.0.2:25010" <<<"$plan"
grep -Fq "ROUTE 95=tcp:127.0.0.3:25110" <<<"$plan"
grep -Fq "CAN_ROUTE 95=tcp:127.0.0.3:25110" <<<"$plan"

if grep -Fq "CAN_ROUTE 90=can:definitely_missing_can0/0x5a" <<<"$plan"; then
  echo "unexpected local CAN route in auto mode with missing can0" >&2
  exit 1
fi

if grep -Fq "ROUTE 91=serial:" <<<"$plan"; then
  echo "serial-disabled route unexpectedly generated a serial target" >&2
  exit 1
fi

if grep -Fq "ROUTE 93=tcp:127.0.0.1:15010" <<<"$plan"; then
  echo "remote=off route unexpectedly inherited the global remote target" >&2
  exit 1
fi

if grep -Fq "DQ01" <<<"$plan"; then
  echo "wrapper still leaked site-specific default routes into config-driven plan" >&2
  exit 1
fi

serial_auto_config="$tmpdir/serial-bridges-serial-auto.env"
missing_serial="$tmpdir/missing-ftdi"
cat >"$serial_auto_config" <<CONFIG
BIND_ADDR=127.0.0.1
LISTEN_PORT=16000
CAN_LISTEN_PORT=16010
MECOM_ADDRESS_ZERO=auto-first
CAN_MECOM_ADDRESS_ZERO=auto-first
MECOM_ROUTE_POLICY=dynamic
ENABLE_SERIAL_ROUTES=auto
ENABLE_CAN_ROUTES=false
ENABLE_CAN_TCP=auto
ENABLE_REMOTE_CAN_ROUTES=true
REMOTE_CAN_HOST=127.0.0.1
REMOTE_CAN_PORT=16010
MECOM_ROUTE_COUNT=2
MECOM_ROUTE_1=addr=90,serial=$missing_serial,can=-,legacy=-
MECOM_ROUTE_2=addr=91,serial=-,can=-,remote=tcp:127.0.0.2:26010
CONFIG

serial_auto_plan="$(
  MEERSTETTER_SERIAL_BRIDGES_CONFIG="$serial_auto_config" \
    "$wrapper" plan
)"

if grep -Fq "ROUTE 90=serial:" <<<"$serial_auto_plan"; then
  echo "auto serial mode emitted a missing FTDI route" >&2
  exit 1
fi

grep -Fq "LISTENER tcp://127.0.0.1:16000 address-zero=90 route-policy=dynamic" <<<"$serial_auto_plan"
grep -Fq "ROUTE 90=tcp:127.0.0.1:16010" <<<"$serial_auto_plan"
grep -Fq "CAN_LISTENER tcp://127.0.0.1:16010 address-zero=90 route-policy=dynamic local-can=0 remote-can=1" <<<"$serial_auto_plan"
grep -Fq "CAN_ROUTE 90=tcp:127.0.0.1:16010" <<<"$serial_auto_plan"
grep -Fq "CAN_ROUTE 91=tcp:127.0.0.2:26010" <<<"$serial_auto_plan"
