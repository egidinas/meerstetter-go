# RMM-1182 CANopen Model — Constructive Feedback

The RMM-1182 CANopen object model is explicitly open for feedback. This note
collects observations from integrating it as a temperature **producer** that
hands values to TEC-1166 controllers over PDO, plus a first read-only session
against real hardware (three RMM-1182 and four TEC-1166 on a 1 Mbit/s bus, see
[`rmm_1182_reverse_engineering.md`](rmm_1182_reverse_engineering.md#live-hardware-confirmation-2026-06-13)).

It is written from the integrator's seat: the goal is that a consuming device or
host can take an RMM PDO and *know what it is holding and whether to trust it*,
without out-of-band configuration knowledge. Everything below is a suggestion,
ordered by how much it would improve real-world usability.

## What already works well

- **Identity is clean and readable.** `0x1018` vendor `0x547` / product `0x49E`
  / revision matched the EDS exactly on live hardware. Product-number discovery
  (`0x49E`) is a reliable way to recognize an RMM on a mixed bus.
- **Sensible default PDO layout.** Out of the box TPDO1 publishes
  `0x4000:01` (channel-1 value-conversion result) as one float32 at the standard
  `0x180 + node` COB-ID. That is exactly the object an integrator wants for
  temperature handover, at a predictable identifier. No remapping was needed to
  start consuming a value.
- **Four parallel channels** (`0x4000:01..04`) map naturally onto a multi-sensor
  rig feeding multiple TECs.
- **Broad PDO capacity** (16 TPDO / 16 RPDO) leaves room for richer telemetry
  later without redesign.

## High-impact suggestions

### 1. Give the PDO an in-band validity / quality signal

This is the single biggest usability gap. On the live bench an unconfigured /
open channel published `0x4000:01 ≈ 9.88e-08` — sensor noise around zero — as a
perfectly ordinary float32, while channel 2 published `NaN`. A consumer cannot
distinguish "a real near-0 °C reading" from "no usable input" by looking at the
PDO. A TEC with object-temperature source selection set to external would feed
that garbage straight into its control loop.

Two inconsistent "no data" encodings appeared on the same device in one session
(tiny-number on ch1, `NaN` on ch2), which makes the problem worse: there is no
single rule a consumer can implement.

Suggestions, in order of preference:

- Define **one canonical invalid sentinel** and emit it for *every* invalid
  condition (open sensor, out-of-range, not-yet-converted). `NaN` is the
  natural choice for a float32 and the TEC side already treats `NaN` as
  "missing." Then "open input" should surface as `NaN`, not as small noise.
- Better still, allow a **status/quality byte** to be co-mapped into the same
  PDO (the frame has room next to a 4-byte float), so the value and its validity
  travel together in one frame and one timestamp.
- Confirm and document that the **"Surveilled Result" `0x4001`** is the
  error/limit-checked variant intended for control use, and recommend mapping
  *that* into the PDO for safety-relevant handover rather than the raw
  `0x4000`. The naming alone (`Result` vs `Surveilled Result`) does not make the
  distinction or the recommendation obvious.

### 2. Make the value self-describing: define `0x4011` Result Type

Each channel has a "Result Type" (`0x4011:<ch>`), but on hardware it read `0`
with no documented meaning. Today "`0x4000` is a temperature" is true only by
configuration convention — the integration handbook has to *warn* readers that
the same object may carry resistance, voltage, or an unconverted value depending
on how the channel was set up. That convention-over-contract is the model's
biggest hazard.

Publishing a documented enum would make a PDO value self-describing:

```
0 = undefined / not configured
1 = temperature [°C]
2 = resistance  [Ω]
3 = voltage     [V]
...
```

With that, a consumer (or our `canmap` registry verifier) can assert "this PDO
slot is currently a temperature" instead of trusting a comment in a spreadsheet.

### 3. Populate the standard identity objects

- `0x1000` **Device Type** read `0x00000000`. Zero is legal but tells generic
  CANopen tooling "no standardized profile." If an applicable CiA profile exists
  (e.g. the measurement-device profile), advertising it lets off-the-shelf tools
  classify the node; if zero is intentional, a one-line note in the protocol
  document would save integrators the lookup. (The TEC-1166 also reports `0`.)
- `0x1008` **Manufacturer Device Name** aborted with `0x06020000` (object does
  not exist). `0x1008/0x1009/0x100A` (device / hardware / software version
  strings) are cheap to add and make pure-CANopen fleet inventory and
  firmware-version pinning possible without dropping to MeCom.

### 4. Don't leave a valid-but-empty TPDO advertised

TPDO2 was enabled (COB-ID `0x2B7`, valid bit clear) but had a mapping count of
`0`. A scanner sees a "live" PDO that never carries data — one of the classic
confusing states when bringing up a bus. Consider either shipping TPDO2 with a
documented default mapping, or leaving its COB-ID marked invalid (bit 31 set)
until it is mapped, so "advertised" and "actually publishing" stay the same
thing.

### 5. Expose per-channel identity

`0x4000` has four channels but the PDO stream alone does not say which physical
input a channel is, nor its configured sensor type. A consumer that only sees
the PDO cannot tell channel 1 from channel 3. A small per-channel
label/sensor-type object (or folding sensor type into the Result Type of §2)
would let the wiring be discovered from the device instead of documented
externally. (Our `canmap` registry solves this on the host side by recording
subindex→meaning, but a self-describing device would not need it.)

## Smaller notes

- The **EDS exists and imports cleanly**, but the referenced *RMM-1182
  Communications Protocol* document is what integrators actually need for MeCom
  parameter IDs, write semantics, and error tables. Publishing it would remove
  most remaining guesswork.
- Documenting the **default transmission type and event/inhibit timing** of
  TPDO1 next to its default mapping would let an integrator confirm the
  refresh-rate budget (the TEC external-temperature path requires ≤100 ms) from
  the datasheet rather than from a capture.

## How we consume the model today

On our side, the [CAN signal registry](can_parameter_publishing_consuming_handbook.md#keeping-track-of-mappings-across-a-fleet)
records each RMM channel→TEC mapping as a reviewable contract and verifies it
against live devices over read-only SDO. That compensates for §1, §2, and §5 at
the host level, but it is compensation: every item above would let the device
model carry that meaning itself, which is the more robust place for it.
