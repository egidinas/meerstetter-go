# Meerstetter Go Implementation Guide

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

For a usable utility, model every attached controller or adapter target as its
own owned device. The project default must stay topology-neutral: deployments
provide the concrete device list. The local test environment currently has four
TEC controllers connected over FTDI USB serial, for example `/dev/ttyUSB0`
through `/dev/ttyUSB3` at `57600` baud or the matching `COMx@57600` targets on
Windows. The connection model stays transparent whether the downstream is
Ethernet, serial, or CAN. The default shape should scale to any number of TEC
controllers: one serialized downstream connection per device, one optional TCP
passthrough listener per device for the original Meerstetter software, and one
TM/TC queue pair per device so telemetry polling and telecommands are
multiplexed and demultiplexed deterministically. CAN endpoints use the same
device model, but the concrete adapter belongs in the application because
SocketCAN, Kvaser, and remote CAN bridges have different ownership rules.

### Transport

Package: `transport`

The transport package parses and dials:

- `tcp:host:port`
- bare `host:port`
- `serial:/dev/ttyUSB0@57600`
- `COM3@57600`
- `can:can0/0x23`

Serial defaults to `57600` baud when no rate is supplied. TCP serial-device
servers commonly expose port `50000`, but applications should keep that as
configuration rather than hardcoding one topology. CAN endpoints are parsed as
first-class transparent targets; dialing them is intentionally delegated to an
application adapter.

### CANopen

Packages: `canopen`, `canopen/eds`, `objectdict`

Meerstetter TEC controllers can also expose values through CANopen. The EDS
parser builds a protocol-neutral `objectdict.Dictionary`, so a UI can present
the same target tree for MeCom and CANopen entries.

The core CANopen package currently owns minimal classic-CAN frame primitives and
expedited SDO upload/download request construction. Host-specific adapters such
as SocketCAN, Kvaser CANlib, or a remote NATS bridge belong in applications.

## Default Runtime Pattern

### Discovery

Package: `discovery`

Discovery produces BusMaster-style trees of `Target` values. A target is a
telemetry source, telecommand destination, or both. It carries:

- semantic group path,
- protocol,
- object dictionary entry,
- unit and value kind,
- node and transport metadata,
- explicit ownership.

Ownership must stay visible. A commandable connection can be owned by the local
node, a remote node, a legacy application, a shared monitor, or a derived data
source. UI and automation should not guess this from network location.

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

The same primitive can be carried over direct Go calls, NATS, REST, HTTP, SSH,
or a local device-server connection.

### Ring-First Readout

Package: `tmtclog`

Telemetry, telecommands, and command events should be appended to the local ring
before live forwarding.

The consumer readout mode is configurable. The normal default is a single latest
sample read for ordinary UI values. Key values that must not lose intermediate
samples can be configured for `ring_since_last_read`, where each controller
session reads every retained sample for that target since its own last sequence.
That is the zero-loss path within the configured retention window.

Live-only streaming should be treated as a legacy or diagnostic fallback.

### Graph Wall and Sequencer

Packages: `graphwall`, `sequencer`

Discovery targets can be assigned to graph-wall tiles without coupling the
library to any web framework. Sequencer steps reference the same target and
TMTC command primitives, so a supervisor or command center can drive devices
without a second command contract.

The standalone utility baseline graph wall is generated from discovery. For
each configured TEC controller it includes catalogue-derived object, sink, and
cascade temperatures, target/ramp values, output power, heat-flow estimates,
and device status. Event/status/error rendering is kept separate as a swimlane
timeline derived from the TM/TC ring so labels and faults do not overlap the
trend area.

## Application Boundary

Keep these pieces outside this repository:

- vendor PDFs and large extracted text dumps,
- lab endpoint lists,
- hardware captures,
- NATS subject naming,
- REST route handlers,
- HDF5 writer implementation details,
- SocketCAN or Kvaser process ownership,
- web UI rendering.

Those application layers can depend on this library while keeping this repo
small, testable, and shareable.
