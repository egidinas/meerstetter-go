#!/usr/bin/env bash
set -euo pipefail

out="${1:-/tmp/meerstetter-go-handoff.tgz}"

if [[ "$out" == docs/gateway/dist/* ]]; then
  install -m 0644 docs/gateway/HANDOFF.md docs/gateway/dist/HANDOFF.md
fi

tar czf "$out" \
  README.md \
  deploy/README.md \
  deploy/example-gateway.json \
  docs/backlog/frontend_hooks.jsonl \
  docs/gateway/HANDOFF.md \
  docs/gateway/UI_GRAPH_WALL_CONTRACT.md \
  docs/gateway/demo/index.html \
  docs/gateway/console \
  docs/gateway/openapi.yaml \
  docs/gateway/types.d.ts

ls -lh "$out"
