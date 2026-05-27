# CoSo Compatibility Bridge

`mecomvseriald` can expose a CoSo-compatible MeCom TCP listener while routing
requests to serial, TCP, or CANopen downstream devices. The bridge is intended
for development, diagnostics, and migration work where an existing CoSo client
must talk to real hardware through a different transport.

## Source of truth

The compatibility surface is data-driven. Keep protocol decisions in the TEC
CANopen SDO map instead of spreading special cases through the router:

| Source | Purpose |
|---|---|
| `mecom/catalogues/sources/tec_canopen_sdo_map.v631.json` | Canonical CoSo-facing MeCom to CANopen SDO map, metadata transforms, and intentionally unsupported commands |
| `mecom/canopen_sdo_map.go` | Embedded map loader, validation, and runtime lookup helpers |
| `cmd/coso-puppet/testdata/coso_tec_v631_oracle.json` | Public-safe connection oracle for the observed CoSo startup sequence and expected bridge behavior |
| `mecomserver/device_bridge.go` | Runtime bridge that applies the map to MeCom frames |

If a new CoSo frame appears, first classify it in the JSON map as native SDO,
metadata transform, cache-backed virtual value, or intentionally unsupported.
Only add Go logic when the map cannot describe the behavior.

## Runtime model

The bridge prefers real data in this order:

1. Native MeCom serial/TCP route reads for virtual metadata when such a route is
   configured for the address.
2. CANopen SDO reads for parameters present in the SDO map.
3. Per-device cache values observed from successful downstream reads or local
   CoSo writes.
4. Explicit compatibility defaults from the map for values that CoSo requires
   but the active transport cannot provide.

The cache is a best-effort aid, not an authority. Placeholder values, `NaN`, and
infinite floats are not persisted. Cache files are scoped per MeCom address so a
value learned from one controller is not reused for another controller.

Unsafe or unimplemented bulk/ring commands should be listed in the map with an
unsupported behavior such as `nack_bulk_read`. This makes the limitation visible
to tests and callers without pretending that simulated data is live hardware.

## Portable setup

Use an explicit cache directory for any persistent bridge deployment. This keeps
developer machines, containers, and lab hosts independent:

```sh
MECOM_DEVICE_CACHE_DIR=./state/mecom-device-cache \
go run ./cmd/mecomvseriald \
  -listen 0.0.0.0:50075 \
  -default-address 75 \
  -route 75=can:can0/75
```

For a mixed route where a native serial connection is available for the same
controller, put that route before the CAN route. The router will use native
MeCom reads for startup metadata and fall back to CANopen where appropriate:

```sh
MECOM_DEVICE_CACHE_DIR=./state/mecom-device-cache \
go run ./cmd/mecomvseriald \
  -listen 0.0.0.0:50075 \
  -default-address 75 \
  -route 75=serial:/dev/serial/by-id/usb-FTDI_TEC75-if00-port0@57600 \
  -route 75=can:can0/75
```

On Windows, use a normal writable directory for the cache and a serial target
accepted by `mecom.ParseEndpoint`, for example `COM3@57600`.

## Validation

Run the repository tests before changing the bridge behavior:

```sh
go test ./...
git diff --check
```

The CoSo oracle can print the public-safe startup request set used by tests:

```sh
go run ./cmd/coso-puppet oracle \
  -file cmd/coso-puppet/testdata/coso_tec_v631_oracle.json \
  -requests
```

When debugging a real connection, capture the client request sequence and add
only public-safe facts to the oracle: command family, parameter ID, instance,
expected source class, and bounded error behavior. Do not commit hostnames,
serial numbers, private trace paths, credentials, proprietary binaries, or
decompiled source.
