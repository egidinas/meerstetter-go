# PiXtend Route Audit - 2026-05-13

## Decision

The live Raspberry Pi route is operational for the current four-controller
Meerstetter-Go graph-wall path. CAN is the primary transport, the Pi edge worker
is running automatically, and the browser/API route at `http://192.168.6.229:18080/`
serves live decoded telemetry.

This is an operational route checkpoint, not the final production closeout. The
remaining hardening work is reconnect/resume, exact controller ring-buffer limit
validation, measured bus budgeting, and deployment packaging.

## Live Evidence

- `meerstettergo.service` and `pixtend-can-ring.service` are active on the Pi.
- `can0` is `UP,LOWER_UP`, `ERROR-ACTIVE`, at `1000000` bit/s on PiXtend
  `spi0.1`.
- Pi-side CAN debug utilities are available at `/usr/local/bin/candump` and
  `/usr/local/bin/cansend`.
- `/api/health` reports `ok=true`, `devices=4`, fresh increasing `latestSeq`,
  and `can_ring.ok=true` with `source=primary_ram` and `storage=primary_ram`.
- `/api/can/ring?tail=true` returns raw CAN records from the primary RAM ring.
- `/api/log/ring?tail=true&limit=800` returns all four TEC nodes:
  `tec-75`, `tec-76`, `tec-81`, and `tec-84`; the latest route verifier
  covered 208 decoded targets in the tail window.
- The route verifiers enforce a 30-second high-priority freshness budget for
  `object_temp_c`, `sink_temp_c`, `target_object_temp_c`, `output_current_a`,
  and `output_voltage_v` across the live instanced TEC targets. The latest
  direct and gateway runs both passed with 80 fresh high-priority target values.
- `/api/discovery/tree` exposes 220 targets and 16 writable target paths.
- `/api/loom/discovery-tree`, `/api/loom/discovery/tree`, and
  `/api/operator/meerstettergo/discovery/tree` expose the same JSON discovery
  contract instead of the browser HTML shell.
- `/api/operator/meerstettergo/health` exposes edge health JSON for operator
  clients using the gateway-style route prefix.
- `/api/target/write` rejects writable target commands without an explicit
  sequencer `lease_id` before selecting any hardware transport. The direct edge
  route returns HTTP 428 for this no-lease probe.
- `/api/loom/source-catalogue` exposes 144 source entries and the command
  metadata needed by Loom/SignalForge/sequencer callers.
- The Loom gateway route rejects missing sequencer lease headers before
  forwarding writes, returning `403 missing X-Loom-Sequencer-Lease`; this is
  the expected gateway-side guard for the same no-lease probe.
- `/api/graph-wall` returns the four-controller temperature, target, power, and
  event tile definitions.
- `/api/tiles?target_id=aggregate:temperatures&aggregate=temperature&limit=1`
  returns live per-device temperature series. This covers the browser regression
  where an aggregate graph-wall pseudo-target was previously treated as an
  exact target and rendered no data.
- `deploy/verify_ui_browser_smoke.sh` captured a headless Chromium screenshot
  and confirmed the live web UI renders populated graph tiles, graph-wall
  assignment controls, target read/write controls, all four TEC nodes, and an
  initialized signal tree without a `loading...` state.
- `/api/log/export` followed by `/api/log/import/review` accepted recent
  telemetry in review mode with no duplicate sequence IDs.
- `deploy/verify_pixtend_route.sh` passed against the live route. The latest
  run proved sequence advance, 208 decoded targets in the tail window, 32 raw
  CAN ring records from the RAM ring, 220 discovery targets, 16 writable paths,
  high-priority freshness within 30 seconds for 80 values, Loom discovery alias
  JSON contracts, no-lease write rejection, 144 Loom/SignalForge catalogue
  entries, live graph-wall points, aggregate pseudo-target tile resolution,
  Arrow IPC export, and NDJSON export/import review without duplicate sequence
  IDs.
- `deploy/verify_loom_gateway_route.sh` passed through the local Loom/operator
  gateway at `http://127.0.0.1:18087/api/operator/meerstettergo`, proving the
  same health, telemetry freshness, catalogue, discovery, write-guard,
  graph-wall, RAM/flash merge, and NDJSON review checks through the backend
  route the UI is expected to use.
- `deploy/verify_pixtend_recovery.sh` passed after restarting
  `meerstettergo.service`, proving telemetry resumes, graph-wall temperature
  tiles recover live points, RAM/flash merged CAN ring remains readable without
  duplicate frame keys, and both Pi services stay active.
- `deploy/verify_pixtend_ring_recovery.sh` passed after restarting only
  `pixtend-can-ring.service`, proving decoded telemetry resumes, the primary
  RAM raw-CAN ring advances again, the flash fallback remains readable, the
  merged RAM/flash ring remains deduplicated, and graph-wall temperature tiles
  still have live points.
- `deploy/verify_mvp_completion.sh` passed with `RUN_RECOVERY=1` after the
  route-alias fix, covering direct edge, Loom gateway, edge autonomy, browser
  UI smoke, decoder restart recovery, CAN-ring-worker restart recovery,
  targeted Meerstetter-Go tests, and targeted Loom adapter tests.

## Routing Contract

- The browser, standalone Meerstetter-Go UI, Loom source catalogue, and
  sequencer-facing routes use the same target model.
- CAN is the preferred active route. Serial FTDI routes remain exposed as
  redundant device-server-style paths in target metadata.
- Consumers read decoded telemetry from `/api/log/ring` and raw CAN capture from
  `/api/can/ring`.
- The hot capture path is the RAM ring. The flash ring is a fallback/bootstrap
  copy for reboot, late ownership, or owner reconnection gaps.
- Duplicate avoidance is sequence-driven: consumers should treat flash replay and
  controller-ring recovery as gap-fill sources, not additional live streams.

## Verification Commands

```sh
./deploy/verify_pixtend_route.sh
BASE_URL=http://192.168.6.229:18080 ./deploy/verify_pixtend_route.sh
./deploy/verify_ui_browser_smoke.sh
BASE_URL=http://127.0.0.1:18087 ./deploy/verify_loom_gateway_route.sh
MAX_SAMPLE_AGE_SECONDS=30 FRESHNESS_NAMES=object_temp_c,sink_temp_c,target_object_temp_c,output_current_a,output_voltage_v ./deploy/verify_pixtend_route.sh
./deploy/verify_pixtend_recovery.sh
./deploy/verify_pixtend_ring_recovery.sh
```

```sh
curl -fsS --max-time 5 http://192.168.6.229:18080/api/health
curl -fsS --max-time 5 'http://192.168.6.229:18080/api/log/ring?tail=true&limit=800'
curl -fsS --max-time 5 'http://192.168.6.229:18080/api/discovery/tree'
curl -fsS --max-time 5 'http://192.168.6.229:18080/api/loom/source-catalogue'
```

```sh
ssh -i /home/svc_pmg_testbed_b/.ssh/router_lan_can pi@192.168.6.229 \
  'systemctl is-active meerstettergo.service pixtend-can-ring.service; ip -details link show can0'
```

## Remaining Hardening

- Validate the exact Meerstetter controller ring-buffer primitive on live
  controllers and encode wrap/limit behavior in tests.
- Add measured bus-capacity budgets for 4, 8, and 16 controller cases.
- Extend the current route-level reconnect/resume proof to real process-stop
  owner timing and controller ring-buffer gap-fill without duplicate samples.
- Promote the live PiXtend config and service layout into deployment-owned
  packaging with explicit site-local/private values separated from public-safe
  examples.
