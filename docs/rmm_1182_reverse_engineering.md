# RMM-1182 Reverse-Engineering Notes

Status: first source-grounded import from `docs/OneDrive_1_03-06-2026.zip`.
This note records what the uploaded RMM software/documentation proves today and
keeps runtime claims separate from catalogue evidence.

## Imported Sources

| Source | Evidence captured |
|---|---|
| `CanOpen.eds` | CANopen object dictionary for `RMM-1182`, product number `0x49E`, vendor number `0x547`, EDS revision modified `06.05.2026`. |
| `Doku/RMM-1182 Preliminary.docx` | User manual for RMM-1182 firmware `v1.00`, release date `2 June 2026`; confirms temperature/value-conversion/logging feature families and references a separate communications protocol document. |
| `Doku/RMM-1182 Connectors.docx` | Connector and jumper pinout; confirms X1/X2 carry supply plus RS485/CAN, with CANH on pin 3 and CANL on pin 4. |
| `Software/appsettings.json` | .NET trace configuration for the RMM configuration software; no protocol map beyond MeCom tracing namespaces. |
| `Software/RMM-1182-Configuration-Software.exe` | Embedded assembly and UI-resource evidence; confirms .NET 8 UI, `MeSoft.CoSoG2.RMM1182`, the bundled MeSoft MeCom command set, export/import XML config, CANopen node/bit-rate UI, and named RMM windows/parameters. |

The source ZIP also says the dedicated `RMM-1182 Communications Protocol`
document exists, but it is not included. The CANopen EDS remains the
authoritative machine-readable RMM CANopen source in this repo; the live USB
evidence below is narrower and only proves the listed serial MeCom reads.

## CANopen Identity

The RMM-1182 EDS was created with port GmbH CANopen tooling and describes an
RMM-1182 slave using port's CANopen library.

| Field | Value |
|---|---|
| Vendor | Meerstetter Engineering GmbH |
| Vendor number | `0x547` |
| Product | `RMM-1182` |
| Product number | `0x49E` |
| Order code | `RMM-1182` |
| Supported bit rates | 10, 20, 50, 125, 250, 500, 800, 1000 kbit/s |
| LSS | Not supported |
| Boot role | Simple boot-up slave |
| PDO capacity | 16 RPDO and 16 TPDO |

The normalized catalogue seed lives at
`mecom/catalogues/sources/rmm_1182_canopen_eds.v100.json`.

## Controller Software MeCom Command Inventory

The Windows controller software bundles MeSoft MeCom libraries inside the
single-file executable. Extracting the embedded `.NET` assemblies and inspecting
`MeSoft.MeCom.Core` method constants proves the following MeCom command tokens:

| Token | Methods | Meaning |
|---|---|---|
| `RS` | `ResetDevice` | Reset target device. |
| `SP` | `TriggerParameterSaveToFlash` | Save parameters to flash. |
| `SA` | `SetDeviceAddress` | Program a MeCom device address; exact field order still needs deeper IL or capture validation. |
| `?BI` | `GetBranchId`, `GetBranchIdSeSo` | Read branch ID. |
| `?VI` | `GetFirmwareVersionInfo` | Read firmware/version information. |
| `?VR` | `GetINT32Value`, `GetFloatValue`, `GetDoubleValue`, `GetINT64Value`, `GetValue` | Read one parameter value by parameter ID and instance. |
| `VS` | `SetINT32Value`, `SetINT64Value`, `SetFloatValue`, `SetDoubleValue`, `SetValue` | Write one parameter value by parameter ID and instance. The generic path references `AddFloat32`, `AddInt32`, `AddDouble64` and `AddInt64` encoders. |
| `?VL` | `GetLimits` | Read parameter limits. |
| `?VM` | `GetMetaData` | Read parameter metadata. |
| `?VX` | `BulkParReadCom` | Bulk-read multiple parameter ID/instance tuples. |
| `?MB` | `DownloadMeBlob` | Download or poll MeBlob transfer data; the method contains two `?MB` uses, a 16-character target length, a 30000 ms busy timeout and analyzing/success/error statuses. |
| `?BC` | `SendCommand` | Generic bootloader/custom command forwarding; exact RMM use is not proven. |

No additional RMM-specific wire command token was found in the controller
application during this pass. The application appears to use the standard
MeSoft MeCom command set through `MeSoft.MeCom.Core`, `MeSoft.MeCom.GeneralApi`,
`MeSoft.MeCom.PhyWrapper` and `MeSoft.CoSoG2.SeSoTCP`.

The bundled SeSoTCP layer exposes wrappers named `Get Parameter`, `Get
Parameter by ID`, `Set Parameter`, `Set Parameter by ID`, `Process Custom
Command`, `Reset Device`, flash readiness polling and a `CRTVStream` helper.
Those are wrapper/API labels, not separate RMM wire tokens.

The structured evidence file is
`mecom/catalogues/sources/rmm_1182_controller_software_mecom.v101.json`.
It keeps proven command tokens separate from WPF/BAML UI labels. The UI
resources confirm RMM parameter families for system telemetry, communication,
board heater, high- and low-resolution measurement, value conversion, IO, fan,
advanced settings and ME settings, but the numeric `MeParID` bindings were not
decoded from BAML in this pass.

## Live USB MeCom Probe

On 2026-06-04, one RMM-1182 connected by USB to the Odyssey computer
`192.168.8.140` was contacted through `COM5` at 57600 baud and MeCom address
`0`. Identity reads returned:

| Command | Value |
|---|---|
| `?IF` | `8170-RMM-1182 SW G01` |
| `?VI` | `00640004` |
| `?BI` | `00000000` |

The physical HR1 input had a Pt100 sensor connected. Read-only `?VM` and `?VR`
probes showed that the RMM USB MeCom path accepts the decimal EDS object labels
as MeParID values for the listed reads. For example, EDS object `0x3001` is
used as MeParID `3001`, which the MeCom wire frame encodes as hexadecimal
field `0BB9`; interpreting the raw object number as decimal `12289` returned
`PAR_NOT_AVAILABLE`.

| MeParID | Instance | Name | Type | Live sample |
|---:|---:|---|---|---:|
| `3000` | 1 | HR1 raw ADC | float32 | `750793.0` |
| `3001` | 1 | HR1 resistance | float32 | `109.340462` |
| `3002` | 1 | HR1 voltage | float32 | `NaN` |
| `3100` | 1 | HR1 measurement type | int32 | `0` |
| `3101` | 1 | HR1 ADC Rs | float32 | `39000` |
| `4000` | 1 | VC1 result | float32 | `23.808823` |
| `4001` | 1 | VC1 surveilled result | float32 | `0` |
| `4011` | 1 | VC1 result type | int32 | `0` |
| `4012` | 1 | VC1 conversion type | int32 | `2` |

This evidence is captured in
`mecom/catalogues/sources/rmm_1182_live_usb_mecom.v100.json`, and the helper
`mecom.DefaultRMM1182HR1Pt100Parameters()` exposes the same read-only parameter
set. The live resistance and conversion result are consistent with a Pt100 near
room temperature, but the enum meaning of conversion type value `2` is not
asserted until the missing communications protocol or software config capture
proves it.

## Object Dictionary Shape

The imported EDS contains:

| Count | Meaning |
|---:|---|
| 154 | Total CANopen objects |
| 80 | Manufacturer-specific objects (`0x2000` and above) |
| 521 | Total subentries |

RMM manufacturer objects are mostly array/record style objects with instance
subindices. The common pattern is:

| Shape | Meaning |
|---|---|
| `sub0` | Highest supported subindex, data type `0x0005`, access `const` |
| `sub1..n` | Channel or instance values |
| `0x0008` | Float32 values, used heavily for measurement and conversion values |
| `0x0004` | Signed integer/configuration values |
| `PDOMapping=1` | Most values can be mapped to PDOs, including many writable configuration entries marked `rww` |

Observed manufacturer subentry access distribution:

| Access | Count | Interpretation |
|---|---:|---|
| `rww` | 86 | Readable/writable configuration entries, likely persistent or write-through depending on firmware semantics. |
| `ro` | 47 | Read-only telemetry/status values. |
| `rw` | 44 | Read/write entries. |
| `rwr` | 4 | Read/write result-like entries; keep as EDS access until live behavior is proven. |

## Manufacturer Object Groups

| Range | Group | Notes |
|---|---|---|
| `0x2160..0x2183` | System telemetry | Driver input voltage, internal supplies, device/junction temperature, operating time and total output counters. |
| `0x2A00..0x2A10` | Board heater | Enable, target temperature, max power, heater voltage, stability indicator and switching frequency. |
| `0x3000..0x3355` | High-resolution measurement | Raw ADC, resistance, voltage, measurement configuration, calibration, surveillance, monitor limits and ADC self-check results. |
| `0x3500..0x3743` | Low-resolution measurement | Raw ADC, resistance, voltage, measurement configuration, calibration and monitor limits. |
| `0x4000..0x4045` | Value conversion | Converted result, flags, result/conversion type, NTC characteristic points, beta model, thermocouple compensation and ADC range conversion endpoints. |
| `0x4300` | Feature key status | Read-only integer feature/license state. |

Channel counts from the EDS:

| Example object | Subindices | Meaning |
|---|---:|---|
| `0x3000` high-resolution raw ADC | 2 | HR measurement channels 1 and 2. |
| `0x3500` low-resolution raw ADC | 2 | LR measurement channels 1 and 2. |
| `0x4000` value-conversion result | 4 | Four converted result channels. |
| `0x2160` driver input voltage | 1 | Device-level telemetry instance. |

## Hardware and Connector Facts

From `RMM-1182 Connectors.docx`:

| Connector | Pins | Function |
|---|---|---|
| X1/X2 | Pin 1 `Vin`, pin 2 `GND`, pin 3 `RS485 A / CANH`, pin 4 `RS485 B / CANL` | Supply and communications; X1 and X2 are connected in parallel. |
| X5 | HR measurement inputs | Two high-resolution measurement channels with IA/IB/UA/UB pins. |
| X6 | LR measurement inputs | Two low-resolution measurement channels. |
| X7 | Power/GPIO | 5 V, 3.3 V, GND, GPIO1-9 with UART/I2C/SPI/ADC alternate functions. |
| X3 | Jumper | Supply/GND selection; hardware 1.10 and newer ties X1/X2 GND through a 0 ohm resistor unless R2 is removed. |

The connector document names GPIO1 through GPIO9. The preliminary manual table
of contents refers to `GPIO1 - GPIO10 Control Signals`, so GPIO10 still needs
confirmation from the communications protocol document, the full datasheet, or
hardware inspection.

## Configuration Software Clues

The Windows configuration software string table confirms UI support for:

- CANopen node ID and bit-rate settings.
- CAN1 enable/disable and CAN1 auto-operational.
- Non-volatile CANopen configuration for SYNC COB-ID, emergency inhibit time,
  producer heartbeat, and PDO communication/mapping config.
- MeCom device addressing, including broadcast address `0` and silent broadcast
  `255`.
- XML configuration export/import.
- RMM windows for system, communication, high/low-resolution measurement,
  value conversion, board heater, IO, fan, graph/log and settings.

These are UI strings, not a complete protocol map. Use them to name hypotheses,
not to assert runtime behavior that the EDS or live captures do not prove.

## Integration Implications

- Add RMM as a catalogue family: `meerstetter.rmm_1182.v100`.
- For USB serial MeCom on this RMM-1182, HR1/VC1 read-only values can be probed
  with decimal EDS labels as MeParID values using the `rmm-1182-hr1-pt100`
  preset.
- CANopen discovery can identify RMM-1182 via product number `0x49E`.
- CANopen SDO reads/writes can be generated from the EDS object dictionary, but
  the USB MeCom read evidence does not prove CANopen SDO routing or write
  behavior.
- Treat `rww`, `rw`, and `rwr` exactly as EDS access classes until live writes
  prove persistence, side effects and save/restore semantics.
- PDO support is broad enough to support a telemetry wall later, but the default
  PDO layout and desired remapping sequence still need live confirmation.

## Live Hardware Confirmation (2026-06-13)

First read-only CANopen SDO/PDO session against the real bench over a Kvaser
USBcan Light on SocketCAN `can0` at 1 Mbit/s. The bench carries seven nodes:
**three RMM-1182** at node IDs `0x37`, `0x38`, `0x39` (product `0x49E`) and
**four TEC-1166** at `0x4B`, `0x4C`, `0x51`, `0x54` (product `0x441`,
firmware revision `0x00060020` = v6.32). All probes were bounded read-only SDO
uploads plus passive capture; nothing on the bus was reconfigured.

Confirmed against RMM node `0x37`:

| Object | Read | Meaning |
|---|---|---|
| `0x1018:01` vendor | `0x547` | Matches the EDS import. |
| `0x1018:02` product | `0x49E` | Matches the EDS import. |
| `0x1018:03` revision | `0x00010000` | Firmware v1.00. |
| `0x1001` error register | `0` | No active error. |
| `0x1800:01` TPDO1 COB-ID | `0x1B7` | Default `0x180 + node`, valid/enabled. |
| `0x1A00:00` TPDO1 count | `1` | One object mapped. |
| `0x1A00:01` TPDO1 map | `0x40000120` | **`0x4000:01` float32 — Value Conversion Result, channel 1.** |
| `0x1801:01` TPDO2 COB-ID | `0x2B7` | Default `0x280 + node`. |
| `0x1A01:00` TPDO2 count | `0` | TPDO2 mapped to nothing (live but empty). |
| `0x4000:00` channel count | `4` | Four value-conversion result channels, matching the EDS. |

This closes the open default-PDO question for the observed RMM node `0x37`
running firmware v1.00: in its observed default state, that node publishes
`0x4000:01` (channel-1 value conversion result) as a single float32 on TPDO1 at
COB-ID `0x180 + node`. SN7/SN8 still need their producer mapping enabled or
confirmed before the same claim can be made for those units. The read-back also
validates the `canmap` package decode path — its `ObserveNode` returns the same
COB-ID, enable bit, and mapping entry as the `teccanprobe` capture.

Data-quality caveat observed live (see the feedback note below): on this
unconfigured bench, RMM `0x4000:01` published ~`9.88e-08` (channel 1, sensor
noise around zero) while `0x4000:02` read `NaN`, `0x4001:01` (surveilled
result) read `0`, and `0x3001:01` (resistance) read `NaN`. So the TPDO happily
broadcasts a meaningless value with no in-band validity flag. The matching
TEC-1166 consumers currently have empty RPDO1 mappings (`0x1600:00` count `0`)
and object-temperature source selection `0x3300:01 = 0`, i.e. the handover is
not yet wired — a clean "nothing configured" baseline.

Constructive RMM CAN-model feedback distilled from this session lives in
[`rmm_can_model_feedback.md`](rmm_can_model_feedback.md).

Standard CANopen objects that **aborted** (worth noting for tooling): `0x1000`
device type read `0x00000000` (no CiA profile advertised); `0x1008` manufacturer
device name aborted `0x06020000` (object does not exist). The TEC-1166 also
reports `0x1000 = 0`.

### Confirmed CANopen write and flash persistence (all three RMM)

Building on the read-only session, the producer mapping was configured over
CANopen on every RMM and persisted, which **closes the open "live writes prove
persistence" task** for the variable-PDO path. On each node the standard safe
variable-PDO sequence was applied (NMT pre-operational → disable TPDO via the
COB-ID invalid bit → set mapping count 0 → write mapping entry `0x40000120` →
set count 1 → re-enable → NMT operational), then parameters were stored with the
CANopen save object `0x1010:01 = "save"`. Store capability `0x1010:01` reads
`0x00000001` (save-on-command) and every download was acknowledged, not aborted.

After configuration the three producers were:

| Node | TPDO1 COB-ID | Mapping | Live value | Notes |
|---|---|---|---|---|
| `0x37` | `0x1B7` | `0x4000:01` | ~`0.0` at ~10 Hz | channel-1 sensor noise, no fixture |
| `0x38` | `0x1B8` | `0x4000:01` | `23.18 °C` at ~88 Hz | real Pt100 (`0x3001:01` ≈ `109 Ω`) |
| `0x39` | `0x1B9` | `0x4000:01` | `NaN`, silent | configured but emits nothing while ch1 is NaN |

Node `0x39` is the clearest single data point for feedback item #1: with an
event-driven TPDO (`0x1800:02 = 0xFE`) and a `NaN` channel value, the node
transmits nothing at all, whereas `0x37` with finite noise transmits at ~10 Hz.
So `NaN` already serves as a de-facto "no valid measurement" signal — the
recommendation is to make that behavior explicit and consistent across channels.

This session also surfaced a bug in this repository's own gateway, now fixed: a
CANopen SDO **abort** (an active "object not available on this device" response,
e.g. polling a TEC-only object on an RMM) was being treated like a transport
failure and tore down the device binding, which made the live read-back
intermittently report configured producers as absent. An abort proves the node
is reachable, so it is now classified as `ErrUnknownParameter` (benign) and the
binding is left intact; the live diff then reads all PDOs reliably from a cold
start.

## Open Reverse-Engineering Tasks

1. Obtain `RMM-1182 Communications Protocol` and harvest MeCom parameter IDs,
   command families, write semantics and error tables.
2. Export an XML configuration from the RMM software and compare field names
   against EDS indexes.
3. Capture startup traffic from the RMM configuration software over CAN/RS485
   and classify SDO/PDO/MeCom command order.
4. Extend bounded live probes beyond HR1/VC1 reads: common system telemetry,
   HR2/LR channels, metadata/limits, and CANopen SDO identity/heartbeat.
5. Confirm whether GPIO10 is real, hidden, or a manual typo.
