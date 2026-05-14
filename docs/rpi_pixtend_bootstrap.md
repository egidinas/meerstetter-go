# Raspberry Pi / PiXtend V2-L Bootstrap Guide

This guide covers everything needed to go from a fresh Raspberry Pi OS SD card
to a working `teccanprobe` session against Meerstetter TEC controllers through
a PiXtend V2-L CAN interface.

## Hardware Overview

The PiXtend V2-L is an RPi HAT that adds two MCP2515-based SPI CAN controllers
(`can0`, `can1`), digital I/O, and analog outputs. The MCP2515 uses a 16 MHz
crystal and shares the RPi SPI bus.

**Hardware jumper requirement**: the PiXtend V2-L analog output section and the
CAN section share a hardware resource. Before powering on, set the CAN/analog
jumper to the CAN position. Refer to the PiXtend V2-L hardware manual section
on the jumper block near the CAN terminal; the board will not enumerate the CAN
interfaces if the jumper is in the analog position.

## OS Prerequisites

- Raspberry Pi OS (Bookworm 64-bit recommended, Bullseye 32-bit works).
- SD card: Class 10 / A1 rated. Avoid boot from slow or worn media — MCP2515
  is interrupt-driven and a slow SD under I/O pressure can cause CAN frame
  loss.
- The filesystem must be read-write. PiXtend ships with a read-only filesystem
  option; if enabled, the SocketCAN netdevice still works but `candump` cannot
  write logs. Confirm with `mount | grep ' / '`.

## Kernel Driver and Device Tree Overlays

### `/boot/firmware/config.txt` (Bookworm) or `/boot/config.txt` (Bullseye)

Add the following lines at the end of the config, replacing any existing
MCP2515 overlay stanza:

```ini
# Enable SPI bus (required by PiXtend and MCP2515)
dtparam=spi=on

# PiXtend V2-L combined overlay: loads pixtendv2l HAT config + both MCP2515
# controllers at 16 MHz crystal, SPI CE0/CE1, interrupt GPIO25/GPIO24
dtoverlay=pixtendv2l
```

The `pixtendv2l` overlay is the preferred single-line configuration. It sets
both MCP2515 controllers at 16 MHz oscillator frequency and wires the interrupt
lines to the correct GPIO pins (GPIO 25 for `can0`, GPIO 24 for `can1`).

If `pixtendv2l` is not available in your kernel overlay directory (check
`/boot/firmware/overlays/pixtendv2l.dtbo`), fall back to explicit MCP2515
overlays:

```ini
dtparam=spi=on
dtoverlay=mcp2515-can0,oscillator=16000000,interrupt=25,spimaxfrequency=1000000
dtoverlay=mcp2515-can1,oscillator=16000000,interrupt=24,spimaxfrequency=1000000
```

The SPI frequency cap of 1 MHz (`spimaxfrequency=1000000`) is conservative and
correct for the PiXtend. Higher values are unstable on the RPi SPI controller
with the MCP2515.

### Reboot and Verify Drivers

```bash
sudo reboot
```

After reboot, confirm the MCP2515 driver bound to both interfaces:

```bash
dmesg | grep -i mcp251x
# Expected: "mcp251x spi0.0 can0: MCP2515 successfully initialized."
#           "mcp251x spi0.1 can1: MCP2515 successfully initialized."

ls /sys/class/net/ | grep can
# Expected: can0  can1
```

If the interfaces do not appear, check:
1. Jumper is in CAN position (not analog).
2. SPI is enabled: `ls /dev/spidev*` should list `spidev0.0` and `spidev0.1`.
3. The overlay file exists: `ls /boot/firmware/overlays/pixtendv2l.dtbo`.

## Bringing Up the CAN Interface

```bash
# Bring up can0 at 1 Mbit/s (standard Meerstetter TEC bitrate)
sudo ip link set can0 up type can bitrate 1000000

# Verify the interface is up with details
ip -details link show can0
```

Expected output includes `can <NOARP,UP,LOWER_UP>` and `bitrate 1000000`. The
`state ERROR-ACTIVE` or `state ERROR-WARNING` are normal on an idle bus with no
other nodes; once a TEC controller is connected and powered, the state settles
to `ERROR-ACTIVE`.

For a persistent bring-up across reboots, add a systemd-networkd `.network` file
or a `@reboot` cron entry:

```bash
# /etc/network/interfaces.d/can0  (ifupdown style)
auto can0
iface can0 inet manual
    pre-up /sbin/ip link set $IFACE up type can bitrate 1000000
    down /sbin/ip link set $IFACE down
```

## Passive Bus Verification

Before running any active probe, verify the bus is live and the MCP2515 is
receiving frames:

```bash
# Install can-utils if not present
sudo apt-get install -y can-utils

# Passive listen: print up to 20 frames then exit
candump -n 20 can0
```

If nothing appears within a few seconds after the TEC controller is powered and
connected, check:
- Termination: CAN bus requires 120 Ω termination at both ends. PiXtend V2-L
  has a solder jumper for the onboard 120 Ω terminator; the TEC controller end
  may already be terminated internally (check the Meerstetter datasheet).
- Wiring: CAN-H to CAN-H, CAN-L to CAN-L, shared ground.
- Bus-off recovery: if the MCP2515 entered bus-off state from a previous error
  burst, cycle `ip link set can0 down && sudo ip link set can0 up type can
  bitrate 1000000`.

### pixtendsrv2 Interference

If `pixtendsrv2` (the PiXtend userspace daemon for digital/analog I/O) is
running, it communicates with the PiXtend over SPI. It does not touch the
MCP2515 directly, but on some kernel versions the shared SPI bus can produce
short CAN error bursts during pixtendsrv2 polling cycles. `teccanprobe` logs a
warning if it detects a bound `pixtendsrv2` process (`-checklist` shows this
check). This is not a hard blocker but will show as CRC errors on a busy
protocol analyzer. If frame loss is suspected, stop pixtendsrv2 during the
probe session.

## Running teccanprobe

`teccanprobe` is the main diagnostics binary. It defaults to the
`pixtend-v2l` adapter profile and performs preflight hardware checks before
opening the CAN socket.

### Install

```bash
# From the meerstetter-go repo root
go install ./cmd/teccanprobe
```

Or run directly:

```bash
go run ./cmd/teccanprobe [flags]
```

### Preflight Checklist

```bash
teccanprobe -if can0 -profile pixtend-v2l -checklist
```

This prints the adapter preflight checks without opening the socket:
- `/dev/spidev0.0` filesystem presence.
- SPI device accessible.
- `pixtendv2l` or `mcp2515` overlay active in `/proc/device-tree`.
- MCP2515 bound in `dmesg` or `/sys`.
- `pixtendsrv2` process warning if bound.

Resolve any `FAIL` items before proceeding.

### Passive Listen

```bash
teccanprobe -if can0 -profile pixtend-v2l
```

Passively listens for CANopen heartbeats, TPDO frames, and MeCom responses.
Press Ctrl-C to stop. Useful for confirming which node IDs are active on the bus.

### Active SDO / MeCom Probe

```bash
# Probe nodes 1 through 8, attempt MeCom identification on each
teccanprobe -if can0 -profile pixtend-v2l -active -nodes 1-8
```

The active probe:
1. Sends SDO upload requests to object `0x1000` (device type) for each node.
2. Attempts a MeCom `?VR` identification query if SDO confirms a Meerstetter
   device type.
3. Logs node ID, device name, firmware version, and serial number.

For a 16-node scan:

```bash
teccanprobe -if can0 -profile pixtend-v2l -active -nodes 1-16
```

### Ring Capture

To capture frames to a ring buffer for offline review:

```bash
teccanprobe -if can0 -profile pixtend-v2l -capture -capture-frames 500
```

## Common Failure Modes

| Symptom | Likely Cause | Fix |
|---|---|---|
| `can0` missing after reboot | Overlay not in config.txt | Add `dtoverlay=pixtendv2l` and reboot |
| `mcp251x` not in `dmesg` | SPI not enabled | Add `dtparam=spi=on` |
| `candump` shows nothing | Wrong jumper position | Move jumper to CAN position |
| Constant error frames | Missing termination | Add 120 Ω at bus ends |
| MCP2515 enters bus-off | Wiring fault or missing termination | Fix wiring, then cycle interface |
| `teccanprobe` checklist FAIL on overlay | Using mcp2515 overlay names instead of pixtendv2l | Use `pixtendv2l` overlay or check exact dtbo name |
| CRC errors during pixtendsrv2 polling | SPI bus contention | Stop pixtendsrv2 during probe |
| SDO timeout on active probe | Node ID not on bus | Verify with passive candump first |

## Interface Naming

The `pixtendv2l` overlay always registers the first MCP2515 as `can0` and the
second as `can1`. This matches the default `-if can0` argument in `teccanprobe`.
If using raw `mcp2515-can0`/`mcp2515-can1` overlays in a non-standard order,
verify which interface corresponds to which physical CAN terminal with
`ip -details link show can0` (look at the SPI CS and interrupt GPIO).
