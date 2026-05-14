# deploy/rpi — PiXtend V2-L CAN Config Artifacts

Ready-to-use config files and a pre-compiled `teccanprobe` binary for
Raspberry Pi deployments. Full bring-up guide: [docs/rpi_pixtend_bootstrap.md](../../docs/rpi_pixtend_bootstrap.md).

## Files

| File | Purpose |
|---|---|
| `boot_config_snippet.txt` | Lines to append to `/boot/firmware/config.txt` (Bookworm) or `/boot/config.txt` (Bullseye) |
| `can0.network` | systemd-networkd unit — auto bring-up `can0` at 1 Mbit/s with bus-off restart |
| `can1.network` | systemd-networkd unit — same for `can1` (only needed if using both ports) |
| `interfaces_can0` | ifupdown alternative for `/etc/network/interfaces.d/can0` |
| `setup.sh` | Install script: applies boot config, installs networkd units, builds teccanprobe |
| `bin/teccanprobe_linux_arm64` | Pre-compiled binary for Raspberry Pi OS 64-bit (aarch64) |
| `bin/teccanprobe_linux_arm7` | Pre-compiled binary for Raspberry Pi OS 32-bit (armv7l) |

## Quick Install (64-bit RPi OS)

```bash
# 1. Apply boot config (requires reboot)
sudo bash deploy/rpi/setup.sh

# OR manually:
cat deploy/rpi/boot_config_snippet.txt | sudo tee -a /boot/firmware/config.txt

# 2. Install systemd-networkd CAN units
sudo cp deploy/rpi/can0.network /etc/systemd/network/80-can0.network
sudo systemctl enable --now systemd-networkd

# 3. Reboot
sudo reboot

# 4. Drop in the pre-compiled binary
sudo cp deploy/rpi/bin/teccanprobe_linux_arm64 /usr/local/bin/teccanprobe
sudo chmod +x /usr/local/bin/teccanprobe

# 5. Verify and probe
teccanprobe -if can0 -profile pixtend-v2l -checklist
teccanprobe -if can0 -profile pixtend-v2l -active -nodes 1-16
```

## Rebuilding the Binary

The pre-compiled binaries are built from the module root. To rebuild:

```bash
# 64-bit RPi OS
GOOS=linux GOARCH=arm64 go build -o deploy/rpi/bin/teccanprobe_linux_arm64 ./cmd/teccanprobe

# 32-bit RPi OS
GOOS=linux GOARCH=arm go build -o deploy/rpi/bin/teccanprobe_linux_arm7 ./cmd/teccanprobe
```
