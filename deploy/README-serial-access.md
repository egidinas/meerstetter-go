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

## After the rule is deployed

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
