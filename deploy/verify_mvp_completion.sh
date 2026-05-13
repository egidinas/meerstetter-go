#!/usr/bin/env bash
set -euo pipefail

# One-command, non-invasive MVP gate for the live Meerstetter-Go PiXtend route.
# Default mode avoids service restarts and controller writes. Set RUN_RECOVERY=1
# for bounded service-restart recovery checks.

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
LOOM_ROOT=${LOOM_ROOT:-"$ROOT/../loom"}
GO_BIN=${GO_BIN:-go}

PI_BASE_URL=${PI_BASE_URL:-http://192.168.6.229:18080}
LOOM_BASE_URL=${LOOM_BASE_URL:-http://127.0.0.1:18087}

RUN_UI=${RUN_UI:-1}
RUN_TESTS=${RUN_TESTS:-1}
RUN_RECOVERY=${RUN_RECOVERY:-0}
RUN_AUTONOMY=${RUN_AUTONOMY:-1}
RUN_OWNER_TAKEOVER=${RUN_OWNER_TAKEOVER:-1}

step() {
    printf '\n== %s\n' "$*"
}

ok() {
    printf 'OK   %s\n' "$*"
}

fail() {
    printf 'FAIL %s\n' "$*" >&2
    exit 1
}

require_file() {
    local path=$1
    [[ -x "$path" ]] || fail "missing executable verifier: $path"
}

require_file "$ROOT/deploy/verify_pixtend_route.sh"
require_file "$ROOT/deploy/verify_loom_gateway_route.sh"

if [[ "$RUN_AUTONOMY" == "1" ]]; then
    require_file "$ROOT/deploy/verify_pixtend_edge_autonomy.sh"
fi

if [[ "$RUN_OWNER_TAKEOVER" == "1" ]]; then
    require_file "$ROOT/deploy/verify_pixtend_owner_takeover.sh"
fi

if [[ "$RUN_UI" == "1" ]]; then
    require_file "$ROOT/deploy/verify_ui_browser_smoke.sh"
fi

if [[ "$RUN_RECOVERY" == "1" ]]; then
    require_file "$ROOT/deploy/verify_pixtend_recovery.sh"
    require_file "$ROOT/deploy/verify_pixtend_ring_recovery.sh"
fi

step "Direct PiXtend edge route"
BASE_URL="$PI_BASE_URL" "$ROOT/deploy/verify_pixtend_route.sh"

step "Loom/operator gateway route"
BASE_URL="$LOOM_BASE_URL" "$ROOT/deploy/verify_loom_gateway_route.sh"

if [[ "$RUN_AUTONOMY" == "1" ]]; then
    step "PiXtend edge autonomy"
    BASE_URL="$PI_BASE_URL" GATEWAY_BASE_URL="$LOOM_BASE_URL" "$ROOT/deploy/verify_pixtend_edge_autonomy.sh"
else
    ok "edge autonomy gate skipped (RUN_AUTONOMY=0)"
fi

if [[ "$RUN_OWNER_TAKEOVER" == "1" ]]; then
    step "PiXtend owner reconnect/takeover route"
    BASE_URL="$PI_BASE_URL" GATEWAY_BASE_URL="$LOOM_BASE_URL" "$ROOT/deploy/verify_pixtend_owner_takeover.sh"
else
    ok "owner reconnect/takeover gate skipped (RUN_OWNER_TAKEOVER=0)"
fi

if [[ "$RUN_UI" == "1" ]]; then
    step "Direct edge browser UI"
    BASE_URL="$PI_BASE_URL" "$ROOT/deploy/verify_ui_browser_smoke.sh"
else
    ok "browser UI smoke skipped (RUN_UI=0)"
fi

if [[ "$RUN_RECOVERY" == "1" ]]; then
    step "Decoder service recovery"
    BASE_URL="$PI_BASE_URL" "$ROOT/deploy/verify_pixtend_recovery.sh"

    step "CAN ring worker recovery"
    BASE_URL="$PI_BASE_URL" "$ROOT/deploy/verify_pixtend_ring_recovery.sh"
else
    ok "bounded recovery gates skipped by default (set RUN_RECOVERY=1 to restart edge services)"
fi

if [[ "$RUN_TESTS" == "1" ]]; then
    if [[ "$GO_BIN" == */* ]]; then
        [[ -x "$GO_BIN" ]] || fail "Go binary not executable: $GO_BIN"
    else
        command -v "$GO_BIN" >/dev/null 2>&1 || fail "Go binary not found on PATH: $GO_BIN"
    fi

    step "Meerstetter-Go targeted tests"
    (
        cd "$ROOT"
        "$GO_BIN" test ./utility ./mecom ./mecomserver ./cmd/mod-ingestor-mecom ./cmd/teccanprobe
    )

    if [[ -d "$LOOM_ROOT/internal/operatoruiapi" && -d "$LOOM_ROOT/internal/signalforgeadapter" ]]; then
        step "Loom targeted tests"
        (
            cd "$LOOM_ROOT"
            "$GO_BIN" test ./internal/operatoruiapi ./internal/signalforgeadapter
        )
    else
        ok "Loom targeted test packages skipped; expected packages not present under $LOOM_ROOT"
    fi
else
    ok "targeted Go tests skipped (RUN_TESTS=0)"
fi

if [[ "$RUN_RECOVERY" == "1" ]]; then
    recovery_line="- bounded decoder-service and CAN-ring-worker restart recovery"
    uncovered_recovery="- power-interruption recovery\n- real gateway/owner process-stop timing\n- end-to-end leased write acceptance"
else
    recovery_line="- bounded service restart recovery skipped; set RUN_RECOVERY=1 to include it"
    uncovered_recovery="- power-interruption recovery\n- real gateway/owner process-stop timing\n- service restart recovery unless RUN_RECOVERY=1 is set\n- end-to-end leased write acceptance"
fi

if [[ "$RUN_UI" == "1" ]]; then
    ui_line="- direct browser UI data population"
else
    ui_line="- direct browser UI data population skipped; set RUN_UI=1 to include it"
fi

if [[ "$RUN_TESTS" == "1" ]]; then
    tests_line="- targeted Meerstetter-Go and Loom adapter tests"
else
    tests_line="- targeted Go tests skipped; set RUN_TESTS=1 to include them"
fi

if [[ "$RUN_AUTONOMY" == "1" ]]; then
    autonomy_line="- PiXtend edge telemetry and CAN ring autonomy during a gateway-idle window"
else
    autonomy_line="- PiXtend edge autonomy skipped; set RUN_AUTONOMY=1 to include it"
fi

if [[ "$RUN_OWNER_TAKEOVER" == "1" ]]; then
    owner_takeover_line="- route-level owner reconnect/takeover after a gateway-idle window"
else
    owner_takeover_line="- owner reconnect/takeover skipped; set RUN_OWNER_TAKEOVER=1 to include it"
fi

cat <<EOF

PASS Meerstetter-Go MVP gate

Verified coverage:
- live PiXtend SocketCAN edge route at $PI_BASE_URL
- Loom/operator gateway route at $LOOM_BASE_URL/api/operator/meerstettergo
- four-controller decoded telemetry, discovery tree, graph wall, write guard, source catalogue, export/import, Arrow IPC
- primary RAM CAN ring plus flash fallback ring and merged deduplicated readout
$autonomy_line
$owner_takeover_line
$ui_line
$tests_line
$recovery_line

Not covered by this run:
$(printf '%b' "$uncovered_recovery")
EOF
