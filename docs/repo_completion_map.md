# Repository Completion Map

This document is the operator-facing map of the current Meerstetter-Go repo. It
does not replace the implementation backlog or MVP audit; it names the
canonical surfaces that should be kept clean, reusable, and verified.

## Current State

The repository is a functional MVP for the four-controller PiXtend CAN route and
a reusable Meerstetter utility library. It is not a final production closeout.

Current live route evidence is tracked in
[`mvp_completion_audit_2026-05-13.md`](mvp_completion_audit_2026-05-13.md).
The authoritative full check is:

```sh
PI_BASE_URL=http://192.168.6.229:18080 \
LOOM_BASE_URL=http://127.0.0.1:18087 \
./deploy/verify_mvp_completion.sh
```

The default gate is intentionally non-invasive. It proves the live PiXtend edge
route, Loom/operator gateway route, graph wall, discovery/source catalogue,
write guard, RAM CAN ring, flash fallback ring, merged deduplicated readout,
Arrow IPC export, and NDJSON export/import review without stopping services or
writing controller values.

## Canonical Runtime Surfaces

These are the surfaces that should be extended rather than bypassed.

- `cmd/meerstetterd`: standalone HTTP utility and browser UI entrypoint.
- `utility`: shared server wiring for health, discovery, graph wall, source
  catalogue, ring logs, target reads/writes, archive export, and operator-route
  aliases.
- `mecom`: MeCom framing, endpoint parsing/opening, typed reads/writes, bulk
  reads, and response parsing.
- `mecomdict`: seeded Meerstetter signal catalogue, readable targets, writable
  targets, units, groups, and instance metadata.
- `canopen`, `canopen/eds`, `objectdict`: CANopen/object-dictionary primitives
  and protocol-neutral catalogue model.
- `canadapter` and `socketcan`: adapter-neutral CAN frame contracts plus the
  proven Linux SocketCAN/PiXtend implementation.
- `canring`: bounded raw-CAN ring files used by the Pi RAM primary ring and Pi
  flash fallback ring.
- `mecomserver`: serialized downstream sharing for serial/TCP MeCom device
  access, including transparent passthrough use cases.
- `tmtc`, `control`, `sequencer`, `mecomautomation`, `export`: reusable
  telemetry, telecommand, write-lease, automation, sequencer, and export
  contracts.
- `deploy/systemd`: Pi service units and wrappers for the edge worker and CAN
  ring capture.
- `deploy/verify_*.sh`: live, bounded, operator-safe verification gates.
- `examples/`: topology examples for local FTDI serial and PiXtend SocketCAN
  deployments.
- `artifacts/`: tracked bring-up binaries for provenance and recovery.

## Live Data Path Contract

The UI and data backend should see every physical path through the same logical
target model:

- Primary path: PiXtend SocketCAN on the Raspberry Pi.
- Redundant path: FTDI/serial MeCom paths through the same device and target
  abstractions.
- Sharing path: TCP device-server style passthrough where a legacy or vendor
  tool needs access without blocking the utility permanently.

Each signal must keep parsable provenance:

- source device and alias or serial-derived name,
- transport path and active/redundant state,
- protocol and address,
- parameter identity,
- instance identity,
- read/write capability,
- freshness and last error state.

The signal tree should remain sorted by type, subtype/group, parameter, device,
and instance. Writable values are catalogue entries too: they expose their write
path and are initialized from current readback before a sequencer or UI write is
accepted.

## Ring Hierarchy

The three retention layers have different jobs and must not be treated as three
independent streams.

- Controller-internal Meerstetter ring/buffer: source of high-priority
  gap-fill when the transport and parameter mapping are proven.
- Pi RAM CAN ring: primary low-latency edge handoff and graph-wall replay path.
- Pi flash CAN ring: fallback/bootstrap retention for late owner connection,
  service restart, or intermittent power; it is not the normal live stream.

Consumers read RAM first, flash only as fallback or explicit inspection, and
merged readout deduplicates mirrored raw frames. Decoded values are keyed by
device, parameter, instance, and sample time or sequence when available so a
late owner can fill gaps without double-counting.

## Verification Gates

Safe default gates:

- `deploy/verify_pixtend_route.sh`: direct edge route.
- `deploy/verify_loom_gateway_route.sh`: Loom/operator gateway route.
- `deploy/verify_pixtend_owner_takeover.sh`: route-level owner idle and
  reattach proof.
- `deploy/verify_ui_browser_smoke.sh`: browser DOM/screenshot population check.
- `deploy/verify_mvp_completion.sh`: wrapper for the safe default set.

Bounded but disruptive gates:

- `RUN_RECOVERY=1 ./deploy/verify_mvp_completion.sh`: restarts edge services and
  verifies recovery. Use only when a short service interruption is acceptable.

Manual or future production gates:

- physical power interruption,
- real gateway/owner process-stop timing,
- live leased write acceptance against the intended controller values,
- CAN congestion and degraded polling behavior,
- controller-internal ring-buffer gap-fill under scripted faults,
- FTDI fallback arbitration and TCP device-server sharing under load,
- HDF5 archive implementation if production requires HDF5 rather than the
  current NDJSON and Arrow IPC export paths.

## Cleanup Rules

- Prefer extending existing packages and HTTP routes over adding parallel
  stacks.
- Keep hardcoded lab paths and addresses in examples, deployment config, or
  verification scripts, not library code.
- Keep raw CAN as evidence and fallback; decoded, typed targets are the primary
  UI/backend contract.
- Keep command/write paths lease-gated. A readable target becoming writable must
  add catalogue metadata, current-value initialization, validation, and a write
  guard.
- Keep PiXtend/SocketCAN proven paths first-class. Add Kvaser, USB-CAN, DIN-rail
  Ethernet, or other adapters behind `canadapter` only when their ownership and
  recovery semantics are verified.
- Update this map, the backlog, and the MVP audit when a new gate is proven or a
  gap is explicitly deferred.
