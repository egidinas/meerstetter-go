# meerstetter-go

Public Go building blocks for Meerstetter TEC/LDD communication.

This repository is intentionally independent from Loom and lab deployments. It
contains protocol code, deterministic fixtures, and small diagnostic commands.
It does not vendor site-specific endpoints, private captures, service units, or
operator UI assets.

## Packages

- `mecom`: MeCom framing, CRC handling, typed reads/writes, response parsing,
  endpoint parsing, and TCP/serial opening.
- `mecomdict`: seeded Meerstetter TEC parameter catalogue with readable and
  writable target metadata.
- `mecomserver`: serialized TCP/serial downstream sharing for MeCom devices.
- `mecomautomation`: Meerstetter LUT/program helpers backed by public
  SignalForge control-program contracts.
- `canopen` and `canopen/eds`: CANopen frame helpers and EDS parsing.
- `objectdict`: protocol-neutral object dictionary and catalogue model.
- `canadapter` and `socketcan`: adapter-neutral CAN frame contracts plus Linux
  SocketCAN helpers.
- `canring`: bounded chunked CAN receive ring files for edge capture.
- `control`, `tmtc`, `sequencer`, and `export`: command authority, telemetry,
  sequencer, and export contracts.

## Commands

- `cmd/mecomprobe`: bounded MeCom read probe for TCP or serial endpoints.
- `cmd/mecomset`: bounded MeCom write probe for TCP or serial endpoints.
- `cmd/teccanprobe`: SocketCAN/CANopen probe for live TEC discovery and
  optional local CAN ring capture.

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

client := mecom.NewClient(conn, mecom.ClientConfig{
    Address: 0x50,
    Timeout: 2 * time.Second,
})
value, err := client.ReadFloat32(context.Background(), 1000, 1)
```

Supported endpoint forms include:

- `tcp:host:port`
- bare `host:port`
- `serial:/dev/ttyUSB0@57600`
- `COM3@57600`
- `can:can0/0x23`

CAN endpoints are parsed as first-class targets, but concrete CAN ownership
belongs to the application adapter. SocketCAN helpers are included for Linux;
Kvaser, remote CAN, and product-specific bridges should live in applications
until their ownership and recovery semantics are proven.

## Verification

```sh
go test ./...
```

The public module depends on public SignalForge releases and has no committed
local `replace` directives. Private Loom deployment wiring, live route checks,
tracked bring-up binaries, and lab-specific examples are intentionally outside
this public baseline.
