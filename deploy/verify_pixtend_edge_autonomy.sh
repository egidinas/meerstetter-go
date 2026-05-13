#!/usr/bin/env bash
set -euo pipefail

# Non-invasive autonomy verifier for the PiXtend Meerstetter-Go edge route.
# It proves that the Pi edge service advances telemetry and CAN ring state
# without continuous gateway polling. It does not stop services or write to TECs.

BASE_URL=${BASE_URL:-http://192.168.6.229:18080}
GATEWAY_BASE_URL=${GATEWAY_BASE_URL:-http://127.0.0.1:18087}
GATEWAY_PREFIX=${GATEWAY_PREFIX:-/api/operator/meerstettergo}
EXPECTED_DEVICES=${EXPECTED_DEVICES:-4}
WINDOW_SECONDS=${WINDOW_SECONDS:-5}
ADVANCE_GRACE_SECONDS=${ADVANCE_GRACE_SECONDS:-20}
ADVANCE_POLL_SECONDS=${ADVANCE_POLL_SECONDS:-2}
TIMEOUT=${TIMEOUT:-5}
CAN_LIMIT=${CAN_LIMIT:-64}
LOG_LIMIT=${LOG_LIMIT:-800}
REQUIRE_GATEWAY=${REQUIRE_GATEWAY:-1}

tmpdir=$(mktemp -d)
cleanup() {
    rm -rf "$tmpdir"
}
trap cleanup EXIT

ok() {
    printf 'OK   %s\n' "$*"
}

warn() {
    printf 'WARN %s\n' "$*"
}

fail() {
    printf 'FAIL %s\n' "$*" >&2
    exit 1
}

require_jq() {
    command -v jq >/dev/null 2>&1 || fail "jq is required"
}

fetch_edge() {
    local path=$1
    local out=$2
    curl -fsS --max-time "$TIMEOUT" "$BASE_URL$path" -o "$out"
}

fetch_gateway() {
    local path=$1
    local out=$2
    curl -fsS --max-time "$TIMEOUT" "$GATEWAY_BASE_URL$GATEWAY_PREFIX$path" -o "$out"
}

require_jq

edge_health1="$tmpdir/edge_health1.json"
edge_health2="$tmpdir/edge_health2.json"
fetch_edge "/api/health" "$edge_health1"
jq -e --argjson expected "$EXPECTED_DEVICES" '
  .ok == true
  and (.devices | tonumber) >= $expected
  and (.latestSeq | tonumber) > 0
  and (.can_ring.configured == true)
  and (.can_ring.ok == true)
  and ((.can_ring.source == "primary_ram") or (.can_ring.storage == "primary_ram"))
  and (.can_ring.stats.TotalRecords | tonumber) > 0
  and (.can_ring.fallback.TotalRecords | tonumber) > 0
' "$edge_health1" >/dev/null || fail "edge health contract failed: $(cat "$edge_health1")"

seq1=$(jq -r '.latestSeq | tonumber' "$edge_health1")
ram1=$(jq -r '.can_ring.stats.TotalRecords | tonumber' "$edge_health1")
flash1=$(jq -r '.can_ring.fallback.TotalRecords | tonumber' "$edge_health1")
ok "edge health starts live at seq=$seq1 ram_frames=$ram1 flash_frames=$flash1"

gateway_catalogue="$tmpdir/gateway_catalogue.json"
if fetch_gateway "/source-catalogue" "$gateway_catalogue"; then
    jq -e '
      .selection_owner == "loom.operator"
      and ([.catalogues[].command_inputs[]? | select(.access == "lease_required")] | length) >= 1
      and ([
        .discovery_policy.remote_routes[]?.gateway_endpoint,
        .catalogues[].capabilities.remote_routes[]?.gateway_endpoint
      ] | index("/api/operator/meerstettergo/health") != null)
    ' "$gateway_catalogue" >/dev/null || fail "gateway catalogue does not advertise Loom ownership and route contract"
    ok "gateway advertises Loom ownership and lease-gated command contract"
else
    if [[ "$REQUIRE_GATEWAY" == "1" ]]; then
        fail "gateway source catalogue unavailable at $GATEWAY_BASE_URL$GATEWAY_PREFIX/source-catalogue"
    fi
    warn "gateway unavailable; continuing direct edge autonomy proof because REQUIRE_GATEWAY=0"
fi

# Deliberately avoid gateway reads during this window. The edge route must keep
# advancing from its own Pi worker and SocketCAN ring ingestion.
sleep "$WINDOW_SECONDS"

fetch_edge "/api/health" "$edge_health2"
jq -e --argjson expected "$EXPECTED_DEVICES" '
  .ok == true
  and (.devices | tonumber) >= $expected
  and (.can_ring.configured == true)
  and (.can_ring.ok == true)
' "$edge_health2" >/dev/null || fail "edge health failed after autonomy window: $(cat "$edge_health2")"

seq2=$(jq -r '.latestSeq | tonumber' "$edge_health2")
ram2=$(jq -r '.can_ring.stats.TotalRecords | tonumber' "$edge_health2")
flash2=$(jq -r '.can_ring.fallback.TotalRecords | tonumber' "$edge_health2")

deadline=$((SECONDS + ADVANCE_GRACE_SECONDS))
while (( (seq2 <= seq1 || ram2 <= ram1) && SECONDS < deadline )); do
    sleep "$ADVANCE_POLL_SECONDS"
    fetch_edge "/api/health" "$edge_health2"
    seq2=$(jq -r '.latestSeq | tonumber' "$edge_health2")
    ram2=$(jq -r '.can_ring.stats.TotalRecords | tonumber' "$edge_health2")
    flash2=$(jq -r '.can_ring.fallback.TotalRecords | tonumber' "$edge_health2")
done

if (( seq2 <= seq1 )); then
    fail "edge telemetry did not advance during ${WINDOW_SECONDS}s gateway-idle window plus ${ADVANCE_GRACE_SECONDS}s grace: $seq1 -> $seq2"
fi
if (( ram2 <= ram1 )); then
    fail "primary RAM CAN ring did not advance during ${WINDOW_SECONDS}s gateway-idle window plus ${ADVANCE_GRACE_SECONDS}s grace: $ram1 -> $ram2"
fi
if (( flash2 < flash1 )); then
    fail "flash fallback CAN ring regressed during ${WINDOW_SECONDS}s window plus ${ADVANCE_GRACE_SECONDS}s grace: $flash1 -> $flash2"
fi
ok "edge advances without continuous gateway reads: seq $seq1 -> $seq2, RAM frames $ram1 -> $ram2"

merged_can_ring="$tmpdir/merged_can_ring.json"
fetch_edge "/api/can/ring?source=merged&limit=$CAN_LIMIT" "$merged_can_ring"
jq -e --argjson min "$CAN_LIMIT" '
  .ok == true
  and .source == "merged"
  and .storage == "primary_ram+fallback_flash"
  and (.records | length) >= $min
  and (.primary.stats.TotalRecords | tonumber) > 0
  and (.fallback.stats.TotalRecords | tonumber) > 0
' "$merged_can_ring" >/dev/null || fail "merged edge CAN ring contract failed: $(cat "$merged_can_ring")"
duplicate_keys=$(jq '[.records[] | "\(.time)|\(.interface)|\(.id_hex)|\(.dlc)|\(.data_hex)"] as $s | ($s | length) - ($s | unique | length)' "$merged_can_ring")
if (( duplicate_keys != 0 )); then
    fail "merged edge CAN ring returned duplicate mirrored frame keys: $duplicate_keys"
fi
ok "merged edge CAN ring reads RAM plus flash fallback without duplicate frame keys"

log_ring="$tmpdir/log_ring.json"
fetch_edge "/api/log/ring?tail=true&limit=$LOG_LIMIT" "$log_ring"
jq -e --argjson expected "$EXPECTED_DEVICES" '
  length > 0
  and ([.[].tm.target_id? | select(type == "string" and startswith("device:")) | split(":")[1]] | unique | length) >= $expected
' "$log_ring" >/dev/null || fail "decoded edge log ring does not include $EXPECTED_DEVICES devices"
device_count=$(jq '[.[].tm.target_id? | select(type == "string" and startswith("device:")) | split(":")[1]] | unique | length' "$log_ring")
target_count=$(jq '[.[].tm.target_id? | select(type == "string")] | unique | length' "$log_ring")
ok "edge decoded log ring covers $device_count devices and $target_count targets"

cat <<EOF

PASS PiXtend edge autonomy route

Verified:
- direct edge telemetry and RAM CAN ring advance during a ${WINDOW_SECONDS}s gateway-idle window
- flash fallback ring remains readable and non-regressing
- merged RAM/flash ring readout deduplicates mirrored frames
- decoded ring includes all expected TEC controllers

Not simulated:
- physical power interruption
- intentional gateway/owner service outage
- CAN bus congestion
EOF
