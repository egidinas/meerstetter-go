# Gateway Operator Console Prototype

This directory preserves the Claude Design operator-console prototype for the
Meerstetter gateway. It is a static React/Babel prototype that can run in mock
mode or against a live `cmd/mecomgw` API.

Run it through the gateway:

```sh
go run ./cmd/mecomgw \
  -config deploy/example-gateway.json \
  -listen 127.0.0.1:18080 \
  -ui-dir docs/gateway/console
```

Open `http://127.0.0.1:18080/ui/`.

Leave Settings -> Gateway URL blank for mock data. Set it to the gateway origin
for live `/api/*` calls.

The console follows `docs/gateway/UI_GRAPH_WALL_CONTRACT.md`:

- temperature hero graph: target object temperature (`3000`), object
  temperature (`1000`), sink temperature (`1001`), optional cascade temperature
  (`52200`);
- power hero graph: output voltage (`1021`), output current (`1020`), output
  power (`1022`);
- write commands use `set_float32` only for `3000`, `write_float32` for generic
  float parameters, and `write_int32` for integer controls such as `2010` and
  `2040`;
- telemetry and telecommand accordions stay at the bottom of the main graph
  surfaces so commands can be staged next to the values they affect.

The sequencer, archive export, and PID writeback panes are UI affordances for
the planned gateway surfaces. The PID advisor observes and suggests only in
this prototype; automatic gain writes are intentionally disabled until the
backend contract is live.
