# Source Material Inventory

This file records the local source material used to derive the public-safe Go
implementation. The source files themselves are not vendored here unless they
are small implementation fixtures or synthetic tests.

## Local Extracted Material

The current working copy expects Meerstetter source material outside this repo.
Set `MECOM_SOURCE_DIR` to the local extraction directory when regenerating
derived references. In the lab workspace this usually points at the adjacent
Loom checkout, for example `../loom/docs/meerstetter/` when both repositories
share the same parent directory.

Important derived references:

- `MECOM_REFERENCE.md`: synthesized MeCom frame, CRC, payload, and topology
  reference for coding agents.
- `GRAPHS_AND_DIAGRAMS.md`: derived visual planning notes for discovery,
  graph-wall, and utility UI patterns.
- `txt/MeCom-Protocol-Specification-5117F.txt`: extracted protocol text used
  to validate frame structure and CRC behavior.
- `txt/TEC-Family-Communication-Protocol-5136AT.txt`: extracted TEC
  communication text used to validate TEC parameter access assumptions.
- `txt/TEC-Family-User-Manual-latest.txt`: extracted controller manual text
  used for device and operating context.
- `TEC-CANopen-v6.31.eds` and `TEC-CANopen-v5.10.eds`: local EDS files used to
  validate the `canopen/eds` parser and `objectdict` model.

## Repo Policy

This repository should not copy full vendor PDFs or full extracted manuals.
Instead, promote only:

- protocol facts needed to implement stable APIs,
- small synthetic fixtures,
- tests that prove frame encoding/decoding,
- object dictionary parser behavior,
- clear links between packages and source-derived design choices.

If official redistribution terms allow vendoring a particular EDS file later,
add it under `testdata/eds/` with a short license note and keep production code
capable of loading external EDS files at runtime.

## Derived Implementation Coverage

Implemented in this repo:

- MeCom CRC-16-CCITT and frame generation.
- MeCom single read, bulk read, write, NACK, and numeric payload parsing.
- Serial/TCP endpoint parsing and dial helpers.
- CANopen EDS parsing into a protocol-neutral object dictionary.
- CANopen expedited SDO request builders.
- BusMaster-style discovery tree contracts.
- Rich TM/TC primitives with idempotent command keys.
- Ring-buffer readout for disconnect recovery within retention.
- Graph-wall, sequencer, export, and device-server contracts.

Still intentionally application-owned:

- actual hardware polling loops,
- node-local connection ownership policy,
- CAN interface drivers,
- NATS/REST/SSH fallback transports,
- UI rendering,
- HDF5 writer binding.
