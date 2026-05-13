#!/usr/bin/env bash
set -euo pipefail

# Non-invasive owner reconnect/takeover verifier for the PiXtend
# Meerstetter-Go route.
#
# This does not stop services and does not write to TEC controllers. It proves
# the route-level contract: the Pi edge keeps advancing during an owner-idle
# window, then the Loom/operator gateway can reattach to the same stream,
# catch up to the observed edge sequence, and keep write access lease-gated.

BASE_URL=${BASE_URL:-http://192.168.6.229:18080}
GATEWAY_BASE_URL=${GATEWAY_BASE_URL:-http://127.0.0.1:18087}
GATEWAY_PREFIX=${GATEWAY_PREFIX:-/api/operator/meerstettergo}
EXPECTED_DEVICES=${EXPECTED_DEVICES:-4}
IDLE_SECONDS=${IDLE_SECONDS:-6}
TAKEOVER_GRACE_SECONDS=${TAKEOVER_GRACE_SECONDS:-20}
TAKEOVER_POLL_SECONDS=${TAKEOVER_POLL_SECONDS:-2}
TIMEOUT=${TIMEOUT:-8}
CAN_LIMIT=${CAN_LIMIT:-64}
LOG_LIMIT=${LOG_LIMIT:-800}

tmpdir=$(mktemp -d)
cleanup() {
    rm -rf "$tmpdir"
}
trap cleanup EXIT

ok() {
    printf 'OK   %s\n' "$*"
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

health_seq() {
    jq -r '.latestSeq | tonumber' "$1"
}

ram_count() {
    jq -r '.can_ring.stats.TotalRecords | tonumber' "$1"
}

flash_count() {
    jq -r '.can_ring.fallback.TotalRecords | tonumber' "$1"
}

assert_health() {
    local file=$1
    local label=$2
    jq -e --argjson expected "$EXPECTED_DEVICES" '
      .ok == true
      and (.devices | tonumber) >= $expected
      and (.latestSeq | tonumber) > 0
      and (.can_ring.configured == true)
      and (.can_ring.ok == true)
      and ((.can_ring.source == "primary_ram") or (.can_ring.storage == "primary_ram"))
      and (.can_ring.stats.TotalRecords | tonumber) > 0
      and (.can_ring.fallback.TotalRecords | tonumber) > 0
    ' "$file" >/dev/null || fail "$label health contract failed: $(cat "$file")"
}

assert_merged_ring() {
    local file=$1
    local label=$2
    jq -e --argjson min "$CAN_LIMIT" '
      .ok == true
      and .source == "merged"
      and .storage == "primary_ram+fallback_flash"
      and (.records | length) >= $min
      and (.primary.stats.TotalRecords | tonumber) > 0
      and (.fallback.stats.TotalRecords | tonumber) > 0
    ' "$file" >/dev/null || fail "$label merged CAN ring contract failed: $(cat "$file")"
    local duplicate_keys
    duplicate_keys=$(jq '[.records[] | "\(.time)|\(.interface)|\(.id_hex)|\(.dlc)|\(.data_hex)"] as $s | ($s | length) - ($s | unique | length)' "$file")
    if (( duplicate_keys != 0 )); then
        fail "$label merged CAN ring returned duplicate mirrored frame keys: $duplicate_keys"
    fi
}

require_jq

edge_before="$tmpdir/edge_before.json"
gateway_before="$tmpdir/gateway_before.json"
catalogue="$tmpdir/source_catalogue.json"

fetch_edge "/api/health" "$edge_before"
assert_health "$edge_before" "edge"
edge_seq_before=$(health_seq "$edge_before")
edge_ram_before=$(ram_count "$edge_before")
edge_flash_before=$(flash_count "$edge_before")
ok "edge starts live at seq=$edge_seq_before ram_frames=$edge_ram_before flash_frames=$edge_flash_before"

fetch_gateway "/health" "$gateway_before"
assert_health "$gateway_before" "gateway"
gateway_seq_before=$(health_seq "$gateway_before")
ok "gateway starts live at seq=$gateway_seq_before"

fetch_gateway "/source-catalogue" "$catalogue"
jq -e '
  [
    .discovery_policy.remote_routes[]?.gateway_endpoint,
    .catalogues[].capabilities.remote_routes[]?.gateway_endpoint
  ] as $routes
  |
  .schema_version == 1
  and .selection_owner == "loom.operator"
  and ([.catalogues[].command_inputs[]? | select(.command_id == "meerstettergo.tec.write" and .access == "lease_required")] | length) >= 1
  and ([$routes[] | select(. == "/api/operator/meerstettergo/health")] | length) >= 1
  and ([$routes[] | select(. == "/api/operator/meerstettergo/can/ring")] | length) >= 1
  and ([$routes[] | select(. == "/api/operator/meerstettergo/target/write")] | length) >= 1
' "$catalogue" >/dev/null || fail "gateway catalogue does not advertise Loom ownership, ring route, and lease-gated write route"
ok "gateway advertises Loom ownership, remote ring route, and lease-gated writes"

# Owner idle window: deliberately avoid gateway reads. Only direct edge reads
# are used after the sleep so the gateway is not acting as the owner/consumer.
sleep "$IDLE_SECONDS"

edge_after="$tmpdir/edge_after.json"
fetch_edge "/api/health" "$edge_after"
assert_health "$edge_after" "edge after owner-idle window"
edge_seq_after=$(health_seq "$edge_after")
edge_ram_after=$(ram_count "$edge_after")
edge_flash_after=$(flash_count "$edge_after")

deadline=$((SECONDS + TAKEOVER_GRACE_SECONDS))
while (( (edge_seq_after <= edge_seq_before || edge_ram_after <= edge_ram_before) && SECONDS < deadline )); do
    sleep "$TAKEOVER_POLL_SECONDS"
    fetch_edge "/api/health" "$edge_after"
    edge_seq_after=$(health_seq "$edge_after")
    edge_ram_after=$(ram_count "$edge_after")
    edge_flash_after=$(flash_count "$edge_after")
done

if (( edge_seq_after <= edge_seq_before )); then
    fail "edge telemetry did not advance during owner-idle window: $edge_seq_before -> $edge_seq_after"
fi
if (( edge_ram_after <= edge_ram_before )); then
    fail "edge RAM CAN ring did not advance during owner-idle window: $edge_ram_before -> $edge_ram_after"
fi
if (( edge_flash_after < edge_flash_before )); then
    fail "edge flash fallback regressed during owner-idle window: $edge_flash_before -> $edge_flash_after"
fi
ok "edge advanced while owner was idle: seq $edge_seq_before -> $edge_seq_after, RAM frames $edge_ram_before -> $edge_ram_after"

gateway_after="$tmpdir/gateway_after.json"
fetch_gateway "/health" "$gateway_after"
assert_health "$gateway_after" "gateway after reattach"
gateway_seq_after=$(health_seq "$gateway_after")

deadline=$((SECONDS + TAKEOVER_GRACE_SECONDS))
while (( gateway_seq_after < edge_seq_after && SECONDS < deadline )); do
    sleep "$TAKEOVER_POLL_SECONDS"
    fetch_gateway "/health" "$gateway_after"
    gateway_seq_after=$(health_seq "$gateway_after")
done

if (( gateway_seq_after < edge_seq_after )); then
    fail "gateway did not catch up to edge sequence after reattach: gateway=$gateway_seq_after edge=$edge_seq_after"
fi
ok "gateway reattached and caught up: gateway seq $gateway_seq_before -> $gateway_seq_after, edge checkpoint=$edge_seq_after"

gateway_ring="$tmpdir/gateway_merged_ring.json"
fetch_gateway "/can/ring?source=merged&limit=$CAN_LIMIT" "$gateway_ring"
assert_merged_ring "$gateway_ring" "gateway"
ok "gateway merged RAM/flash CAN ring remains deduplicated after reattach"

gateway_log="$tmpdir/gateway_log_ring.json"
fetch_gateway "/log/ring?tail=true&limit=$LOG_LIMIT" "$gateway_log"
jq -e --argjson expected "$EXPECTED_DEVICES" '
  length > 0
  and ([.[].tm.target_id? | select(type == "string" and startswith("device:")) | split(":")[1]] | unique | length) >= $expected
' "$gateway_log" >/dev/null || fail "gateway decoded log ring does not include $EXPECTED_DEVICES devices after reattach"
ok "gateway decoded ring still covers all expected devices after reattach"

gateway_tiles="$tmpdir/gateway_tiles.json"
fetch_gateway "/tiles?aggregate=temperature&limit=1" "$gateway_tiles"
jq -e '
  (.series | length) > 0
  and (([.series[].points | length] | add) > 0)
  and ([.series[].latest? | select(. != null)] | length) > 0
' "$gateway_tiles" >/dev/null || fail "gateway temperature tile has no live points after reattach"
ok "gateway graph-wall temperature tile has live points after reattach"

tree="$tmpdir/tree.json"
write_payload="$tmpdir/write_no_lease.json"
write_response="$tmpdir/write_no_lease.out"
fetch_gateway "/discovery/tree" "$tree"
writable_target=$(jq -r '[.. | objects | select(.metadata.writable? == "true") | .id][0] // empty' "$tree")
[[ -n "$writable_target" ]] || fail "gateway no writable target available for write-guard verification"
jq -n --arg target_id "$writable_target" '{target_id:$target_id,value:0}' > "$write_payload"
write_code=$(curl -sS --max-time "$TIMEOUT" -o "$write_response" -w '%{http_code}' -H 'Content-Type: application/json' -X POST --data-binary "@$write_payload" "$GATEWAY_BASE_URL$GATEWAY_PREFIX/target/write")
case "$write_code" in
    403)
        grep -q 'X-Loom-Sequencer-Lease' "$write_response" || fail "gateway write rejection did not mention X-Loom-Sequencer-Lease: $(cat "$write_response")"
        ;;
    428)
        grep -q 'lease_id' "$write_response" || fail "gateway write rejection did not mention lease_id: $(cat "$write_response")"
        ;;
    *)
        fail "gateway write without sequencer lease was not safely rejected after reattach: code=$write_code body=$(cat "$write_response")"
        ;;
esac
ok "gateway write path remains lease-gated after reattach"

cat <<EOF

PASS PiXtend owner reconnect/takeover route

Verified:
- direct edge telemetry and RAM CAN ring advance during a ${IDLE_SECONDS}s owner-idle window
- flash fallback remains readable and non-regressing
- Loom/operator gateway reattaches and catches up to the direct edge sequence
- gateway merged RAM/flash ring remains deduplicated
- gateway decoded ring and graph-wall tile remain populated after reattach
- gateway write path remains lease-gated

Not simulated:
- stopping the real Loom gateway process
- stopping a dedicated owner process
- physical power interruption
- CAN bus congestion
- controller-internal ring-buffer gap-fill
EOF
