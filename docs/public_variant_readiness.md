# Public Variant Readiness

Date: 2026-05-12

Meerstetter-Go is the public protocol baseline for MeCom/Meerstetter work. It
should stay independently useful and free of Loom deployment assumptions.

## Gates

- Build and test from a fresh clone using public dependencies only.
- Keep vendor/protocol-general code here: MeCom framing, object dictionaries,
  CANopen/EDS parsing, transport-neutral TM/TC, ring logging contracts, bounded
  diagnostics, and public export interfaces.
- Keep private deployment details out: hostnames, IP routes, controller
  nicknames, credentials, lab presets, real graph defaults, and procedure names.
- Treat SocketCAN, Kvaser, serial, TCP, and remote bridges as adapter
  boundaries. The public library may define interfaces; applications own real
  device access.

## Legacy Harvest Destinations

- MeCom multiplexer/server ownership loops: public if protocol-general.
- `VerifySetpoint` and command-result correlation: public contracts plus
  Loom-local procedure use.
- Dictionary-driven alarm evaluation: public for vendor-generic dictionary
  alarms; private thresholds stay in Loom.
- HDF5/FORT export: public schema/interface only unless implementation has no
  non-public fixture dependency.
