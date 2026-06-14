# MVP Proposal: RMM/TEC Direct CAN Signal Setup With Meerstetter-Go

Date: 2026-06-14

This is no longer a request for a large new firmware feature. A bounded live
test on 2026-06-14 proved that the current devices can already use standard
CANopen Process Data Objects (PDOs) for runtime signal handover:

- an RMM, a TEC, or the host can publish a PDO;
- one PDO can be consumed by several TECs;
- one TEC can consume several producers through separate Receive PDO (RPDO)
  records and separate target objects;
- a TEC can route an imported external object into the local object-temperature
  path with source selector value `7`;
- a TEC can publish its own measured object temperature as a Transmit PDO
  (TPDO), and another TEC can consume it.

The remaining request to Meerstetter is a narrow documentation, metadata, and
persistence MVP: confirm the official setup sequence, flash-save behavior,
timing/liveness semantics, and machine-readable object metadata so
Meerstetter-Go can turn the proven runtime mechanism into a reproducible setup
workflow.

## Operating Modes

Meerstetter-Go should handle the same signal configuration in four modes:

| Mode | What Meerstetter-Go can do | What remains unproven |
|---|---|---|
| No CAN adapter | Edit a CAN signal registry, validate roles, COB-IDs, mapping lengths, and source selectors; export/import reusable patterns; show dry-run command plans. | Live node state, PDO traffic, and real device consumption. |
| Single direct USB/serial device | Read and write MeCom-visible values on one attached RMM or TEC, subject to normal access rules. This is useful for setup, diagnosis, and operator education. | Peer-to-peer bus behavior, multi-device routing, and PDO consumption by another device. |
| CAN adapter, read-only | Capture traffic and read SDO identity, mappings, source selectors, event timers, and received values. | Whether a new proposed mapping can be written successfully. |
| CAN adapter, write-enabled safe window | Apply runtime SDO/NMT configuration, verify PDO delivery, route imported values into TEC source selectors, and restore runtime state. | Persistence after power cycle unless an explicit flash-save sequence is executed and tested. |

This distinction matters. Without CAN, the configuration is validated intent.
With direct USB/serial, one device can be diagnosed and prepared. Only live CAN
proves the host-free device-to-device signal path.

## Verified Runtime Facts

The live bench used `can0` at `1 Mbit/s` through a Kvaser USB CAN adapter:

| Role | Nodes |
|---|---|
| RMM | `0x37`, `0x38`, `0x39` |
| TEC | `0x4B`, `0x4C`, `0x51`, `0x54` |

All TEC outputs were verified disabled before and after the runtime tests. No
flash save was performed.

Verified runtime cases:

1. RMM `0x38` TPDO on COB-ID `0x1B8` was consumed by TEC `0x51` and TEC `0x54`
   at the same time into external object temperature `0x4200:01`.
2. TEC `0x51` used source selector `0x3300:01 = 7` to route `0x4200:01` into
   object temperature `0x2100:01`, with output still disabled.
3. TEC `0x51` could consume different producers sequentially by retargeting one
   RPDO to different COB-IDs.
4. TEC `0x51` could consume two producers simultaneously by using RPDO1 and
   RPDO2 with different target objects (`0x4200:01` and `0x4201:01`).
5. A host-origin test PDO could populate `0x4200:01`; after publication stopped,
   the value became `NaN` after the observed missing-refresh timeout.
6. One classic CAN PDO carried two `float32` values into `0x4200:01` and
   `0x4201:01`.
7. One RMM producer fed multiple TEC local control-source bindings at once.
8. TEC `0x54` published `0x2100:01` on TPDO1; TEC `0x51` consumed that value
   into `0x4200:01`.

The CAN Parameter Publishing and Consuming Handbook records the exact object
paths, command sequence, restore state, and limits:
`docs/can_parameter_publishing_consuming_handbook.md`.

## What Is No Longer A Vendor Feature Request

These items should not be presented as open firmware requirements anymore:

- basic CANopen SDO read/write access to PDO communication and mapping objects;
- runtime TPDO/RPDO configuration;
- TEC consumption of an external object-temperature value through RPDO;
- TEC source-selector value `7` routing an imported external object into the
  local object-temperature path;
- one-to-many PDO fan-out;
- many-to-one consumption through multiple RPDOs;
- host-origin test publication for bench verification;
- TEC-to-TEC runtime handover.

They are real runtime capabilities. What is still missing is the official,
reproducible, persistent, and safety-documented setup contract.

## Remaining Vendor MVP Ask

The useful Meerstetter MVP is deliberately small:

1. **Official setup recipes.** Publish RMM-1182-to-TEC and TEC-to-TEC PDO setup
   recipes, including the safe SDO/NMT order, valid object choices, restore
   sequence, and error handling.
2. **Persistence rules.** Document the exact flash-save path for PDO mapping,
   COB-ID, event timer, source selector, heartbeat/liveness settings, and any
   required preconditions. Include erase/write limits and rollback advice.
3. **Source-selector semantics.** Confirm the enum values and meanings for TEC
   object and sink temperature source selection, especially `7` for external
   object/fixed-sink paths.
4. **Timing and trigger semantics.** Confirm TPDO event timer units, default
   behavior when the timer is `0`, synchronous options if supported, and the
   recommended update rates for TEC control.
5. **Liveness and fault semantics.** Document stale RPDO timeout, heartbeat or
   equivalent producer supervision, Emergency Message (EMCY) behavior, status
   objects, `NaN`, sensor fault, warning state, and safe output reaction.
6. **Machine-readable metadata.** Provide EDS/catalogue metadata for
   PDO-capable objects, source-selector enums, safety timeouts, writability,
   units, value quality, and persistence support.
7. **Single-device guidance.** State clearly what can be prepared or diagnosed
   through a single USB/serial attachment, and what requires live CAN.

## Meerstetter-Go Scope

Meerstetter-Go can own the operator ergonomics around the vendor contract:

- offline CAN signal registry and pattern import/export;
- guarded dry-run plans for SDO/NMT write sequences;
- explicit `-commit` gates for live bus writes;
- host-origin test PDO publication for consumer-side proof;
- live read-back and drift reports;
- direct USB/serial single-device diagnostics;
- documentation that separates intent, live evidence, drift, and unknown state.

The new `cmd/canpdoctl` helper is intentionally conservative: it is dry-run by
default, it uses Network Management (NMT) pre-operational/operational bracketing
for PDO mapping changes, it keeps `-map none` disabled, and it requires explicit
operator intent before transmitting.

## Acceptance Tests For The Minimal Version

1. Without CAN, a registry or pattern can be loaded, validated, exported, and
   reported as "not live verified".
2. With one directly attached USB/serial device, Meerstetter-Go can read
   diagnostic values and apply only documented safe writes.
3. With live CAN, all expected RMM and TEC nodes answer identity SDO reads and
   have unique node IDs.
4. A producer publishes a documented, typed signal as a TPDO at the expected
   rate.
5. One or more TEC consumers import that TPDO into documented external objects.
6. A TEC control source uses the imported value only after explicit source
   selection and freshness proof.
7. Stopping the producer invalidates the imported value within the documented
   timeout and drives the documented safe reaction.
8. After an intentional flash-save and power-cycle test, the device-to-device
   flow resumes without host polling.
9. Meerstetter-Go can compare the live state against the registry and report
   match, drift, or unknown per signal.

## Non-Goals For The First MVP

- A full new graphical CoSo workflow for shared signals.
- A general cross-device signal framework covering every future device family.
- Host-bound USB/serial control-value routing as the primary control path.
- New UART, SPI, or I2C local sensor firmware.
- Automatic fallback from an invalid imported control source to another sensor
  without an explicitly documented safe transfer strategy.

## Local Evidence

- `docs/can_parameter_publishing_consuming_handbook.md`
- `docs/reference/can_signal_registry.example.json`
- `docs/rmm_1182_reverse_engineering.md`
- `mecom/catalogues/sources/rmm_1182_canopen_eds.v100.json`
- `mecom/catalogues/sources/tec_canopen_sdo_map.v632.json`
- `mecom/catalogues/sources/tec_metadata_index.v632.json`
- `cmd/canpdoctl`
- `cmd/teccanprobe`
