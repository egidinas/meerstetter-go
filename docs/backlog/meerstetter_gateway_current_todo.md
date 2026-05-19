# Meerstetter Gateway Current TODO

Last updated: 2026-05-19

This is the restart-safe backlog distilled from the current operator-review
chat. It is intentionally broader than the code diff so a future agent can
resume after compaction without needing the transcript.

## Non-negotiable Architecture Invariants

- SignalForge owns reusable graph/tile/source-catalogue/control UI primitives.
  Meerstetter-Go, Gossamer, and Loom consume those primitives and only keep
  product-specific shaping locally.
- The canonical graph path is SignalForge graph tiles rendered with
  `signalforge.tile.uplot` / `UPlotTileRenderer`. Do not add a local canvas,
  sparkline, or custom uPlot adapter as the canonical renderer.
- If a generally useful primitive is missing, add it to SignalForge first:
  tile history helpers, JSON signal catalogue loading, catalogue projections,
  unit metadata, semantic hover cards, assignment stores, graph style options,
  zoom controls, and graph-wall layout primitives.
- Gossamer must stay public-safe and consume public SignalForge only. No Loom,
  `loom-gossamer-shared`, or private-path imports.
- Loom must also be brought into the same graph/tile/catalogue compliance once
  Meerstetter-Go and Gossamer are aligned.
- Public deployment should serve the latest intended version. If router routing
  is involved, prefer Linux-hosted services with current Go/Node toolchains and
  let the router only route/proxy.

## Gateway Routes, Redundancy, and Transport Semantics

- Treat serial communication as first-class, not merely fallback. Some users
  may use serial as the primary path.
- `cmd/mecomvseriald` should stay explicit about its scope:
  - it accepts downstream `serial:` and `tcp:` MeCom endpoints;
  - it rejects `can:` routes until a typed CAN adapter bridge is provided.
- Supported/desired route families:
  - `Kvaser USB CAN`: direct USB CAN path to TEC controllers.
  - `USB FTDI RS485`: serial MeCom path.
  - `PiXtend CAN`: Raspberry Pi / PiXtend CAN route.
  - Remote router hop only as fallback when local network paths are unavailable.
- Fix misleading UI route labels:
  - PiXtend CAN must not be shown as warm serial/FTDI.
  - Use short labels in operator surfaces: `Kvaser USB CAN`,
    `USB FTDI RS485`, `PiXtend CAN`.
- Show route redundancy explicitly on device cards:
  - hot route,
  - warm/standby route,
  - fallback route,
  - negotiated capabilities,
  - route health,
  - per-route read/write statistics where the backend exposes them.
- Do not call a route a priority lane unless it supports controller-internal
  ring / CRTVStream readout. Generic CAN sampling is not the same thing.
- Add optional boost mode:
  - capability-negotiated,
  - e.g. full buffer readout over CAN or serial where supported plus lazy reads
    on another route,
  - show state in settings and on device cards,
  - report per-bus statistics in boost mode.
- 2026-05-19 CoSo bridge/write-readback checkpoint:
  - Address-zero routing is runtime deployment policy, not a hardcoded serial
    number. Public defaults keep `MECOM_ADDRESS_ZERO=disabled`; site
    deployments may use `route-order` for deterministic discovery or a fixed
    configured MeCom address only as an operator override.
  - The live user bridge on `:50000` has been changed from `ADDRESS_ZERO=76`
    to `MECOM_ADDRESS_ZERO=route-order` and restarted. It now listens with
    `-address-zero route-order`.
  - `mecomserver` request handling now propagates per-request cancellation to
    downstream serial/TCP exchange and closes blocked downstream connections
    when clients disconnect or request timeout expires, preventing stale
    occupied sessions and `CLOSE-WAIT` buildup.
  - Gateway command writes now close the lifecycle loop for numeric known
    parameters by readback-verifying completed commands and returning/recording
    `readback_mismatch` (HTTP 409) when confirmed value differs from requested
    value.
  - Generic CLI `mecomset -set` now infers catalogue datatype; use
    `-set-int`/`-set-float` only to force type.

## Readout Scheduling and History

- The priority data lane is the controller-internal ring-buffer readout. Use it
  for priority signals when supported.
- Keep a short full-resolution hot buffer, target 15 minutes, for:
  - immediate inspection,
  - signal noise improvement by oversampling,
  - fault investigation,
  - per-signal preview graphs.
- Long-term default history should be a bounded 3-day ring when no explicit
  recording is running.
- Raw device samples may be pulled at full rate, but the default long-term tile
  path should be smoothed/decimated to 1 Hz unless a logging setting requests
  full resolution.
- Use oversampling for noise improvement and store the derived/smoothed data in
  the long-term tile ring. Preserve raw data only for the hot buffer or explicit
  full-resolution logging.
- Interleave:
  - priority hot/ring reads,
  - opened-tree boosted lazy reads,
  - normal lazy round-robin reads,
  - very-low-priority static metadata reads.
- Opened signal-tree values should not show stale data silently. Add visible
  age indicators and boost reads for opened values.
- Lazy reads must be initialized, so dictionary rows do not stay empty with
  missing quality.
- Archive support needs both export and import. Imported archives must be able
  to seed graph tiles/history for review.

## Graph Wall and Tile Contract

- 2026-05-19 implementation checkpoint:
  - Gateway poll/read paths now mirror successful reads into graph history, so
    live polling can seed backend graph tiles instead of only an in-browser
    buffer.
  - Graph tiles no longer fabricate zero-valued points for missing series.
    Missing/unreadable series are returned empty with `quality: missing` and
    diagnostics so disconnected sensors do not corrupt autoscale.
  - Live/minute graph tiles now use a 15-minute in-memory raw hot buffer, so
    same-second samples are not collapsed before the browser can inspect
    signal noise and recent transitions.
  - Longer graph tile windows continue through the SignalForge `tilehistory`
    path as a bounded 3-day 1 Hz mean/reduced history tier.
  - Fleet hero graph assignments seed all configured temperature and supply
    channels. SN76 remains the origin: non-origin traces and disconnected
    temperature traces are deselected by default, not deleted.
  - The UI uses SignalForge `UPlotTileRenderer` plus `SharedTimeAxis` for the
    hero/wall tiles, with Auto X, Auto Y, and manual Y bounds exposed.
  - Temperature-controller setting rows now show same-channel live electrical
    readings: measured power, voltage, and current.
  - Follow-up verification: graph tiles must preserve every configured
    temperature-control channel by exact `device:param:instance` identity.
    Missing or detached/open sensors stay in the tile and legend with
    `default_visible: false`, empty/non-null history arrays when unreadable,
    and a visible quality/reason, so operators can re-enable them deliberately
    without letting them corrupt default autoscale.
  - 2026-05-19 live verification on `:18082`:
    - fleet graph tiles preserve exact `device:param:instance` series identity
      from backend to browser legend;
    - SN76/SN75 detached or missing object-temperature channels remain present
      in the temperature tile but default to hidden, so they do not drive the
      default y-axis range;
    - all eight configured supply power channels read live `1022` values and
      display milliwatt-level measurements instead of being rounded to zero;
    - SN76 channel 1 cascade enable reads off (`53120:1 = 0`); the visible
      ~45 °C value is parameter `3000` target/control-loop temperature, while
      object temperature `1000:1` is a detached/open sensor and should remain
      hidden by default;
    - browser verification confirmed SignalForge `signalforge.tile.uplot`
      rendering, visible time-axis DOM, and `°C` axis/legend formatting after
      refreshing the vendored SignalForge web package.
  - 2026-05-19 channel-identity guardrail:
    - SignalForge now exports `graphSeriesIdentityKey` as the shared graph
      series identity primitive, so consumers do not maintain local fallbacks
      that accidentally key by display label;
    - Meerstetter-Go consumes that SignalForge primitive for tile/legend
      identity instead of keeping a local parser;
    - `cmd/mecomgw` has a gateway contract test proving graph tile live reads
      preserve exact `device:param:instance` identity across sibling devices
      and channel instances;
    - the same test keeps detached and missing sensors in the tile payload but
      default-hidden with explicit quality, so operators can deliberately
      re-enable them without allowing open sensors to drive autoscale.
  - 2026-05-19 exact-read freshness checkpoint:
    - `/api/devices/{id}/read?params=param:instance` now stamps successful
      per-value reads with `at` and `age_ms`, including fallback single-value
      reads after a failed bulk read;
    - the live frontend queues exact lazy reads for opened dictionary values
      that are missing or stale, so non-priority catalogue entries initialize
      from the gateway instead of staying empty forever;
    - frontend semantics tests now guard the per-channel polling bundles:
      temperature channels must read object, sink, target, cascade, and
      same-channel electrical values for their own instance; supply channels
      must read measured current/voltage/power plus voltage/current/output
      controls for their own instance;
    - backend command tests now cover temperature-target writes (`3000`) and
      require command activity to preserve exact device, parameter, instance,
      unit, requested value, and lifecycle metadata;
    - disconnected/open temperature sensors remain selectable in the tile and
      legend but default to hidden and are excluded from default autoscale;
    - stored cascade targets (`53123`) are guarded to render only when live
      cascade enable (`53120`) is active, so inactive cascade settings do not
      look like active control-loop state;
    - the currently served `:18082` process was restarted after this change so
      local/LAN UI and API responses match the checked source.
  - 2026-05-19 final gateway checkpoint:
    - Write-readback verification is now automated in `mecom.Commander` and
      propagated to the gateway with `409 Conflict` on mismatch.
    - Standardized CAN/serial adapter naming and route labels in the catalogue.
    - Implemented `/api/log/export` and `/api/log/import` for unified telemetry,
      command, and dictionary archives.
    - Imported archives now correctly seed the `graphHistory` cache for live
      tiles.
  - Remaining: promote the 15-minute full-resolution hot buffer and 3-day
    downsampled ring from in-memory gateway state to a durable backend store
    fed by the controller-internal ring/priority lane where supported.
- The UI must initialize graphs from backend-served tile history, not only from
  a live in-browser buffer.
- A late UI connection should load standard history by default:
  - all available setup history if permanent recording is active,
  - otherwise the bounded 3-day ring.
- Keep a visible time axis on graph tiles.
- Add manual zoom controls:
  - pan/zoom through uPlot/SignalForge primitives,
  - auto X,
  - auto Y,
  - user-editable top and bottom bounds,
  - a clean reset to backend/tile defaults.
- Consider 15-minute preview tiles and 3-hour tiles in addition to live, minute,
  hour, day, and 3-day levels. Choose tile sizes based on practical operator
  use, not arbitrary tiers.
- Default graph assignment rules:
  - SN76 is the origin/test controller and should be listed first.
  - Default hero graphs should show origin controller values first and hide
    other controllers by default until selected.
  - Graph assignment from the catalogue defaults to a new graph for the current
    value unit; do not mix units unless the operator intentionally chooses that.
- Separate default graph units:
  - temperature graph,
  - power graph,
  - voltage graph,
  - current graph.
- For power-supply channels, show power by default. Voltage and current can be
  added by user choice.
- Do not bunch voltage/current/power with temperature.
- Temperature graph labels should be compact for arbitrary controller counts:
  - object temperature: `OT`,
  - sink temperature: `ST`,
  - nominal object temperature: `NOT`,
  - then `SN-channel` shorthand and custom nickname if present.
- If cascade control is active, show cascade target temperature and control-loop
  target temperature. If cascade is not active, show only the control-loop
  target temperature.
- Hide clear detached-sensor outliers from autoscale by default, e.g. open
  temperature channels around -60 °C, but make the filtering visible/toggleable.
- The two main graphs must share a pixel-perfect aligned timeline/x-axis and
  use the available vertical space without wasted blank area.

## Signal Catalogue and Semantic Metadata

- The catalogue must be JSON-driven, not hardcoded in frontend code.
- Support multiple tree projections over the same canonical signal catalogue.
  The tree shape may override how a group is represented without changing the
  underlying truth.
- Seed catalogue JSON from all available sources:
  - MeCom protocol definitions,
  - CAN/EDS definitions,
  - datasheets,
  - latest Meerstetter TEC configuration software value lists,
  - local/default configuration,
  - safe exploratory reads around known MeCom ID ranges.
- Include documented and newly discovered readable parameters, including poorly
  documented values such as `40000` output-stage temperatures.
- Harvest useful metadata from Meerstetter TEC configuration software where
  legally and practically possible:
  - tooltip/help text,
  - limits,
  - defaults,
  - data type,
  - units,
  - access mode,
  - warning/safety text,
  - grouping hints.
- Public repo data should stay polished and safe. Non-public or manufacturer
  hidden details should be documented locally/private where appropriate, not
  leaked into public artefacts.
- Every catalogue value should expose, when known:
  - display name,
  - MeCom ID,
  - instance/channel,
  - data type,
  - unit and long unit label,
  - limits,
  - default value,
  - read/write access,
  - source/evidence,
  - visibility,
  - readout priority,
  - preferred readout route,
  - quality,
  - age/staleness,
  - tooltip/help text.
- Render `degC` as `°C` in values and `Degree Celsius` in long tags/tooltips.
- Default signal tree state should be collapsed.
- Top-level Receive PDO / communication-parameter detail must be hidden under
  collapsed diagnostics/details groups by default.
- Sort and group values to surface operator-relevant items first, with
  diagnostics/debug/details behind secondary groups.
- Clicking a group/subgroup count should open a detail list view. Clicking the
  title should still collapse/expand the tree.
- The detail list should bundle by device card first, then by instance inside
  the device card, leaving horizontal space for metadata.
- Always visually separate values belonging to different TEC controllers.
- Show matching telemetry and telecommand/control values together as a
  user-friendly projection, while preserving backend canonical entries.
- Preserve the full write lifecycle for paired values:
  - current value,
  - prospective new value,
  - actual write request,
  - confirmed new state or rejection.
- A single-value hover popup should show the full semantic metadata that flows
  from backend to frontend.
- Clicking a value should be able to open a small recent-history preview graph
  from the 15-minute hot buffer, with an option to zoom out to the 3-day reduced
  ring.

## User Customization Overlay

- 2026-05-19 implementation checkpoint:
  - SignalForge now owns a generic semantic overlay web primitive for aliases,
    notes, labels, tags, hidden state, provenance, and import/export by
    semantic target.
  - Meerstetter-Go consumes that primitive for channel alias overlays under a
    separate `mecomgw.channelAliases` namespace.
  - The device signal tree can edit a channel alias/note inline; Settings can
    export/import the overlay JSON.
  - Canonical channel role defaults and the MeCom signal catalogue are kept
    separate; saved channel metadata strips user overlay fields before
    persistence.
- Allow modification of channel aliases/nicknames directly in the device signal
  tree.
- Store aliases/nicknames in a separate user customization file so they do not
  pollute the canonical signal map.
- The overlay should be importable/exportable and should preserve provenance:
  - device ID,
  - serial number,
  - channel/instance,
  - user alias,
  - optional fixture note,
  - updated timestamp,
  - author/source.
- UI labels should prefer `raw ID + alias`, not alias alone, so diagnostics
  remain unambiguous.
- Remaining gap: fixture notes and channel roles should become data-driven
  through this overlay or a similar separate configuration layer, not scattered
  as hardcoded strings.

## Fixture and TEC Controller Semantics

- SN76 is the origin/test controller and represents the bottom-right quadrant
  of the 8-OH fixture:
  - channel 1: front-right-bottom heat-sink thermal zone,
  - HR1: cascade control temperature,
  - LR1/LR2: heat-sink monitoring sensors and direct cascade-control input,
  - channel 2: power supply for that test spot,
  - channels 3 and 4: back-right-bottom heat-sink zones.
- SN75 respectively represents top-front-right and top-back-right.
- Pattern across controllers:
  - channels 1 and 3 are temperature-controller channels,
  - channels 2 and 4 are power-supply channels.
- Default setup may set temperature-controller targets to 25 °C and configure
  power-supply channels according to the fixture pattern, but this must be
  explicit, reversible, and command-logged.
- User notes are a TEC controller feature for writing ASCII data into
  controller non-volatile storage. Add large-data/string write support before
  exposing this as a command surface.

## Power-Supply and Derived Control UX

- The power-supply interaction must not make one button do double duty. Voltage
  or current priority mode should be implicit from which limiting value is
  active.
- Expose current and voltage limits directly.
- When setting a power-supply channel, always show the corresponding live data
  at full precision/resolution, including low mW-level measurement noise.
- If a write such as SN76 channel 2 at 28 V does nothing, the UI must show the
  command lifecycle and the confirmed live state so this is diagnosable.
- Add derived control concepts where appropriate:
  - power target that tracks changing load resistance,
  - total dissipated power limit adjusted by the TEC power model.
- General derived-control primitives should live in SignalForge unless they are
  strictly TEC-specific.

## Command Activity, Leases, and Authority

- Command activity must show every staged, sent, accepted, rejected, completed,
  or failed command. No silent writes.
- Command activity should be a right-side accordion/drawer, visible by default
  and hideable.
- Lease ownership changes infrequently and can sit at the bottom of the command
  activity drawer by default.
- Command rows need clear units and meaning for all counters and rates. If a
  statistic is frames, bytes, kB, or gateway-global, label it exactly.
- Write authority and route provenance must be clear for each command.

## UI Layout and Interaction

- The Claude Design package is a visual goal, not the functional source of
  truth. Final behavior must be backend-driven from the real TS source.
- Expand user-facing abbreviations where space allows:
  - Meerstetter Gateway instead of `mecomgw`,
  - temperature instead of `temp`,
  - command/setpoint instead of `cmd` where appropriate,
  - measured instead of `actual`,
  - CAN bus where helpful.
- Keep compact technical IDs where useful:
  - parameter IDs,
  - units,
  - endpoint strings,
  - renderer identity,
  - device IDs.
- Move low-value top-bar status into the sidebar or command drawer; preserve
  vertical graph space.
- Signal catalogue belongs in the operate sidebar by default, not as a separate
  page replacing the hero graphs.
- Device info belongs in the left accordion below the signal catalogue.
- Side accordions should be user-resizable by mouse drag.
- Pinned value cards should sit above the graph wall and support value display,
  current setpoint, and write action where writable.
- Lower duplicate telemetry/telecommand blocks should be removed after their
  contents are available through the signal dictionary and pinned cards.
- Reduce dead whitespace throughout.
- All values shown in UI need clear meaning, proper units, and hover/help text
  where available.
- Provide local and remote UI links after deployment, including bearer-token
  remote access if configured.
- Browser verification is required before claiming the public/local UI works.

## Archive, Import, and Deployment

- History archive must have an import function, not only export.
- Imported history should initialize tiles and graph previews.
- The web UI should be reachable locally and remotely when configured; remote
  bearer-token links should be documented or surfaced by the gateway.
- Keep upload/import flow for design UI packages, but final integration must
  land in `web/src/*.tsx`, not only exported JSX/HTML.
- Push verified UI work so external design/review agents can build on the
  actual current branch.

## Research and Private Notes

- Research the best .NET reverse-engineering/decompilation tools for harvesting
  usable metadata from Meerstetter software in an owned/legal context.
- Document useful patterns and discovered metadata carefully.
- Do not publish private passwords, bypasses, or proprietary hidden settings in
  the public repository. Private/non-public findings belong in the private Loom
  repo or local private notes with clear provenance.

## Validation Checklist

- Backend:
  - `go test ./...`
  - route capability tests for serial/CAN/Kvaser/PiXtend labels,
  - catalogue JSON load tests,
  - tile/history tests including late UI initialization.
- Frontend:
  - `npm --prefix web run build`
  - contract tests if present,
  - browser smoke test at the served `/ui/` endpoint,
  - DOM check for `data-graph-renderer="signalforge.tile.uplot"`,
  - no local canvas graph-wall fork.
- Guards:
  - no forbidden private imports,
  - no hardcoded signal catalogue in TSX except graceful fallback labels,
  - no misleading route labels,
  - no empty visible catalogue rows after lazy initialization,
  - no missing time axis on graph tiles.
