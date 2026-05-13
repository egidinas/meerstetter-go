# MVP Completion Audit - 2026-05-13

## Decision

The Raspberry Pi route is working as a functional Meerstetter-Go MVP for the
four-controller PiXtend CAN setup. It is not yet a production closeout. The
current route is good enough for live graph-wall use, source-catalogue
integration, round-trip log review, and sequencer-facing target metadata.

Do not mark the broader objective complete until the remaining hardening items
below are closed or explicitly deferred.

## Latest Live Verification

2026-05-13 19:49 UTC: the Raspberry Pi route was rechecked after the latest
power cycle and PiXtend jumper correction.

- Pi host: `192.168.6.229`.
- Edge services: `meerstettergo.service` and `pixtend-can-ring.service` active.
- CAN: `can0` is `UP,LOWER_UP`, `ERROR-ACTIVE`, 1 Mbit/s, MCP251x on
  `spi0.1`.
- Loom gateway health: `ok=true`, four devices, RAM raw-CAN ring primary,
  flash ring fallback present with records.
- Polling status: four controllers, 208 targets, 208 fresh, zero stale, zero
  not-sampled, zero errors.
- Active controllers: `tec-75` (`0x4b`), `tec-76` (`0x4c`), `tec-81`
  (`0x51`), and `tec-84` (`0x54`) via `canopen:can0`.
- `BASE_URL=http://127.0.0.1:18087 ./deploy/verify_loom_gateway_route.sh`
  passed, including graph-wall tiles, discovery tree, source catalogue,
  guarded write rejection, merged RAM/flash CAN ring, Arrow IPC export, and
  NDJSON export/import review.
- Repository checks passed:
  `/home/svc_pmg_testbed_b/.local/go/bin/go test ./utility ./mecom ./mecomserver ./cmd/mod-ingestor-mecom ./cmd/teccanprobe`
  and Loom
  `/home/svc_pmg_testbed_b/.local/go/bin/go test ./internal/operatoruiapi ./internal/signalforgeadapter`.
- Direct edge browser smoke passed at `http://192.168.6.229:18080/` with a
  nonblank screenshot, 220 discovery targets, two graph-wall tiles, and 208
  live event rows.

2026-05-13 19:55 UTC: added and ran the default non-invasive MVP gate:

```sh
PI_BASE_URL=http://192.168.6.229:18080 \
LOOM_BASE_URL=http://127.0.0.1:18087 \
./deploy/verify_mvp_completion.sh
```

Result: `PASS Meerstetter-Go MVP gate`. The wrapper passed the direct PiXtend
route, Loom/operator gateway route, direct browser UI smoke, targeted
Meerstetter-Go tests, and targeted Loom adapter tests. Default mode deliberately
skipped edge service restart checks; run with `RUN_RECOVERY=1` when a bounded
restart recovery check is desired.

2026-05-13 20:08 UTC: ran the recovery-included MVP gate:

```sh
PI_BASE_URL=http://192.168.6.229:18080 \
LOOM_BASE_URL=http://127.0.0.1:18087 \
RUN_RECOVERY=1 \
./deploy/verify_mvp_completion.sh
```

Result: `PASS Meerstetter-Go MVP gate`. The run passed the direct PiXtend
route, Loom/operator gateway route, browser UI smoke, decoder-service restart
recovery, CAN-ring-worker restart recovery, targeted Meerstetter-Go tests, and
targeted Loom adapter tests. The decoder restart recovered API health,
telemetry sequence advancement, merged RAM/flash CAN-ring readout without
duplicate mirrored frame keys, and graph-wall temperature tile points. The
ring-worker restart recovered decoded telemetry and primary RAM raw-CAN
advancement, preserved the flash fallback counter, returned merged RAM/flash
CAN-ring data without duplicate mirrored frame keys, and restored graph-wall
temperature tile points.

Subsequent fast non-invasive route run:

```sh
PI_BASE_URL=http://192.168.6.229:18080 \
LOOM_BASE_URL=http://127.0.0.1:18087 \
RUN_UI=0 RUN_TESTS=0 RUN_RECOVERY=0 \
./deploy/verify_mvp_completion.sh
```

Result: `PASS Meerstetter-Go MVP gate`. The run passed the direct PiXtend
route, Loom/operator gateway route, and the PiXtend edge autonomy verifier. In
the integrated autonomy gate the edge telemetry sequence advanced from `70512`
to `71136`, the primary RAM raw-CAN ring advanced from `51832` to `53196`, the
flash fallback remained readable/non-regressing, and merged RAM/flash readout
still had no duplicate mirrored frame keys.

2026-05-13 20:22 UTC: fixed and redeployed the edge API route table so the
Loom discovery aliases return JSON contracts instead of the web application
shell:

- `/api/loom/discovery-tree`
- `/api/loom/discovery/tree`
- `/api/operator/meerstettergo/discovery/tree`
- `/api/operator/meerstettergo/health`

The recovery-included MVP gate was rerun after deployment:

```sh
PI_BASE_URL=http://192.168.6.229:18080 \
LOOM_BASE_URL=http://127.0.0.1:18087 \
RUN_RECOVERY=1 \
./deploy/verify_mvp_completion.sh
```

Result: `PASS Meerstetter-Go MVP gate`. This run passed the direct PiXtend
edge route, Loom/operator gateway route, PiXtend edge autonomy verifier, direct
browser UI smoke, decoder-service restart recovery, CAN-ring-worker restart
recovery, targeted Meerstetter-Go tests, and targeted Loom adapter tests. The
direct route exposed 220 discovery targets, 16 writable paths, 208 decoded
targets in the log tail, 80 high-priority fresh values within 30 seconds, and a
deduplicated RAM/flash merged CAN ring. The gateway route exposed the same
four-controller health, 208 fresh polling targets, 144 source-catalogue entries,
16 write rows, and 10 remote routes through
`/api/operator/meerstettergo`.

2026-05-13 20:34 UTC: reran the default non-invasive MVP gate after the plain
`/health` edge-health alias was deployed and after the Pi reported live again
at `.229`:

```sh
PI_BASE_URL=http://192.168.6.229:18080 \
LOOM_BASE_URL=http://127.0.0.1:18087 \
./deploy/verify_mvp_completion.sh
```

Result: `PASS Meerstetter-Go MVP gate`. The direct PiXtend route passed both
`/health` and `/api/health`, sequence advancement, decoded telemetry, primary
RAM raw-CAN readout, flash fallback readout, merged RAM/flash deduplication,
discovery aliases, source catalogue, graph-wall tile data, Arrow IPC export,
and NDJSON import review. The Loom/operator gateway route passed the same
four-controller health, source catalogue, polling status, discovery tree, write
guard, log ring, graph wall, archive manifest, Arrow IPC, and import-review
checks. The edge autonomy section showed direct edge telemetry advancing from
sequence `19500` to `21216` and the primary RAM raw-CAN ring advancing from
`62744` to `64108` while the gateway was idle. Browser smoke passed against the
direct edge UI with 220 targets, graph-wall tiles, and live event rows.
Targeted Meerstetter-Go and Loom adapter tests passed. This run did not include
bounded service restarts or physical fault injection.

2026-05-13 21:07 UTC: reran the recovery-included MVP gate against the current
clean repo state and live Pi at `.229`:

```sh
PI_BASE_URL=http://192.168.6.229:18080 \
LOOM_BASE_URL=http://127.0.0.1:18087 \
RUN_RECOVERY=1 \
./deploy/verify_mvp_completion.sh
```

Result: `PASS Meerstetter-Go MVP gate`. The run verified the direct PiXtend
SocketCAN edge route, Loom/operator gateway route, edge autonomy during a
gateway-idle window, direct browser UI population, targeted Meerstetter-Go
tests, targeted Loom adapter tests, decoder-service restart recovery, and
CAN-ring-worker restart recovery. It confirmed four live TEC controllers,
220 discovery targets, 16 writable paths, 208 decoded polling targets, 144
source-catalogue entries, 16 write rows, 10 remote routes, fresh high-priority
telemetry within 30 seconds, RAM primary CAN-ring data, flash fallback CAN-ring
data, merged RAM/flash CAN-ring readout without duplicate mirrored frame keys,
graph-wall tile data, Arrow IPC export, and NDJSON export/import review. The
decoder restart recovered API health, telemetry sequence advancement, merged
raw CAN readout, and graph-wall temperature points. The CAN-ring-worker restart
recovered decoded telemetry, primary RAM raw-CAN advancement, preserved the
flash fallback counter, restored merged raw CAN readout, and restored graph-wall
temperature points.

This run did not simulate physical power interruption or a real owner
disconnect/takeover timing event.

## Scope Evidence

| Requirement | Current status | Evidence | Remaining gap |
| --- | --- | --- | --- |
| Live PiXtend CAN route | Working | `can0` is up on PiXtend `spi0.1`, `ERROR-ACTIVE`, 1 Mbit/s; `meerstettergo.service` and `pixtend-can-ring.service` are active and enabled; `deploy/verify_pixtend_recovery.sh` proves decoder restart recovery with live sequence advancement; `deploy/verify_pixtend_ring_recovery.sh` proves ring-worker restart recovery with RAM raw-CAN advancement; `deploy/verify_pixtend_edge_autonomy.sh` proves direct edge telemetry and RAM CAN-ring counters advance during a gateway-idle window with bounded flush grace. The 2026-05-13 21:07 UTC recovery-included MVP gate passed against the clean repo state. | Add physical power-interruption and real owner-disconnect/takeover timing regression checks. |
| Four TEC controllers | Working | `/api/log/ring?tail=true&limit=80` returned `tec-75`, `tec-76`, `tec-81`, and `tec-84`; the recovery-included MVP gate verifies four live decoded controllers through direct edge and Loom gateway routes. | Revalidate after controller power cycling or other physical topology changes. |
| Decoded telemetry | Working with freshness gate | The live direct and Loom gateway verifiers require `object_temp_c`, `sink_temp_c`, `target_object_temp_c`, `output_current_a`, and `output_voltage_v` to be fresh across all live instanced TEC targets within `MAX_SAMPLE_AGE_SECONDS=30`; the latest gateway run passed with 80 fresh high-priority target values and the polling-status route reported 208/208 targets fresh. | Define tier-specific freshness budgets for lower-priority catalogue variables and add congestion/power-cycle freshness regressions. |
| Signal tree | Working | `/api/discovery/tree`, `/api/loom/discovery-tree`, `/api/loom/discovery/tree`, and `/api/operator/meerstettergo/discovery/tree` expose the JSON discovery contract with 220 targets and 16 writable target paths. | Continue classifying undocumented parameters before making them active writes. |
| SignalForge/Loom catalogue | Working for catalogue, read freshness, and write guards | `/api/loom/source-catalogue` exposes 144 entries, command metadata, target read routes, and lease-required write routes. `deploy/verify_loom_gateway_route.sh` now verifies the running Loom/operator gateway contract: `selection_owner=loom.operator`, remote read/write route metadata, live polling freshness, target-read route availability, and safe gateway rejection for writes without a sequencer lease. | Prove real owner-disconnect/takeover timing and end-to-end leased write acceptance before routine writes. |
| Graph wall | Working at API, served UI, and browser-rendered level | `/api/graph-wall` returns temperature, target, power, and event tiles; `/` loads the shared graph-wall renderer and live API routes; the aggregate pseudo-target tile route returns live device series; `deploy/verify_ui_browser_smoke.sh` uses headless Chromium to confirm live plots, graph-wall assignment controls, target controls, and all four TEC nodes instead of `loading...`. | Add richer browser layout regression checks if this becomes a CI/deployment gate. |
| Temporary logging | Working | `/api/log/ring` serves decoded telemetry from the in-memory log ring; `/api/can/ring` serves raw CAN records from the primary RAM ring. | Add pressure tests for ring sizing and dropped-frame accounting. |
| Flash fallback | Configured, exposed, and reconciled with RAM | `/api/health` reports RAM as primary and flash as fallback/bootstrap; `/api/can/ring?source=fallback_flash` reads the fallback explicitly; `/api/can/ring?source=merged` returns a RAM-plus-flash tail without duplicate frame keys in the live route and recovery verifiers; `deploy/verify_pixtend_ring_recovery.sh` proves bounded `pixtend-can-ring.service` restart recovery; `deploy/verify_pixtend_edge_autonomy.sh` proves the fallback remains readable and non-regressing while the primary RAM ring advances. | Prove intentional owner-disconnect recovery, physical power-interruption recovery, bus-congestion behavior, and controller-ring gap-fill under scripted fault tests. |
| Permanent export and review | MVP working with durable archive contract | `/api/log/export` emits NDJSON, `/api/log/export?format=arrow_ipc` emits a binary Arrow IPC telemetry stream, `/api/log/import/review` reviews NDJSON without committing, and `/api/log/archive/manifest` exposes the stable NDJSON/Arrow/HDF5 stream contract for telemetry, raw CAN, command events, object dictionary snapshots, and graph-wall assignments. | HDF5 remains a planned archive contract if required for final production. |
| Redundant transports | Metadata path present | CAN is active/preferred; serial FTDI and Kvaser-compatible routes are exposed through the same target model. | Validate live FTDI fallback arbitration and TCP device-server sharing under load. |
| Write path | Present and guarded | Writable targets are initialized with current values and expose sequencer write metadata requiring a lease. Direct no-lease probes are rejected at the edge with HTTP 428, and gateway no-lease probes are rejected with `X-Loom-Sequencer-Lease` enforcement before forwarding. | Add end-to-end command receipt tests against live hardware before routine writes. |
| One-command MVP gate | Working, with bounded recovery coverage verified | `deploy/verify_mvp_completion.sh` ties together the direct PiXtend route verifier, Loom gateway verifier, PiXtend edge autonomy verifier, browser UI smoke, targeted Go tests, and optional bounded service-restart checks. The default run, fast no-UI/no-test route run, post-route-fix `RUN_RECOVERY=1` run, post-`/health`-alias default run, and current clean-state `RUN_RECOVERY=1` run all passed against `.229` and the local Loom gateway. | The gate still does not simulate Pi power loss or a real owner-disconnect/takeover timing event. |

## Prompt-to-Artifact Checklist

| Requested capability | Artifact or verifier | Status |
| --- | --- | --- |
| Primary PiXtend CAN route | `meerstettergo.service`, `pixtend-can-ring.service`, `deploy/verify_pixtend_route.sh` | Live verified on `.229`. |
| Redundant route model | Source catalogue transport metadata, FTDI/Kvaser route metadata, Loom gateway proxy routes | Metadata path present; live FTDI arbitration still pending. |
| Same backend/UI contract for all paths | `/api/loom/source-catalogue`, `/api/operator/meerstettergo/*`, target read/write routes | Live gateway verified; write path is lease guarded. |
| In-RAM primary ring plus flash fallback | `/api/can/ring`, `/api/can/ring?source=fallback_flash`, `/api/can/ring?source=merged` | Live verified with deduped RAM/flash merge. |
| Edge worker independent of owner | `deploy/verify_pixtend_edge_autonomy.sh` | Live verified for gateway-idle edge advancement; intentional owner outage still pending. |
| Four-controller graph wall | `/api/graph-wall`, `/api/tiles`, `deploy/verify_ui_browser_smoke.sh` | API and browser smoke verified. |
| Full signal catalogue and writable paths | `/api/discovery/tree`, `/api/loom/source-catalogue`, write-guard probes | 220 targets and 16 initialized writable paths verified. |
| Export and reimport/review | `/api/log/export`, `/api/log/export?format=arrow_ipc`, `/api/log/import/review`, archive manifest | NDJSON and Arrow IPC verified; HDF5 remains manifest-only. |
| Conservative reliability gate | `deploy/verify_mvp_completion.sh`, recovery verifiers | Functional MVP verified; physical fault injection remains open. |

## Code Contracts

- HTTP routes are registered in `utility/server.go` for health, source
  catalogue, discovery tree, graph wall, log ring, export/import review, raw CAN
  ring, target read, and target write.
- The Loom/SignalForge catalogue builder advertises live support, metadata-only
  discovery, target read/write routes, route budgets, and the PiXtend SocketCAN
  transport path.
- Loom has proxy routes for Meerstetter-Go source catalogue, target read, and
  lease-gated target write so Loom can treat Meerstetter-Go as a data source.
- Meerstetter-Go rejects writable target requests that omit an explicit
  sequencer `lease_id` before hardware transport selection, keeping catalogue
  write visibility separate from unowned device mutation.
- The SignalForge adapter builds graph-wall offers from typed source signals and
  rejects raw-default graph layers and write-capable signals without leases.

## Verification Runbook

Run these after any deployment, reboot, or bus wiring change:

```sh
PI_BASE_URL=http://192.168.6.229:18080 \
LOOM_BASE_URL=http://127.0.0.1:18087 \
./deploy/verify_mvp_completion.sh
```

The MVP gate is non-invasive by default: it does not write to TEC controllers
and does not restart edge services. Set `RUN_RECOVERY=1` to include the bounded
`meerstettergo.service` and `pixtend-can-ring.service` restart recovery gates:

```sh
PI_BASE_URL=http://192.168.6.229:18080 \
LOOM_BASE_URL=http://127.0.0.1:18087 \
RUN_RECOVERY=1 \
./deploy/verify_mvp_completion.sh
```

```sh
./deploy/verify_pixtend_route.sh
```

```sh
./deploy/verify_pixtend_edge_autonomy.sh
```

The edge autonomy verifier is non-invasive. It fetches the gateway catalogue
once, leaves the gateway idle during a short direct-edge window, and requires
the PiXtend edge telemetry sequence plus primary RAM raw-CAN ring counter to
advance independently. It also verifies the flash fallback and merged ring
remain readable without duplicate mirrored frame keys.

The direct and gateway verifiers default to a 30-second high-priority freshness
budget for `object_temp_c`, `sink_temp_c`, `target_object_temp_c`,
`output_current_a`, and `output_voltage_v`. Override with
`MAX_SAMPLE_AGE_SECONDS` and `FRESHNESS_NAMES` when testing different polling
tiers. The direct PiXtend and Loom gateway verifiers use an 800-record decoded
log tail by default so the four-controller multi-instance polling cycle is
covered without false freshness failures.

```sh
./deploy/verify_ui_browser_smoke.sh
```

```sh
./deploy/verify_pixtend_recovery.sh
```

```sh
./deploy/verify_pixtend_ring_recovery.sh
```

```sh
BASE_URL=http://127.0.0.1:18087 ./deploy/verify_loom_gateway_route.sh
```

```sh
curl -fsS --max-time 5 'http://192.168.6.229:18080/health'
curl -fsS --max-time 5 'http://192.168.6.229:18080/api/health'
curl -fsS --max-time 5 'http://192.168.6.229:18080/api/log/ring?tail=true&limit=800'
curl -fsS --max-time 5 'http://192.168.6.229:18080/api/discovery/tree'
curl -fsS --max-time 5 'http://192.168.6.229:18080/api/loom/source-catalogue'
curl -fsS --max-time 5 'http://192.168.6.229:18080/api/graph-wall'
```

```sh
ssh -i /home/svc_pmg_testbed_b/.ssh/router_lan_can \
  -o BatchMode=yes -o ConnectTimeout=5 pi@192.168.6.229 \
  'systemctl is-active meerstettergo.service pixtend-can-ring.service; ip -details link show can0'
```

Repository checks:

```sh
/home/svc_pmg_testbed_b/.local/go/bin/go test ./utility ./mecom ./mecomserver ./cmd/mod-ingestor-mecom ./cmd/teccanprobe
```

```sh
cd /home/svc_pmg_testbed_b/loom
/home/svc_pmg_testbed_b/.local/go/bin/go test ./internal/operatoruiapi ./internal/signalforgeadapter
```

## Not Yet Complete

- Exact Meerstetter controller ring-buffer primitive behavior, limits, and wrap
  semantics still need live characterization and tests.
- Bus-capacity budgets now have a conservative reusable estimator in `mecom`;
  measured numbers for 4, 8, and 16 controller cases still need to replace the
  default frame-timing assumptions.
- High-priority decoded telemetry freshness is now verifier-gated on the live
  route. Lower-priority catalogue variables still need tier-specific freshness
  budgets and congestion/power-cycle regression tests.
- RAM ring and flash fallback have a no-duplicate merge primitive plus live
  route, decoder restart recovery, and ring-worker service restart verifier
  coverage. Owner-disconnect, power-interruption recovery, bus-congestion
  behavior, and controller-ring gap-fill still need scripted proof.
- Durable archival export now has a stable manifest contract. NDJSON and Arrow
  IPC telemetry export are implemented and live-verifier gated. HDF5 remains a
  planned writer if it becomes a production requirement.
- The live Loom ownership path is verifier-covered for gateway catalogue
  ownership metadata, read freshness, and no-lease write rejection. Real
  owner-disconnect/takeover timing and leased write acceptance still need
  scripted proof before routine writes.
- Browser visual smoke is now automated with headless Chromium; the route
  verifier also covers the aggregate pseudo-target tile query that previously
  left the graph wall empty. Richer pixel/layout regression remains optional
  hardening.
