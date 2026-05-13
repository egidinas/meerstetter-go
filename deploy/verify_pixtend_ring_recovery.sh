#!/usr/bin/env bash
set -euo pipefail

# Bounded recovery verifier for the PiXtend SocketCAN ring worker.
# This restarts only pixtend-can-ring.service, then verifies that the edge API,
# RAM ring, flash fallback, merged raw-CAN view, and decoded telemetry recover.
# It does not write to TEC controllers.

BASE_URL=${BASE_URL:-http://192.168.6.229:18080}
PI_HOST=${PI_HOST:-pi@192.168.6.229}
SSH_KEY=${SSH_KEY:-$HOME/.ssh/router_lan_can}
TIMEOUT=${TIMEOUT:-10}
WAIT_SECONDS=${WAIT_SECONDS:-25}
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
        if fetch "/api/health" "$out" 2>/dev/null && jq -e '
          .ok == true
          and (.devices | tonumber) >= 4
          and (.latestSeq | tonumber) > 0
          and .can_ring.ok == true
          and (.can_ring.stats.TotalRecords | tonumber) > 0
          and (.can_ring.fallback.TotalRecords | tonumber) > 0
        ' "$out" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    return 1
}

wait_for_ring_advance() {
    local before_seq=$1
    local before_primary=$2
    local out=$3
    local deadline=$((SECONDS + WAIT_SECONDS))
    while (( SECONDS < deadline )); do
        if fetch "/api/health" "$out" 2>/dev/null; then
            local seq primary
            seq=$(jq -r '.latestSeq | tonumber' "$out" 2>/dev/null || printf '0')
            primary=$(jq -r '.can_ring.stats.TotalRecords | tonumber' "$out" 2>/dev/null || printf '0')
            if (( seq > before_seq && primary > before_primary )); then
                return 0
            fi
        fi
        sleep 1
    done
    return 1
}

assert_merged_ring() {
    local path=$1
    jq -e --argjson min "$CAN_LIMIT" '
      .ok == true
      and .source == "merged"
      and .storage == "primary_ram+fallback_flash"
      and (.records | length) >= $min
      and (.primary.stats.TotalRecords | tonumber) > 0
      and (.fallback.stats.TotalRecords | tonumber) > 0
    ' "$path" >/dev/null || fail "merged CAN ring contract failed: $(cat "$path")"

    local duplicate_keys
    duplicate_keys=$(jq '[.records[] | "\(.time)|\(.interface)|\(.id_hex)|\(.dlc)|\(.data_hex)"] as $s | ($s | length) - ($s | unique | length)' "$path")
    if (( duplicate_keys != 0 )); then
        fail "merged CAN ring returned duplicate mirrored frame keys: $duplicate_keys"
    fi
}

require_jq

before="$tmpdir/health_before.json"
fetch "/api/health" "$before"
jq -e '
  .ok == true
  and (.devices | tonumber) >= 4
  and (.latestSeq | tonumber) > 0
  and .can_ring.ok == true
  and (.can_ring.stats.TotalRecords | tonumber) > 0
  and (.can_ring.fallback.TotalRecords | tonumber) > 0
' "$before" >/dev/null || fail "pre-restart health failed: $(cat "$before")"
before_seq=$(jq -r '.latestSeq | tonumber' "$before")
before_primary_records=$(jq -r '.can_ring.stats.TotalRecords | tonumber' "$before")
before_fallback_records=$(jq -r '.can_ring.fallback.TotalRecords | tonumber' "$before")
ok "pre-restart health ok seq=$before_seq primary_records=$before_primary_records fallback_records=$before_fallback_records"

before_merged="$tmpdir/merged_before.json"
fetch "/api/can/ring?source=merged&limit=$CAN_LIMIT" "$before_merged" || fail "merged CAN ring did not respond before restart"
assert_merged_ring "$before_merged"
ok "merged CAN ring readable before ring-worker restart"

ssh_pi 'systemctl is-active meerstettergo.service pixtend-can-ring.service >/dev/null'
ok "Pi services are active before ring-worker restart"

ssh_pi 'sudo systemctl restart pixtend-can-ring.service'
ok "restarted pixtend-can-ring.service"

ssh_pi 'systemctl is-active pixtend-can-ring.service >/dev/null'
ok "pixtend-can-ring.service is active after restart command"

after="$tmpdir/health_after.json"
wait_for_health "$after" || fail "health/CAN ring did not recover within ${WAIT_SECONDS}s"
after_seq=$(jq -r '.latestSeq | tonumber' "$after")
after_primary_records=$(jq -r '.can_ring.stats.TotalRecords | tonumber' "$after")
after_fallback_records=$(jq -r '.can_ring.fallback.TotalRecords | tonumber' "$after")
ok "post-restart health ok seq=$after_seq primary_records=$after_primary_records fallback_records=$after_fallback_records"

later="$tmpdir/health_later.json"
wait_for_ring_advance "$after_seq" "$after_primary_records" "$later" || fail "decoded telemetry and primary RAM ring did not advance within ${WAIT_SECONDS}s after ring-worker restart"
later_seq=$(jq -r '.latestSeq | tonumber' "$later")
later_primary_records=$(jq -r '.can_ring.stats.TotalRecords | tonumber' "$later")
later_fallback_records=$(jq -r '.can_ring.fallback.TotalRecords | tonumber' "$later")
if (( later_fallback_records < after_fallback_records )); then
    fail "flash fallback ring counter regressed after recovery window: $after_fallback_records -> $later_fallback_records"
fi
ok "decoded telemetry and primary RAM ring advance after restart: seq=$after_seq->$later_seq primary=$after_primary_records->$later_primary_records fallback=$after_fallback_records->$later_fallback_records"

after_merged="$tmpdir/merged_after.json"
fetch "/api/can/ring?source=merged&limit=$CAN_LIMIT" "$after_merged" || fail "merged CAN ring did not respond after restart"
assert_merged_ring "$after_merged"
ok "merged CAN ring readable after ring-worker restart without duplicate mirrored frame keys"

tiles="$tmpdir/tiles.json"
fetch "/api/tiles?aggregate=temperature&limit=1" "$tiles" || fail "temperature graph tile route did not respond after ring-worker restart"
jq -e '
  (.series | length) > 0
  and (([.series[].points | length] | add) > 0)
  and ([.series[].latest? | select(. != null)] | length) > 0
' "$tiles" >/dev/null || fail "temperature graph tiles did not recover live points"
ok "graph-wall temperature tile has live points after ring-worker restart"

ssh_pi 'systemctl is-active meerstettergo.service pixtend-can-ring.service >/dev/null'
ok "Pi services remain active after ring-worker recovery check"

printf 'PASS PiXtend ring-worker recovery verified at %s\n' "$BASE_URL"
