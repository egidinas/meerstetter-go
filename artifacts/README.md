# Deployment Artifacts

This directory contains tracked binaries that were built during the live
PiXtend/FTDI bring-up. They are kept out of the repository root so source,
examples, deployment wrappers, and generated deliverables are easy to separate.

Source of truth remains the Go code under `cmd/` and the deployment wrappers in
`deploy/systemd`. Rebuild these binaries from source when changing behavior.

## Layout

- `linux-armv7/meerstetterd`: Raspberry Pi service binary. The systemd unit
  installs/runs it as `/usr/local/bin/meerstetterd`.
- `linux-armv7/teccanprobe`: Raspberry Pi SocketCAN capture-ring worker. The
  systemd unit installs/runs it as `/usr/local/bin/teccanprobe`.
- `linux-armv7/mecomprobe`: Raspberry Pi MeCom diagnostic utility.
- `linux-amd64/mecomprobe-ftdi`: Linux host FTDI/serial diagnostic utility.
- `linux-amd64/mecomset-ftdi`: Linux host FTDI/serial write utility.

## Operational Rule

Do not add new root-level binaries. Put checked-in operational artifacts under a
platform-specific `artifacts/<platform>/` directory, and keep disposable local
builds in the ignored `bin/` directory.
