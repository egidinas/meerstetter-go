#!/usr/bin/env bash
set -euo pipefail

# Non-invasive live route verifier for the Loom/operator gateway path to the
# PiXtend Meerstetter-Go edge. It checks the HTTP data contract only; it does
# not write to TEC controllers.

BASE_URL=${BASE_URL:-http://127.0.0.1:18087}
PREFIX=${PREFIX:-/api/operator/meerstettergo}
EXPECTED_DEVICES=${EXPECTED_DEVICES:-4}
MIN_SOURCE_ENTRIES=${MIN_SOURCE_ENTRIES:-120}
MIN_DISCOVERY_TARGETS=${MIN_DISCOVERY_TARGETS:-160}
MIN_WRITABLE_TARGETS=${MIN_WRITABLE_TARGETS:-16}
# Four TEC controllers with multi-instance high-priority polling can spread
# target updates across more than a small tail slice. Keep the default wide
# enough to avoid false freshness failures while still staying bounded.
LOG_LIMIT=${LOG_LIMIT:-800}
CAN_LIMIT=${CAN_LIMIT:-32}
TIMEOUT=${TIMEOUT:-10}
MAX_SAMPLE_AGE_SECONDS=${MAX_SAMPLE_AGE_SECONDS:-30}
FRESHNESS_NAMES=${FRESHNESS_NAMES:-object_temp_c,sink_temp_c,target_object_temp_c,output_current_a,output_voltage_v}

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
    curl -fsS --max-time "$TIMEOUT" "$BASE_URL$PREFIX$path" -o "$out"
}

fetch_arrow() {
    local path=$1
    local out=$2
    local headers=$3
    curl -fsS --max-time "$TIMEOUT" -D "$headers" "$BASE_URL$PREFIX$path" -o "$out"
}

require_jq() {
    command -v jq >/dev/null 2>&1 || fail "jq is required"
}

require_jq

health1="$tmpdir/health1.json"
health2="$tmpdir/health2.json"
fetch "/health" "$health1"
jq -e --argjson expected "$EXPECTED_DEVICES" '
  .ok == true
  and (.devices | tonumber) >= $expected
  and (.latestSeq | tonumber) > 0
  and (.can_ring.configured == true)
  and (.can_ring.ok == true)
  and ((.can_ring.source == "primary_ram") or (.can_ring.storage == "primary_ram"))
  and (.can_ring.stats.TotalRecords | tonumber) > 0
  and (.can_ring.fallback.TotalRecords | tonumber) > 0
' "$health1" >/dev/null || fail "gateway health contract failed: $(cat "$health1")"
ok "gateway health ok with $(jq -r '.devices' "$health1") devices and RAM primary CAN ring"

sleep 2
fetch "/health" "$health2"
seq1=$(jq -r '.latestSeq | tonumber' "$health1")
seq2=$(jq -r '.latestSeq | tonumber' "$health2")
if (( seq2 <= seq1 )); then
    fail "gateway latestSeq did not advance across 2s window: $seq1 -> $seq2"
fi
ok "gateway telemetry sequence advances: $seq1 -> $seq2"

catalogue="$tmpdir/source_catalogue.json"
fetch "/source-catalogue" "$catalogue"
entry_count=$(jq '[.catalogues[].entries[]?] | length' "$catalogue")
write_rows=$(jq '[.catalogues[].entries[]? | select(.access == "read_write")] | length' "$catalogue")
command_inputs=$(jq '[.catalogues[].command_inputs[]?] | length' "$catalogue")
route_count=$(jq '[.catalogues[].discovery_policy.remote_routes[]?] | length' "$catalogue")
route_count=$(jq '[
  .discovery_policy.remote_routes[]?.gateway_endpoint,
  .catalogues[].capabilities.remote_routes[]?.gateway_endpoint
] | unique | length' "$catalogue")
jq -e '
  [
    .discovery_policy.remote_routes[]?.gateway_endpoint,
    .catalogues[].capabilities.remote_routes[]?.gateway_endpoint
  ] as $routes
  |
  .schema_version == 1
  and .selection_owner == "loom.operator"
  and ([.catalogues[].command_inputs[]? | select(.command_id == "meerstettergo.tec.write" and .access == "lease_required")] | length) >= 1
  and ([.catalogues[].entries[]? | select(.semantic_status == "available_initialized")] | length) > 0
  and ([.catalogues[].entries[]? | select(.graph_source == "meerstettergo_edge")] | length) > 0
  and ([.catalogues[].entries[]? | select(.remote_route.gateway_endpoint | startswith("/api/operator/meerstettergo/target/read"))] | length) > 0
  and ([.catalogues[].entries[]? | select(.metadata.loom_read_path | startswith("/api/operator/meerstettergo/target/read"))] | length) > 0
  and ([$routes[] | select(. == "/api/operator/meerstettergo/health")] | length) >= 1
  and ([$routes[] | select(. == "/api/operator/meerstettergo/source-catalogue")] | length) >= 1
  and ([$routes[] | select(. == "/api/operator/meerstettergo/discovery/tree")] | length) >= 1
  and ([$routes[] | select(. == "/api/operator/meerstettergo/graph-wall")] | length) >= 1
  and ([$routes[] | select(. == "/api/operator/meerstettergo/tiles")] | length) >= 1
  and ([$routes[] | select(. == "/api/operator/meerstettergo/polling/status")] | length) >= 1
  and ([$routes[] | select(. == "/api/operator/meerstettergo/log/ring")] | length) >= 1
  and ([$routes[] | select(. == "/api/operator/meerstettergo/can/ring")] | length) >= 1
  and ([$routes[] | select(. == "/api/operator/meerstettergo/target/read")] | length) >= 1
  and ([$routes[] | select(. == "/api/operator/meerstettergo/target/write")] | length) >= 1
' "$catalogue" >/dev/null || fail "gateway source catalogue missing required Loom/SignalForge metadata"
if (( entry_count < MIN_SOURCE_ENTRIES )); then
    fail "gateway source catalogue entry count too low: $entry_count < $MIN_SOURCE_ENTRIES"
fi
if (( write_rows < MIN_WRITABLE_TARGETS )); then
    fail "gateway source catalogue write rows too low: $write_rows < $MIN_WRITABLE_TARGETS"
fi
if (( command_inputs < 1 )); then
    fail "gateway source catalogue has no command input contract"
fi
if (( route_count < 8 )); then
    fail "gateway source catalogue remote route coverage too low: $route_count"
fi
ok "gateway source catalogue exposes $entry_count entries, $write_rows write rows, and $route_count remote routes"

polling="$tmpdir/polling_status.json"
fetch "/polling/status" "$polling"
jq -e --argjson expected "$EXPECTED_DEVICES" --argjson min_targets "$MIN_DISCOVERY_TARGETS" '
  .ok == true
  and .live == true
  and (.device_count | tonumber) >= $expected
  and (.target_count | tonumber) >= $min_targets
  and (.fresh_count | tonumber) > 0
  and (.stale_count | tonumber) == 0
  and (.error_count | tonumber) == 0
  and ([.devices[].device_id] | unique | length) >= $expected
' "$polling" >/dev/null || fail "gateway polling status contract failed: $(cat "$polling")"
ok "gateway polling status is live for $(jq -r '.device_count' "$polling") devices and $(jq -r '.target_count' "$polling") targets"

tree="$tmpdir/tree.json"
fetch "/discovery/tree" "$tree"
target_count=$(jq '[.. | objects | select(has("id") and has("metadata"))] | length' "$tree")
writable_count=$(jq '[.. | objects | select(.metadata.writable? == "true")] | length' "$tree")
typed_count=$(jq '[.. | objects | select(.metadata.type? != null and .metadata.subtype? != null and .metadata.parameter? != null and .metadata.device? != null and .metadata.instance? != null)] | length' "$tree")
typed_count=$(jq '[.. | objects | select(
  .node_id? != null
  and .metadata.category? != null
  and .metadata.subtype? != null
  and .metadata.parameter_name? != null
  and (.metadata.mecom_instance? != null or .metadata.instance? != null)
  and .metadata.preferred_transport? != null
)] | length' "$tree")
if (( target_count < MIN_DISCOVERY_TARGETS )); then
    fail "gateway discovery target count too low: $target_count < $MIN_DISCOVERY_TARGETS"
fi
if (( writable_count < MIN_WRITABLE_TARGETS )); then
    fail "gateway writable target count too low: $writable_count < $MIN_WRITABLE_TARGETS"
fi
if (( typed_count < MIN_SOURCE_ENTRIES )); then
    fail "gateway typed discovery metadata count too low: $typed_count < $MIN_SOURCE_ENTRIES"
fi
ok "gateway discovery tree exposes $target_count targets, $writable_count writable paths, and $typed_count typed rows"

writable_target=$(jq -r '[.. | objects | select(.metadata.writable? == "true") | .id][0] // empty' "$tree")
if [[ -z "$writable_target" ]]; then
    fail "gateway no writable target available for write-guard verification"
fi
write_payload="$tmpdir/write_no_lease.json"
write_response="$tmpdir/write_no_lease.out"
jq -n --arg target_id "$writable_target" '{target_id:$target_id,value:0}' > "$write_payload"
write_code=$(curl -sS --max-time "$TIMEOUT" -o "$write_response" -w '%{http_code}' -H 'Content-Type: application/json' -X POST --data-binary "@$write_payload" "$BASE_URL$PREFIX/target/write")
case "$write_code" in
    403)
        grep -q 'X-Loom-Sequencer-Lease' "$write_response" || fail "gateway write rejection did not mention X-Loom-Sequencer-Lease: $(cat "$write_response")"
        ;;
    428)
        grep -q 'lease_id' "$write_response" || fail "gateway write rejection did not mention lease_id: $(cat "$write_response")"
        ;;
    *)
        fail "gateway write without sequencer lease was not safely rejected: code=$write_code body=$(cat "$write_response")"
        ;;
esac
ok "gateway writable target route rejects command without explicit sequencer lease"

log_ring="$tmpdir/log_ring.json"
fetch "/log/ring?tail=true&limit=$LOG_LIMIT" "$log_ring"
jq -e --argjson expected "$EXPECTED_DEVICES" '
  length > 0
  and ([.[].tm.target_id? | select(type == "string" and startswith("device:")) | split(":")[1]] | unique | length) >= $expected
' "$log_ring" >/dev/null || fail "gateway decoded log ring does not include $EXPECTED_DEVICES devices"
decoded_targets=$(jq '[.[].tm.target_id? | select(type == "string")] | unique | length' "$log_ring")
ok "gateway decoded log ring covers $decoded_targets targets in tail window"

freshness="$tmpdir/freshness.json"
jq -n \
  --slurpfile ring "$log_ring" \
  --arg names "$FRESHNESS_NAMES" \
  --argjson expected "$EXPECTED_DEVICES" \
  --argjson max_age "$MAX_SAMPLE_AGE_SECONDS" \
  --arg now "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
  def ts_epoch:
    sub("\\.[0-9]+Z$"; "Z")
    | fromdateiso8601;

  ($names | split(",") | map(select(length > 0))) as $required
  | ($now | ts_epoch) as $now_epoch
  | ($ring[0] | map(select(.kind == "telemetry" and (.tm.target_id? | type == "string")))) as $records
  | ($records | map(.tm.target_id | split(":")[1]) | unique) as $devices
  | ($records
      | map(. + {freshness_device: (.tm.target_id | split(":")[1]), freshness_name: (.tm.name // "")})
      | map(select(.freshness_name as $name | ($required | index($name)) != null))
      | sort_by(.tm.target_id)
      | group_by(.tm.target_id)
      | map(max_by((.tm.time // .time) | ts_epoch))) as $latest
  | ($latest | map(.freshness_device + "|" + .freshness_name)) as $observed
  | {
      devices: $devices,
      required_names: $required,
      max_age_seconds: $max_age,
      missing: [
        $devices[] as $device
        | $required[] as $name
        | select(($observed | index($device + "|" + $name)) == null)
        | {device: $device, name: $name}
      ],
      stale: [
        $latest[]
        | (((.tm.time // .time) | ts_epoch) as $sample_epoch
          | select(($now_epoch - $sample_epoch) > $max_age)
          | {target_id: .tm.target_id, age_seconds: ($now_epoch - $sample_epoch), time: (.tm.time // .time)})
      ],
      latest_count: ($latest | length)
    }
' > "$freshness"
jq -e --argjson expected "$EXPECTED_DEVICES" '
  (.devices | length) >= $expected
  and (.missing | length) == 0
  and (.stale | length) == 0
' "$freshness" >/dev/null || fail "gateway freshness budget failed: $(cat "$freshness")"
fresh_count=$(jq '.latest_count' "$freshness")
ok "gateway freshness budget covers $fresh_count high-priority live values within ${MAX_SAMPLE_AGE_SECONDS}s"

merged_can_ring="$tmpdir/merged_can_ring.json"
fetch "/can/ring?source=merged&limit=$CAN_LIMIT" "$merged_can_ring"
jq -e --argjson min "$CAN_LIMIT" '
  .ok == true
  and .source == "merged"
  and .storage == "primary_ram+fallback_flash"
  and (.records | length) >= $min
  and (.primary.stats.TotalRecords | tonumber) > 0
  and (.fallback.stats.TotalRecords | tonumber) > 0
' "$merged_can_ring" >/dev/null || fail "gateway merged CAN ring contract failed: $(cat "$merged_can_ring")"
merged_duplicate_keys=$(jq '[.records[] | "\(.time)|\(.interface)|\(.id_hex)|\(.dlc)|\(.data_hex)"] as $s | ($s | length) - ($s | unique | length)' "$merged_can_ring")
if (( merged_duplicate_keys != 0 )); then
    fail "gateway merged CAN ring returned duplicate mirrored frame keys: $merged_duplicate_keys"
fi
ok "gateway merged CAN ring reconciles RAM and flash fallback without duplicate frame keys"

graph="$tmpdir/graph_wall.json"
fetch "/graph-wall" "$graph"
jq -e '
  length >= 4
  and ([.[].target.name?] | index("All Temperatures") != null)
  and ([.[].target.name?] | index("All Target Values") != null)
  and ([.[].target.name?] | index("All Output Power") != null)
  and ([.[].target.name?] | index("Event Swimlane") != null)
' "$graph" >/dev/null || fail "gateway graph wall is missing required four-controller tiles"
ok "gateway graph wall exposes temperature, target, power, and event tiles"

tiles="$tmpdir/tiles.json"
fetch "/tiles?aggregate=temperature&limit=1" "$tiles"
jq -e '
  (.series | length) > 0
  and (([.series[].points | length] | add) > 0)
  and ([.series[].latest? | select(. != null)] | length) > 0
' "$tiles" >/dev/null || fail "gateway temperature tile has no live points"
ok "gateway temperature graph tile has live points"

archive_manifest="$tmpdir/archive_manifest.json"
fetch "/log/archive/manifest" "$archive_manifest"
jq -e '
  .schema == "meerstettergo.archive.manifest"
  and ([.formats[].name] | index("ndjson") != null)
  and ([.formats[].name] | index("arrow_ipc") != null)
  and ([.formats[].name] | index("hdf5") != null)
  and ([.streams[].name] | index("telemetry_samples") != null)
  and ([.streams[].name] | index("can_frames") != null)
  and ([.streams[].name] | index("command_events") != null)
  and ([.streams[].name] | index("object_dictionary_snapshots") != null)
  and ([.streams[].name] | index("graph_wall_assignments") != null)
  and .review_policy.preferred_live_source == "pixtend_socketcan"
' "$archive_manifest" >/dev/null || fail "gateway archive manifest contract failed: $(cat "$archive_manifest")"
ok "gateway archive manifest exposes durable NDJSON/Arrow/HDF5 stream contract"

exported="$tmpdir/export.ndjson"
curl -fsS --max-time "$TIMEOUT" "$BASE_URL$PREFIX/log/export?tail=true&limit=$LOG_LIMIT" -o "$exported"
if [[ ! -s "$exported" ]]; then
    fail "gateway log export produced no data"
fi

arrow_export="$tmpdir/export.arrow"
arrow_headers="$tmpdir/export.arrow.headers"
fetch_arrow "/log/export?format=arrow_ipc&tail=true&limit=$LOG_LIMIT" "$arrow_export" "$arrow_headers"
if [[ ! -s "$arrow_export" ]]; then
    fail "gateway Arrow IPC export produced no data"
fi
grep -qi '^content-type: application/vnd\.apache\.arrow\.stream' "$arrow_headers" || fail "gateway Arrow IPC export returned unexpected headers: $(cat "$arrow_headers")"
arrow_marker=$(od -An -tx1 -N4 "$arrow_export" | tr -d ' \n')
if [[ "$arrow_marker" != "ffffffff" ]]; then
    fail "gateway Arrow IPC export does not start with stream continuation marker: $arrow_marker"
fi
ok "gateway Arrow IPC export emits a non-empty telemetry stream"

review="$tmpdir/review.json"
curl -fsS --max-time "$TIMEOUT" -X POST --data-binary "@$exported" "$BASE_URL$PREFIX/log/import/review" -o "$review"
jq -e --argjson expected "$EXPECTED_DEVICES" '
  .ok == true
  and .mode == "review_only"
  and .committed == false
  and (.duplicate_seq_count | tonumber) == 0
  and (.devices | length) >= $expected
' "$review" >/dev/null || fail "gateway export/import review failed: $(cat "$review")"
ok "gateway NDJSON export/import review succeeds without duplicate sequence IDs"

printf 'PASS Loom/operator gateway route verified at %s%s\n' "$BASE_URL" "$PREFIX"
