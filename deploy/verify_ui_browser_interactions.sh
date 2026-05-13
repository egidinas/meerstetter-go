#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
exec node "$ROOT/deploy/verify_ui_browser_interactions.mjs"
