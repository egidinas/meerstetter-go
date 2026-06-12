# CAN Parameter Publishing and Consuming Handbook

This handbook explains how CANopen parameter publishing and consuming works in
this repository, with the specific RMM-to-TEC and TEC-to-TEC temperature
handover use case in mind.

It is based on the public CANopen PDO model, the imported Meerstetter TEC v6.32
catalogue, the imported RMM-1182 EDS, and the repository tests that guard those
imports. It does not claim that any live device was reconfigured during this
write-up.

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
2. Disable the target PDO by setting bit 31 in the COB-ID entry.
3. Write mapping subindex `0` to `0`.
4. Write mapping entry subindexes `1..n`.
5. Write mapping subindex `0` to the number of mapped objects.
6. Configure the communication parameters: COB-ID, transmission type, event
   timer or inhibit time if used.
7. Re-enable the PDO by clearing bit 31 in the COB-ID entry.
8. Return the producer/consumer nodes to NMT operational.
9. Verify with SDO reads and passive CAN capture before relying on the value.
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

Transmission type defaults to `0xFE` in the imported metadata.

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
2. Configure TEC A RPDO mapping to store the first four payload bytes into
   `0x4200:01` using mapping entry `0x42000120`.
3. Configure TEC B the same way, with the same RPDO COB-ID and same mapping.
4. Write `0x3300:01 = 7` on both TECs.
5. Transmit periodic little-endian `float32` CAN payloads at that COB-ID.
6. Verify that both TECs report the same external object temperature by SDO or
   MeCom reads.

This is valid because PDO consumption is broadcast. There is no "address" in
the PDO payload. The COB-ID and payload layout are the contract.

### RMM Channel Feeds One Or More TECs

Use this when the RMM is the temperature producer.

1. Confirm which RMM channel already reports the desired temperature value.
2. Configure an RMM TPDO to publish `0x4000:<channel>` with mapping entry
   `0x4000SS20`, where `SS` is the subindex in two hex digits. For channel 1,
   this is `0x40000120`.
3. Configure each consuming TEC RPDO to the RMM TPDO COB-ID.
4. Map each TEC RPDO target to `0x4200:<instance>` or `0x4201:<instance>`.
5. Set the matching TEC source-selection parameter to `7`.
6. Passively capture the RMM TPDO and verify the byte value against an SDO read
   of the RMM source object.
7. Verify each TEC external-temperature object by SDO/MeCom read.

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
4. Set the consumer source-selection parameter to `7`.
5. Verify the consumer sees the producer value and trips the missing-refresh
   safety behavior if the producer stops.

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
go run ./cmd/teccanprobe -if can0 -active -nodes 0x4b,0x4c -sdo 0x1A00:0:byte:tpdo1-count,0x1A00:1:uint32:tpdo1-map1,0x1600:0:byte:rpdo1-count,0x1600:1:uint32:rpdo1-map1,0x3300:1:int32:object-source,0x4200:1:float32:external-object
```

Use explicit `-sdo` reads to prove mapping state without changing the bus. Prior
read-only probing in this project showed why this matters: devices can be
reachable by node ID while having empty TPDO mappings, so reachability alone is
not proof that a producer is publishing the desired value.

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
- TEC source selection is not set to `7`.
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
