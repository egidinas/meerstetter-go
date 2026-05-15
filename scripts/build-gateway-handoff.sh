#!/usr/bin/env bash
set -euo pipefail

out="${1:-/tmp/meerstetter-go-handoff.tgz}"

tar czf "$out" \
  README.md \
  deploy/README.md \
  deploy/example-gateway.json \
  docs/backlog/frontend_hooks.jsonl \
  docs/gateway/HANDOFF.md \
  docs/gateway/demo/index.html \
  docs/gateway/openapi.yaml \
  docs/gateway/types.d.ts

ls -lh "$out"
