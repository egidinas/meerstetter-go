# Implementation Backlog

This backlog is scoped to the reusable library. Product-specific Loom,
Gossamer, or standalone utility work should link to these items rather than
duplicating protocol code.

## P1 Library Completeness

- Add a MeCom object dictionary loader for documented parameter tables.
  Keep the output in `objectdict.Dictionary` so MeCom and CANopen discovery use
  the same target model.
- Add runtime dictionary merge helpers for MeCom parameters plus CANopen EDS
  entries. The merge must preserve protocol-specific IDs while exposing one
  semantic target when both protocols refer to the same controller value.
- Add CANopen expedited SDO response parsing and typed value decoding.
- Add command-result correlation helpers that map MeCom sequence numbers and
  CANopen SDO replies into `tmtc.CommandEvent`.

## P2 Universal Utility Support

- Add an application-facing polling scheduler interface that reads through
  `tmtclog.Recorder` before publishing live telemetry and honors
  `tmtclog.ReadPolicy` per target.
- Complete the multi-device server runner around `mecomserver.HubConfig`. The
  config, target discovery, local HTTP server, and per-device passthrough
  listener scaffold exist; remaining work is the real downstream owner loop,
  smart TM mux, TC demux, and reconnect behavior.
- Add reconnect/resume helpers using ring sequence numbers.
- Promote graph-wall fixture generation from the current utility baseline into
  a public helper. The current baseline assigns HR temperature, LR temperature,
  output power, target value, and status for every configured TEC controller.
- Add sequencer execution interfaces that send `tmtc.Telecommand` and subscribe
  to acknowledged results.
- Add concrete CAN adapter integration points for SocketCAN, Kvaser CANlib, and
  remote CAN bridges. The endpoint and discovery model already carry CAN
  targets; dialing is intentionally adapter-owned today.

## P3 Export and UI Contracts

- Define an HDF5 export schema contract for telemetry, telecommands, command
  events, object dictionary snapshots, and graph-wall assignments.
- Extend the minimal utility web UI into a small Windows/macOS/Linux utility:
  collapsible BusMaster-style tree, graph-wall assignment editing, live trend
  tiles, and REST-backed command controls.
- Extend event swimlane metadata for command events, warnings, faults, setpoint
  changes, and device reconnects. The first `/api/events/swimlane` route exists
  and derives command failures plus telemetry quality warnings from the ring.
- Add REST command endpoints for third-party implementations. Commands must use
  the same idempotent `tmtc.Telecommand` and acknowledged result contract as the
  sequencer.

## Integration Notes

- Default storage must be ring-first: append locally, then forward. Default
  consumer readout can be latest-value single reads, while configured key
  targets use `ring_since_last_read` for zero-loss catch-up.
- Rich decoded telemetry is the default. Raw frames are compatibility evidence.
- Telecommands are idempotent unless a command explicitly declares otherwise.
- Connection ownership must be explicit and visible in discovery output.
- Passthrough semantics are transport-transparent for Ethernet, serial, and
  CAN; only the low-level adapter changes.

## Public / Private Boundary

- Meerstetter-Go is the public/general MeCom and Meerstetter implementation.
  General protocol framing, object dictionaries, TCP/serial/CAN transports,
  ring-buffer readout, REST API, discovery UI, graph wall integration, logging,
  FORT/HDF5 archive support, and passthrough server behavior belong here when
  they are vendor/protocol-general.
- Private deployment details do not belong here: real lab topology, controller
  nicknames, hostnames, IP routes, credentials, private presets, customer
  procedure names, and testbed-specific default graphs must stay in the private
  implementation or local untracked config.
- Shared modules consumed by this repo must remain neutral primitives. If a
  feature needs MeCom-specific behavior, keep it in Meerstetter-Go unless it can
  be expressed as a protocol-independent interface.
- The canonical cross-repo boundary work is tracked in
  `/home/svc_pmg_testbed_b/shared/loom-gossamer-shared/docs/backlog/shared_loom_gossamer_backlog.md`
  as S-LG-12 through S-LG-15.
- Public readiness and legacy-harvest gates are tracked in
  `docs/public_variant_readiness.md`.
