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

Apply the same rule in peer repositories:

1. Core hardware, protocol, or product packages should not directly alias
   SignalForge contracts unless SignalForge is intentionally part of their public
   API.
2. Put SignalForge interop in a small adapter package at the edge of the repo.
3. Add a package-dependency check proving the core package does not import
   SignalForge and the adapter package is the only package in that boundary that
   imports the relevant SignalForge contract package.
4. Prefer explicit conversions with copy semantics for slices and maps.
5. Keep heavyweight SignalForge storage/export dependencies out of runtime
   closures; if module-graph tooling still fetches them, fix that upstream by
   splitting SignalForge modules or moving optional storage packages behind a
   separate module.
