# meerstetter-go

Public Go building blocks for Meerstetter TEC/LDD communication.

This repository is intentionally independent from Loom and lab deployments. It
contains protocol code, deterministic fixtures, and small diagnostic commands.
It does not vendor site-specific endpoints, private captures, service units, or
operator UI assets.

For browser-based public review, open `docs/public_review.html`. It documents
the package map, command-authority boundary, SignalForge dependency, and
verification gates without adding an operator UI to this library repository.

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

- `cmd/mecomprobe`: bounded MeCom read probe for TCP or serial endpoints
  (registry-driven, supports bulk `?VX` reads).
- `cmd/mecomset`: bounded MeCom write probe for TCP or serial endpoints.
- `cmd/mecompoll`: transport-agnostic continuous polling table. Each target
  is `ENDPOINT=ADDRESS`; `mecom.NewForEndpoint` picks the right concrete
  client (ASCII or CANopen SDO) per endpoint.
- `cmd/mecomrun`: executes a `sequencer.Script` JSON via a `tmtc.Commander`
  built on top of `mecom.Commander`. Reference end-to-end automation loop.
- `cmd/mecomvseriald`: address-routed Linux device server. One TCP listener
  fans out addressed MeCom frames to per-device serial/TCP downstreams. See
  `deploy/` for systemd unit + udev rule + deployment walkthrough.
- `cmd/teccanprobe`: SocketCAN/CANopen probe for live TEC discovery and
  optional local CAN ring capture.

## Example

```go
// Transport-agnostic: NewForEndpoint picks the right client.
ep, _ := mecom.ParseEndpoint("serial:/dev/ttyUSB0@57600")
client, err := mecom.NewForEndpoint(ctx, ep, mecom.ClientConfig{
    Address: 0x4b, Timeout: 2 * time.Second,
}, nil) // nil dialer is fine for non-CAN endpoints
if err != nil { panic(err) }
defer client.Close()

value, err := client.ReadFloat32(ctx, 1000, 1)

// Write via a Commander built from any WriteClient.
writer := client.(mecom.WriteClient)
cmdr := mecom.NewCommander(writer, 2*time.Second)
_, err = cmdr.Send(tmtc.Telecommand{
    Name:      "set_float32",
    Arguments: map[string]any{"param": 3000, "instance": 1, "value": 25.0},
})
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

## Portability and adapter boundary

The core library is intended to be usable outside the PiXtend/Linux setup. The
transport boundary is explicit so another host or adapter can provide the same
MeCom client surface without changing catalogue, sequencer, or command code.

| Surface | Linux | Windows | Notes |
|---------|-------|---------|-------|
| `mecom` framing, CRC, reads/writes, endpoint parsing | yes | yes | Pure Go protocol code. |
| TCP endpoints (`tcp:host:port`, `host:port`) | yes | yes | Recommended cross-platform path for device servers and remote bridges. |
| Serial endpoints | yes (`/dev/...`) | yes (`COM3@57600`) | Uses Go serial support; stable device naming is host-specific. |
| `cmd/mecomprobe`, `cmd/mecomset`, `cmd/mecompoll`, `cmd/mecomrun` over TCP/serial | yes | yes | These commands do not require Linux when used with TCP/serial endpoints. |
| `cmd/mecomvseriald` router logic | yes | yes | Portable as a process; `deploy/` service assets are Linux-specific. |
| `socketcan` and stock `can:can0/...` binary dialer | yes | no | SocketCAN is Linux-only. |
| CAN on Windows | adapter required | adapter required | The public API accepts an injected `mecom.CANDialer`; a Kvaser CANlib, PCAN, Vector, SLCAN, Ethernet-CAN, or TCP-CAN bridge adapter can implement it. Such adapters should remain optional until proven. |
| `deploy/` systemd and udev files | yes | no | Use a Windows service wrapper or run interactively on Windows. |

For Windows hosts today, the proven routes are direct TCP, serial `COMx@baud`,
or the single-port `mecomvseriald` device-server pattern. Native Windows CAN is
not blocked by the library design, but it needs a concrete adapter package for
the selected hardware driver.

## Verification

```sh
go test ./...
git diff --check
```

The public module depends on public SignalForge releases and has no committed
local `replace` directives. Private Loom deployment wiring, live route checks,
tracked bring-up binaries, and lab-specific examples are intentionally outside
this public baseline.
