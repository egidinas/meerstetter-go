# Handoff brief — design a frontend for meerstetter-go

You are the design agent. Your job is to produce a browser UI (and/or
TypeScript client) on top of the meerstetter-go gateway. This file is the
single-source brief; everything you need is linked from here.

## What this system is

`meerstetter-go` is a Go toolkit that talks to Meerstetter TEC controllers
over serial, TCP, or CAN. A bundled HTTP gateway (`cmd/mecomgw`) fronts the
library with a JSON/SSE surface intended for browser consumption. The
gateway is the **only** API contract the frontend depends on; the underlying
transport is opaque.

## Read these in order

1. `README.md` — repository overview, package map, build instructions.
2. `deploy/README.md` — how the device server (`mecomvseriald`) sits in
   front of physical hardware, and how `mecomgw` sits in front of that.
3. `docs/gateway/openapi.yaml` — **authoritative** description of every
   gateway endpoint. Generate clients/types from this, not from prose.
4. `docs/gateway/types.d.ts` — pre-built TypeScript types matching the
   schema. Drop into a TS project as-is.
5. `deploy/example-gateway.json` — concrete config shape (four-TEC layout).
6. `docs/gateway/demo/index.html` — tiny dependency-free browser probe for
   the API. Use it to see the expected information architecture, not as the
   final visual design.
7. `docs/backlog/frontend_hooks.jsonl` — what was implemented this wave and
   why. Useful for understanding intent.
8. `docs/gateway/UI_GRAPH_WALL_CONTRACT.md` — graph-wall, signal dictionary,
   channel-role, and telecommand-placement rules for the richer UI.

## What the gateway provides

The full list lives in `openapi.yaml`. Headline endpoints:

| Method | Path | Purpose |
|--------|------|---------|
| GET    | `/api/healthz` | liveness |
| GET    | `/api/devices` | device list + bind status |
| GET    | `/api/catalogue` | parameter catalogue (names, units, types, writability) |
| GET    | `/api/leases` | currently active write leases |
| POST   | `/api/devices/{id}/lease` | acquire write authority for a device |
| DELETE | `/api/devices/{id}/lease` | release it |
| GET    | `/api/devices/{id}/read?params=ID:INSTANCE,...` | one-shot bulk read |
| GET    | `/api/devices/{id}/poll?params=...&interval=2s` | SSE stream of `Telemetry` |
| POST   | `/api/devices/{id}/write` | issue a setpoint/control command (requires lease) |

Write requests carry the lease token in the `X-Lease-Token` request header.

## Run the bundled demo UI

From the repository root:

```sh
go run ./cmd/mecomgw \
  -config deploy/example-gateway.json \
  -listen 127.0.0.1:18080 \
  -ui-dir docs/gateway/demo
```

Then open:

```text
http://127.0.0.1:18080/ui/
```

This same-origin mode is the safest way to prototype. If a separate browser
origin needs to call the gateway directly, start the gateway with a narrowly
scoped CORS allow-list:

```sh
go run ./cmd/mecomgw \
  -config deploy/example-gateway.json \
  -listen 127.0.0.1:18080 \
  -ui-dir docs/gateway/demo \
  -allow-origin https://claude.ai
```

Use `-allow-origin '*'` only for an isolated local test gateway, never for a
shared hardware-facing deployment.

## Rich operator-console prototype

The Claude Design operator console is preserved under
`docs/gateway/console/`. Serve it through the gateway when you want the dense
graph-wall, signal-dictionary, and bottom telemetry/telecommand accordion view:

```sh
go run ./cmd/mecomgw \
  -config deploy/example-gateway.json \
  -listen 127.0.0.1:18080 \
  -ui-dir docs/gateway/console
```

Open `http://127.0.0.1:18080/ui/`. The older `docs/gateway/demo/` surface is
the minimal API smoke console.

## What the UI needs to cover

Pick from these; mark "out of scope" if you choose not to do something:

- **Device fleet view** — live status pill per device (`bound`, `last_error`),
  one row per device from `/api/devices`. Pill colour from `last_error`
  category once you map sentinel error strings (see `mecom/errors.go`).
- **Per-device readout** — pick parameters from the catalogue, see live
  values via SSE `/poll`. Show `quality` (ok/nan/missing/unreachable) as a
  badge, not as the value itself.
- **Setpoint editor** — for writable parameters (catalogue field
  `writable: true` — currently `param 3000`, `1020`, `1021`, `2010`, `2040`),
  show current value plus a form that calls `/write`. Must:
  1. acquire a lease first (`POST /lease` with a `holder` identifier),
  2. attach `X-Lease-Token` on every write,
  3. release the lease when the user navigates away or hits Cancel.
- **Lease awareness** — show the active holder of every device on the
  fleet view (read from `/api/leases`). Visualise "someone else holds this"
  vs "you hold this" vs "free".
- **Error rendering** — on every JSON response, an `error` field is
  human-readable. HTTP status maps to category:
  `423 Locked` = lease problem, `503` = transport unreachable, `504` =
  timeout, `403` = read-only, `409` = device rejected, `501` = not supported.
- **Signal dictionary and graph-wall assignments** — use
  `docs/gateway/UI_GRAPH_WALL_CONTRACT.md` for the required hierarchy:
  signal group, signal subgroup, signal, device, instance. The default
  temperature graph must include target temperature, object temperature, and
  sink temperature. Power-supply channels should default to voltage, current,
  and power.

## Constraints and out-of-scope

- **No auth / no users yet.** The lease `holder` is a free-form string. Assume
  the frontend supplies a stable client identifier (browser session ID, or
  prompt-the-user once). Real authn/authz is a later wave.
- **No historical storage.** The gateway only serves live values. The bundled
  demo keeps a small client-side log only. If you need a chart, buffer SSE
  events in the client; do not ask the gateway for history.
- **Writes are real.** Hardware is on the other side. Don't auto-poll a
  write endpoint, don't speculatively acquire leases, and confirm before
  any setpoint/output-enable change.
- **CAN is read-write, serial is write-only via leased commands.** Both
  transports look identical at the HTTP layer; the `endpoint` field tells
  you which is in use but the UI should not branch on it.
- **No WebSocket yet.** SSE only. If you'd prefer a single multi-device WS
  stream, that's a wave-2 backlog item; don't assume it exists.
- **SignalForge/Gossamer boundary.** SignalForge graph/source/catalogue and
  control-program concepts are the reusable public model. Gossamer is visual
  and interaction inspiration only; do not depend on Gossamer routes,
  fixtures, or private deployment details.

## How to verify your design against the live API

If a gateway URL has been shared with you, the smallest end-to-end check:

```sh
curl -sS $GATEWAY/api/healthz
curl -sS $GATEWAY/api/devices | jq
TOKEN=$(curl -sS -X POST $GATEWAY/api/devices/tec-75/lease \
          -d '{"holder":"design-claude","ttl":"5m"}' | jq -r .token)
curl -sS "$GATEWAY/api/devices/tec-75/read?params=1000:1,1001:1,3000:1" | jq
# Don't actually issue writes without explicit human approval.
```

If no live URL is available, work from `openapi.yaml` and treat the
schema as canonical. Every handler is parity-tested against the schema in
CI (`cmd/mecomgw/openapi_test.go`).

## How to ask for changes to the gateway

If you find the JSON shape inconvenient, append a backlog entry to
`docs/backlog/frontend_hooks.jsonl` describing what you need and why.
Don't expand the gateway surface yourself; the device-side code is
hardware-facing and reviewed separately.

## Quick reference: file locations

```
README.md                                top-level overview
deploy/README.md                         deployment + transport context
deploy/example-gateway.json              gateway config example
docs/gateway/openapi.yaml                schema (authoritative)
docs/gateway/types.d.ts                  TypeScript types
docs/gateway/HANDOFF.md                  this file
docs/gateway/UI_GRAPH_WALL_CONTRACT.md   graph wall + signal dictionary rules
docs/gateway/demo/index.html             dependency-free browser probe
docs/backlog/frontend_hooks.jsonl        backlog with completion notes
cmd/mecomgw/                             gateway source
cmd/mecomgw/openapi_test.go              schema parity test
mecom/errors.go                          sentinel error catalogue
mecom/writelease/                        lease primitive
mecomserver/stats.go                     BrokerStats type
```
