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
  downstream device across multiple clients while preserving serialized access.
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

## Scope

This repo owns protocol-correct reusable code. Applications still decide device
ownership, polling cadence, command authority, and UI behavior. Telemetry
readout should default to `tmtclog.NewRecorder`, which commits TM/TC events to
the local ring before forwarding live updates and lets controller sessions
resume from their last seen sequence. That is the zero-loss path within the
configured retention window; raw live-only streaming is a compatibility mode.

Loom and Gossamer should consume these packages as shared contracts. Product
code should add only app-specific adapters: NATS, REST, HTTP, SSH fallback,
SocketCAN, Kvaser CANlib, HDF5 writer implementation, and UI rendering.
