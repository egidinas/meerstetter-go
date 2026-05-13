# meerstetter-go

Public-safe Go building blocks for Meerstetter device communication.

The repository is intentionally independent from Loom. It contains reusable
protocol code for:

- MeCom over TCP or serial transports.
- CANopen object dictionaries parsed from EDS files.
- CAN/CANopen frame primitives that can be connected to SocketCAN, Kvaser,
  or another host-specific adapter.

Protocol definitions and tests use small synthetic fixtures. Vendor PDFs,
site-specific endpoints, and lab captures are not vendored here.

Reference vectors cover the captured MeComAPI CRC-16-CCITT frame example
`#0015AB?VR03E801C21A\r`. The MeCom endpoint parser defaults to `115200` baud
when no explicit serial rate is supplied; lab deployments should pin
controller-specific rates such as `@57600` in config. Ethernet
serial-device-server targets typically use TCP port `50000`.

## Packages

- `objectdict`: shared semantic model for parameters, CANopen objects, and
  discovery output.
- `canopen/eds`: EDS parser that builds an `objectdict.Dictionary`.
- `canopen`: minimal CANopen frame and SDO request primitives.
- `canring`: fixed-size chunked CAN receive ring files for flash-conscious
  edge capture.
- `mecom`: MeCom framing, numeric encoding/decoding, response parsing,
  endpoint parsing/opening through shared transport helpers, and a small
  synchronous client over `io.ReadWriter`.
- `mecomdict`: seeded Meerstetter TEC parameter catalogue, including readable
  and writable target metadata.
- `mecomserver`: MeCom device server pattern for sharing one TCP or serial
  downstream device across multiple clients while preserving serialized access;
  the hub config models many transparent Ethernet, serial, and CAN targets with
  per-device passthrough, queue, and ring-retention defaults.
- `canadapter`: adapter-neutral CAN frame contracts used by SocketCAN and other
  host-specific backends.
- `socketcan`: Linux SocketCAN helpers for PiXtend and other SocketCAN hosts.
- `tmtc`: shared rich telemetry and telecommand primitives with idempotent
  command keys, ACK/result events, and transport-neutral publisher interfaces.
- `control`: leased write ownership and command authority primitives.
- `mecomautomation`: LUT/program helpers for Meerstetter automation workflows.
- `export`: shared export interface, including HDF5 as a first-class target
  without forcing a CGO dependency into this core library.
- `sequencer`: script/step/result contracts that can be executed over the same
  TMTC command primitives.
- `utility`: reusable standalone server wiring for discovery, graph-wall,
  ring-log, event swimlane, source catalogue, and per-device passthrough. It
  consumes shared SignalForge/Loom-compatible graph-wall and ring-log contracts
  without making them local packages.
- `cmd/meerstetterd`: small local utility binary exposing the `utility` server
  through HTTP and a minimal browser UI.
- `cmd/teccanprobe`: bounded SocketCAN/CANopen probe for live TEC discovery.
- `cmd/mod-mecom-server`: TCP device-server bridge for one downstream MeCom
  target.

## Documentation

- [`docs/README.md`](docs/README.md): repository-local MeCom, CANopen, TMTC,
  logging, graph-wall, and utility architecture guide.
- [`docs/repo_completion_map.md`](docs/repo_completion_map.md): canonical repo
  surfaces, live data-path contract, verification gates, and cleanup rules.
- [`docs/source_inventory.md`](docs/source_inventory.md): local source-material
  inventory used while deriving the implementation, including which files stay
  outside this public-safe repo.
- [`docs/implementation_backlog.md`](docs/implementation_backlog.md): remaining
  implementation work for the universal Meerstetter utility.
- [`docs/public_variant_readiness.md`](docs/public_variant_readiness.md): public
  dependency gates and legacy-harvest boundaries.
- [`artifacts/README.md`](artifacts/README.md): tracked live-bring-up binaries
  organized by platform; local rebuilds stay in ignored `bin/`.

## Example

```go
ep, ok := mecom.ParseEndpoint("serial:/dev/ttyUSB0@57600")
if !ok {
    panic("invalid endpoint")
}
conn, err := mecom.Open(ep, 2*time.Second)
if err != nil {
    panic(err)
}
defer conn.Close()

client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x50, Timeout: 2*time.Second})
value, err := client.ReadFloat32(context.Background(), 1000, 1)
```

## Standalone utility

Print a starter config:

```sh
go run ./cmd/meerstetterd -print-default-config > meerstetterd.json
```

Run the local utility:

```sh
go run ./cmd/meerstetterd -config meerstetterd.json
```

The default HTTP UI listens on `127.0.0.1:18080`. It exposes a BusMaster-style
discovery tree, a baseline graph wall, and command/status/error swimlanes. The
baseline graph wall is catalogue-driven and includes object, sink, and cascade
temperatures, target/ramp values, output power, heat-flow estimates, and device
status for every configured controller instance.

REST endpoints:

- `GET /health`
- `GET /api/health`
- `GET /api/devices`
- `GET /api/tec/catalogue`
- `GET /api/loom/source-catalogue`
- `GET /api/operator/meerstettergo/source-catalogue`
- `GET /api/discovery/tree`
- `GET /api/graph-wall`
- `GET /api/tiles?tile_id=<id>&tail=true&limit=<n>`
- `GET /api/log/ring?tail=true&limit=<n>`
- `GET /api/log/ring?after_seq=<n>&limit=<n>`
- `GET /api/log/export?tail=true&limit=<n>`
- `GET /api/log/archive/manifest`
- `GET /api/operator/meerstettergo/log/archive/manifest`
- `POST /api/log/import/review`
- `GET /api/log/review?tail=true&limit=<n>`
- `GET /api/can/ring?limit=<n>`
- `GET /api/can/ring?source=fallback_flash&limit=<n>`
- `GET /api/can/ring?source=merged&limit=<n>`
- `GET /api/events/swimlane?after_seq=<n>`
- `GET /api/target/read?id=<target_id>`
- `POST /api/target/write`
- `GET /api/operator/meerstettergo/target/read?id=<target_id>`
- `POST /api/operator/meerstettergo/target/write`

The operator routes intentionally mirror the standalone routes so Loom,
SignalForge, sequencer code, and the browser UI see the same source catalogue,
read targets, write targets, freshness, and active/redundant transport metadata.
Writable catalogue entries expose their write path and require an explicit
sequencer `lease_id` on every write request. Calls without a lease are rejected
before a hardware transport is selected.

The repository default is topology-neutral: it demonstrates one configurable
device, while real deployments provide their own device list. See
[`examples/meerstetterd.local-ftdi-test.json`](examples/meerstetterd.local-ftdi-test.json)
for the local test environment with four TEC controllers on FTDI USB serial
(`/dev/ttyUSB0` through `/dev/ttyUSB3` at `57600` baud). On Windows the same
shape uses targets such as `COM3@57600`, `COM4@57600`, and so on.

TCP serial-device-server endpoints and SocketCAN endpoints use the same config
model. SocketCAN read targets are supported with `socketcan:<if>?addr=<addr>`;
for example,
[`examples/meerstetterd.pixtend-can-four-tec.json`](examples/meerstetterd.pixtend-can-four-tec.json)
defines four PiXtend-attached TEC controllers on `can0`. Kvaser, USB-CAN, and
remote-CAN adapters still belong behind adapter implementations until each path
is proven with live hardware.

### PiXtend SocketCAN capture ring

`cmd/teccanprobe` can write received SocketCAN frames into a bounded ring file.
On PiXtend, the production service uses a RAM-backed ring as the primary hot
path and mirrors the same frames into a flash fallback ring:

```sh
sudo ./teccanprobe \
  -if can0 \
  -listen 30s \
  -active \
  -ring-path /run/meerstettergo/pixtend-can0.ring \
  -ring-size 256MiB \
  -ring-chunk 4MiB \
  -fallback-ring-path /var/lib/meerstettergo/pixtend-can0.ring \
  -fallback-ring-size 8GiB \
  -fallback-ring-chunk 4MiB
```

The ring file is pre-sized, written at deterministic chunk offsets, and synced
only when a chunk is committed. The systemd wrapper in
[`deploy/systemd`](deploy/systemd) seeds the RAM ring from the flash fallback at
startup, then captures to both rings during normal operation.

The routing rule is intentionally strict to avoid duplicates:

1. Consumers read the Pi RAM ring first. This is the low-latency owner handoff
   and graph-wall replay path.
2. If the RAM ring is unavailable after reboot, late owner connection, or
   service restart, consumers read the Pi flash ring as a degraded fallback.
   The flash ring is a recovery copy, not an additional live stream.
3. If host-side history still has a gap and the active transport exposes the
   Meerstetter controller ring/buffer primitive, the controller buffer is polled
   for high-priority values and merged by device address, parameter, instance,
   and sample time/sequence when available. Direct SocketCAN currently uses the
   decoded live queue plus Pi RAM/flash rings until the controller-ring mapping
   is characterized on that transport.

`/api/can/ring` follows the same policy by default: primary RAM first, flash
only on primary failure. `source=fallback_flash` reads the flash fallback
explicitly for bootstrap/recovery inspection while RAM is healthy.
`source=merged` returns a reconciled RAM-plus-flash tail and collapses mirrored
frames by timestamp, CAN ID, DLC, payload, and interface so a late owner can
gap-fill without double-counting the same raw frame. `/api/health` reports the
active capture state under `can_ring` without replaying raw records; `/health`
is the same lightweight edge-health alias for service managers and route
probes. If one ring fails during capture, the probe prints a `ring-error` for
that role and keeps the remaining ring plus CAN receive path running.

Live deployment checks use the full MVP gate by default, with split route and
bounded recovery gates available for focused operator checks:

```sh
PI_BASE_URL=http://192.168.6.229:18080 \
LOOM_BASE_URL=http://127.0.0.1:18087 \
./deploy/verify_mvp_completion.sh
```

```sh
BASE_URL=http://192.168.6.229:18080 ./deploy/verify_pixtend_route.sh
BASE_URL=http://192.168.6.229:18080 ./deploy/verify_pixtend_recovery.sh
BASE_URL=http://192.168.6.229:18080 \
  GATEWAY_BASE_URL=http://127.0.0.1:18087 \
  ./deploy/verify_pixtend_owner_takeover.sh
```

The recovery gate restarts only `meerstettergo.service`; it does not write to
TEC controllers. It verifies that `pixtend-can-ring.service` remains active,
telemetry sequence numbers advance again, RAM/flash ring counters do not
regress, and the merged CAN ring plus graph-wall temperature tile recover.

The owner reconnect gate is non-invasive. It leaves the gateway/owner idle for
a short direct-edge window, verifies the PiXtend RAM ring advances, then
requires the Loom/operator gateway to catch up, keep merged RAM/flash CAN
readout deduplicated, keep graph-wall data populated, and keep writable targets
lease-gated. It is route-level proof, not physical power-loss or real process
stop fault injection.

Checked-in Pi and host binaries from the live bring-up are stored under
[`artifacts/`](artifacts/). Deployment services still run the installed
`/usr/local/bin/meerstetterd` and `/usr/local/bin/teccanprobe` paths; the
artifact directory is for provenance and recovery, not an alternate runtime
location.

## Scope

This repo owns protocol-correct reusable code. Applications still decide device
ownership, polling cadence, command authority, and UI behavior. The default
device model should scale to any number of TEC controllers with transparent
Ethernet, serial, or CAN attachment, one serialized owner per downstream, and a
TCP passthrough path where the original Meerstetter software needs access.
Telemetry readout should default to the shared ring-first recorder contract,
which commits TM/TC events to the local ring before forwarding live updates.
Consumer sessions can use latest-value reads by default and opt key targets into
`ring_since_last_read` for zero-loss catch-up within the configured retention
window; raw live-only streaming is a compatibility mode.

Loom and Gossamer should consume these packages as shared contracts. Product
code should add only app-specific adapters and deployment wiring such as NATS,
SSH fallback, Kvaser CANlib ownership, and HDF5 writer implementation. This
repo keeps the neutral Meerstetter HTTP/UI utility, SocketCAN/PiXtend helpers,
and source-catalogue contracts reusable and testable.
