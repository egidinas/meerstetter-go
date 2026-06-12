# CAN Parameter Publishing and Consuming Handbook

This handbook explains how CANopen parameter publishing and consuming works in
this repository, with the specific RMM-to-TEC and TEC-to-TEC temperature
handover use case in mind.

It is based on the public CANopen PDO model, the imported Meerstetter TEC v6.32
catalogue, the imported RMM-1182 EDS, and the repository tests that guard those
imports. It does not claim that any live device was reconfigured during this
write-up.

No prior CAN or CANopen knowledge is assumed. If you already know CANopen, skip
ahead to the Executive Summary.

## CAN and CANopen in Five Minutes

**CAN bus.** All devices share one pair of wires. There is no central master and
no point-to-point addressing: every message is broadcast, and every device on
the bus sees every message. A message (a *frame*) consists of an identifier
(11 bits, usually written in hex like `0x1A1`) and up to eight bytes of
payload. The identifier does not say *who sent it* or *who should receive it* —
it only says *what kind of message this is*. Receivers decide for themselves
which identifiers they care about. CANopen default identifiers often encode a
function code plus node ID by convention, but the raw CAN frame still only
carries the resulting identifier.

**CANopen.** A set of rules layered on top of CAN that gives those raw frames
meaning. Its central idea is the **object dictionary**: every device contains a
table of values, each addressed by a 16-bit *index* and an 8-bit *subindex*,
written `0x4200:01`. Think of it as the device's complete settings-and-status
spreadsheet: every temperature reading, setpoint, and configuration option has
a fixed cell address. Meerstetter devices expose the same values over their
proprietary MeCom protocol (by numeric parameter ID, e.g. `52200`) and over
CANopen (by index, e.g. `0x4200`) — two doors into the same room.

**SDO — the phone call.** A Service Data Object transfer reads or writes one
object dictionary entry on one specific device. You ask node 5 "what is in cell
`0x4200:01`?" and node 5 answers you, and only you. SDOs are slow but precise:
use them for configuration and for checking state.

**PDO — the radio broadcast.** A Process Data Object is a device repeatedly
broadcasting a small set of live values (e.g. a temperature, every 50 ms) with
no request and no reply. The producer packs values into a frame and sends it
under an agreed identifier; any number of consumers listen for that identifier
and unpack the bytes into their own object dictionary cells. From the
producer's side it is called a **TPDO** (Transmit PDO); from a consumer's side
the same frame is an **RPDO** (Receive PDO).

**COB-ID.** The CAN identifier a particular PDO is sent under. This is the
entire "addressing" of a PDO: producer and consumers simply agree on a number.
If the producer transmits under `0x1A1` and a consumer listens on `0x1A1`, they
are connected. If they disagree, nothing happens — silently.

**PDO mapping.** The agreement about what the payload bytes *mean*: "bytes 0–3
are a float32 going into cell `0x4200:01`". Mapping is itself stored in the
object dictionary, so you configure it with SDO writes.

**NMT state.** A simple per-device run state. In *operational*, the device
sends and acts on PDOs. In *pre-operational*, PDOs are off but SDOs still work —
this is the state in which mapping changes are normally allowed. A device that
answers SDOs but was never switched to operational will sit silently and
produce no PDOs; this is a classic gotcha.

Putting it together for this repository's use case: an RMM measures a
temperature and broadcasts it as a TPDO; one or two TECs are configured (via
SDO) to consume that broadcast as an RPDO and store it in their
external-temperature cell; finally each TEC is told (also via SDO) to use the
external value in its control loop instead of its own sensor.

```
                 SDO ("phone call"): configure + verify, one-to-one
   Host ────────────────────────────────────────────────┐
    │                    │                               │
    ▼                    ▼                               ▼
 ┌──────┐  TPDO       ┌───────┐                      ┌───────┐
 │ RMM  │ ═══════════▶│ TEC A │                      │ TEC B │
 │      │  broadcast  └───────┘                      └───────┘
 └──────┘  COB-ID 0x1A1   ▲                              ▲
              ║           RPDO listens on 0x1A1          ║
              ╚══════════════════════════════════════════╝
                    same frame, consumed by both TECs
```

## Executive Summary

CANopen has two different jobs here:

- Service Data Objects (SDO) read and write object dictionary entries. Use SDOs
  to inspect values, configure PDO mappings, set source-selection parameters,
  and verify the final device state.
- Process Data Objects (PDO) move live process values. A Transmit PDO (TPDO) is
  produced by one device or host. A Receive PDO (RPDO) is consumed by one or
  more devices that are configured to listen to the same CAN identifier and
  decode the same payload layout.

For the TEC v6.32 external-temperature feature, the important receive targets
are:

| MeCom ID | CANopen object | Type | Direction | Meaning |
|---:|---|---|---|---|
| 52200 | `0x4200:<instance>` | `float32` | RPDO receive | External Object Temperature |
| 52201 | `0x4201:<instance>` | `float32` | RPDO receive | Fixed Sink Temperature |
| 6300 | `0x3300:<instance>` | `int32` | SDO config | Object Temperature Source Selection |
| 6304 | `0x3304:<instance>` | `int32` | SDO config | Sink Temperature Source Selection |

The TEC catalogue states that source-selection value `7` selects the external
temperature path. For `52200`, the imported metadata also records the safety
requirement: the external object temperature must refresh every 100 ms or
faster, and a missing refresh longer than 5 s forces `NaN` and stops the
controller.

## General CANopen Model

CANopen PDO behavior is producer/consumer, not request/response. A producer
sends a CAN frame with the configured COB-ID. Every consumer that has an RPDO
configured for that COB-ID receives the same bytes. This is why one temperature
value can feed more than one TEC controller, provided every consumer uses the
same mapping and byte layout.

A PDO has two object-dictionary records:

| Function | RPDO range | TPDO range | What it controls |
|---|---|---|---|
| Communication parameters | `0x1400` to `0x15FF` | `0x1800` to `0x19FF` | COB-ID, validity, transmission type, timing |
| Mapping parameters | `0x1600` to `0x17FF` | `0x1A00` to `0x1BFF` | Which object indexes/subindexes are packed into the frame |

Each mapping entry is a 32-bit value:

```
(object_index << 16) | (subindex << 8) | bit_length
```

In other words, the eight hex digits of a mapping entry read left to right as
*index, subindex, length in bits*. Worked example: `0x42000120` splits into
`4200` (object index `0x4200`, external object temperature), `01` (subindex 1,
i.e. instance 1), and `20` (hex for 32 — a 32-bit value). So this single number
says "four bytes of this PDO's payload belong to `0x4200:01`".

Examples:

| Object | Mapping entry | Meaning |
|---|---:|---|
| `0x4200:01` | `0x42000120` | TEC external object temperature instance 1, 32-bit float |
| `0x4201:01` | `0x42010120` | TEC fixed sink temperature instance 1, 32-bit float |
| `0x2100:01` | `0x21000120` | TEC object temperature instance 1, 32-bit float |
| `0x4000:01` | `0x40000120` | RMM value conversion result channel 1, 32-bit float |

CANopen numeric payloads are little-endian in this codebase. A single 32-bit
float consumes four PDO payload bytes. A classic CANopen PDO carries up to
eight payload bytes, so the TEC v6.32 two-slot PDO model can carry two
`float32` values per PDO.

## Safe Configuration Sequence

Use SDO writes for configuration. For live equipment, first back up the existing
communication and mapping objects, ensure the output/control state is safe, then
write the new mapping. The standard variable-PDO sequence is:

1. Put the node in a state where mapping changes are allowed. For variable PDO
   mapping this is normally NMT pre-operational.
2. Disable the target PDO by setting bit 31 in the COB-ID entry. (The COB-ID
   is stored as a 32-bit value; its top bit is not part of the identifier but a
   "this PDO is invalid" flag, so setting it switches the PDO off without
   losing the configured identifier.)
3. Write mapping subindex `0` to `0`.
4. Write mapping entry subindexes `1..n`.
5. Write mapping subindex `0` to the number of mapped objects.
6. Configure the communication parameters: COB-ID, transmission type, event
   timer or inhibit time if used.
7. Re-enable the PDO by clearing bit 31 in the COB-ID entry.
8. Return the producer/consumer nodes to NMT operational.
9. Verify the communication COB-ID, mapping entries, received object value, and
   passive CAN traffic before relying on the value.
10. Save parameters only after the runtime behavior is proven.

Do not skip the backup step. The RMM and TEC devices can both expose many
configurable PDOs, and losing the previous mapping can make later diagnosis
harder than the original setup.

## TEC v6.32 Specific Model

The imported TEC v6.32 files add the capability needed for direct temperature
handover:

- Release note feature: "External Object and Sink Temperature Values can be used
  as CANopen RPDO."
- `52200` / `0x4200` is `float32`, read-write, four instances, RPDO-capable,
  flash-save capable.
- `52201` / `0x4201` is `float32`, read-write, four instances, RPDO-capable,
  flash-save capable.
- `1000` / `0x2100` is `float32`, read-only, four instances, TPDO-capable.
- `1001` / `0x2101` is `float32`, read-only, four instances, TPDO-capable.
- `6300` / `0x3300` selects the object temperature source. Value `7` selects
  the external object temperature path.
- `6304` / `0x3304` selects the sink temperature source. Value `7` selects the
  fixed sink external-temperature path.

TEC v6.32 exposes four RPDOs and four TPDOs:

| PDO | Communication object | Mapping object | Default COB-ID | Slots |
|---|---|---|---|---:|
| RPDO1 | `0x1400` | `0x1600` | `$NODEID + 0x200` | 2 |
| RPDO2 | `0x1401` | `0x1601` | `$NODEID + 0x300` | 2 |
| RPDO3 | `0x1402` | `0x1602` | `$NODEID + 0x400` | 2 |
| RPDO4 | `0x1403` | `0x1603` | `$NODEID + 0x500` | 2 |
| TPDO1 | `0x1800` | `0x1A00` | `$NODEID + 0x180` | 2 |
| TPDO2 | `0x1801` | `0x1A01` | `$NODEID + 0x280` | 2 |
| TPDO3 | `0x1802` | `0x1A02` | `$NODEID + 0x380` | 2 |
| TPDO4 | `0x1803` | `0x1A03` | `$NODEID + 0x480` | 2 |

`$NODEID` is the device's CANopen node ID, so with node ID `0x4B`, TPDO1 is
sent under COB-ID `0x4B + 0x180 = 0x1CB` by default. These defaults exist so
that out of the box no two nodes collide; you are free to override them, which
is exactly what the handover patterns below do.

Transmission type defaults to `0xFE` in the imported metadata, meaning the
device decides when to send (typically event-driven or on its internal cycle)
rather than being polled by a sync message.

## RMM-1182 Specific Model

The imported RMM-1182 EDS identifies the device as vendor `0x547`, product
`0x49E`, with all common CAN bitrates including 1000 kbit/s enabled. It exposes
16 RPDOs and 16 TPDOs. The EDS includes 64 PDO communication/mapping objects.

Useful PDO-capable RMM objects for temperature handover are:

| CANopen object | Type | Instances/channels | Meaning |
|---|---|---:|---|
| `0x4000:<channel>` | `float32` | 4 | Value Conversion: Result |
| `0x4001:<channel>` | `float32` | 4 | Value Conversion: Surveilled Result |
| `0x4011:<channel>` | `int32` | 4 | Value Conversion: Result Type |
| `0x3001:<channel>` | `float32` | 2 | High Resolution Measurement: Resistance |

For RMM-to-TEC temperature control, `0x4000:<channel>` is the primary candidate
only when the RMM channel is already configured to convert the measurement into
a temperature value. `0x3001:<channel>` is resistance, not temperature, and
should not be used as a TEC temperature input unless an external conversion
stage is intentionally part of the design.

## Concrete Patterns

### Host Publishes One Arbitrary Value To Both TECs

Use this when proving the TEC consumer side without relying on an RMM producer.

1. Pick a COB-ID that does not collide with another live producer.
2. Confirm the TEC output/control state is safe before selecting an external
   temperature source.
3. Configure TEC A RPDO mapping to store the first four payload bytes into
   `0x4200:01` using mapping entry `0x42000120`.
4. Configure TEC B the same way, with the same RPDO COB-ID and same mapping.
5. Transmit periodic little-endian `float32` CAN payloads at that COB-ID at
   10 Hz or faster.
6. Verify that both TECs report the same external object temperature by SDO or
   MeCom reads.
7. Only after the value and cadence are proven, write `0x3300:01 = 7` on both
   TECs to bind the external value into the object-temperature control path.

This is valid because PDO consumption is broadcast. There is no "address" in
the PDO payload. The COB-ID and payload layout are the contract.

### RMM Channel Feeds One Or More TECs

Use this when the RMM is the temperature producer.

1. Confirm which RMM channel already reports the desired temperature value.
2. Configure an RMM TPDO to publish `0x4000:<channel>` with mapping entry
   `(0x4000 << 16) | (channel << 8) | 0x20`. For channel 1 this is
   `0x40000120`; for channel 2 this is `0x40000220`.
3. Configure each consuming TEC RPDO to the RMM TPDO COB-ID.
4. Map each TEC RPDO target to `0x4200:<instance>` or `0x4201:<instance>`.
5. Passively capture the RMM TPDO and verify the byte value against an SDO read
   of the RMM source object.
6. Verify each TEC external-temperature object by SDO/MeCom read.
7. Confirm the producer refresh cadence is 10 Hz or faster for object
   temperature use.
8. Only after the value and cadence are proven, and the TEC output/control state
   is safe, set the matching TEC source-selection parameter to `7`.

Two TECs can consume the same RMM TPDO. They should not require separate RMM
producers unless they need different payload layouts or independent timing.

### TEC Feeds Another TEC

Use this when one TEC's own measured value should become another TEC's external
input.

1. On the producer TEC, map `0x2100:<instance>` into a TPDO for object
   temperature, or `0x2101:<instance>` for sink temperature.
2. On the consumer TEC, set an RPDO COB-ID equal to the producer TEC TPDO
   COB-ID.
3. On the consumer TEC, map the RPDO payload into `0x4200:<instance>` or
   `0x4201:<instance>`.
4. Passively capture the producer TPDO and verify the consumer sees the
   producer value by SDO/MeCom read.
5. Confirm the producer refresh cadence is 10 Hz or faster for object
   temperature use.
6. Only after the value and cadence are proven, and the TEC output/control state
   is safe, set the consumer source-selection parameter to `7`.
7. Stop the producer in a safe test window and verify the missing-refresh safety
   behavior.

## Keeping Track of Mappings Across a Fleet

The mapping that makes all of this work lives in the flash of every device,
scattered across communication and mapping objects, and on the wire a COB-ID is
just a number with no name attached. A bench with one RMM and four TECs has
dozens of these settings. Six months later, nobody remembers why `0x1A1`
exists. Worse, when you build a second copy of a testbed, there is nothing to
copy *from* except the devices themselves.

This repository solves that with a **CAN signal registry**: a single
version-controlled file that is the source of truth for every PDO contract on a
bus, plus tooling that reads the live devices back and reports where they
disagree with the file. The `canmap` Go package owns the format and the checks;
the gateway serves it; the web UI shows it live.

### The registry, keyed by COB-ID

The registry has two lists. `nodes` binds a **role** (a stable symbolic name
like `rmm` or `tec-a`) to a concrete CANopen node ID. `signals` describes each
PDO contract, keyed by its COB-ID — because the COB-ID *is* the contract on the
bus. Each signal names one producer and any number of consumers in terms of
roles, so the wiring survives hardware swaps. A starter file lives at
[`reference/can_signal_registry.example.json`](reference/can_signal_registry.example.json).

The format mirrors the concepts from the primer exactly: a signal's `producer`
has a `tpdo` and a `mapping`; each `consumer` has an `rpdo`, a `mapping`, and
the `source_selects` (e.g. `0x3300:01 = 7`) that route the received value into
the control loop. The `canmap` package validates a registry on load and
rejects the mistakes from the Failure Modes list before they reach hardware:
duplicate COB-IDs, consumer slot lengths that do not mirror the producer,
payloads over 64 bits, node-ID collisions, and references to undefined roles.

### Live read-back and drift

Documentation that is never checked rots. Run the gateway with a registry and
it will, on request, read back every reachable node's live PDO configuration
over SDO — strictly read-only, the same bounded SDO reads `teccanprobe` uses —
and compare it to the registry. Every signal gets a verdict:

- **match**: the device's COB-ID, mapping, enable bit, and source selects equal
  the registry.
- **drift**: the device disagrees; the report says exactly which aspect, what
  was expected, and what was found.
- **unknown**: the node was offline or has no CANopen endpoint, so it could not
  be checked. Drift is never inferred from absence.

```bash
# Serve the registry and expose it at /api/canmap.
go run ./cmd/mecomgw -config gateway.json -canmap bench_a_signals.json

# Registry only:
curl localhost:8080/api/canmap
# Registry plus live device read-back and per-signal verdicts:
curl 'localhost:8080/api/canmap?live=1'
```

In the web UI, the **CAN signal map** view renders the same data: each signal
as a producer/consumer table, every row tagged in sync / drift / unknown, with
the offending aspect spelled out underneath any drifting row. This is the live
picture you wanted — the documented intent and the actual device state side by
side, refreshed on demand.

### Patterns: cloning a testbed

A concrete registry describes *one* bench. To stand up a copy you do not want to
hand-edit node IDs and hope. Export the registry as a **pattern** instead: the
same wiring with every node ID stripped, leaving only roles. On the copy,
import the pattern and supply fresh role-to-node bindings; the tooling
instantiates a concrete registry for that bench, re-validating COB-IDs and node
IDs in the process.

```bash
# On the reference bench: download the role-only pattern.
curl 'localhost:8080/api/canmap/export?format=pattern' -o testbed_pattern.json

# On a copy: instantiate it for this bench's node IDs.
curl -X POST localhost:8080/api/canmap/import \
  -H 'Content-Type: application/json' \
  -d '{
        "name": "bench-b",
        "pattern": '"$(cat testbed_pattern.json)"',
        "bindings": [
          {"role": "rmm",   "node_id": 32},
          {"role": "tec-a", "node_id": 81},
          {"role": "tec-b", "node_id": 82}
        ]
      }'
```

The same export/import buttons exist in the web UI's CAN signal map view.
Importing never writes to the devices — it only records the intended mapping.
Immediately after an import, `GET /api/canmap?live=1` shows every signal as
drift or unknown: that report *is* the to-do list for bringing the copied bench
into line, and it turns green signal by signal as you apply the safe
configuration sequence above. This keeps the human work — actually configuring
the devices — honest, while the cloning, validation, and tracking are
mechanical.

## Practical Verification

Offline verification performed by the repository:

- `mecom/catalogue_sources_test.go` verifies that TEC v6.32 exposes `52200` and
  `52201` as RPDO-capable external temperature targets, that `1000` and `1001`
  are TPDO-capable measured temperatures, and that the metadata carries four
  RPDOs, four TPDOs, source-selection value `7`, and the refresh policy.
- `mecom/canopen_sdo_map_test.go` verifies that the default CANopen SDO map
  resolves `52200`, `52201`, `6300`, and `6304` as direct TEC v6.32 CANopen
  objects with four instances and write support.
- `mecom/rmm_test.go` verifies the RMM family constants and catalogue entry.
- `docs/rmm_1182_reverse_engineering.md` records the RMM EDS findings and the
  known gap: default PDO layout and live remapping behavior still need hardware
  confirmation.

Read-only live verification path:

```bash
go run ./cmd/teccanprobe -if can0 -listen 5s
go run ./cmd/teccanprobe -if can0 -active -nodes 0x4b,0x4c -sdo 0x1800:1:uint32:tpdo1-cobid,0x1A00:0:byte:tpdo1-count,0x1A00:1:uint32:tpdo1-map1,0x1400:1:uint32:rpdo1-cobid,0x1600:0:byte:rpdo1-count,0x1600:1:uint32:rpdo1-map1,0x3300:1:int32:object-source,0x4200:1:float32:external-object
```

Use explicit `-sdo` reads to prove mapping state without changing the bus. Prior
read-only probing in this project showed why this matters: devices can be
reachable by node ID while having empty TPDO mappings, so reachability alone is
not proof that a producer is publishing the desired value.

For an RMM producer, include the producer node in the same read-only check and
read its source object too. For channel 1, probe `0x4000:1:float32:rmm-value`
and compare that SDO value against the captured TPDO payload before enabling
any TEC source-selection parameter.

Write verification path, only in a safe hardware window:

1. Record existing `0x140x`, `0x160x`, `0x180x`, `0x1A0x`, `0x3300`, and
   `0x3304` values for every involved node.
2. Confirm TEC control output state is safe for test values and stale-value
   timeout.
3. Configure one disposable RPDO on one TEC first.
4. Publish a known `float32` test value at the chosen COB-ID at 10 Hz or faster.
5. Read back `0x4200:01` / MeCom `52200` and confirm the expected value.
6. Stop publishing and confirm the documented missing-refresh behavior before
   enabling any control loop that depends on it.
7. Repeat for the second TEC as a multi-consumer test.
8. Only then configure the RMM or TEC producer TPDO and save persistent
   parameters.

## Failure Modes To Check First

- The node is reachable over SDO but not in NMT operational, so TPDOs are not
  being transmitted.
- The producer TPDO mapping count is zero.
- The consumer RPDO COB-ID does not match the producer TPDO COB-ID.
- Two producers use the same COB-ID.
- Producer and consumer mapping lengths differ.
- The RMM channel publishes resistance or status, not converted temperature.
- TEC source selection is not set to `7`, or it was set before the external
  value and refresh cadence were proven.
- The publish period violates the TEC external-temperature refresh requirement.
- The mapping was tested in RAM but not saved, or was saved before validation
  and overwrote a useful previous setup.

## Source Traceability

- Official CANopen PDO background: <https://www.can-cia.org/can-knowledge/pdo-protocol>
- TEC v6.32 parameter inventory:
  `mecom/catalogues/sources/tec_parameter_overview.v632.json`
- TEC v6.32 CANopen SDO map:
  `mecom/catalogues/sources/tec_canopen_sdo_map.v632.json`
- TEC v6.32 metadata and release-note extraction:
  `mecom/catalogues/sources/tec_metadata_index.v632.json`
- RMM-1182 EDS import:
  `mecom/catalogues/sources/rmm_1182_canopen_eds.v100.json`
- RMM reverse-engineering notes:
  `docs/rmm_1182_reverse_engineering.md`
