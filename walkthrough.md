# Meerstetter-Go Capability Walkthrough

This document is the public-safe capability map for `meerstetter-go`. It
describes what the repository can do today, which Meerstetter device families
are represented by definition data, and where the implementation deliberately
stops.

## Current Scope

`meerstetter-go` is a Go toolkit for Meerstetter MeCom devices and CANopen
adjacent tooling. It owns protocol-general code that can be reused outside a
private lab deployment:

| Area | Current capability |
|---|---|
| MeCom protocol | Frame construction and parsing, CRC, typed `float32`, `int32`, and latin1/string values, endpoint parsing, NACK decoding, single reads, bulk reads, writes, and bounded readback verification. |
| Transports | TCP and serial on Linux/Windows; Linux SocketCAN through repository adapters; adapter-neutral CAN contracts for other CAN libraries. |
| Multi-device routing | `cmd/mecomvseriald` exposes one address-routed TCP listener and fans MeCom frames out to serial, TCP, or CAN downstreams. |
| HTTP gateway | `cmd/mecomgw` exposes browser-safe JSON/SSE routes for device status, catalogue lookup, reads, polling, writes with leases, command logs, graph history, graph tiles, and log import/export. |
| Browser UI | `web/` provides a Vite/React UI for fleet overview, per-device drill-down, lease-managed writes, graph wall tiles, signal dictionary browsing, and help text. |
| Diagnostics | `cmd/mecomprobe`, `cmd/mecomset`, `cmd/mecompoll`, `cmd/mecomrun`, and `cmd/teccanprobe` cover bounded reads, writes, polling, typed automation scripts, and SocketCAN discovery. |
| Edge capture | `canring` stores bounded chunked CAN receive history and merges RAM/flash snapshots for recent-frame recovery. |
| SignalForge integration | Uses public SignalForge graph/source-catalogue/control-program contracts while keeping MeCom, CANopen SDO maps, transport probes, and device-specific recovery in this repository. |

## Device Family Model

The catalogue model is definition-based. Definitions carry stable metadata:

- `system`: currently `mecom`
- `family`: currently `meerstetter`
- `sub_family`: currently `tec`, `ldd`, or `daq`
- `variant`: for example `tec`, `ldd_130x`, or `ldd_1321`
- `version`: source-document or software-version tag where known

The current definition registry is intentionally family-aware:

| Definition | Status | Notes |
|---|---|---|
| `meerstetter.tec.v631` | Active runtime catalogue | TEC parameter catalogue, help text, metadata index, CANopen EDS extraction, and CANopen SDO bridge map are available. |
| `meerstetter.ldd_130x.v221` | Reverse-engineered catalogue source | LDD-130x software and documentation data are captured as JSON definitions and exported to the UI dictionary, but not promoted to routine write controls. |
| `meerstetter.ldd.v1` | Family scaffold | Base LDD definition for future LDD variants. |
| `meerstetter.ldd_1321.v1` | Variant scaffold | Placeholder definition metadata; no dedicated harvested catalogue is claimed yet. |
| `meerstetter.daq.v1` | Family scaffold | Definition slot for DAQ-like Meerstetter devices; no active DAQ parameter catalogue is claimed yet. |

The code path for this lives in `mecom/catalogue.go`. It normalizes definition
tokens, resolves family/subfamily variants, and stamps generated signal metadata
with `definition_ref`, `definition_system`, `definition_family`,
`definition_sub_family`, `definition_variant`, and `definition_version`.

## Catalogue Assets

The compiled browser-facing definitions are in `web/src/data/`:

| File | Role |
|---|---|
| `mecom-catalogue-definitions.json` | Lists available definition bundles. Currently includes TEC v6.31 and LDD-130x v2.21. |
| `mecom-catalogue.json` | Compiled TEC runtime/UI catalogue. Current file contains 584 rows. |
| `mecom-ldd-130x-catalogue.json` | Compiled LDD-130x catalogue export. Current file contains 277 rows. |
| `mecom-operator-projection.json` | TEC operator projection used by the UI. |
| `mecom-protocol-families.json` | Protocol family metadata for the UI. |

The harvested and reverse-engineered source data live in
`mecom/catalogues/sources/`:

| Source | Current content |
|---|---|
| `tec_tooltips.v631.json` | 123 TEC help/tooltip entries harvested from vendor software. |
| `tec_metadata_index.v631.json` | TEC hidden/manufacturer-gate metadata candidates and public-safe safety policy. |
| `tec_canopen_sdo_map.v631.json` | 32 TEC MeCom-to-CANopen SDO mappings, 4 bridge transforms, and documented unsupported paths. |
| `canopen_eds.v631.json` | 365 TEC CANopen EDS objects. |
| `ldd_130x_canopen_eds.v221.json` | 250 LDD-130x CANopen EDS objects. |
| `ldd_130x_metadata_index.v221.json` | LDD-130x definition metadata, 155 hidden candidates, 275 software labels, and 3 documentation cross-check groups. |
| `ldd_130x_ui_metadata.v221.json` | LDD-130x UI/resource metadata, 275 parameter contexts, 68 UI tree paths, and 791 harvested UI strings. |
| `ldd_130x_default_config_5261h.v221.json` | LDD-130x default-configuration source data. |

Harvest scripts preserve source encodings in metadata instead of relying on
unlabelled hardcoded assumptions. LDD-130x UI resources currently record
`resource_string_encoding: utf-16le`; default/config text records ASCII source
encoding where applicable.

## Runtime Semantics

The protocol clients expose a consistent read contract:

- single-value reads return typed values or an error;
- bulk reads are best-effort for per-parameter misses, timeouts, and unsupported
  values, filling those slots with `NaN`;
- transport, framing, context-cancellation, and unexpected protocol errors are
  returned as top-level errors instead of being hidden as `NaN`;
- writes are typed and can be followed by readback verification;
- readback mismatches wrap the exported `ErrReadbackMismatch` sentinel for
  `errors.Is` checks.

The gateway write path is lease-gated. UI/device writes require a lease token,
and command outcomes are logged for operator review.

## CANopen Bridge Status

CANopen support has runtime SDO map resolution. The SDO maps (e.g. TEC and LDD-130x) are loaded from JSON, validated on startup, and consulted dynamically by the CANopen client and the HTTP gateway to resolve parameter details and writability based on the target device's definition family. It distinguishes three cases:

- direct SDO-backed MeCom IDs;
- bridge transforms that intentionally synthesize MeCom-compatible behavior;
- unsupported or non-SDO paths, such as big-data metadata or ring-stream
  commands, that must not be treated as ordinary 32-bit SDO values.

The LDD-130x CANopen SDO bridge map is fully wired for dynamic selection and runtime lookup.

## UI Exposure

The browser UI can consume multiple catalogue definition bundles. TEC remains
the active operator/runtime catalogue. LDD-130x appears as a definition-backed
dictionary with source evidence, protocol aliases, UI tree paths, hover/help
text, default values where available, and safety notes.

LDD entries are intentionally marked as advanced metadata candidates unless a
separate safety review promotes a parameter into active polling or write
controls.

## What Is Not Claimed

- No hardened Internet authentication is provided by this repository.
- No Windows CAN adapter implementation ships here; Windows can use TCP/serial
  or an externally injected CAN adapter.
- No persistent database is included; graph/log state is process-local with
  import/export routes.
- No firmware update/download protocol is implemented.
- No active DAQ catalogue or live DAQ gateway behavior is claimed yet.
- LDD-130x data is reverse-engineered catalogue/UI/protocol metadata, not a
  blanket approval for unattended polling or writing.
- Runtime family selection is generalized for parameter writability and SDO map lookup paths (supporting dynamic resolution for TEC and LDD-130x).

## Where To Start

- Use `README.md` for the project overview and quick start.
- Use `docs/README.md` for the documentation index.
- Use `docs/gateway/openapi.yaml` for the authoritative HTTP route contract.
- Use `docs/gateway/readout_scheduling.md` before implementing polling loops.
- Use `docs/tmtc_signalforge_boundary.md` before moving generic dictionary,
  graph, or control-program behavior between repositories.
