# Meerstetter-Go Implementation Guide

This repository contains reusable Go primitives for Meerstetter TEC and LDD
communication. It is deliberately smaller than an application: it should be
safe to share, easy to test, and independent from Loom, Gossamer, or any lab
deployment.

## Protocol Layers

### MeCom

Package: `mecom`

MeCom is a strict request-response protocol over serial-like transports. The
host sends `#` client frames, the device replies with `!` server frames, and
every frame is terminated by `\r`.

The package owns:

- CRC-16-CCITT calculation for frame integrity.
- Single reads with `?VR`.
- Bulk reads with `?VX`.
- Writes with `VS`.
- Float32, int32, and string payload encoding.
- NACK decoding into named Go errors.
- A small synchronous client over `io.ReadWriter`.

Applications should use the synchronous client behind one serialized owner per
physical connection. Multiple UI sessions may subscribe to decoded telemetry,
but they should not concurrently write to the same serial/TCP downstream
without an explicit command arbiter.

### Transport Helpers

Package surface: `mecom.ParseEndpoint`, `mecom.Open`

The MeCom package exposes endpoint parsing and TCP/serial opening directly, so
the public module does not depend on private shared transport packages.
Supported endpoint shapes are:

- `tcp:host:port`
- bare `host:port`
- `serial:/dev/ttyUSB0@57600`
- `COM3@57600`
- `can:can0/0x23`

The parser defaults serial endpoints to `115200` baud when no rate is supplied.
Real deployments should pin controller-specific rates explicitly. CAN endpoints
are parsed as targets, but the concrete bus owner is selected by the
application because SocketCAN, Kvaser, and remote CAN bridges have different
ownership rules.

### CANopen

Packages: `canopen`, `canopen/eds`, `objectdict`

Meerstetter TEC controllers can expose values through CANopen. The EDS parser
builds a protocol-neutral `objectdict.Dictionary`, so applications can present
the same target tree for MeCom and CANopen entries.

The core CANopen package currently owns minimal classic-CAN frame primitives and
expedited SDO upload/download request construction. Host-specific adapters such
as SocketCAN, Kvaser CANlib, or a remote bridge belong at the application
boundary.

## Raspberry Pi / PiXtend V2-L

For complete bring-up instructions — OS prerequisites, device-tree overlays,
SocketCAN interface configuration, passive bus verification, and `teccanprobe`
usage — see [rpi_pixtend_bootstrap.md](rpi_pixtend_bootstrap.md).

The PiXtend V2-L profile (`pixtend-v2l`) is the default adapter profile in
`teccanprobe` and includes preflight checks for SPI, the `pixtendv2l` kernel
overlay, and MCP2515 driver binding. Run `teccanprobe -checklist` to confirm
hardware state before opening the CAN socket.

## Runtime Contracts

### Telemetry and Telecommand

Package: `tmtc`

Rich decoded telemetry is the default contract. Raw bytes may be attached as
evidence, but they are compatibility data, not the primary API.

Telecommands are idempotent by default. If a caller does not provide an
idempotency key, the library derives one from the target, command name, and raw
payload. Commands should emit acknowledged status events:

- accepted,
- sent,
- acked,
- completed,
- rejected,
- failed.

### Catalogue and Automation

Packages: `mecomdict`, `mecomautomation`, `sequencer`

The public catalogue identifies protocol targets, value kinds, units, read/write
capability, and Meerstetter parameter metadata. Automation helpers adapt public
SignalForge control programs into MeCom LUT preload commands. Starting outputs,
changing controller authority, or binding to operator workflows remains an
application decision.

### Application Boundary

Keep these pieces outside this repository:

- vendor PDFs and large extracted text dumps,
- lab endpoint lists,
- hardware captures,
- NATS subject naming,
- product-specific REST/UI surfaces,
- Kvaser process ownership and driver-specific recovery policy,
- deployment units and private route checks.

This repository owns reusable protocol and contract code. Applications own live
device access, lease policy, route supervision, and UI/backend integration.
