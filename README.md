# meerstetter-go

Go toolkit for Meerstetter TEC (Thermoelectric Controller) and LDD communication,
plus an HTTP gateway, a multi-device serial/CAN router, and a browser UI.

---

## What this does today

### Protocol library — `mecom`
Pure Go implementation of the Meerstetter MeCom wire protocol: framing, CRC, typed
reads and writes, response parsing, endpoint parsing, and transport selection.
Works on Linux and Windows. Transport is injected — the library does not know
whether it is running over serial, TCP, or CAN; only the calling application
decides that.

### Multi-device address-routed server — `cmd/mecomvseriald`
One TCP listener that fans out to multiple physical MeCom devices. Each MeCom
request frame carries the destination device address, so one server port can
serve all devices without port-per-device allocation. Handles serial, TCP, and
CAN (via SocketCAN on Linux) downstreams and reconnects automatically if a link
drops.

### HTTP gateway — `cmd/mecomgw`
JSON/SSE HTTP API that sits in front of `mecomvseriald` (or direct device
endpoints) and exposes a stable browser-safe surface:

| Method | Path | Purpose |
|--------|------|---------|
| GET    | `/api/healthz` | Liveness probe |
| GET    | `/api/devices` | Device list, bind status, active route |
| GET    | `/api/settings` | Gateway-level bridge and route configuration |
| GET    | `/api/catalogue` | Full parameter catalogue (names, units, types, writability) |
| GET    | `/api/leases` | Currently active write leases |
| GET    | `/api/commands` | Recent write attempts and outcomes |
| POST   | `/api/devices/{id}/lease` | Acquire exclusive write authority |
| DELETE | `/api/devices/{id}/lease` | Release write authority |
| GET    | `/api/devices/{id}/read` | One-shot bulk parameter read |
| GET    | `/api/devices/{id}/poll` | SSE stream of live telemetry |
| POST   | `/api/devices/{id}/write` | Issue a typed telecommand (requires lease) |
| GET    | `/api/graph/history/export` | Export graph history as Apache Arrow IPC |
| POST   | `/api/graph/history/import` | Import a previously exported history |
| GET    | `/api/graph/availability` | Signal availability summary |
| GET    | `/api/graph/sparklines` | Compact per-signal sparkline data |
| GET    | `/api/graph/tiles/...` | Tile-level graph data for the wall UI |
| GET    | `/api/log/export` | Export gateway event log |
| POST   | `/api/log/import` | Import a gateway event log |

Write requests carry the lease token in the `X-Lease-Token` request header.
The OpenAPI specification lives in `docs/gateway/openapi.yaml`; generate clients
from that, not from this prose.

### Browser UI — `web/`
React + TypeScript single-page app built with Vite. Connects to `mecomgw` via
its HTTP/SSE endpoints. Features:

- **Fleet overview** (hero view): combined temperature/power traces across all
  channels, live-updated via SSE.
- **Device drill-down**: per-device parameter dictionary with current values,
  units, and writability; inline setpoint writing with lease management.
- **Graph wall**: configurable tile-based signal wall with time-range control
  and assignment persistence.
- **Signal dictionary**: browsable catalogue of all MeCom parameters with
  operator/expert visibility, help text, and semantic counterpart links.
- **Help view**: inline operator reference.

The UI is bundled and served by `mecomgw -ui-dir web/dist`. Release tags attach
a pre-built browser bundle; source checkouts can build it from `web/`.

### Diagnostic commands

| Command | What it does |
|---------|-------------|
| `cmd/mecomprobe` | One-shot bulk MeCom read probe (registry-driven, supports bulk `?VX` reads). Accepts serial, TCP, or CAN endpoints. |
| `cmd/mecomset` | One-shot bounded write probe. |
| `cmd/mecompoll` | Continuous polling table across any number of endpoints, mixing TCP and CAN in one invocation. |
| `cmd/mecomrun` | Executes a `sequencer.Script` JSON automation via a typed command chain. |
| `cmd/teccanprobe` | Linux-only: SocketCAN/CANopen discovery probe. Reads identity, status, temperatures, and output-enable state for every TEC found on a CAN bus. Optionally captures raw frames to a ring file. |

---

## What it does not do yet

- **No authentication** — the gateway has CORS and a simple access-token cookie
  mechanism for same-LAN use, but is not hardened for Internet exposure. Do not
  put it on a public interface without a reverse proxy and real auth.
- **No Windows CAN** — SocketCAN is Linux-only. The library defines a
  `mecom.CANDialer` interface so a Kvaser, PCAN, or Vector adapter can be
  injected, but no Windows CAN adapter ships in this repository.
- **No persistent storage** — graph history, log files, and assignment state
  live in process memory. The export/import endpoints let operators preserve
  sessions manually; there is no database.
- **No multi-user write concurrency** — the lease model serializes writes to one
  operator at a time per device; it does not handle conflicting operators across
  multiple gateway instances.
- **No alarm management** — the quality tagging (`ok / missing / nan / detached`)
  detects sensor-detach heuristically but does not escalate or latch alarms.
- **No firmware upgrade** — the gateway reads and writes MeCom parameters; it
  does not implement the Meerstetter firmware download protocol.

---

## Signal catalogue and UI projection

The verified JSON assets that define the operator parameter surface are in
`web/src/data/`:

| File | Contents |
|------|----------|
| `mecom-catalogue.json` | Full compiled TEC parameter catalogue: IDs, names, units, types, access levels, help text, semantic counterparts |
| `mecom-operator-projection.json` | Maps operator-visible parameter IDs onto UI tree paths and bundles; validated by `web/scripts/test-mecom-projection.mjs` |
| `mecom-protocol-families.json` | Protocol family boundaries (MeCom vs CANopen) |

The raw source files from which the catalogue is compiled live in
`mecom/catalogues/sources/`. Use `scripts/harvest_coso_catalogue_sources.py` to
regenerate them from a Meerstetter CoSo installation.

---

## Quick start

### 1. Run the gateway (no hardware needed for testing)

```sh
go run ./cmd/mecomgw \
  -config deploy/example-gateway.json \
  -listen 127.0.0.1:18080
```

The example config describes four TEC devices over CAN. Without real hardware the
devices will appear in the API with `"bound": false`.

### 2. Add the UI

```sh
cd web && npm install && npm run build && cd ..

go run ./cmd/mecomgw \
  -config deploy/example-gateway.json \
  -listen 127.0.0.1:18080 \
  -ui-dir web/dist
```

Then open `http://127.0.0.1:18080/ui/`.

### 3. Connect real hardware

Serial (FTDI RS-485):
```sh
# Start the address-routed device server
go run ./cmd/mecomvseriald \
  -listen 0.0.0.0:50000 \
  -route 75=serial:/dev/serial/by-id/usb-FTDI_FT230X_..._A-if00-port0@57600 \
  -route 76=serial:/dev/serial/by-id/usb-FTDI_FT230X_..._B-if00-port0@57600

# Point the gateway at it
go run ./cmd/mecomgw \
  -config deploy/example-gateway.json \
  -listen 127.0.0.1:18080 \
  -ui-dir web/dist
```

CAN (Linux SocketCAN):
```sh
# Bring up the CAN interface (1 Mbit/s, restart on error)
sudo ip link set can0 type can bitrate 1000000 restart-ms 100
sudo ip link set can0 up

# Discover all TECs on the bus
go run ./cmd/teccanprobe -iface can0

# Gateway config already has CAN endpoints; just run it
go run ./cmd/mecomgw -config deploy/example-gateway.json -listen 127.0.0.1:18080
```

See `deploy/README.md` for the full systemd production deployment walkthrough and
serial port permission setup.

---

## Packages

| Package | Purpose |
|---------|---------|
| `mecom` | MeCom framing, CRC, typed reads/writes, endpoint parsing, TCP/serial/CAN opener |
| `mecom/writelease` | Write-lease token management |
| `mecomdict` | Seeded TEC parameter catalogue with typed metadata |
| `mecomserver` | Serialized TCP/serial downstream sharing; CAN-to-ASCII bridge |
| `mecomautomation` | LUT/program helpers for SignalForge control-program contracts |
| `canopen`, `canopen/eds` | CANopen frame helpers and EDS parsing |
| `canadapter` | Adapter-neutral CAN frame contracts |
| `socketcan` | Linux SocketCAN raw socket helpers |
| `canring` | Bounded chunked CAN receive ring files for edge capture |
| `objectdict` | Protocol-neutral object dictionary and catalogue model |
| `control`, `export` | Command authority and export contracts |

---

## Portability

| Surface | Linux | Windows |
|---------|-------|---------|
| `mecom` framing, CRC, reads/writes | ✅ | ✅ |
| TCP endpoints | ✅ | ✅ |
| Serial endpoints | ✅ (`/dev/tty…`) | ✅ (`COM3@57600`) |
| `mecomvseriald`, `mecomgw`, diagnostic commands over TCP/serial | ✅ | ✅ |
| `socketcan`, `teccanprobe`, `cmd/mecomvseriald` CAN dialer | ✅ | ❌ (Linux-only) |
| CAN on Windows | adapter required | adapter required |
| `deploy/` systemd/udev assets | ✅ | ❌ |

For Windows: use direct TCP, `COMx@baud`, or the `mecomvseriald` device-server
over TCP. Implement `mecom.CANDialer` to inject a Windows CAN adapter.

---

## Verification

```sh
# Go unit tests
go test ./...

# Compile-and-link gate for Windows packages from a Linux host
GOOS=windows GOARCH=amd64 go test -exec=true ./...

# UI data contract tests (requires Node)
cd web && npm test
```

The Windows command skips running generated Windows test binaries but catches
accidental Linux-only imports in portable packages.

---

## Repository layout

```
cmd/
  mecomgw/          HTTP/SSE gateway (main production binary)
  mecomvseriald/    Address-routed multi-device serial/CAN server
  mecomprobe/       One-shot bounded read probe
  mecomset/         One-shot bounded write probe
  mecompoll/        Continuous transport-agnostic poller
  mecomrun/         Sequencer-script executor
  teccanprobe/      Linux SocketCAN discovery probe
deploy/             systemd unit, udev rules, example config, deployment walkthrough
docs/
  gateway/          OpenAPI spec, TypeScript types, UI graph-wall contract
  backlog/          Deferred work items and implementation notes
mecom/              Core protocol library + TEC parameter catalogue
mecomserver/        Downstream server, router, CAN bridge
web/                React/TypeScript browser UI
  src/data/         mecom-catalogue.json, mecom-operator-projection.json
  scripts/          Node.js data contract test scripts
scripts/            Build and catalogue harvest scripts
```

---

## Dependencies

- [`go.bug.st/serial`](https://github.com/bugst/go-serial) — serial port support
- [`apache/arrow-go`](https://github.com/apache/arrow-go) — Arrow IPC for graph history export
- [`egidinas/signalforge`](https://github.com/egidinas/signalforge) — reusable graph, source-catalogue, and control-program contracts
