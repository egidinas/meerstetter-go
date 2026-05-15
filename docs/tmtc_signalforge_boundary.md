# TM/TC and SignalForge Boundary

Meerstetter-Go keeps hardware/protocol ownership at the MeCom edge. The core
`tmtc` package is therefore a local Meerstetter contract: callers can use it
without importing SignalForge, Arrow/Parquet storage packages, or any other
upstream automation framework.

SignalForge remains the generic control, graph, and automation context. When a
caller needs to exchange TM/TC values with SignalForge, use the explicit bridge
package:

- `github.com/egidinas/meerstetter-go/tmtc` owns the Meerstetter public TM/TC
  API and has no SignalForge import.
- `github.com/egidinas/meerstetter-go/tmtc/signalforge` is the only TM/TC
  adapter boundary that imports `github.com/egidinas/signalforge/contracts`.
- Bridge functions copy byte slices and maps so callers cannot accidentally
  mutate values across the boundary.

This avoids a hidden type-identity dependency while still keeping field-level
compatibility with the SignalForge contracts.

## Cross-repository review checklist

Apply the same rule in peer repositories, including Gossamer:

1. Core hardware, protocol, or product packages should not directly alias
   SignalForge contracts unless SignalForge is intentionally part of their public
   API.
2. Put SignalForge interop in a small adapter package at the edge of the repo.
3. Add a package-dependency check proving the core package does not import
   SignalForge and the adapter package is the only package that does.
4. Prefer explicit conversions with copy semantics for slices and maps.
5. Keep heavyweight SignalForge storage/export dependencies out of runtime
   closures; if module-graph tooling still fetches them, fix that upstream by
   splitting SignalForge modules or moving optional storage packages behind a
   separate module.

## Gossamer review snapshot

Gossamer is intentionally more coupled to SignalForge than Meerstetter-Go: its
public README describes SignalForge contracts, Arrow telemetry, graph-wall,
tile-bundle, JSON, and safe-path primitives as part of the public build. That is
an acceptable boundary if Gossamer is treated as a SignalForge-backed portfolio
application rather than as a hardware/protocol edge library.

The same dependency caveat remains visible there: Gossamer's module file pins
`github.com/egidinas/signalforge v0.2.0` while also carrying heavy Arrow,
Parquet, CEL, DuckDB, and Go tooling transitive requirements in the root module.
That matches the upstream SignalForge module-shape concern: optional storage,
tile, and analysis stacks are useful for Gossamer, but they should not leak into
smaller edge libraries by type aliases or root-module imports.

The graph-wall fix quoted in review is present on Gossamer `main`: the shared
axis component now writes both `--time-axis-grid-left/right` and the legacy
`--time-axis-left/right` variables from measured plot bounds, while responsive
CSS still consumes the legacy pair on mobile. That is the correct compatibility
patch for the reported mobile alignment regression.

Gossamer follow-up checks should therefore focus on boundaries, not on that
axis fix:

- keep SignalForge as an explicit public app dependency only where Gossamer
  needs shared graph/tile/evidence contracts;
- avoid introducing direct SignalForge aliases in independent domain packages;
- add package-level dependency tests if any Gossamer package is meant to remain
  product-local or clean-room independent;
- continue moving heavyweight optional SignalForge stacks upstream into separate
  modules if smaller repos consume only contracts/control primitives.
