# Documentation

Start here to understand the project or to integrate with it.

## For the Meerstetter team

If you want to evaluate the gateway or wire your own UI on top of it:

| Document | What it covers |
|---|---|
| [`../README.md`](../README.md) | Project overview, quick-start, full feature and limitation list |
| [`../deploy/README.md`](../deploy/README.md) | How to run the device server and gateway on real hardware |
| [`gateway/openapi.yaml`](gateway/openapi.yaml) | **Authoritative HTTP API reference** — every route, request/response shape, SSE events |
| [`gateway/types.d.ts`](gateway/types.d.ts) | TypeScript types matching the gateway JSON responses (use directly in a browser client) |
| [`rpi_pixtend_bootstrap.md`](rpi_pixtend_bootstrap.md) | Complete bring-up guide for Raspberry Pi + PiXtend V2-L CAN interface |

## Protocol and package reference

| Document | What it covers |
|---|---|
| [`gateway/readout_scheduling.md`](gateway/readout_scheduling.md) | Which routes support ring/CRTVStream readout vs. polled readout — read before implementing a display refresh loop |
| [`gateway/UI_GRAPH_WALL_CONTRACT.md`](gateway/UI_GRAPH_WALL_CONTRACT.md) | Graph wall tile contracts and the SSE streaming shape used by the graph tile endpoints |
| [`lut_tmtc_automation.md`](lut_tmtc_automation.md) | `mecomautomation` package — how LUT preload telecommands are generated |
| [`tmtc_signalforge_boundary.md`](tmtc_signalforge_boundary.md) | Where `meerstetter-go` ends and the SignalForge ecosystem begins; the only import boundary between them |
| [`SYNERGY.md`](SYNERGY.md) | Apache Arrow IPC data standard and schema used for high-performance telemetry export |
| [`public_variant_readiness.md`](public_variant_readiness.md) | Gates for what belongs in this public repository vs. a private deployment repo |

## Reference data

| File | What it covers |
|---|---|
| [`reference/tec_can_parameters_seed.go`](reference/tec_can_parameters_seed.go) | Seed fragment from TEC CAN parameter discovery. Not part of the build — the live catalogue is in `mecom` and `mecomdict`. |

## Protocol overview

### MeCom (serial / TCP / CAN)

`mecom` is a strict request-response protocol. The host sends `#` frames, the
device replies with `!` frames, every frame terminated by `\r`.

The package owns CRC-16-CCITT framing, single reads (`?VR`), bulk reads
(`?VX`), writes (`VS`), float32/int32/string encoding, NACK decoding, and a
synchronous client over `io.ReadWriter`.

Supported endpoint shapes for `mecom.ParseEndpoint`:

```
tcp:host:port
host:port
serial:/dev/ttyUSB0@57600
COM3@57600
can:can0/0x23
```

Multiple UI sessions may subscribe to decoded telemetry, but must not write to
the same connection concurrently without a command arbiter.

### CANopen

`canopen` / `canopen/eds` / `objectdict` — TEC controllers can expose values
through CANopen. The EDS parser builds a protocol-neutral `objectdict.Dictionary`
so MeCom and CANopen targets share the same tree shape. Concrete bus adapters
(SocketCAN, Kvaser, remote bridge) live at the application boundary.
