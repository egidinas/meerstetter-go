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
`#0015AB?VR03E801C21A\r`. Serial endpoints default to `57600` baud when no
explicit rate is supplied. Ethernet serial-device-server targets typically use
TCP port `50000`.

## Packages

- `objectdict`: shared semantic model for parameters, CANopen objects, and
  discovery output.
- `canopen/eds`: EDS parser that builds an `objectdict.Dictionary`.
- `canopen`: minimal CANopen frame and SDO request primitives.
- `mecom`: MeCom framing, numeric encoding/decoding, response parsing, and a
  small synchronous client over `io.ReadWriter`.
- `mecomserver`: MeCom device server pattern for sharing one TCP or serial
  downstream device across multiple clients while preserving serialized access;
  the hub config models many transparent Ethernet, serial, and CAN targets with
  per-device passthrough, queue, and ring-retention defaults.
- `transport`: endpoint parsing plus TCP and serial dial helpers.
- `discovery`: BusMaster-style collapsible TM/TC target tree with explicit
  ownership, protocol, dictionary entry, and graph assignment metadata.
- `tmtc`: shared rich telemetry and telecommand primitives with idempotent
  command keys, ACK/result events, and transport-neutral publisher interfaces.
- `tmtclog`: bounded loop buffer for TM/TC troubleshooting and later export.
- `export`: shared export interface, including HDF5 as a first-class target
  without forcing a CGO dependency into this core library.
- `graphwall`: graph-wall assignment contracts for wiring discovery targets to
  UI tiles.
- `sequencer`: script/step/result contracts that can be executed over the same
  TMTC command primitives.
- `utility`: reusable standalone server wiring for discovery, graph-wall,
  ring-log, event swimlane, and per-device passthrough.
- `cmd/meerstetterd`: small local utility binary exposing the `utility` server
  through HTTP and a minimal browser UI.

## Documentation

- [`docs/README.md`](docs/README.md): repository-local MeCom, CANopen, TMTC,
  logging, graph-wall, and utility architecture guide.
- [`docs/source_inventory.md`](docs/source_inventory.md): local source-material
  inventory used while deriving the implementation, including which files stay
  outside this public-safe repo.
- [`docs/implementation_backlog.md`](docs/implementation_backlog.md): remaining
  implementation work for the universal Meerstetter utility.
- [`docs/public_variant_readiness.md`](docs/public_variant_readiness.md): public
  dependency gates and legacy-harvest boundaries.

## Example

```go
ep, ok := transport.ParseEndpoint("serial:/dev/ttyUSB0@57600")
if !ok {
    panic("invalid endpoint")
}
conn, err := transport.Dial(context.Background(), ep, 2*time.Second)
if err != nil {
    panic(err)
}
defer conn.Close()

client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x50})
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
baseline graph wall includes HR temperature, LR temperature, output power,
target value, and device status for every configured controller.

REST endpoints:

- `GET /api/health`
- `GET /api/devices`
- `GET /api/discovery/tree`
- `GET /api/graph-wall`
- `GET /api/log/ring?after_seq=<n>`
- `GET /api/events/swimlane?after_seq=<n>`

The repository default is topology-neutral: it demonstrates one configurable
device, while real deployments provide their own device list. See
[`examples/meerstetterd.local-ftdi-test.json`](examples/meerstetterd.local-ftdi-test.json)
for the local test environment with four TEC controllers on FTDI USB serial
(`/dev/ttyUSB0` through `/dev/ttyUSB3` at `57600` baud). On Windows the same
shape uses targets such as `COM3@57600`, `COM4@57600`, and so on.

TCP serial-device-server endpoints and CAN endpoints are still supported by the
same config model. CAN endpoints are parsed and carried through discovery today;
a concrete SocketCAN, Kvaser, or remote-CAN adapter still belongs at the
application boundary.

## Scope

This repo owns protocol-correct reusable code. Applications still decide device
ownership, polling cadence, command authority, and UI behavior. The default
device model should scale to any number of TEC controllers with transparent
Ethernet, serial, or CAN attachment, one serialized owner per downstream, and a
TCP passthrough path where the original Meerstetter software needs access.
Telemetry readout should default to `tmtclog.NewRecorder`, which commits TM/TC
events to the local ring before forwarding live updates. Consumer sessions can
use latest-value reads by default and opt key targets into
`ring_since_last_read` for zero-loss catch-up within the configured retention
window; raw live-only streaming is a compatibility mode.

Loom and Gossamer should consume these packages as shared contracts. Product
code should add only app-specific adapters: NATS, REST, HTTP, SSH fallback,
SocketCAN, Kvaser CANlib, HDF5 writer implementation, and UI rendering.
