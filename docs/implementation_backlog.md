# Implementation Backlog

This backlog is scoped to the reusable library. Product-specific Loom,
Gossamer, or standalone utility work should link to these items rather than
duplicating protocol code.

## Done Baseline (2026-05-12)

- Created the private GitHub repository `egidinas/meerstetter-go`, pushed
  `main`, and preserved the current integration baseline in commit
  `06dca7da707a574b7dd0c5aa7843f14b635176b3`.
- Added the first reusable MeCom readout pipeline: high-priority ring-buffer
  lane, lower-priority round-robin bulk lane, manual front-of-queue polling,
  congestion-aware longer ring reads, and asynchronous reduction primitives.
- Added a reusable Meerstetter TEC catalogue/source-offer path for arbitrary
  controller counts. The current catalogue includes high-priority object,
  sink, cascade, target, and ramp temperatures, electrical output signals,
  status/error values, and derived electrical/thermal estimates.
- Added channel-mode-aware thermal/electrical estimation for resistor,
  power-supply, and Peltier-driver channels, including aggregate estimates and
  confidence/provenance fields.
- Added generic `control` transition and PID observation primitives. These are
  intentionally advisory by default and do not write controller PID parameters
  without an explicit higher-level policy.
- Added a CAN adapter catalogue with SocketCAN/PiXtend and Kvaser USB/DIN-rail
  as first-class paths. Common SocketCAN-compatible USB/Ethernet adapters are
  listed as easy but not yet proven.
- Added utility graph-wall defaults focused on a four-controller TEC bank with
  in-memory/ring-first signal handling.
- Confirmed the current PiXtend CAN route works with four TEC controllers on
  the live bus. This validates the four-controller graph-wall baseline as a
  real deployment case, not only a fixture.
- Added LUT/TMTC automation scaffolding plus sample-program documentation for
  a simple four-cycle workflow.
- Verified the baseline with `/home/svc_pmg_testbed_b/.local/go/bin/go test
  ./...`.

## P1 Library Completeness

- Promote the current built-in MeCom catalogue into a documented object
  dictionary loader. Keep the output in `objectdict.Dictionary` so MeCom and
  CANopen discovery use the same target model.
- Add runtime dictionary merge helpers for MeCom parameters plus CANopen EDS
  entries. The merge must preserve protocol-specific IDs while exposing one
  semantic target when both protocols refer to the same controller value.
- Complete CANopen expedited SDO typed value decoding and object-dictionary
  mapping. Keep existing frame primitives as the transport-level foundation.
- Add command-result correlation helpers that map MeCom sequence numbers,
  CANopen SDO replies, and sequencer acknowledgements into `tmtc.CommandEvent`.
- Add full catalogue discovery for documented and currently undocumented
  MeCom parameters. Discovery must scan available instances, record confidence
  and provenance, and keep unknown parameters out of active polling until they
  are classified.
- Add channel-mode discovery and reconciliation. Per-channel resistor,
  power-supply, and Peltier-driver mode must be visible in the catalogue and
  must gate derived thermal estimates.
- Replace nearest-point Peltier model lookup with calibrated interpolation from
  module data. Capture temperature gradient, current, voltage, pumped heat, and
  dissipated heat per channel and globally.
- Validate the exact MeCom ring-buffer primitive on live controllers and encode
  its limits, wrap behavior, and failure modes in tests.

## P2 Universal Utility Support

- Complete the application-facing polling scheduler interface around the new
  queue/readout primitives. It must read through `tmtclog.Recorder`, publish
  reduced values at consumer-requested rates, and honor `tmtclog.ReadPolicy`
  per target.
- Complete the multi-device server runner around `mecomserver.HubConfig`. The
  config, target discovery, local HTTP server, and per-device passthrough
  listener scaffold exist; remaining work is the real downstream owner loop,
  smart TM mux, TC demux, and reconnect behavior.
- Add reconnect/resume helpers using ring sequence numbers.
- Add a bus-capacity estimator for 4, 8, and 16 controller cases. The estimator
  must derive safe poll budgets from measured frame timing, adapter backend,
  bitrate, ring-buffer read length, and configured consumer rates.
- Add graceful degradation policies for congestion: prefer less frequent,
  longer ring-buffer reads for high-priority values, slow the round-robin bulk
  lane first, and surface degraded freshness explicitly.
- Promote graph-wall fixture generation from the current utility baseline into
  a public helper. The current baseline assigns object, sink, cascade, target,
  ramp, electrical output, derived thermal estimates, and status for the
  four-controller focus case.
- Add sequencer execution interfaces that send `tmtc.Telecommand` and subscribe
  to acknowledged results.
- Add concrete CAN adapter integration points for SocketCAN, Kvaser CANlib,
  Kvaser DIN-rail Ethernet, and remote CAN bridges. The endpoint and discovery
  model already carry CAN targets; dialing is intentionally adapter-owned
  today.
- Add an operator-gated device self-tuning adapter. The default loop may
  observe and recommend, but controller self-tune and PID writes must require
  explicit caller policy.
- Add a reusable transition-characterization service that records setpoint,
  load, mode, and disturbance transitions and emits advisory PID snapshots
  without being Meerstetter-specific.
- Keep edge-device logging flash-safe by default: bounded in-memory rings on
  PiXtend, explicit off-device forwarding, and optional archive/export outside
  the Pi hot path.

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
- Add REST/API surfaces for catalogue discovery, poll-queue state, adapter
  health, channel modes, derived model confidence, and advisory PID snapshots.
- Add graph-wall layouts optimized for the four-controller case while allowing
  reasonable arbitrary counts up to 16 controllers.
- Add utility UI affordances for manual poll priority: user-triggered reads
  should move targets to the front of the queue and return when that value next
  comes around, not force a special single-value slow path.
- Add archive handoff contracts so long-term logging and signal reduction can
  run on a different node when PiXtend CPU, bus, or flash endurance limits are
  reached.

## Newly Identified Decision Gates

- Full catalogue discovery is required before broad polling is trusted. Unknown
  parameters may be discovered and recorded, but they need classification before
  becoming default graph-wall or control-loop inputs.
- PID optimization must stay advisory until there is enough transition history
  to prove stability, identify operating regions, and define rollback rules.
  Minor automatic adjustments need separate policy and evidence.
- Peltier heat-flow estimates must respect per-channel mode. Resistor and
  power-supply channels may expose electrical output, but must not be reported
  as Peltier pumped-heat estimates.
- CAN adapter support must be proven per backend. SocketCAN/PiXtend and Kvaser
  paths are first-class targets; other common adapters can be listed as easy
  candidates only after smoke tests.
- Polling must scale by measuring the bus, not assuming it. The scheduler must
  publish what rate is actually achieved and degrade lower-priority values
  first.

## Integration Notes

- Default storage must be ring-first: append locally, then forward. Default
  consumer readout is queue-based; configured key targets use ring-buffer
  polling for zero-loss catch-up and signal-noise improving reduction.
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
