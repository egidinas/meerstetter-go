# Plan — SignalForge shared UI module

Status: **approved, ready for Phase 1 implementation** (2026-05-15).
Author: Claude Opus 4.7. Related: [`UI_GRAPH_WALL_CONTRACT.md`](./UI_GRAPH_WALL_CONTRACT.md),
backlog ids 8/9/10/11/13 in [`../backlog/frontend_hooks.jsonl`](../backlog/frontend_hooks.jsonl).

## TL;DR

1. Build meerstetter-go's console as Vite + TS + React 18 (Phase 1) — no
   behaviour change, the in-browser Babel pipeline goes away.
2. Extract the dictionary/wall/tile **contracts**, adapters, uPlot renderer,
   and tile-pyramid client into `signalforge/web/` (Phase 2) — shared module,
   ESM-only, backend-shape driven.
3. Wire all eligible consumers (meerstetter-go, gossamer, loom) onto the
   SignalForge-owned module and delete the duplicates (Phase 3).

History becomes uncapped on every consumer (cross-cutting principle below).
Renderer is uPlot only. SignalForge owns the neutral catalogue/wall/tile
contracts and adapter types. localStorage is namespaced per consumer. Gossamer
is free to consume SignalForge primitives, but it must not consume
`loom-gossamer-shared` or any Loom-private package. Reusable code migrates
through SignalForge; private or lab-specific behavior stays in Loom or its
deprecated compatibility archive until replaced.

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

The directive: extract a shared SignalForge UI foundation so eligible apps
consume the same **signal dictionary contract**, **wall-management contract**,
**uPlot tile renderer**, and **tile-pyramid client** fundamentals. Each backend
still owns the concrete catalogue, routes, fixture shape, campaign semantics,
and product-specific content; consumer adapters translate backend responses
into the SignalForge contracts. Today no app has a build-time link to the
others; the reusable renderer and contract glue are copy-paste candidates that
will drift the moment we stop watching.

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

- `signalforge/web/` exists, ships ESM as the primary artefact. The eligible
  consumers are ESM by Phase 3, so UMD is not built unless a fourth
  script-tag consumer ever appears.
- meerstetter-go console is a Vite + TS + React 18 app at `meerstetter-go/web/`,
  serving `web/dist/` via `mecomgw -ui-dir`.
- Eligible apps (meerstetter-go, gossamer, loom) import contracts, adapter
  interfaces, shared hooks, tile renderer, and tile client from
  `signalforge-web`; no consumer imports `loom-gossamer-shared`.
- Consumer backends remain the authority for content and specifics. The shared
  web module renders SignalForge-shaped data; it does not encode MeCom,
  Gossamer campaign, or Loom operator semantics directly.
- Loom's local `HeroGraph` / `OperatorGraphPrimitives` / `operatorGraphCanvas`
  reduce to thin adapters over the shared primitives.
- `docs/gateway/console/` deleted after one full release of stability behind
  `web/dist/`.

## Phase 1 — meerstetter-go onto Vite + TS + React 18

**Goal:** the in-browser-Babel console becomes a built bundle, **no behaviour
change visible to the operator**.

### Pre-flight checklist (before first commit)

- [ ] Working tree clean (`git -C meerstetter-go status` empty).
- [ ] Branch created from `main` (suggest `phase-1/vite-console`).
- [ ] Node ≥ 20 available (gossamer uses `^20`; match it).
- [ ] `mecomgw` binary buildable on the dev machine (`go build ./cmd/mecomgw`).
- [ ] Snapshot of current `docs/gateway/console/` rendered in a browser, with
      screenshots of: Heroes view, Signal Dictionary (Telemetry tab), Signal
      Dictionary (Telecommands tab), Channels editor, Settings panel. These
      become the reference for "no behaviour change."
- [ ] Brume2 deploy script located (`deploy/example-scripts/*` and any
      operator-side wrapper); identify which file currently passes
      `-ui-dir docs/gateway/console`.

### npm dependencies (mirror gossamer; pin majors)

```jsonc
// web/package.json — runtime
"dependencies": {
  "react": "^18.3.1",
  "react-dom": "^18.3.1"
}
// web/package.json — dev
"devDependencies": {
  "@types/react": "^18.3.12",
  "@types/react-dom": "^18.3.1",
  "@vitejs/plugin-react": "^4.3.4",
  "typescript": "^5.6.3",
  "vite": "^5.4.10"
}
```

(React 18, not 19, because the existing console code is React 18 and the
port should be mechanical. Upgrade to 19 is a separate decision later.)

### Vite + TS config (minimum viable)

```ts
// web/vite.config.ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig({
  plugins: [react()],
  base: "./",                       // relative paths so /ui/ subpath serving works
  build: { outDir: "dist", sourcemap: true, target: "es2022" },
  server: { port: 5174 }            // avoid clash with gossamer's 5173
});
```

```jsonc
// web/tsconfig.json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "skipLibCheck": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "esModuleInterop": true,
    "allowSyntheticDefaultImports": true
  },
  "include": ["src"]
}
```

### `MecomAPI` global → typed module

The existing console attaches `window.MecomAPI = { … }` from `mock.js` and
relies on `/* global MecomAPI */` comments in JSX files. Port:

1. `mock.ts` exports `mockMecomAPI` and a `live`-mode factory.
2. A small `src/api/mecom.ts` module exposes the same shape with a
   `setMode("mock" | "live")` switch (preserve the existing UI toggle).
3. JSX files import `mecomAPI` instead of reaching for `window.MecomAPI`.
4. The window assignment is kept in `src/main.tsx` for any debug use:
   `(window as any).mecomAPI = mecomAPI`.

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

### First commit — "scaffold builds, dist serves stub"

The minimum-viable first commit on `phase-1/vite-console`:

- `web/package.json`, `web/tsconfig.json`, `web/vite.config.ts`,
  `web/index.html` (links `/src/main.tsx` only), `web/src/main.tsx`
  (renders `<div>SignalForge console — Vite scaffold</div>` and nothing
  else), `web/.gitignore` (`node_modules/`, `dist/`).
- Root `.gitignore` patched so `web/node_modules` and `web/dist` aren't
  tracked.
- Verify: `npm --prefix web ci && npm --prefix web run build` succeeds;
  `mecomgw -ui-dir web/dist -listen :8081` serves the stub at
  `http://localhost:8081/ui/`.
- Commit message: `Phase 1: scaffold web/ Vite + TS + React 18`.

This commit is reversible by a single `git revert`; everything below builds
on it.

### Subsequent commits — port files in dependency order

One commit per file (or small group), each green-builds and green-renders:

1. `mock.ts` (no JSX, port first; verify `npm run build` still passes).
2. `components.tsx` — atoms (`Pill`, `Chip`, `Panel`), `MultiChart`,
   telemetry buffer. **Note:** `MultiChart` will be deleted in Phase 3, but
   ports as-is here to keep behaviour identical.
3. `tweaks-panel.tsx` (depends only on React).
4. `views-hero.tsx` — `WALLS`, `useAssignments`, `Heroes`. After this commit,
   `main.tsx` can render `<Heroes>` and verify in-browser.
5. `views-accordion.tsx`.
6. `views-dict.tsx` — `SignalDictionaryView`, `SignalBlock`, `MonitorRow`,
   `CommandRow`, `ChannelsEditor`.
7. `views-main.tsx`.
8. `app.tsx` — wires everything together.
9. `styles.css` — copy verbatim (do **not** rewrite to CSS modules in
   Phase 1; defer styling refactor).
10. `main.tsx` — replace stub with `<App/>` mount.

After commit 10, `web/dist/` is functionally equivalent to the legacy console.

### Cutover commits

11. `scripts/build-gateway-handoff.sh:18` — replace bundled directory from
    `docs/gateway/console` to `web/dist`. Add a build step at the top:
    `npm --prefix web ci && npm --prefix web run build`.
12. Brume2 deploy script (path identified in pre-flight) — point `-ui-dir`
    at `web/dist`. Pre-build locally and ship the artefact tarball; do **not**
    run npm on the Pi (low memory, no Node assumed).
13. `docs/gateway/HANDOFF.md` — document `npm run dev` for HMR development
    and the new build/deploy flow.

### Verification — Phase 1

```bash
# In meerstetter-go/
git checkout phase-1/vite-console
npm --prefix web ci
npm --prefix web run build                 # must succeed, no TS errors
go test ./...                              # must stay green
go build ./cmd/mecomgw
./mecomgw -ui-dir web/dist -listen :8081 & # serve new bundle
# Manual: load http://localhost:8081/ui/, compare to pre-flight screenshots
#         at the same viewport. No visible delta on Heroes, Dict (Telemetry
#         + Telecommands), Channels editor, Settings.
./mecomgw -ui-dir docs/gateway/console -listen :8082 &  # legacy still works
# Tarball
bash scripts/build-gateway-handoff.sh /tmp/handoff-phase1.tgz
tar tzf /tmp/handoff-phase1.tgz | grep -E "web/dist/(index\.html|assets/)"
```

If any step fails, the rollback below applies.

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

**Goal:** define backend-driven catalogue/wall/tile contracts and move only the
thin reusable UI primitives into SignalForge. Consumers implement adapters that
translate their backend responses into those contracts; product-specific content
remains owned by each backend.

### Module layout

```
signalforge/
  web/
    package.json            (name "signalforge-web", version pinned)
    tsconfig.json
    vite.config.ts          (lib mode → ESM; UMD only if needed)
    src/
      index.ts              (see public exports below)
      types/                (graph_tile.v1, wall config, assignment types — re-exported)
        openapi.d.ts        (generated by openapi-typescript)
        index.ts            (public type re-exports)
      dict/
        SignalDictionary.tsx
        SignalBlock.tsx
        ChannelsEditor.tsx
        useAssignments.ts   (load/save/forWall/hasAssignment, localStorage namespace required)
      walls/
        WallManager.tsx     (list / create / rename / delete)
        useWalls.ts         (localStorage-backed; namespace required)
      render/
        UPlotTileRenderer.tsx    (port of gossamer uPlotAdapter — sole renderer; meerstetter-go's drawCanvasChart is retired)
        markers.ts               (port of gossamer markers.ts)
        decimation.ts            (port of gossamer decimation.ts)
        timeAxis.tsx             (port of gossamer timeAxis.tsx)
        visualPolicy.ts          (port of gossamer visualPolicy.ts)
      tiles/
        TileClient.ts       (fetches /api/.../tiles?level=…, picks tier by zoom, caches)
        useTileSeries.ts    (React hook fronting TileClient)
    dist/
      signalforge-web.es.js
      signalforge-web.d.ts
```

The shared module is deliberately not a product UI. It must not contain
consumer-specific signal lists, campaign fixtures, route paths, lease/auth
policy, or operator vocabulary. Those stay in each backend or consumer adapter.

### Public exports (`signalforge/web/src/index.ts`)

```ts
// React components
export { SignalDictionary } from "./dict/SignalDictionary";
export { ChannelsEditor }   from "./dict/ChannelsEditor";
export { WallManager }      from "./walls/WallManager";
export { UPlotTileRenderer } from "./render/UPlotTileRenderer";

// React hooks
export { useAssignments } from "./dict/useAssignments";
export { useWalls }       from "./walls/useWalls";
export { useTileSeries }  from "./tiles/useTileSeries";

// Plain TS — for non-React contexts (Node tests, CLI tools)
export { TileClient } from "./tiles/TileClient";

// Adapter interfaces (consumers implement)
export type {
  SignalCatalogueAdapter,
  TileAdapter,
  AssignmentsStore,
} from "./types";

// Contract types (mirrored from Go via openapi-typescript)
export type {
  GraphTile,
  TileSeries,
  WallConfig,
  Assignment,
  SemanticSignal,
} from "./types";
```

Anything not in this list is internal to the package and can change without
a minor-version bump.

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

Tag `signalforge-web@0.1.0` once Phase 2 lands. Eligible consumers pin to
the tag. Breaking changes require a minor bump and explicit consumer-side
upgrade.

**Consumption mechanism — defaults for v0.1.x:**

Use a **git URL with subdirectory** in each consumer's `package.json`:

```jsonc
"dependencies": {
  "signalforge-web": "github:egidinas/signalforge#v0.1.0"
}
```

…and a `package.json` `workspaces` entry pointing into `signalforge/web/` if
the consumer keeps signalforge as a sibling checkout (preferred during
active development — edit, no publish needed).

If npm + git URL behaves badly (lockfile churn, subdir resolution), the
fallback is `npm pack` in `signalforge/web/` and committing the tarball to
each consumer for v0.1.x; promote to a real npm publish once the API is
stable.

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

## Phase 3 — eligible consumers adopt SignalForge primitives

**Goal:** delete duplicate generic renderer, assignment, wall, and tile-client
plumbing without centralizing product-specific UI. After this phase, the shared
contracts and render fundamentals live in exactly one place — consumed by
eligible meerstetter-go, gossamer, and loom surfaces through backend-shaped
adapters.

### meerstetter-go

1. `npm --prefix web add signalforge-web@^0.1.0` (mechanism per Versioning section).
2. Replace `web/src/views-dict.tsx` with a thin wrapper that constructs the
   `MecomCatalogueAdapter` and renders `<SignalDictionary adapter={...} namespace="mecomgw" />`.
3. Replace `web/src/components.tsx` `MultiChart` with `<UPlotTileRenderer
   tileAdapter={mecomgwTileAdapter} wallId={...} />`. Delete the local
   `drawCanvasChart`, `chartNumber`, `droppedAxisWarnings`, `MultiChart`,
   `Sparkline`, and the `TELE_BUF`/`recordTelemetry`/`getTelemetry` block
   (the live tier moves to `MecomCatalogueAdapter`).
4. Replace `WALLS` const with `useWalls({ namespace: "mecomgw" })` from the
   shared module + a "+ New wall" button on the main view. `wallForDevice`
   becomes a fallback factory call against `useWalls`.
5. Delete `docs/gateway/console/` (one release after Phase 1 cutover proven
   stable).

### gossamer

1. `npm --prefix web add signalforge-web@^0.1.0` (mechanism per Versioning section).
2. Add a new operator-side route (e.g. `/operator/wall-config`) that hosts
   `<SignalDictionary adapter={campaignAdapter} />` so users can build/edit walls
   alongside the existing campaign-driven views. The Gossamer backend emits or
   adapts campaign catalogues into the SignalForge catalogue/wall/tile shapes.
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
   read-only mode for the existing operator views. Loom's backend remains the
   authority for lab/operator-specific content; the adapter only maps that
   content into neutral SignalForge web contracts.
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
| meerstetter-go gains a 50KB uPlot dep it didn't have before | Accepted per resolved decision 1. Net win: one renderer to maintain, gossamer's marker/zoom/log-axis features become free for meerstetter-go. |
| meerstetter-go's mecomgw tile API doesn't exist yet (backlog id 11) | Phase 2's `TileAdapter` interface ships first; `MecomgwTileAdapter` is a stub returning live values until id 11 lands. Keeps the contract correct without blocking. |
| Consolidating three apps' API surfaces into a signalforge-owned OpenAPI is broader than \"port the dict\" | Per resolved decision 2. Phase 2 starts with neutral catalogue/wall/tile schemas seeded from meerstetter-go's existing `docs/gateway/openapi.yaml`, then adds adapter-facing shapes for gossamer tiles/graph models and loom operator telemetry. Each consumer keeps its routes and backend ownership; the shared spec defines the adapter contract, not a forced route layout. |
| Loom has its own evolving operator vocabulary (HeroGraph, OperatorGraphPrimitives, capability-module browser) that may not map cleanly | Phase 3's loom step is deliberately incremental: only the graph + dict primitives migrate. Non-graph operator surfaces stay loom-local until they're ready to share. Loom's existing `validate-*` contract tests are the regression net. |

## Resolved decisions (2026-05-15)

1. **Renderer:** uPlot only. Meerstetter-go's `drawCanvasChart` is retired
   in Phase 3; meerstetter-go gains uPlot as a dep. One renderer
   implementation serves all eligible consumers.
2. **Contract ownership:** SignalForge owns the neutral catalogue/wall/tile
   contracts and generated TS types. Phase 2 seeds them from meerstetter-go's
   existing `docs/gateway/openapi.yaml`, then folds in adapter-facing shapes
   for gossamer tiles/graph models and loom operator telemetry. Consumer routes
   and product-specific backend responses remain local; each consumer imports
   the generated types and provides an adapter.
3. **localStorage namespacing:** the shared `useAssignments`/`useWalls` hooks
   take a required `namespace` config. meerstetter-go passes `"mecomgw"`,
   gossamer passes `"gossamer"`, loom passes `"loom"`. No collision risk if
   any of them ever co-host.
4. **Package name:** unscoped `signalforge-web`. Avoids needing an npm
   organization. Re-scope to `@egidinas/signalforge-web` later if/when the
   org exists; consumers update their import paths in one commit.
5. **OpenAPI codegen tool:** `openapi-typescript` for types-only. Clients
   stay hand-written (they wrap lease/auth/error-mapping logic that
   generated clients handle awkwardly). Run as `npm run gen:types` in
   `signalforge/web/`; output committed to `signalforge/web/src/types/openapi.d.ts`
   and re-exported via `src/types/index.ts`.
6. **Lineage:** Loom is the premium parent; gossamer is the junior
   derivative. Gossamer's graph code is the extraction baseline; loom-only
   operator features (testbed view, librarian, capability-module browser)
   stay loom-local.
7. **History cap:** none. The tile pyramid serves whatever the backend has
   on each consumer; client-side caps are migration debt to be removed.

---

## Verification (end-to-end, after Phase 3)

```bash
# Automated — all four repos
npm --prefix /home/svc_pmg_testbed_b/signalforge/web test
npm --prefix /home/svc_pmg_testbed_b/signalforge/web run build

npm --prefix /home/svc_pmg_testbed_b/meerstetter-go/web test
npm --prefix /home/svc_pmg_testbed_b/meerstetter-go/web run build
go -C /home/svc_pmg_testbed_b/meerstetter-go test ./...

npm --prefix /home/svc_pmg_testbed_b/gossamer/web run build
node /home/svc_pmg_testbed_b/gossamer/web/scripts/browser-smoke.mjs
go -C /home/svc_pmg_testbed_b/gossamer test ./...

npm --prefix /home/svc_pmg_testbed_b/loom/web run build
# Loom's contract suite — must all pass:
for t in /home/svc_pmg_testbed_b/loom/web/scripts/validate-*.mjs; do
  node "$t" || exit 1
done
go -C /home/svc_pmg_testbed_b/loom test ./...
```

Manual checks:

1. meerstetter-go gateway shows the existing two heroes plus one
   user-created wall containing two custom signals from the dict; the new
   wall persists across reload.
2. gossamer's tvac_qualification view renders unchanged from a
   pre-Phase-3 screenshot baseline; new `/operator/wall-config` route lets
   a user define a custom wall and it persists.
3. loom's hero-graph and operator views render unchanged from
   pre-adoption screenshots.
4. `signalforge/web/demo/` page works in isolation against fixture data.
5. Hosted: `gossamer.jmeyer.space` reflects the new bundle after deploy
   (`bash gossamer/scripts/deploy_brume2_ui.sh`); meerstetter-go gateway
   on Brume2 reflects new bundle after its deploy script runs.

## Estimated scope

- Phase 1: 2–3 days (port 7 JSX files + mock + Vite config + cutover + deploy
  script + Brume2 verification).
- Phase 2: 4–6 days (extraction, adapter design, OpenAPI consolidation across
  three apps, uPlot tile renderer, tile-pyramid client, demo page, tests).
- Phase 3: 4–6 days (eligible consumers wired + visual + contract-test regression
  checks + delete legacy console + gossamer wall-config route + loom
  HeroGraph/OperatorGraphPrimitives reduction).

Total: ~2.5 weeks of focused work, plus review and deploy soak.
