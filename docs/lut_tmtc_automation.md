# MeCom LUT and TMTC Automation

This repository keeps Meerstetter-specific behavior at the protocol edge. The
generic automation shape lives in `github.com/egidinas/signalforge/controlprogram`:
program ID, target fanout, cycle count, hold steps, and setpoints.

`mecomautomation` adapts that neutral program into MeCom LUT preload commands:

- `FourCycleSampleProgram()` creates a safe preload-only sample for four TEC
  controllers by default.
- Passing target IDs fans the same program out to any reasonable controller
  count, including 16 targets.
- `PreloadTelecommands()` emits one idempotent TMTC command per target.
- `PreloadScript()` emits a one-step sequencer script for explicit operator
  execution.

The sample intentionally does not enable output or start regulation. It only
preloads the LUT/program so a separate authorized sequencer step can decide when
the controller may run.

Polling remains separate from automation preloads. High-priority values should
use MeCom ring-buffer polling; lower-priority values should use round-robin
multi-parameter reads. The PiXtend edge should keep polling deterministic and
bounded, leaving reduction and logging to downstream nodes unless an in-memory
ring buffer is needed for short local recovery windows.
