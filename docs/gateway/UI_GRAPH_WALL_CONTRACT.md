# Gateway UI graph-wall contract

This document is the frontend contract for richer Meerstetter-Go operator
surfaces. It keeps three boundaries explicit:

- `cmd/mecomgw` remains the live HTTP/SSE API truth.
- SignalForge provides reusable public graph, source-catalogue, and control
  program concepts.
- Gossamer is visual and interaction inspiration only. Do not import private
  routes, fixtures, deployment assumptions, or Loom-specific details.

If a field or endpoint is not in `docs/gateway/openapi.yaml`, the UI may mock
or plan it, but must label it as mock/planned.

## Signal dictionary hierarchy

The UI should organize telemetry and telecommands with this stable path:

```text
Signal group / Signal subgroup / Signal / Device / Instance
```

Definitions:

- **Signal group**: broad operator domain. Prefer SignalForge categories and
  aggregates such as `thermal`, `power`, `electrical`, `control`, `status`,
  and `fault`.
- **Signal subgroup**: semantic grouping within the domain, for example
  `object`, `target`, `sink`, `cascade`, `output`, `stage`, `mode`, or
  `stability`.
- **Signal**: display name plus MeCom parameter ID, for example
  `object temperature (1000)`.
- **Device**: gateway device ID, alias, or serial label from `/api/devices`.
- **Instance**: MeCom instance or channel number.

Every detail pane and hover card should expose a parseable provenance string:

```text
device=<device_id> param=<parameter_id> instance=<instance> endpoint=<endpoint>
```

The endpoint is operational provenance only. The UI should not branch its
behavior on CAN versus serial when the gateway exposes the same capability.

## Default graph assignments

The default wall should be useful before the operator customizes anything.
Use SignalForge `graphwall` semantics for assignment state: `wall_id`,
`tile_id`, `kind`, `target_id`, `position`, and `options`.

### Fleet temperature-controller hero graph

For channels operating as temperature controllers, graph these together per
device/channel:

| Parameter | Signal ID | Series role | Notes |
|-----------|-----------|-------------|-------|
| 3000 | `target_object_temp_c` | target/control | Target line. Writable when gateway marks it writable. |
| 1000 | `object_temp_c` | actual | Main resulting controlled temperature. |
| 1001 | `sink_temp_c` | auxiliary | Thermal headroom and heat rejection context. |
| 52200 | `cascade_temp_c` | auxiliary | Show when available; hide cleanly otherwise. |
| 1200 | `temperature_stable` | status lane | State lane, not a temperature axis. |

The graph is incomplete if target, object, and sink temperatures are not all
visible for at least one temperature-controller channel.

### Fleet power-supply hero graph

For channels operating as power supplies, graph and tabulate these together:

| Parameter | Signal ID | Series role | Notes |
|-----------|-----------|-------------|-------|
| 1021 | `output_voltage_v` | actual | Measured output voltage; do not expose as a setpoint editor. |
| 1020 | `output_current_a` | actual | Measured output current; do not expose as a setpoint editor. |
| 1022 | `output_power_w` | actual | Derived/read power indicator. |
| 2010 | `output_stage_enable` | status/control | Command card with explicit confirmation. |
| 2040 | `operating_mode` | status/control | Command card with explicit confirmation. |

Power-supply channels should not be forced into the temperature graph. They
belong in the power hero graph and in a channel workspace focused on voltage,
current, power, mode, and output state.

### Device and channel workspace

The per-device workspace should default to the same signal families:

- Temperature-controller channel: target, object, sink, cascade if present,
  and stable state.
- Power-supply channel: voltage, current, power, mode, and output state.

Operators may pin additional signals from the dictionary into any graph. The
assignment UI should show exactly which graph receives the signal.

## Telecommands and command cards

Writable catalogue entries should appear in both the signal dictionary and the
contextual command-card area.

Known write-capable parameters in the current gateway contract:

| Parameter | Signal ID | Value path |
|-----------|-----------|------------|
| 3000 | `target_object_temp_c` | `set_float32` |
| 1020 | `output_current_a` | `write_float32` |
| 1021 | `output_voltage_v` | `write_float32` |
| 2010 | `output_stage_enable` | `write_int32` |
| 2040 | `operating_mode` | `write_int32` |

Command-card behavior:

1. Read the current value before enabling the input field.
2. Acquire a lease before staging a write.
3. Stage locally, then require an explicit Commit action.
4. Send `X-Lease-Token` with every write request.
5. Require stronger confirmation for output enable, reset, save, and any
   persistent flash write.
6. Never auto-save to flash.

Every telecommand should append a command activity row with:

- command ID, timestamp, and status,
- device, instance, parameter ID, signal name, and unit,
- previous value when known,
- requested value,
- lease holder,
- transport endpoint provenance,
- error category and message if rejected.

## Channel role handling

A channel can be used as a temperature controller or as a power supply. The UI
must make that role visible and must not permanently infer it from one missing
sample.

Preferred role source:

1. A live or cached operating-mode parameter when available.
2. Gateway/device configuration metadata when available.
3. A visible local UI assumption with an "unconfirmed" badge.

If role metadata is missing, show both candidate bundles but keep the primary
graph assignment conservative.

## Live, client-side, and planned surfaces

Live today through `cmd/mecomgw`:

- health,
- devices,
- catalogue,
- leases,
- one-shot reads,
- SSE polling,
- leased writes.

Client-side first:

- graph assignments,
- pinned signals,
- command-card placement,
- short in-browser chart buffers,
- mock scenarios for unavailable hardware.

Planned or backend-needed:

- durable command event history,
- historical replay from Pi RAM/flash ring buffers,
- TEC internal ring-buffer replay,
- archive export endpoints,
- PID advisor apply loop,
- sequencer execution endpoints.

The UI may design these planned surfaces, but they should be visually marked
as planned unless a live endpoint is present.

## QA gates for the design agent

Before the UI is considered aligned with this contract:

- At 1440 px and 900 px widths, labels do not overlap.
- The fleet temperature hero graph shows target, object, and sink temperature.
- Cascade temperature appears when data exists and disappears cleanly when it
  does not.
- The fleet power hero graph shows voltage, current, and power for supply
  channels.
- The signal dictionary can filter telemetry versus telecommands.
- The hierarchy `group / subgroup / signal / device / instance` is visually
  parseable.
- Each graph assignment and command-card assignment is visible and reversible.
- Write controls require lease ownership and surface command activity.
- Mock mode and live mode are visibly distinct.
