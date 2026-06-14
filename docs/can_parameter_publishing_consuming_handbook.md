# CAN Parameter Publishing and Consuming Handbook

This handbook explains how CANopen parameter publishing and consuming works in
this repository, with the Meerstetter RMM-to-TEC and TEC-to-TEC temperature
handover use cases in mind.

It is based on the public CANopen PDO model, the imported Meerstetter TEC v6.32
catalogue, the imported RMM-1182 EDS, repository tests, and a bounded live CAN
verification on 2026-06-14. The live verification used `can0` at 1 Mbit/s
through a Kvaser USB CAN adapter, three RMM nodes (`0x37`, `0x38`, `0x39`), and
four TEC nodes (`0x4B`, `0x4C`, `0x51`, `0x54`). All TEC outputs were verified
disabled before and after runtime tests. No flash save was performed.

No prior CAN or CANopen knowledge is assumed. If you already know CANopen, skip
to the Executive Summary or the Live Verification Matrix.

## CAN and CANopen in Five Minutes

**CAN bus.** All devices share one pair of wires. There is no central master and
no point-to-point addressing: every message is broadcast, and every device on
the bus sees every message. A message, or frame, has an 11-bit identifier
usually written in hex, such as `0x1B8`, and up to eight payload bytes. The
identifier does not say "from node X to node Y". Receivers decide which
identifiers they care about.

**CANopen.** CANopen gives those raw frames a device model. Its central idea is
the **object dictionary**: every device contains a table of values, each
addressed by a 16-bit index and an 8-bit subindex, written for example
`0x4200:01`. Meerstetter devices expose many of the same values over MeCom
parameter IDs and CANopen object addresses.

**SDO - the phone call.** A Service Data Object reads or writes one object on
one node. Use SDO for configuration and verification: read a mapping, write a
source selector, check a received value.

**PDO - the radio broadcast.** A Process Data Object moves live process values.
The producer sends a small payload under one CAN identifier. Any number of
consumers can listen to that same identifier and unpack the payload into their
own object dictionaries. From the producer side the frame is a **TPDO**
(Transmit PDO). From the consumer side the same frame is an **RPDO** (Receive
PDO).

**COB-ID.** The CAN identifier used by a PDO. Producer and consumer are
connected only if they use the same COB-ID and compatible payload mapping.

**PDO mapping.** The byte layout. A mapping entry says which object dictionary
cell occupies which payload bits. Example: `0x42000120` means object
`0x4200:01`, 32 bits. CANopen payloads used here are little-endian.

**NMT state.** CANopen Network Management controls whether a node is
operational, pre-operational, stopped, or resetting. Mapping changes are
normally applied in pre-operational state. PDO traffic requires operational
state.

For the main use case, an RMM publishes a temperature as a TPDO. One or more
TECs consume the same frame as an RPDO, store it in an external temperature
object, and can then explicitly select that external object as a local control
source.

```text
              SDO: configure and verify, one host-to-one node
   Host ------------------------------------------------------+
    |                    |                                   |
    v                    v                                   v
 +------+  TPDO      +-------+                           +-------+
 | RMM  | =========> | TEC A |                           | TEC B |
 +------+ COB-ID     +-------+                           +-------+
          0x1B8          ^                                   ^
                         |                                   |
                         +-------- same broadcast frame ------+
```

## Executive Summary

The mechanism is real and useful:

- An RMM, a TEC, or the host can produce a PDO.
- One PDO frame can be consumed by several TECs at the same time.
- One TEC can consume several producers at the same time if different RPDO
  records and different target objects are used.
- A TEC can route an imported external object or sink value into local measured
  temperature objects through source selector value `7`.
- If the external object stream stops, the TEC v6.32 external object cell was
  observed to become `NaN` after the missing-refresh timeout.
- Device-to-device flow does not need the host once the devices are configured,
  but the host is still needed for setup, audit, drift checks, and recovery.

The mechanism also has hard limits:

- One RPDO listens to one COB-ID at a time. Many-to-one requires several RPDOs,
  different target objects, or time-separated reconfiguration.
- A classic CANopen PDO carries at most eight bytes. Two `float32` values fit;
  three do not.
- Producer and consumer mapping must match exactly in type, length, byte order,
  and meaning.
- Source selection must be explicit. Receiving a value into `0x4200` or
  `0x4201` does not by itself make a TEC use it for control.
- Runtime SDO writes are not persistence. Saving to flash is a separate,
  intentional step and was not performed in this live verification.

## Connection Modes

| Connection | What works | What is not proven |
|---|---|---|
| No CAN adapter | Registry editing, validation, import/export, dry-run command plans, tests, and documentation. | No live node state, no PDO traffic, no proof that a device consumed a value. |
| Single direct USB/serial MeCom device | Read and write MeCom-visible values on that attached device, subject to the device's normal access rules. Useful for setup and diagnosis. | No peer-to-peer bus, no RMM-to-TEC PDO path, no multi-device proof, and no CANopen PDO mapping unless the attachment exposes a CANopen path too. |
| CAN adapter, read-only | Passive capture plus SDO read-back of identity, PDO mapping, source selectors, and received values. | No proof that a proposed new mapping can be written. |
| CAN adapter, write-enabled safe window | Apply runtime PDO mappings, source selectors, and event timers; verify value delivery; restore runtime state. | Persistence after power cycle unless flash save is intentionally executed and tested. |
| Already configured device bus | Device-to-device flow can continue without a host forwarding values. | The host is still needed to inspect, clone, document, and repair configuration. |

## TEC v6.32 Objects

The TEC v6.32 catalogue exposes the key objects needed for direct
temperature handover:

| MeCom ID | CANopen object | Type | Role | Meaning |
|---:|---|---|---|---|
| 52200 | `0x4200:<instance>` | `float32` | RPDO target | External object temperature |
| 52201 | `0x4201:<instance>` | `float32` | RPDO target | Fixed sink temperature |
| 1000 | `0x2100:<instance>` | `float32` | TPDO source | Object temperature |
| 1001 | `0x2101:<instance>` | `float32` | TPDO source | Sink temperature |
| 6300 | `0x3300:<instance>` | `int32` | SDO config | Object temperature source selection |
| 6304 | `0x3304:<instance>` | `int32` | SDO config | Sink temperature source selection |
| 6410 | `0x2410:<instance>` | `uint32`/`int32` | SDO safety state | Output enable, where `0` was disabled on the tested devices |

The imported TEC metadata records source-selection value `7` as the external
path. For `0x4200`, the metadata also records the safety requirement: the
external object value must refresh every 100 ms or faster, and a missing refresh
longer than 5 s forces `NaN` and stops the controller. The live test confirmed
the stale external-object value becoming `NaN` after publication stopped.

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

The mapping count at subindex `0` is the activation gate. A latent value can
remain in mapping subindex `1` while count is `0`; that slot is inactive. The
live TEC test also showed that writing `0x00000000` into an inactive mapping
entry can be rejected with abort `0x06040041`. For restore checks, trust the
mapping count, COB-ID, event timer, source selector, and output-enable state,
not cosmetic clearing of every latent slot.

## RMM-1182 Objects

The imported RMM-1182 EDS identifies vendor `0x547`, product `0x49E`, with all
common CAN bitrates including 1000 kbit/s enabled. It exposes 16 RPDOs and 16
TPDOs.

Useful PDO-capable RMM objects for temperature handover are:

| CANopen object | Type | Instances | Meaning |
|---|---|---:|---|
| `0x4000:<channel>` | `float32` | 4 | Value Conversion: Result |
| `0x4001:<channel>` | `float32` | 4 | Value Conversion: Surveilled Result |
| `0x4011:<channel>` | `int32` | 4 | Value Conversion: Result Type |
| `0x3001:<channel>` | `float32` | 2 | High Resolution Measurement: Resistance |

For TEC temperature control, `0x4000:<channel>` is the primary candidate when
the RMM channel is configured to convert the measurement into a temperature.
`0x3001:<channel>` is resistance, not temperature.

## Safe Runtime Configuration Sequence

Use SDO writes for configuration and SDO reads for proof. For live equipment:

1. Read and record the existing communication objects, mapping objects, source
   selectors, event timers, and output-enable state.
2. Confirm the control output state is safe for the test.
3. Put the node into NMT pre-operational.
4. Disable the PDO by setting bit 31 in the COB-ID entry.
5. Write mapping subindex `0` to `0`.
6. Write mapping entries `1..n`.
7. Write communication parameters: COB-ID, transmission type, and event timer
   if needed.
8. Write mapping subindex `0` to the number of active mapped objects.
9. Re-enable the PDO by clearing bit 31, or intentionally leave bit 31 set if
   the goal is a disabled PDO record. In `canpdoctl`, `-map none` leaves the
   final PDO record disabled by default.
10. Return the node to NMT operational.
11. Verify COB-ID, mapping count, mapping entries, event timer, passive traffic,
   and target-object value.
12. Set `0x3300:<instance> = 7` or `0x3304:<instance> = 7` only after the value,
   cadence, and safe output state are proven.
13. Save to flash only after runtime behavior and rollback expectations are
   clear.

## Tooling

`cmd/teccanprobe` is the bounded inspection tool. It can passively capture
frames and actively read SDO objects:

```bash
go run ./cmd/teccanprobe -if can0 -listen 5s
go run ./cmd/teccanprobe -if can0 -active -nodes 0x51,0x54 \
  -sdo 0x1400:1:uint32:rpdo1-cobid,0x1600:0:byte:rpdo1-count,0x1600:1:uint32:rpdo1-map1,0x3300:1:int32:object-source,0x4200:1:float32:external-object,0x2410:1:uint32:output-enabled
```

`cmd/canpdoctl` is the guarded setup helper. It is dry-run by default; `-commit`
is required before it transmits anything.

The committed commands below are individual building blocks. After each runtime
PDO change, use the role-specific read-back examples and the restore section
before enabling a TEC source selector or leaving the bench unattended.

```bash
# Show the exact SDO/NMT plan without touching the bus.
go run ./cmd/canpdoctl pdo-apply -node 0x51 -dir rpdo -pdo 1 \
  -cob-id 0x1B8 -map 0x4200:1:32 -transmission 0xFE

# Apply the runtime mapping.
go run ./cmd/canpdoctl pdo-apply -if can0 -node 0x51 -dir rpdo -pdo 1 \
  -cob-id 0x1B8 -map 0x4200:1:32 -transmission 0xFE -commit

# Write a source selector after the imported value is proven.
go run ./cmd/canpdoctl sdo-write -if can0 -node 0x51 \
  -object 0x3300:1 -type int32 -value 7 -commit

# Publish a host-origin test PDO.
go run ./cmd/canpdoctl pdo-send -if can0 -cob-id 0x2A0 \
  -type float32 -value 26.125 -count 120 -period 50ms -commit

# Apply a TPDO with a 100 ms event timer.
go run ./cmd/canpdoctl pdo-apply -if can0 -node 0x54 -dir tpdo -pdo 1 \
  -cob-id 0x1D4 -map 0x2100:1:32 -transmission 0xFE -event-ms 100 -commit

# Clear the active mapping and disable the event timer again.
go run ./cmd/canpdoctl pdo-apply -if can0 -node 0x54 -dir tpdo -pdo 1 \
  -cob-id 0x1D4 -map none -transmission 0xFE -event-ms 0 -commit
```

For a non-empty mapping that should remain intentionally invalid, add
`-disabled` to `pdo-apply`. For `-map none`, `canpdoctl` already leaves bit 31
set in the final COB-ID write. Mapping entries with subindex `0` are rejected
by default because they are often count/control entries; use
`-allow-sub0-map` only for a confirmed scalar object.

## Live Verification Matrix

All rows below were runtime-only. Source selectors and PDO mappings were
restored, and TEC outputs were read back as disabled.

| Case | Runtime setup | Proof observed | Conclusion |
|---|---|---|---|
| RMM one-to-many | RMM node `0x38` TPDO1 `0x1B8`, TEC `0x51` and `0x54` RPDO1 both mapped to `0x4200:01`. | Both TEC external-object cells tracked the RMM value around `23.6 degC`. | One producer can feed multiple TEC consumers. |
| One imported object in local loop | TEC `0x51` `0x3300:01` set to `7` after `0x4200:01` was populated. | `0x2100:01` followed the external-object value while output remained `0`; selector restored to `6`. | Source selector `7` routes the imported external object into the local object-temperature path. |
| Sequential many-to-one | TEC `0x51` RPDO1 was retargeted from RMM `0x37` COB-ID `0x1B7` to RMM `0x39` COB-ID `0x1B9`. | `0x4200:01` followed the current selected producer; for node `0x39` it became the producer's `NaN`. | One RPDO follows one current COB-ID; retargeting works but is not simultaneous. |
| Simultaneous many-to-one | TEC `0x51` RPDO1 `0x1B7 -> 0x4200:01`; RPDO2 `0x1B8 -> 0x4201:01`; source selectors `0x3300:01` and `0x3304:01` set to `7`. | `0x2100:01` followed RMM `0x37`; `0x2101:01` followed RMM `0x38`; output remained `0`; selectors restored. | A TEC can consume several producers at once when each uses a separate RPDO and target object. |
| Host-only producer | Host sent COB-ID `0x2A0` with float32 `26.125` into TEC `0x51` RPDO1. | `0x4200:01` became `26.125`; because source selector was restored to `6`, `0x2100:01` stayed local. After publication stopped and more than 5 s elapsed, `0x4200:01` became `NaN`. | The consumer path can be proven without an RMM producer; stale timeout was observed. |
| Two values in one PDO | TEC `0x51` RPDO1 COB-ID `0x2A1`, mapping `0x4200:01` and `0x4201:01`; host sent raw payload for `27.25` and `28.5`. | SDO reads returned `27.25` and `28.5` in the two external cells. | One classic CAN PDO can carry two float32 temperature values. |
| One-to-many into active local loops | TEC `0x51` and `0x54` both consumed RMM `0x38` on `0x1B8 -> 0x4200:01`; both `0x3300:01` selectors set to `7`. | Both TEC object-temperature values followed the same RMM value while outputs remained disabled; selectors restored to `6`. | One producer can feed several local control-source bindings at the same time. |
| TEC-to-TEC | TEC `0x54` TPDO1 mapped `0x2100:01` to COB-ID `0x1D4`; TEC `0x51` RPDO1 consumed `0x1D4 -> 0x4200:01`; TPDO event timer set to `100 ms`. | Passive frame `1D4#...` was seen and `0x51 0x4200:01` matched `0x54 0x2100:01` around `25.8 degC`; event timer restored to `0`. | TEC measured values can be published and consumed by another TEC using standard PDO mapping. |

Restore read-back after the tests showed:

- `0x51` RPDO1/RPDO2 active mapping counts `0`, source selectors
  `0x3300:01 = 6` and `0x3304:01 = 6`, output-enable `0`.
- `0x54` RPDO1 active mapping count `0`, TPDO1 active mapping count `0`,
  TPDO1 event timer `0`, source selectors `0x3300:01 = 6` and
  `0x3304:01 = 7`, output-enable `0`.
- Other TEC nodes `0x4B` and `0x4C` had output-enable `0` and active PDO
  mapping counts `0`.

Latent mapping subindex values can remain after count is set to `0`; that is
expected on the observed TEC firmware and should not be treated as active
traffic by itself.

## Configuration Recipes

### RMM Channel Feeds One Or More TECs

Use this when an RMM is the temperature producer.

1. Confirm the RMM channel reports a converted temperature, not raw resistance.
2. Confirm the RMM TPDO publishes that source object, usually
   `0x4000:<channel>`.
3. Configure each TEC RPDO to the same RMM TPDO COB-ID.
4. Map each TEC target to `0x4200:<instance>` or `0x4201:<instance>`.
5. Read the RMM source value and the TEC external target values by SDO.
6. Capture the PDO frame and confirm cadence.
7. Only then write the TEC source selector to `7`.

Example for RMM node `0x38` TPDO1 on `0x1B8` feeding TEC node `0x51`:

```bash
go run ./cmd/canpdoctl pdo-apply -if can0 -node 0x51 -dir rpdo -pdo 1 \
  -cob-id 0x1B8 -map 0x4200:1:32 -transmission 0xFE -commit

go run ./cmd/teccanprobe -if can0 -active -nodes 0x38 \
  -sdo 0x4000:1:float32:rmm-value

go run ./cmd/teccanprobe -if can0 -active -nodes 0x51 \
  -sdo 0x4200:1:float32:tec-external,0x2410:1:uint32:output-enabled
```

### Host Publishes A Test Value

Use this to prove the TEC consumer side without relying on an RMM producer.

```bash
go run ./cmd/canpdoctl pdo-apply -if can0 -node 0x51 -dir rpdo -pdo 1 \
  -cob-id 0x2A0 -map 0x4200:1:32 -transmission 0xFE -commit

go run ./cmd/canpdoctl pdo-send -if can0 -cob-id 0x2A0 \
  -type float32 -value 26.125 -count 120 -period 50ms -commit

go run ./cmd/teccanprobe -if can0 -active -nodes 0x51 \
  -sdo 0x4200:1:float32:tec-external,0x2410:1:uint32:output-enabled
```

For two values in one frame:

```bash
go run ./cmd/canpdoctl pdo-apply -if can0 -node 0x51 -dir rpdo -pdo 1 \
  -cob-id 0x2A1 -map 0x4200:1:32,0x4201:1:32 -transmission 0xFE -commit

go run ./cmd/canpdoctl pdo-send -if can0 -cob-id 0x2A1 \
  -raw "00 00 da 41 00 00 e4 41" -count 20 -period 50ms -commit

go run ./cmd/teccanprobe -if can0 -active -nodes 0x51 \
  -sdo 0x4200:1:float32:tec-external-object,0x4201:1:float32:tec-external-sink,0x2410:1:uint32:output-enabled
```

### TEC Feeds Another TEC

Use this when one TEC's measured object temperature should become another TEC's
external object input.

```bash
# Producer TEC 0x54 publishes object temperature on TPDO1.
go run ./cmd/canpdoctl pdo-apply -if can0 -node 0x54 -dir tpdo -pdo 1 \
  -cob-id 0x1D4 -map 0x2100:1:32 -transmission 0xFE -event-ms 100 -commit

# Consumer TEC 0x51 receives it as external object temperature.
go run ./cmd/canpdoctl pdo-apply -if can0 -node 0x51 -dir rpdo -pdo 1 \
  -cob-id 0x1D4 -map 0x4200:1:32 -transmission 0xFE -commit

# Verify before source selection.
go run ./cmd/teccanprobe -if can0 -active -nodes 0x54 \
  -sdo 0x2100:1:float32:producer-object,0x1800:5:uint16:producer-event-ms

go run ./cmd/teccanprobe -if can0 -active -nodes 0x51 \
  -sdo 0x4200:1:float32:consumer-external,0x2410:1:uint32:output-enabled
```

The event timer matters for observable periodic TPDO traffic. With event timer
`0`, the TEC may still send on internal events, but a periodic handover should
not rely on that unless the vendor documents the trigger semantics.

### Restore Runtime State

Restore to a known non-consuming runtime state by setting active mapping counts
to zero, leaving the PDO invalid, and setting source selectors back to their
desired local/default values:

```bash
go run ./cmd/canpdoctl pdo-apply -if can0 -node 0x51 -dir rpdo -pdo 1 \
  -cob-id 0x251 -map none -transmission 0xFE -commit

go run ./cmd/canpdoctl sdo-write -if can0 -node 0x51 \
  -object 0x3300:1 -type int32 -value 6 -commit
```

If the producer TPDO event timer was changed, restore it explicitly:

```bash
go run ./cmd/canpdoctl pdo-apply -if can0 -node 0x54 -dir tpdo -pdo 1 \
  -cob-id 0x1D4 -map none -transmission 0xFE -event-ms 0 -commit
```

## CAN Signal Registry

The bus-level contract should live in a version-controlled CAN signal registry,
not only in device flash. The registry names nodes by role, records COB-IDs,
TPDO/RPDO mappings, source selectors, expected rate, and safety notes. It can
be loaded without CAN hardware, exported as a role-only pattern, imported for a
second bench, and compared to live SDO read-back when CAN is available.

The important separation is:

- registry-only mode is intent;
- live read-back mode is evidence;
- drift means the live device state disagrees with the registry;
- unknown means the node was not reachable or could not be checked.

The example registry lives at
[`reference/can_signal_registry.example.json`](reference/can_signal_registry.example.json).

## Remaining Limits And Vendor MVP Scope

The live tests prove that core CANopen TPDO/RPDO communication, TEC external
temperature consumption, source selector routing, host-origin test publishing,
and TEC-to-TEC handover can work with the current system. Those should no
longer be framed as requests for a large new firmware feature.

The useful MVP request to Meerstetter is narrower:

1. Publish official RMM-1182 and TEC v6.32 PDO setup recipes for the external
   object/sink temperature paths, including safe runtime sequence and flash-save
   sequence.
2. Confirm source-selection enum values and semantics, especially `7` for
   external object and fixed-sink external temperature.
3. Confirm periodic TPDO trigger behavior: event timer units, default behavior
   when event timer is `0`, synchronous options if any, and recommended update
   rates for TEC control.
4. Provide machine-readable EDS/catalogue metadata for PDO-capable objects,
   source-selector enums, safety timeouts, status/quality bits, and writability.
5. Document liveness and fault handling: stale RPDO timeout, heartbeat or
   equivalent producer supervision, EMCY/status objects, `NaN`, sensor fault,
   and warning-state semantics.
6. Document the single-device USB/serial setup path and explicitly state what
   can and cannot be proven without CAN.

`meerstetter-go` can cover the operator ergonomics: dry-run plans, guarded SDO
writes, host-origin test PDOs, registry/pattern import/export, live drift
reports, and single-device MeCom diagnostics. Meerstetter should own the
official device semantics, persistence rules, and safety recommendations.

## Failure Modes To Check First

- The node answers SDO but is not NMT operational, so no PDO is emitted.
- Producer TPDO mapping count is `0`.
- Consumer RPDO COB-ID does not match producer TPDO COB-ID.
- Two producers use the same COB-ID.
- Producer and consumer mapping lengths or types differ.
- The RMM publishes resistance, `NaN`, or a diagnostic status instead of a
  converted temperature.
- TEC source selector is not set to `7`, or it was set before value/cadence was
  proven.
- The producer refresh period violates the TEC external-temperature freshness
  requirement.
- A runtime mapping was tested but not saved, then lost on reboot.
- A mapping entry remains visible in subindex `1` while mapping count is `0`;
  that is latent, not active.

## Verification Commands

Repository verification:

```bash
go test ./...
git diff --check
```

Live read-only verification:

```bash
go run ./cmd/teccanprobe -if can0 -listen 5s
go run ./cmd/teccanprobe -if can0 -active -nodes 0x37,0x38,0x39,0x4B,0x4C,0x51,0x54 \
  -sdo 0x1000:0:uint32:device-type,0x1018:1:uint32:vendor,0x1018:2:uint32:product,0x1018:3:uint32:revision,0x1001:0:byte:error-register
```

Live write verification should be performed only in a safe window with output
state checked before and after every source-selection test.

## Source Traceability

- Official CANopen PDO background:
  <https://www.can-cia.org/can-knowledge/pdo-protocol>
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
- Local setup helper:
  `cmd/canpdoctl`
- Local inspection helper:
  `cmd/teccanprobe`
