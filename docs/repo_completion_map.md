# Repository Completion Map

This document is the public-safe map of the Meerstetter-Go repository. It names
the reusable surfaces that should stay clean, independently testable, and free
of lab deployment assumptions.

## Current State

The repository is a public protocol and contract baseline for Meerstetter work.
It is not the deployment repository for any specific testbed route.

The default verification gate is:

```sh
go test ./...
```

## Canonical Public Surfaces

- `mecom`: MeCom framing, endpoint parsing/opening, typed reads/writes, bulk
  reads, and response parsing.
- `mecomdict`: seeded Meerstetter signal catalogue, readable targets, writable
  targets, units, groups, and instance metadata.
- `mecomserver`: serialized downstream sharing for serial/TCP MeCom device
  access, including transparent passthrough use cases.
- `canopen`, `canopen/eds`, `objectdict`: CANopen/object-dictionary primitives
  and protocol-neutral catalogue model.
- `canadapter` and `socketcan`: adapter-neutral CAN frame contracts plus Linux
  SocketCAN helpers.
- `canring`: bounded raw-CAN ring files for local edge capture.
- `tmtc`, `control`, `sequencer`, `mecomautomation`, `export`: reusable
  telemetry, telecommand, write-lease, automation, sequencer, and export
  contracts.
- `cmd/mecomprobe`, `cmd/mecomset`, `cmd/teccanprobe`: bounded diagnostic
  commands.

## Boundary Rules

- Keep hardcoded lab paths, hostnames, addresses, and credentials out of the
  public module.
- Keep raw CAN as evidence and fallback; decoded, typed targets are the primary
  UI/backend contract.
- Keep command/write paths lease-gated. A readable target becoming writable must
  add catalogue metadata, current-value initialization, validation, and a write
  guard.
- Add Kvaser, USB-CAN, DIN-rail Ethernet, or other adapters behind application
  boundaries until their ownership and recovery semantics are verified.
- Prefer public SignalForge contracts for reusable catalogue, graph, and
  control-program shapes. Keep Loom-only command authority and deployment
  policy in Loom.
