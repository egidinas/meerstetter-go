#!/usr/bin/env bash
set -euo pipefail

# Bounded recovery verifier for the PiXtend Meerstetter-Go edge route.
# This script restarts only the API/decoder service and verifies that the
# SocketCAN ring remains readable and live telemetry resumes. It does not write
# to TEC controllers.

BASE_URL=${BASE_URL:-http://192.168.6.229:18080}
PI_HOST=${PI_HOST:-pi@192.168.6.229}
SSH_KEY=${SSH_KEY:-$HOME/.ssh/router_lan_can}
TIMEOUT=${TIMEOUT:-10}
WAIT_SECONDS=${WAIT_SECONDS:-20}
CAN_LIMIT=${CAN_LIMIT:-32}

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

fetch() {
    local path=$1
    local out=$2
    curl -fsS --max-time "$TIMEOUT" "$BASE_URL$path" -o "$out"
}

require_jq() {
    command -v jq >/dev/null 2>&1 || fail "jq is required"
}

ssh_pi() {
    ssh -i "$SSH_KEY" -o BatchMode=yes -o ConnectTimeout="$TIMEOUT" "$PI_HOST" "$@"
}

wait_for_health() {
    local out=$1
    local deadline=$((SECONDS + WAIT_SECONDS))
    while (( SECONDS < deadline )); do
        if fetch "/api/health" "$out" 2>/dev/null && jq -e '.ok == true' "$out" >/dev/null; then
            return 0
        fi
        sleep 1
    done
    return 1
}

require_jq

before="$tmpdir/health_before.json"
fetch "/api/health" "$before"
jq -e '
  .ok == true
  and (.devices | tonumber) >= 4
  and (.latestSeq | tonumber) > 0
  and .can_ring.ok == true
' "$before" >/dev/null || fail "pre-restart health failed: $(cat "$before")"
before_seq=$(jq -r '.latestSeq | tonumber' "$before")
before_primary_records=$(jq -r '.can_ring.stats.TotalRecords | tonumber' "$before")
before_fallback_records=$(jq -r '.can_ring.fallback.TotalRecords | tonumber' "$before")
ok "pre-restart health ok seq=$before_seq primary_records=$before_primary_records fallback_records=$before_fallback_records"

ssh_pi 'systemctl is-active meerstettergo.service pixtend-can-ring.service >/dev/null'
ok "Pi services are active before restart"

ssh_pi 'sudo systemctl restart meerstettergo.service'
ok "restarted meerstettergo.service"

after="$tmpdir/health_after.json"
wait_for_health "$after" || fail "health did not recover within ${WAIT_SECONDS}s"
jq -e '
  .ok == true
  and (.devices | tonumber) >= 4
  and (.latestSeq | tonumber) > 0
  and .can_ring.ok == true
' "$after" >/dev/null || fail "post-restart health failed: $(cat "$after")"
after_seq=$(jq -r '.latestSeq | tonumber' "$after")
after_primary_records=$(jq -r '.can_ring.stats.TotalRecords | tonumber' "$after")
after_fallback_records=$(jq -r '.can_ring.fallback.TotalRecords | tonumber' "$after")
if (( after_primary_records < before_primary_records )); then
    fail "primary ring record counter regressed: $before_primary_records -> $after_primary_records"
fi
if (( after_fallback_records < before_fallback_records )); then
    fail "fallback ring record counter regressed: $before_fallback_records -> $after_fallback_records"
fi
ok "post-restart health ok seq=$after_seq primary_records=$after_primary_records fallback_records=$after_fallback_records"

sleep 2
later="$tmpdir/health_later.json"
fetch "/api/health" "$later"
later_seq=$(jq -r '.latestSeq | tonumber' "$later")
if (( later_seq <= after_seq )); then
    fail "telemetry sequence did not advance after recovery: $after_seq -> $later_seq"
fi
ok "telemetry resumes after restart: $after_seq -> $later_seq"

merged="$tmpdir/merged_can_ring.json"
fetch "/api/can/ring?source=merged&limit=$CAN_LIMIT" "$merged" || fail "merged CAN ring did not respond within ${TIMEOUT}s after restart"
jq -e --argjson min "$CAN_LIMIT" '
  .ok == true
  and .source == "merged"
  and .storage == "primary_ram+fallback_flash"
  and (.records | length) >= $min
  and (.primary.stats.TotalRecords | tonumber) > 0
  and (.fallback.stats.TotalRecords | tonumber) > 0
' "$merged" >/dev/null || fail "merged CAN ring failed after restart: $(cat "$merged")"
duplicate_keys=$(jq '[.records[] | "\(.time)|\(.interface)|\(.id_hex)|\(.dlc)|\(.data_hex)"] as $s | ($s | length) - ($s | unique | length)' "$merged")
if (( duplicate_keys != 0 )); then
    fail "merged CAN ring returned duplicate mirrored frame keys after restart: $duplicate_keys"
fi
ok "merged CAN ring readable after restart without duplicate mirrored frame keys"

tiles="$tmpdir/tiles.json"
fetch "/api/tiles?aggregate=temperature&limit=1" "$tiles" || fail "temperature graph tile route did not respond within ${TIMEOUT}s after restart"
jq -e '
  (.series | length) > 0
  and (([.series[].points | length] | add) > 0)
  and ([.series[].latest? | select(. != null)] | length) > 0
' "$tiles" >/dev/null || fail "temperature graph tiles did not recover live points"
ok "graph-wall temperature tile recovers live points"

ssh_pi 'systemctl is-active meerstettergo.service pixtend-can-ring.service >/dev/null'
ok "Pi services remain active after recovery check"

printf 'PASS PiXtend recovery verified at %s\n' "$BASE_URL"
