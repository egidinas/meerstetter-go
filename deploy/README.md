# Meerstetter-Go Linux Device Server Deployment

This directory bundles everything needed to run the **mecomvseriald** Linux
device server — a single addressable TCP endpoint that fronts multiple
Meerstetter MeCom controllers over serial, USB-FTDI, or TCP.

## Why a device server

MeCom request frames carry the destination device address in the wire format
(`#75...` for address 0x4B). A small server can:

- accept many concurrent TCP clients (sniffers, controllers, dashboards),
- serialize requests per physical downstream link (one owned connection per
  device — no contention, no garbled frames),
- route each addressed frame to the correct downstream by inspecting the
  address byte (no per-device port allocation),
- automatically reconnect downstream if the serial cable bounces.

This is the **preferred** mode for any setup with more than one device on the
network or more than one client wanting to talk to a device.

## Components in this directory

| File | Purpose |
|------|---------|
| `mecomvseriald.service` | systemd unit for the device server (production deployment) |
| `mecomvseriald.default` | `/etc/default/mecomvseriald` environment overrides |
| `60-ftdi-meerstetter.rules` | udev rule that grants console-user access to FT230X adapters |
| `start-meerstetter-serial-bridges.sh` | config-driven launcher for serial, local CAN, remote CAN, and legacy fallback routes |
| `meerstetter-serial-bridges.service` | systemd service template for the config-driven launcher |
| `serial-bridges.env.example` | public-safe example config for the launcher |
| `README-serial-access.md` | serial-permission walkthrough (run this first) |

## Quick start

```sh
# 1. Install the binary
go install github.com/egidinas/meerstetter-go/cmd/mecomvseriald@latest
# or from a local checkout:
go build -o /usr/local/bin/mecomvseriald ./cmd/mecomvseriald

# 2. Deploy the udev rule so the runtime user can open ttyUSB*
sudo cp deploy/60-ftdi-meerstetter.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
sudo udevadm trigger --subsystem-match=tty

# 3. Run interactively to confirm routing
mecomvseriald \
  -listen 0.0.0.0:50000 \
  -address-zero disabled \
  -route 75=serial:/dev/serial/by-id/usb-FTDI_FT230X_Basic_UART_CONTROLLER_A-if00-port0@57600 \
  -route 76=serial:/dev/serial/by-id/usb-FTDI_FT230X_Basic_UART_CONTROLLER_B-if00-port0@57600 \
  -route 81=serial:/dev/serial/by-id/usb-FTDI_FT230X_Basic_UART_CONTROLLER_C-if00-port0@57600 \
  -route 84=serial:/dev/serial/by-id/usb-FTDI_FT230X_Basic_UART_CONTROLLER_D-if00-port0@57600

# 4. Verify from another shell using mecompoll (transport-agnostic)
mecompoll \
  -targets "tcp:127.0.0.1:50000=75,tcp:127.0.0.1:50000=76,tcp:127.0.0.1:50000=81,tcp:127.0.0.1:50000=84" \
  -interval 2s

# Or mix CAN and TCP in one poller invocation:
mecompoll -targets "can:can0/0x4b=75,can:can0/0x4c=76,tcp:127.0.0.1:50000=81,tcp:127.0.0.1:50000=84"
```

## systemd deployment

```sh
sudo cp deploy/mecomvseriald.service /etc/systemd/system/
sudo cp deploy/mecomvseriald.default /etc/default/mecomvseriald   # edit first
sudo systemctl daemon-reload
sudo systemctl enable --now mecomvseriald
journalctl -u mecomvseriald -f
```

The unit:
- starts after `network-online.target` and waits for the four ttyUSB devices,
- runs hardened (`NoNewPrivileges`, `ProtectSystem=strict`, `PrivateTmp`),
- adds `dialout` as a supplementary group so the binary can open serial ports,
- restarts on failure with a 2 s backoff.

## Endpoint and target format

`mecomvseriald` accepts `-route ADDRESS=TARGET` flags. Address is the MeCom
device address (1–254). Target uses the canonical `mecom` endpoint parser:

| Form | Example |
|------|---------|
| `serial:PATH@BAUD` | `serial:/dev/ttyUSB0@57600` |
| `serial:/dev/serial/by-id/...@BAUD` | (preferred — stable across replug) |
| `COM3@57600` | (Windows) |
| `tcp:HOST:PORT` | `tcp:192.168.1.10:50010` |
| bare `HOST:PORT` | `192.168.1.10:50010` |

Use `serial:/dev/serial/by-id/...` rather than raw `ttyUSB[0-3]` paths: the
by-id name encodes the FT230X serial number, so the route is stable even if
Linux re-enumerates the ports.

The router itself is a normal Go TCP process. It can also run on Windows with
COM-port routes, for example:

```sh
mecomvseriald -listen 0.0.0.0:50000 -route 75=COM3@57600
```

### Address-zero discovery clients

Some legacy tools, including commissioning software that discovers a device
before switching to an explicit address, may initially send MeCom frames to
address `0`. A multi-device server cannot infer the operator's intended serial
number from those frames. Keep the public default disabled and set an explicit
deployment policy only where needed:

| `MECOM_ADDRESS_ZERO` / `-address-zero` | Behavior |
|----------------------------------------|----------|
| `disabled` | Reject address-zero requests with a clear device-server error. |
| `auto-first` | Resolve to the first active configured route after the wrapper expands serial, local CAN, and remote CAN availability. |
| `route-order` | Assign new address-zero client connections through the configured route order. |
| fixed address | Send all address-zero requests to that configured MeCom address. |

Use `auto-first` for a single-device compatibility listener whose first active
route is already the intended commissioning target. Use fixed-address mode only
as a site-local operator setting, for example when a Windows commissioning tool
is intentionally pointed at one current device. Do not bake a serial number into
the binary, public service unit, or checked-in defaults.

The files in this `deploy/` directory are Linux deployment helpers only:
systemd handles restart policy and udev handles serial permissions. On
Windows, run the binary interactively during commissioning or wrap it with a
supervisor such as WinSW/NSSM. CAN adapters are intentionally outside this
serial device server; platform-specific CAN hardware should implement the
`mecom.CANDialer` boundary instead.

## Client side

Once the server is up, every client uses the same TCP endpoint and varies the
MeCom device address per request. The reference clients in this repo:

- `cmd/mecomprobe` — bounded read probe (one-shot diagnostic, registry-driven)
- `cmd/mecomset` — bounded write probe (one-shot setpoint write)
- `cmd/mecompoll` — transport-agnostic continuous poller (serial / TCP / CAN)
- `cmd/mecomrun` — runs a `sequencer.Script` JSON via `tmtc.Commander`
- `cmd/teccanprobe` — one-shot SocketCAN/CANopen discovery probe

Any of these accept `tcp:host:50000` and the device address; the server does
the routing.

## Config-driven serial/CAN launcher

The `start-meerstetter-serial-bridges.sh` script is a deployment wrapper around
`mecomvseriald`. It keeps the topology in a site-local config file and expands
each configured device row into the available downstream transports:

- optional local serial route for the main addressed listener,
- local SocketCAN route when the CAN interface is up and error-active,
- optional remote TCP/CAN route, for example a Raspberry Pi or PiXtend edge,
- optional legacy port-per-device `socat` fallback for tools that require a
  transparent byte stream.

Copy `serial-bridges.env.example` to a local path, edit the route rows, then
preview the generated router command before starting anything:

```sh
cp deploy/serial-bridges.env.example ~/.config/meerstetter/serial-bridges.env
MEERSTETTER_SERIAL_BRIDGES_CONFIG=~/.config/meerstetter/serial-bridges.env \
  deploy/start-meerstetter-serial-bridges.sh plan
```

Route rows use `MECOM_ROUTE_COUNT` plus `MECOM_ROUTE_N` entries:

```ini
MECOM_ROUTE_1=addr=75,serial=CONTROLLER_A,can=0x4b,legacy=50002
MECOM_ROUTE_2=addr=81,serial=-,can=0x51,legacy=-
MECOM_ROUTE_3=addr=90,serial=-,can=-,remote=tcp:192.168.1.10:50010
```

The key/value row fields are `addr`, `serial`, `can`, `remote`, and `legacy`.
Only `addr` is required. Use `-` or `off` to omit a field. A `serial` value can
be a short FTDI serial, a full `/dev/serial/by-id/...` path, or a full
`serial:/dev/...@baud` target. With `ENABLE_SERIAL_ROUTES=auto`, missing
`/dev` serial targets are skipped while CAN and remote routes for the same
address remain active. When `remote` is omitted, the row inherits
`REMOTE_CAN_HOST`/`REMOTE_CAN_PORT` if the remote route is enabled and healthy;
set `remote=off` to prevent that inheritance for one device.

The compact four-field form remains accepted for existing configs:

```ini
MECOM_ROUTE_1=75:CONTROLLER_A:0x4b:50002
MECOM_ROUTE_2=76:-:0x4c:-
```

Use `ENABLE_CAN_TCP=auto` to expose the CAN compatibility listener on
`CAN_LISTEN_PORT` when any local CAN or remote route is active.

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `open: permission denied` in journal | runtime user lacks serial access | install udev rule (`60-ftdi-meerstetter.rules`) or add user to `dialout` |
| `no downstream route for MeCom address 0xNN` | client sent frame for an address with no `-route` | check `-route` flags and the client's `Address` field |
| `no downstream route for MeCom address 0x00` | legacy/discovery client sent address zero and address-zero routing is disabled | set `MECOM_ADDRESS_ZERO` to `auto-first`, `route-order`, or an explicit current device address as a site-local operator override |
| Clients see frames cross-replied | two clients hit the same downstream concurrently | check the unit isn't started twice; broker serializes per address, not across them |
| `mecomvseriald` exits with `address 0 is reserved` | broadcast address 0 cannot be routed | use the device's real address (1–254) |
| Bridge stays up but clients see EOF | downstream serial port unplugged | server reconnects automatically with `reconnect-delay`; check `journalctl -u mecomvseriald` |
