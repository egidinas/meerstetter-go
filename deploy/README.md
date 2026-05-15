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
| `start-meerstetter-serial-bridges.sh` | socat-based port-per-device fallback (compatibility only) |
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

## Fallback: port-per-device socat bridge

The `start-meerstetter-serial-bridges.sh` script in this directory is a
**compatibility fallback** for legacy tools that need exclusive transparent
byte-stream access to one physical link (e.g. Meerstetter's original Windows
software). It binds one TCP port per FTDI adapter and does not inspect frames.
Prefer the device server unless a specific legacy client requires this mode.

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `open: permission denied` in journal | runtime user lacks serial access | install udev rule (`60-ftdi-meerstetter.rules`) or add user to `dialout` |
| `no downstream route for MeCom address 0xNN` | client sent frame for an address with no `-route` | check `-route` flags and the client's `Address` field |
| Clients see frames cross-replied | two clients hit the same downstream concurrently | check the unit isn't started twice; broker serializes per address, not across them |
| `mecomvseriald` exits with `address 0 is reserved` | broadcast address 0 cannot be routed | use the device's real address (1–254) |
| Bridge stays up but clients see EOF | downstream serial port unplugged | server reconnects automatically with `reconnect-delay`; check `journalctl -u mecomvseriald` |
