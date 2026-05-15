# Plan — SignalForge shared UI module

Status: **draft, awaiting review**. Author: Claude Opus 4.7 (2026-05-15).
Related: [`UI_GRAPH_WALL_CONTRACT.md`](./UI_GRAPH_WALL_CONTRACT.md), backlog ids
8/9/10/11/13 in [`../backlog/frontend_hooks.jsonl`](../backlog/frontend_hooks.jsonl).

## Why

Three consumers want the same operator surface:

- **meerstetter-go** — `docs/gateway/console/` served by `mecomgw -ui-dir`. In-browser
  Babel JSX, React 18 from unpkg, no build step.
- **gossamer/web** — Vite + TypeScript + React 19 + uPlot, full npm pipeline.
- **loom/web** — Vite + TypeScript + React + uPlot. The premium parent of
  gossamer (gossamer is the junior derivative). Already imports uPlot
  directly; ships its own `HeroGraph`, `OperatorGraphPrimitives`,
  `operatorGraphCanvas`, source-catalogue contract tests. No dict/wall
  primitive yet but the rest of the operator vocabulary is fully present.

The directive: extract a shared SignalForge UI module so all three apps consume
the same **signal dictionary**, **wall-management**, **uPlot tile renderer**,
and **tile-pyramid client** code. Today no app has a build-time link to the
others; dict and renderer are copy-paste candidates that will drift the moment
we stop watching.

**Lineage and starting points:** Loom is the premium parent; gossamer is the
junior derivative carrying a working implementation of the graph contract.
For the shared graph primitives, **gossamer is the chosen baseline** — its
`uPlotAdapter.ts`, `markers.ts`, `decimation.ts`, `timeAxis.tsx`,
`visualPolicy.ts` are the extraction source. Loom-specific features beyond
gossamer's contract (testbed view, librarian, capability-module browser, etc.)
remain loom-local and are not in this plan's scope. Meerstetter-go is the
toolchain outlier and gates Phase 1.

**Cross-cutting principle: history is uncapped in principle on every consumer.**
Whatever the backend can produce — RAM ring, flash ring, HDF5 archive, future
storage — is what the operator sees, paged through the tile pyramid. No
arbitrary client-side window or buffer cap. Today's caps are accidents of the
current implementations:

- meerstetter-go `components.jsx:10` `TELE_MAX = 720` (~6 min at 500 ms cadence)
- meerstetter-go `MultiChart` `timeWindowMs = 90_000` (90 s render window)
- gossamer/loom currently fetch fixed-resolution graph models per campaign

The end state replaces all three with the SignalForge tile-pyramid contract
(`graph_tile.v1`, levels `live`/`minute`/`hour`, more added as backends grow)
and a `TileClient` that picks the right tier for the current zoom. Initial
backends per consumer:

- meerstetter-go: RAM-ring + flash-ring per backlog id 11, later HDF5.
- gossamer: existing fixture pyramid + future live data.
- loom: whatever loom's backend already serves; tile client adapts.

## End state

- `signalforge/web/` exists, ships ESM as the primary artefact. All three
  consumers are ESM by Phase 3, so UMD is not built unless a fourth
  script-tag consumer ever appears.
- meerstetter-go console is a Vite + TS + React 18 app at `meerstetter-go/web/`,
  serving `web/dist/` via `mecomgw -ui-dir`.
- All three apps (meerstetter-go, gossamer, loom) import dict view, walls
  hook, tile renderer, and tile client from `signalforge-web`.
- Loom's local `HeroGraph` / `OperatorGraphPrimitives` / `operatorGraphCanvas`
  reduce to thin adapters over the shared primitives.
- `docs/gateway/console/` deleted after one full release of stability behind
  `web/dist/`.

## Phase 1 — meerstetter-go onto Vite + TS + React 18

**Goal:** the in-browser-Babel console becomes a built bundle, **no behaviour
change visible to the operator**.

### Files added

| Path | Purpose |
|---|---|
| `web/package.json` | npm + scripts, mirrors gossamer/web layout |
| `web/vite.config.ts` | Vite, React plugin, base `./` for relative serving under `/ui/` |
| `web/tsconfig.json` | strict TS, React JSX, `module: "ESNext"` |
| `web/index.html` | replaces `docs/gateway/console/index.html` |
| `web/src/main.tsx` | bootstraps `App` from existing JSX, ported to TS |
| `web/src/{app,components,views-*,tweaks-panel}.tsx` | mechanical port of the seven `.jsx` files |
| `web/src/mock.ts` | mock API client (no JSX in the source file, port as `.ts`) |
| `web/src/styles.css` | copy of `docs/gateway/console/styles.css` (kept identical for diff sanity) |
| `.gitignore` additions | `web/dist/`, `web/node_modules/` |

### Files modified

| Path | Change |
|---|---|
| `cmd/mecomgw/main.go` | no change — `-ui-dir` already accepts any path; deploy passes `web/dist/` instead of `docs/gateway/console/` |
| `scripts/build-gateway-handoff.sh:18` | replace `docs/gateway/console` bundle line with `web/dist` after a `npm --prefix web run build` step |
| `deploy/example-scripts/*` (any that reference the console path) | update to `web/dist/` |
| `docs/gateway/HANDOFF.md` | document the new build step |

### Files preserved (unchanged this phase)

- `docs/gateway/console/*` stays in tree as the rollback target. Removal happens at
  the start of Phase 3 only after `web/dist/` has shipped one stable release.

### Cutover steps

1. Scaffold `web/` (config + empty `main.tsx` rendering "hello").
2. Port files in dependency order: `mock` → `components` → `tweaks-panel` →
   `views-hero` → `views-accordion` → `views-dict` → `views-main` → `app`. Each
   port keeps the same exports; only changes are JSX → TSX, module imports,
   removing the Babel-standalone `/* global */` comments, fixing implicit-any.
3. `npm --prefix web run build` succeeds.
4. Local manual: launch `mecomgw -ui-dir web/dist`, exercise both heroes, dict
   view, command cards. Capture before/after screenshots at the same viewport.
5. Update `scripts/build-gateway-handoff.sh` to bundle `web/dist`.
6. Update Brume2 RPi deploy script to `npm install && npm run build` before
   shipping (or pre-build and ship the artefact only).

### Acceptance criteria (Phase 1)

- `npm --prefix web run build` succeeds with no TS errors.
- `mecomgw -ui-dir web/dist` serves a console functionally identical to the
  pre-migration one (verified by side-by-side screenshot diff and clicking
  through every tab + dict assignment + command card).
- `scripts/build-gateway-handoff.sh` produces a working tarball.
- `go test ./...` still green (Phase 1 doesn't touch Go).
- Old `docs/gateway/console/` still serves correctly via the same `-ui-dir` flag —
  rollback is "point `-ui-dir` back at the old path."

### Rollback (Phase 1)

`mecomgw -ui-dir docs/gateway/console` reverts. No data migration. Time-to-revert
< 1 minute.

---

## Phase 2 — `signalforge/web/` shared module

**Goal:** extract the dictionary, wall management, and tile renderer/client into
SignalForge so all three apps import them.

### Module layout

```
signalforge/
  web/
    package.json            (name TBD — see open question 4; version pinned)
    tsconfig.json
    vite.config.ts          (lib mode → ESM; UMD only if needed)
    src/
      index.ts              (public exports)
      types/                (graph_tile.v1, wall config, assignment types — re-exported)
      dict/
        SignalDictionary.tsx
        SignalBlock.tsx
        ChannelsEditor.tsx
        useAssignments.ts   (load/save/forWall/hasAssignment, localStorage keys configurable)
      walls/
        WallManager.tsx     (list / create / rename / delete)
        useWalls.ts         (localStorage-backed)
      render/
        UPlotTileRenderer.tsx    (port of gossamer uPlotAdapter — sole renderer; meerstetter-go's drawCanvasChart is retired)
      tiles/
        TileClient.ts       (fetches /api/.../tiles?level=…, picks tier by zoom, caches)
        useTileSeries.ts    (React hook fronting TileClient)
    dist/
      signalforge-web.es.js
      signalforge-web.d.ts
```

### What's extracted from where

| Origin | Becomes |
|---|---|
| `meerstetter-go/docs/gateway/console/views-dict.jsx:14` `SignalDictionaryView` | `signalforge/web/src/dict/SignalDictionary.tsx` |
| `views-dict.jsx:135` `SignalBlock`, `MonitorRow`, `CommandRow` | `dict/SignalBlock.tsx` (data adapters injected, not hardcoded to MecomAPI) |
| `views-dict.jsx:374` `ChannelsEditor` | `dict/ChannelsEditor.tsx` (mecom-specific bits stay in meerstetter-go, this becomes a generic device/role editor with injected device adapter) |
| `views-hero.jsx:92` `useAssignments` + `loadAssignments`/`saveAssignments` | `dict/useAssignments.ts` (localStorage key prefix injected) |
| `views-hero.jsx:35` `WALLS` const + `wallForDevice` | `walls/useWalls.ts` — promoted from a hardcoded const to a user-managed list. `wallForDevice` becomes a fallback factory for synthetic per-device walls (kept, callers that don't pre-create a wall still get one). |
| `components.jsx:9-25` `TELE_BUF` + `recordTelemetry` + `getTelemetry` | replaced by `tiles/TileClient.ts` for `minute`/`hour` tiers. A small live-tier buffer (~90s) may stay in `MecomCatalogueAdapter` to back `useLiveValue`, but is no longer the source of chart history. |
| `components.jsx:192` `drawCanvasChart` + `MultiChart` | **deleted**. Replaced by `render/UPlotTileRenderer.tsx` driven by gossamer's adapter. Pulls uPlot into meerstetter-go for the first time (~50KB gz). |
| gossamer `web/src/components/tiles/uPlotAdapter.ts` + `markers.ts` + `decimation.ts` + `timeAxis.tsx` + `visualPolicy.ts` | `render/UPlotTileRenderer.tsx` and helpers under `render/` |
| gossamer `web/src/api.ts` tile-fetch logic | generalized into `tiles/TileClient.ts` |
| `signalforge/graphwall/graphwall.go` types | TS mirror in `web/src/types/`, kept in lock-step via OpenAPI/codegen |

### Boundary contract — adapters, not couplings

The shared module is **agnostic** of any one device API. Each app injects:

```ts
interface SignalCatalogueAdapter {
  list(): SemanticSignal[];          // group/subgroup/name/unit/role/etc.
  channelsForSignal(s): Channel[];   // device + instance pairs
  // Subscribe to the latest value; returns an unsubscribe fn. No RxJS.
  subscribeLive(deviceId, paramId, instance,
                cb: (snap: { value: number | null; quality: string }) => void): () => void;
  formatValue(value, unit, paramId?): string;
  write?(deviceId, command, leaseToken): Promise<void>;
}

interface TileAdapter {
  fetchTile(wallId, cardId, level): Promise<GraphTile>;
  // levels: "live" | "minute" | "hour" — server picks what it has
}

interface AssignmentsStore {
  // localStorage namespace keeps consumers from colliding if ever co-hosted.
  // meerstetter-go uses "mecomgw"; gossamer uses "gossamer"; loom uses "loom".
  namespace: string;
}
```

meerstetter-go provides a `MecomCatalogueAdapter` and `MecomgwTileAdapter` (the
latter wraps backlog id 11's history endpoints). gossamer provides
`CampaignCatalogueAdapter` and a static-fixture `TileAdapter`.

### Versioning and consumption

Tag `signalforge-web@0.1.0` once Phase 2 lands. Both consumers pin to the tag.
Breaking changes require a minor bump and explicit consumer-side upgrade.

**Consumption mechanism (decision needed):**

- *npm publish to a registry* (npmjs.com or a private registry) — cleanest, but
  requires an account and CI pipeline.
- *git URL in package.json* (`"signalforge-web": "git+https://.../signalforge.git#v0.1.0&path=web"`) —
  works today, no registry needed, but git+path subdir support is shaky and
  some lock-file flows misbehave.
- *git submodule + relative path* — robust but adds submodule overhead to both
  consumers. Recommended fallback if no registry account is available.

Default to **git URL** for the v0.1.x line; promote to npm publish once the
package shape is stable.

### Acceptance criteria (Phase 2)

- `signalforge/web/dist/` builds clean (ESM + types).
- Unit tests cover the assignments store, wall manager, and tile-tier picking
  end-to-end — these three are pure logic and should have meaningful test
  cases, not just smoke coverage.
- A demo page in `signalforge/web/demo/` renders the dict + a wall + a tile from
  static fixtures so SignalForge contributors can iterate without either consumer.

### Rollback (Phase 2)

Phase 2 doesn't change either consumer yet. If the shared module is unusable, no
consumer is harmed.

---

## Phase 3 — all three consumers adopt the shared module

**Goal:** delete duplicate code. After this phase, the dict, walls hook, and
tile renderer live in exactly one place — consumed by meerstetter-go,
gossamer, and loom.

### meerstetter-go

1. `npm --prefix web add @egidinas/signalforge-web@^0.1.0`.
2. Replace `web/src/views-dict.tsx` with a thin wrapper that constructs the
   `MecomCatalogueAdapter` and renders `<SignalDictionary adapter={...} />`.
3. Replace `web/src/components.tsx` `MultiChart` with `<CanvasTileRenderer
   tileAdapter={mecomgwTileAdapter} wallId={...} />`.
4. Replace `WALLS` const with `useWalls()` from the shared module + a "+ New
   wall" button on the main view.
5. Delete `docs/gateway/console/` (one release after Phase 1 cutover proven
   stable).

### gossamer

1. `npm --prefix web add signalforge-web@^0.1.0` (mechanism per Versioning section).
2. Add a new operator-side route (e.g. `/operator/wall-config`) that hosts
   `<SignalDictionary adapter={campaignAdapter} />` so users can build/edit walls
   alongside the existing campaign-driven views.
3. Replace `OperatorGraphWall.tsx` internal tile rendering with
   `<UPlotTileRenderer ... />` from the shared module.
4. Existing campaign-driven walls remain — `useWalls` simply has a "preset"
   bucket (campaign-defined) alongside the user-managed bucket.

### loom

1. `npm --prefix web add signalforge-web@^0.1.0`.
2. Replace `web/src/HeroGraph.tsx` with a thin wrapper around
   `<UPlotTileRenderer ...>` driven by a `LoomTileAdapter` (wrapping whatever
   loom's current data source is — `operatorGraphCanvas.ts` callers).
3. Replace `OperatorGraphPrimitives.tsx`'s graph parts with shared imports.
   Non-graph operator primitives (testbed view, librarian, capability module
   browser, etc.) remain loom-local.
4. Add a wall-config surface using `<SignalDictionary adapter={loomAdapter} />`
   if loom wants user-managed walls; otherwise consume `useWalls` in
   read-only mode for the existing operator views.
5. Loom's many `validate-*` contract tests under `web/scripts/` provide a
   strong regression suite — every adoption commit must keep them green.

### Acceptance criteria (Phase 3)

- meerstetter-go: shared dict serves `+ New wall` flow end-to-end; new wall
  appears in main view, accepts assigned signals, renders tile-pyramid history.
- gossamer: new wall-config route works against tvac_qualification fixtures; no
  visual regression on existing campaign views.
- loom: every `web/scripts/validate-*.mjs` contract test stays green;
  hero-graph and operator-graph-primitive views render identically to
  pre-adoption.
- Bundle-size delta is reported per consumer. Meerstetter-go gains uPlot
  weight (resolved decision 1); gossamer and loom should be neutral or
  smaller (treeshaken shared module replaces duplicated local code).
- Zero duplicate `useAssignments`, `WALLS`, or tile-render code paths across
  the four repos (meerstetter-go, gossamer, loom, signalforge).

---

## Out of scope (this plan)

- HDF5 backend for backlog id 11. The shared `TileAdapter` interface is ready
  for it; the actual HDF5 reader is separate work.
- Marker-rail collision avoidance generalization (keep gossamer's `markers.ts`
  in-app for now; port if/when meerstetter-go starts emitting markers).
- New campaigns or new SignalForge contracts — this plan only moves existing
  code into shared homes.
- Mobile responsive work beyond what each app already has.
- Auth/lease changes to mecomgw.

---

## Risks

| Risk | Mitigation |
|---|---|
| Phase 1 breaks live console on Brume2 RPi | Keep `docs/gateway/console/` in-tree; rollback is one flag flip. |
| Vite build adds friction to "edit JSX, refresh browser" workflow | `npm run dev` HMR is faster than current Babel-standalone reload; teach the difference in HANDOFF.md. |
| Shared module API drift between Phase 2 and Phase 3 | Pin shared module to a tag; consumers upgrade explicitly, not via floating range. |
| meerstetter-go gains a 50KB uPlot dep it didn't have before | Accepted per resolved open question 1. Net win: one renderer to maintain, gossamer's marker/zoom/log-axis features become free for meerstetter-go. |
| meerstetter-go's mecomgw tile API doesn't exist yet (backlog id 11) | Phase 2's `TileAdapter` interface ships first; `MecomgwTileAdapter` is a stub returning live values until id 11 lands. Keeps the contract correct without blocking. |
| Consolidating three apps' API surfaces into a signalforge-owned OpenAPI is broader than \"port the dict\" | Per resolved open question 2. Phase 2 starts with meerstetter-go's existing `docs/gateway/openapi.yaml` as the seed and folds in gossamer's tile + graph-model routes plus loom's relevant operator/telemetry endpoints. Each consumer keeps its current routes during transition; the unified spec becomes truth at the start of Phase 3. |
| Loom has its own evolving operator vocabulary (HeroGraph, OperatorGraphPrimitives, capability-module browser) that may not map cleanly | Phase 3's loom step is deliberately incremental: only the graph + dict primitives migrate. Non-graph operator surfaces stay loom-local until they're ready to share. Loom's existing `validate-*` contract tests are the regression net. |

## Resolved decisions (2026-05-15)

1. **Renderer:** uPlot only. Meerstetter-go's `drawCanvasChart` is retired
   in Phase 3; meerstetter-go gains uPlot as a dep. One implementation, two
   consumers — matches the directive.
2. **OpenAPI ownership:** SignalForge owns the unified OpenAPI. Phase 2 seeds
   it from meerstetter-go's existing `docs/gateway/openapi.yaml` and folds in
   gossamer's tile + graph-model routes. The TS types in `signalforge/web/src/types/`
   are generated from this single source. Both consumers' Go and TS code import
   from there going forward; meerstetter-go's local copy is deleted at the end
   of Phase 3.
3. **localStorage namespacing:** the shared `useAssignments`/`useWalls` hooks
   take a required `namespace` config. meerstetter-go passes `"mecomgw"`,
   gossamer passes `"gossamer"`. No collision risk if both ever co-host.

## Open questions still pending

4. **Package scope name.** `signalforge-web`, `@signalforge/web`,
   `@egidinas/signalforge-web`, or unscoped? Affects the npm registry account
   needed (scoped packages on npmjs.com require an org). Defaulting to
   unscoped `signalforge-web` until decided.
5. **OpenAPI codegen tool.** `openapi-typescript` (lightweight, ESM-friendly)
   vs `openapi-typescript-codegen` (richer, generates clients too) vs
   hand-written types from the YAML. Lean toward `openapi-typescript` for
   types-only; clients stay hand-written so they can wrap auth/lease logic.

---

## Verification (end-to-end, after Phase 3)

1. `npm test` in `meerstetter-go/web/`, `gossamer/web/`, and `loom/web/` — green.
2. `go test ./...` in all relevant repos (meerstetter-go, gossamer, signalforge,
   loom) — green.
3. Manual: meerstetter-go gateway shows the existing two heroes plus one
   user-created wall containing two custom signals from the dict.
4. Manual: gossamer's tvac_qualification view renders unchanged; new
   `/operator/wall-config` route lets a user define a custom wall and it
   persists.
5. Manual: loom's hero-graph and operator views render unchanged from
   pre-adoption screenshots.
6. `signalforge/web/demo/` page works in isolation against fixture data.

## Estimated scope

- Phase 1: 2–3 days (port 7 JSX files + mock + Vite config + cutover + deploy
  script + Brume2 verification).
- Phase 2: 4–6 days (extraction, adapter design, OpenAPI consolidation across
  three apps, uPlot tile renderer, tile-pyramid client, demo page, tests).
- Phase 3: 4–6 days (three consumers wired + visual + contract-test regression
  checks + delete legacy console + gossamer wall-config route + loom
  HeroGraph/OperatorGraphPrimitives reduction).

Total: ~2.5 weeks of focused work, plus review and deploy soak.
