# Meerstetter-Go Shared Synergy

This document describes how `meerstetter-go` aligns with the shared primitives and data standards of the `signalforge` and `loom` ecosystem.

## Data Standard: Apache Arrow

`meerstetter-go` has adopted Apache Arrow IPC as its primary high-performance telemetry transport format.

### Endpoints
- **Export**: `/api/log/export?format=arrow` returns a stream of Arrow IPC records.
- **Import**: `/api/log/import` accepts Arrow IPC streams (detected via `Content-Type: application/vnd.apache.arrow.stream`).
- **Graph Tiles**: `/api/graph/tiles/...` supports the `X-Format: arrow` header to return binary telemetry for high-performance frontend visualization.

### Schema
We use the canonical `signalforge.telemetry.arrow.v2` schema defined in `signalforge/arrowtelemetry`:
- `timestamp`: Int64 (Nanoseconds)
- `sensor`: Dictionary-encoded String (format: `device:param:instance`)
- `value`: Float64
- `unit`: Dictionary-encoded String
- `campaign`: Dictionary-encoded String
- `source`: Dictionary-encoded String
- `role`: Dictionary-encoded String
- `kind`: Dictionary-encoded String
- `family`: Dictionary-encoded String ("mecom")
- `quality`: Dictionary-encoded String
- `state`: Dictionary-encoded String (nullable)

## UI Parity with Meerstetter CoSo

The frontend has been enhanced to achieve feature parity with the Meerstetter Configuration Software (CoSo).

### Hierarchical Discovery
The Signal Dictionary uses a hierarchical tree structure with memoized components for high performance. It supports multiple "Projections" (e.g., Operator view vs Protocol view).

### Semantic Bundling
Telemetry and Telecommands are logically grouped. For example, a "Target Temperature" (Telecommand) will show its paired "Measured Temperature" (Telemetry) live value in the same row, providing immediate feedback to the operator.

### Safety Metadata
Parameters now include:
- **Safety Notes**: Explicit warnings for dangerous operations (e.g., resets, flash writes).
- **Visibility Levels**: Manufacturer vs Advanced vs Operator visibility.
- **Reverse Engineering Evidence**: Traceability back to the original CoSo artifacts.

## Historical UX Patterns

`meerstetter-go` implements a "Time-Travel" UX for telemetry:

### Live vs. Historical Modes
The UI provides a global toggle (and per-graph overrides) between "Live" (streaming) and "Historical" (backfilled) modes. Switching to historical mode freezes the current viewport and backfills data from the `/api/graph/tiles` range endpoints.

### Range Selection & Exploration
- **Timeline Ruler**: A shared time axis across multiple charts allowing synchronized scrubbing.
- **Manual Zoom/Pan**: First-class controls (+/-/←/→) for precise navigation through deep historical buffers.
- **High-Res Raw Buffer**: 15 minutes of "Hot" raw data capture (10-100Hz) available alongside the 3-day SNR-improved derived trends.

## Archival Export: HDF5

For long-term archival and integration with scientific tools (MATLAB, Python/H5Py), `meerstetter-go` supports HDF5 export via `libhdf5_serial.so.103`.

### Endpoint
- **HDF5 Export**: `/api/log/export?format=hdf5`

### Structure
The exported `.h5` file follows a hierarchical structure:
- `/telemetry`
    - `/{device}_{param}_{instance}`
        - `timestamps`: Int64 array (UnixNano)
        - `values`: Float64 array
