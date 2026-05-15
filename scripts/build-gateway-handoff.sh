#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${1:-/tmp/meerstetter-go-handoff.tgz}"

# Build the Vite UI bundle if node_modules exists; skip in CI environments
# where it was already built by a prior step.
if [[ -d "$REPO_ROOT/web/node_modules" ]]; then
  echo "==> building web/dist..."
  npm --prefix "$REPO_ROOT/web" run build
elif [[ ! -d "$REPO_ROOT/web/dist" ]]; then
  echo "ERROR: web/dist does not exist and node_modules is absent — run 'npm --prefix web install && npm --prefix web run build' first" >&2
  exit 1
fi

tar czf "$out" \
  -C "$REPO_ROOT" \
  README.md \
  deploy/README.md \
  deploy/example-gateway.json \
  docs/backlog/frontend_hooks.jsonl \
  docs/gateway/HANDOFF.md \
  docs/gateway/UI_GRAPH_WALL_CONTRACT.md \
  docs/gateway/demo/index.html \
  docs/gateway/console \
  docs/gateway/openapi.yaml \
  docs/gateway/types.d.ts \
  web/dist

ls -lh "$out"
