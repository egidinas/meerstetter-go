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
- Confirmed the PiXtend SocketCAN route is up on the live Raspberry Pi and can
  serve decoded MeCom-over-CAN telemetry from all four configured TEC
  controllers. The deployed utility exposes the four-controller source
  catalogue, graph-wall defaults, target read/write routes, RAM-ring primary
  capture, flash-ring fallback capture, log review, and export/import review
  endpoints from the same route.
- Added LUT/TMTC automation scaffolding plus sample-program documentation for
  a simple four-cycle workflow.
- Verified the baseline with the locally installed Go toolchain by running
  `go test ./...` from the repository root.

## Live Route Verification (2026-05-13)

- Raspberry Pi route is live at `http://192.168.6.229:18080/` with SSH access
  through the lab router key. `meerstettergo.service` and
  `pixtend-can-ring.service` are active and enabled.
- SocketCAN is the primary route: `can0` is `UP,LOWER_UP`, `ERROR-ACTIVE`, at
  1 Mbit/s on PiXtend `spi0.1`. Pi-side CAN debug utilities are present at
  `/usr/local/bin/candump` and `/usr/local/bin/cansend`.
- The deployed edge route reports `ok=true`, four devices, fresh increasing log
  sequence numbers, `can_ring.source=primary_ram`, RAM-ring primary capture at
  `/run/meerstettergo-ring/pixtend-can0.ring`, and flash-ring fallback capture
  at `/var/lib/meerstettergo/pixtend-can0.ring`.
- The raw CAN route now supports explicit RAM/flash reconciliation:
  `/api/can/ring?source=merged` combines the primary RAM ring and flash fallback
  ring, keeps the newest tail window, and collapses mirrored frames so owner
  bootstrap and late connection can inspect fallback history without duplicate
  frame keys.
- Live discovery found the four CAN TEC nodes `tec-75`, `tec-76`, `tec-81`,
  and `tec-84`, each exposing instances `1..4`. The discovery tree currently
  exposes 220 targets, the Loom/SignalForge source catalogue exposes 144
  entries, and 16 writable target setpoint paths are present.
- The live telemetry ring contains decoded MeCom-over-CAN values with
  `catalogue_active=true`, `active_readout=mecom_vx_round_robin_queue`, CAN as
  the active/preferred transport, and serial FTDI routes as redundant targets.
  Direct SocketCAN keeps controller internal ring-buffer readout capability-gated
  until the MeCom CRTVStream mapping is characterized on that transport.
- UI-facing routes are populated: `/api/graph-wall` returns the four-controller
  temperature, target, power, and event tiles; `/api/discovery/tree`,
  `/api/loom/discovery-tree`, `/api/loom/discovery/tree`, and
  `/api/operator/meerstettergo/discovery/tree` return the sorted signal tree as
  JSON; `/health`, `/api/health`, and `/api/operator/meerstettergo/health`
  expose edge health for service managers, direct route probes, and the Loom
  operator route; `/api/loom/source-catalogue` returns the sequencer-facing
  source catalogue and write command metadata; `/api/log/export` and
  `/api/log/import/review` round-trip recent telemetry in review mode; and
  `/api/log/archive/manifest` declares the durable archive stream contract for
  decoded telemetry, raw CAN, command events, object dictionary snapshots, and
  graph-wall assignments.
- Added `deploy/verify_pixtend_route.sh` as a repeatable pass/fail route gate.
  The latest live run passed with sequence advancement, 208 decoded targets in
  the tail window, 80 fresh high-priority values within the 30-second freshness
  window, 32 raw CAN records from the primary RAM ring, readable flash fallback
  records, merged RAM-plus-flash CAN records without duplicate frame keys, 220
  discovery targets, 16 writable paths, 144 source-catalogue entries, JSON Loom
  discovery/operator aliases, live graph-wall points, aggregate pseudo-target
  tile resolution, Arrow IPC materialization, and NDJSON import review without
  duplicate sequence IDs.
- Closed the live UI data gap that left graph-wall tiles empty in the browser:
  aggregate graph-wall pseudo-targets are no longer sent or interpreted as exact
  target IDs, and the route verifier now covers the exact aggregate temperature
  tile query used by the UI.
- Added `deploy/verify_pixtend_recovery.sh` as a bounded recovery gate. It
  restarts only `meerstettergo.service`, verifies `pixtend-can-ring.service`
  stays active, checks RAM and flash ring counters do not regress, confirms
  decoded telemetry sequence numbers advance again, and confirms the merged
  RAM-plus-flash ring and graph-wall temperature tile recover live data.
- Added `deploy/verify_pixtend_ring_recovery.sh` as a bounded ring-worker
  recovery gate. It restarts only `pixtend-can-ring.service`, verifies decoded
  telemetry and the primary RAM raw-CAN ring advance again, keeps flash
  fallback readable, confirms merged RAM-plus-flash ring data remains
  deduplicated, and confirms graph-wall temperature tiles remain live.
- Added `deploy/verify_mvp_completion.sh` as the top-level live MVP gate. After
  the Loom/operator route-alias fix and Pi redeploy, the `RUN_RECOVERY=1` run
  passed the direct PiXtend route, Loom/operator gateway route, PiXtend edge
  autonomy gate, direct browser UI smoke, bounded `meerstettergo.service`
  recovery, bounded `pixtend-can-ring.service` recovery, targeted
  Meerstetter-Go tests, and targeted Loom adapter tests. It confirms API
  health, telemetry sequence advancement, merged RAM/flash CAN-ring readout
  without duplicate mirrored frame keys, graph-wall tile recovery, post-restart
  service active state, browser-populated UI data, and the JSON discovery
  aliases consumed by the Loom gateway. The gate still avoids TEC writes;
  physical power-loss, real process-stop owner timing, end-to-end leased write
  acceptance, and bus-congestion checks remain separate hardening work.
- Added `deploy/verify_pixtend_owner_takeover.sh` as a non-invasive route-level
  owner reconnect gate. It verifies that the PiXtend edge keeps advancing while
  the gateway/owner is idle, that the Loom/operator gateway reattaches and
  catches up to the direct edge sequence, that merged RAM/flash CAN readout
  stays deduplicated, that decoded and graph-wall data stay populated after
  reattach, and that writable targets remain lease-gated. The top-level MVP gate
  now runs this verifier by default.
- Reran the default non-invasive MVP gate after the plain `/health` edge alias
  was deployed. It passed the direct PiXtend route, Loom/operator gateway
  route, PiXtend edge autonomy gate, direct browser UI smoke, targeted
  Meerstetter-Go tests, and targeted Loom adapter tests. The direct edge
  reported four devices, latest sequence advancement, primary RAM CAN ring at
  `/run/meerstettergo-ring/pixtend-can0.ring`, flash fallback at
  `/var/lib/meerstettergo/pixtend-can0.ring`, 220 discovery targets, 16 writable
  paths, 144 source-catalogue entries, 80 fresh high-priority values, and
  deduplicated merged RAM/flash CAN-ring readout.
- Added `deploy/verify_pixtend_edge_autonomy.sh` as a non-invasive edge-worker
  independence gate. The latest live run proved direct edge telemetry advanced
  from sequence `70512` to `71136` and the primary RAM raw-CAN ring advanced
  from `51832` to `53196` during a gateway-idle window with bounded flush
  grace, while the flash fallback stayed readable/non-regressing and the merged
  RAM/flash tail stayed free of duplicate mirrored frame keys. This proves the
  PiXtend edge worker is not dependent on continuous gateway polling;
  intentional gateway outage, owner handoff, bus congestion, and physical power
  interruption remain separate fault-injection tests.
- Route evidence is captured in
  [`docs/pixtend_route_audit_2026-05-13.md`](pixtend_route_audit_2026-05-13.md).
- MVP completion status and the remaining closeout gates are captured in
  [`docs/mvp_completion_audit_2026-05-13.md`](mvp_completion_audit_2026-05-13.md).
- Cleaned the repository surface after the live PiXtend bring-up by moving
  tracked deployment binaries out of the root into platform-specific
  `artifacts/` directories, documenting their install/runtime role, and
  preserving the hidden TEC CAN parameter seed fragment as an ignored-build
  reference under `docs/reference`.
- Tightened the MVP audit to reflect what the Loom/operator gateway verifier
  already proves live: source-catalogue ownership metadata, remote read/write
  routes, polling freshness, target-read availability, RAM/flash ring merge,
  graph-wall data, Arrow/NDJSON export, import review, and gateway-side
  no-lease write rejection. Remaining work is now focused on real process-stop
  owner timing and leased write acceptance, not basic catalogue plumbing.
- Tightened the browser smoke gate so a passing UI run now proves not only that
  the graph wall and 220-target signal tree render, but also that the rendered
  target rows expose parsable device, instance, parameter, active transport,
  readout, and writable/read-only provenance for the four-controller PiXtend
  route.
- Promoted the browser interaction verifier into the default MVP gate. The
  canonical run now exercises graph-wall focus mode, graph-wall filters, exposed
  route links, in-page import review, writable controls, and visual provenance
  chip prefixes in addition to the DOM/screenshot population smoke.
- Reran the canonical non-invasive MVP gate after that promotion. It passed the
  direct PiXtend edge route, Loom/operator gateway route, edge autonomy,
  owner-reconnect/takeover, browser DOM smoke, browser interactions, targeted
  Meerstetter-Go tests, and targeted Loom adapter tests. A repository-wide
  `/home/svc_pmg_testbed_b/.local/go/bin/go test ./...` also passed. The
  remaining physical power-loss, real process-stop owner timing, leased-write,
  bus-congestion, and controller-ring gap-fill checks are explicitly tracked as
  production hardening rather than MVP blockers.

Remaining route-hardening work is tracked below as reusable packaging,
real process-stop owner timing, power-interruption recovery tests, measured bus
budgeting, and exact live validation of the controller ring-buffer primitive
limits.

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
- Promote the proven PiXtend four-controller config into deployment-owned
  packaging. The private live service has validated the route; the remaining
  reusable work is packaging, config provenance, reconnect handling, and clear
  separation between public-safe examples and site-local controller details.
- Complete the multi-device server runner around `mecomserver.HubConfig`. The
  config, target discovery, local HTTP server, and per-device passthrough
  listener scaffold exist; remaining work is the real downstream owner loop,
  smart TM mux, TC demux, and reconnect behavior.
- Add reconnect/resume helpers using ring sequence numbers. The edge route now
  has a tested RAM-plus-flash merge primitive plus decoder-service and
  ring-worker restart recovery verifiers plus route-level owner reconnect and
  catch-up coverage; remaining work is real process-stop owner timing,
  power-interruption recovery, end-to-end leased write acceptance, and
  controller-ring gap-fill regression coverage.
- Added a conservative `mecom` bus-capacity estimator for 4, 8, and 16
  controller cases. It derives the ring-slot budget, overflow into the
  round-robin queue, queue cycle time, and estimated CAN utilization from the
  active TEC catalogue and adapter timing assumptions. Remaining work is to
  replace the conservative frame-count assumptions with measured SocketCAN,
  Kvaser USB, Kvaser DIN-rail, and remote-bridge timings.
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

- Keep the implemented Arrow IPC telemetry writer verifier-gated and implement
  HDF5 archive output only if it becomes a production requirement. Both formats
  stay described by `/api/log/archive/manifest`.
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
- Added `deploy/verify_ui_browser_smoke.sh` as an automated browser smoke test
  for the live graph wall. Keep richer pixel/layout regression checks as
  optional hardening if UI changes become frequent.

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
  `../shared/loom-gossamer-shared/docs/backlog/shared_loom_gossamer_backlog.md`
  as S-LG-12 through S-LG-15.
- Public readiness and legacy-harvest gates are tracked in
  `docs/public_variant_readiness.md`.
