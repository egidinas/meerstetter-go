# Serial Access Setup for Meerstetter FTDI Backup Links

## Problem
The 4 FTDI FT230X USB-serial adapters (ttyUSB0–3) are owned by `root:dialout`.
Neither the testbed service account nor the interactive users are in the `dialout`
group. The socat TCP-to-serial bridges fail silently in forked children.

## Fix (one command, needs root once)

```bash
sudo cp deploy/60-ftdi-meerstetter.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
sudo udevadm trigger --subsystem-match=tty
```

`TAG+="uaccess"` makes systemd-logind grant access to the currently logged-in
console user automatically — no group membership change needed.

## Preferred: one addressed TCP device server

MeCom serial requests include the destination device address in every frame, so
multiple FTDI-backed controllers can share one LAN TCP listener when the server
routes by that address. This is the preferred mode because clients only need one
endpoint and the server still serializes access per physical serial link.

```bash
go run ./cmd/mecomvseriald \
  -listen 0.0.0.0:50000 \
  -route 75=serial:/dev/serial/by-id/usb-FTDI_FT230X_Basic_UART_CONTROLLER_A-if00-port0@57600 \
  -route 76=serial:/dev/serial/by-id/usb-FTDI_FT230X_Basic_UART_CONTROLLER_B-if00-port0@57600 \
  -route 81=serial:/dev/serial/by-id/usb-FTDI_FT230X_Basic_UART_CONTROLLER_C-if00-port0@57600 \
  -route 84=serial:/dev/serial/by-id/usb-FTDI_FT230X_Basic_UART_CONTROLLER_D-if00-port0@57600
```

Clients then use the same TCP target for each addressed controller:

```bash
./mecomtcppoll \
  -targets "tcp:127.0.0.1:50000=75,tcp:127.0.0.1:50000=76,tcp:127.0.0.1:50000=81,tcp:127.0.0.1:50000=84" \
  -interval 2s
```

## Compatibility fallback: transparent port-per-device bridge

Use this when an existing raw serial client cannot tolerate a device-server
router and needs exclusive transparent byte-stream access to one physical link.

Start the socat bridges:
```bash
bash deploy/start-meerstetter-serial-bridges.sh &
```

This binds:
- :50002 → DQ01FKRM (ttyUSB2)  
- :50003 → DQ01FSW6 (ttyUSB3)  
- :50004 → DQ01GVYX (ttyUSB0)  
- :50005 → DQ01GW7L (ttyUSB1)  

All at 57600 baud (MeCom default).

## Verify data flow

```bash
# First pass: probe all 4 ports to map address → port
for port in 50002 50003 50004 50005; do
  for addr in 75 76 81 84; do
    ./mecomtcppoll -targets "tcp:127.0.0.1:${port}=${addr}" -interval 99s -timeout 500ms 2>&1 | grep -v "error"
  done
done

# Continuous poll once mapping is known (example):
./mecomtcppoll \
  -targets "tcp:127.0.0.1:50002=75,tcp:127.0.0.1:50003=76,tcp:127.0.0.1:50004=81,tcp:127.0.0.1:50005=84" \
  -interval 2s
```
