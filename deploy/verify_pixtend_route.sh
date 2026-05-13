#!/usr/bin/env bash
set -euo pipefail

# Non-invasive live route verifier for the PiXtend Meerstetter-Go edge path.
# It checks the HTTP data contract only; it does not write to TEC controllers.

BASE_URL=${BASE_URL:-http://192.168.6.229:18080}
EXPECTED_DEVICES=${EXPECTED_DEVICES:-4}
MIN_SOURCE_ENTRIES=${MIN_SOURCE_ENTRIES:-120}
MIN_DISCOVERY_TARGETS=${MIN_DISCOVERY_TARGETS:-160}
MIN_WRITABLE_TARGETS=${MIN_WRITABLE_TARGETS:-16}
MIN_DECODED_TARGETS=${MIN_DECODED_TARGETS:-32}
# Four TEC controllers with multi-instance high-priority polling can exceed a
# small tail window before every required value comes around. Keep this wide
# enough to verify the route without changing the live service.
LOG_LIMIT=${LOG_LIMIT:-800}
CAN_LIMIT=${CAN_LIMIT:-32}
TIMEOUT=${TIMEOUT:-5}
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
    curl -fsS --max-time "$TIMEOUT" "$BASE_URL$path" -o "$out"
}

fetch_arrow() {
    local path=$1
    local out=$2
    local headers=$3
    curl -fsS --max-time "$TIMEOUT" -D "$headers" "$BASE_URL$path" -o "$out"
}

require_jq() {
    command -v jq >/dev/null 2>&1 || fail "jq is required"
}

require_jq

health1="$tmpdir/health1.json"
health2="$tmpdir/health2.json"
health_alias="$tmpdir/health_alias.json"
fetch "/api/health" "$health1"
jq -e --argjson expected "$EXPECTED_DEVICES" '
  .ok == true
  and (.devices | tonumber) >= $expected
  and (.latestSeq | tonumber) > 0
  and (.can_ring.configured == true)
  and (.can_ring.ok == true)
  and ((.can_ring.source == "primary_ram") or (.can_ring.storage == "primary_ram"))
' "$health1" >/dev/null || fail "health contract failed: $(cat "$health1")"
ok "health ok with $(jq -r '.devices' "$health1") devices and RAM CAN ring"

fetch "/health" "$health_alias"
jq -e '.ok == true and (.can_ring.configured == true)' "$health_alias" >/dev/null || fail "plain /health alias failed: $(cat "$health_alias")"
ok "plain /health returns edge health JSON"

sleep 2
fetch "/api/health" "$health2"
seq1=$(jq -r '.latestSeq | tonumber' "$health1")
seq2=$(jq -r '.latestSeq | tonumber' "$health2")
if (( seq2 <= seq1 )); then
    fail "latestSeq did not advance across 2s window: $seq1 -> $seq2"
fi
ok "telemetry sequence advances: $seq1 -> $seq2"

log_ring="$tmpdir/log_ring.json"
fetch "/api/log/ring?tail=true&limit=$LOG_LIMIT" "$log_ring"
jq -e --argjson expected "$EXPECTED_DEVICES" '
  length > 0
  and ([.[].tm.target_id? | select(type == "string" and startswith("device:")) | split(":")[1]] | unique | length) >= $expected
' "$log_ring" >/dev/null || fail "decoded log ring does not include $EXPECTED_DEVICES devices"
decoded_targets=$(jq '[.[].tm.target_id? | select(type == "string")] | unique | length' "$log_ring")
if (( decoded_targets < MIN_DECODED_TARGETS )); then
    fail "decoded target coverage too low: $decoded_targets < $MIN_DECODED_TARGETS"
fi
ok "decoded log ring covers $decoded_targets targets in tail window"

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
' "$freshness" >/dev/null || fail "freshness budget failed: $(cat "$freshness")"
fresh_count=$(jq '.latest_count' "$freshness")
ok "freshness budget covers $fresh_count high-priority live values within ${MAX_SAMPLE_AGE_SECONDS}s"

can_ring="$tmpdir/can_ring.json"
fetch "/api/can/ring?limit=$CAN_LIMIT" "$can_ring"
jq -e --argjson min "$CAN_LIMIT" '
  .ok == true
  and (.source == "primary_ram" or .storage == "primary_ram")
  and (.records | length) >= $min
  and (.stats.TotalRecords | tonumber) > 0
' "$can_ring" >/dev/null || fail "raw CAN ring contract failed: $(cat "$can_ring")"
ok "raw CAN ring returns $CAN_LIMIT records from primary RAM ring"

fallback_can_ring="$tmpdir/fallback_can_ring.json"
fetch "/api/can/ring?source=fallback_flash&limit=$CAN_LIMIT" "$fallback_can_ring"
jq -e --argjson max "$CAN_LIMIT" '
  .ok == true
  and .source == "fallback_flash"
  and .storage == "fallback_flash"
  and (.records | length) > 0
  and (.records | length) <= $max
  and (.stats.TotalRecords | tonumber) > 0
' "$fallback_can_ring" >/dev/null || fail "fallback flash CAN ring contract failed: $(cat "$fallback_can_ring")"
fallback_duplicate_seq=$(jq '[.records[].seq] as $s | ($s | length) - ($s | unique | length)' "$fallback_can_ring")
if (( fallback_duplicate_seq != 0 )); then
    fail "fallback flash CAN ring returned duplicate local sequence IDs: $fallback_duplicate_seq"
fi
ok "flash fallback CAN ring is readable with $(jq '.records | length' "$fallback_can_ring") records and no local duplicate sequence IDs"

merged_can_ring="$tmpdir/merged_can_ring.json"
fetch "/api/can/ring?source=merged&limit=$CAN_LIMIT" "$merged_can_ring"
jq -e --argjson min "$CAN_LIMIT" '
  .ok == true
  and .source == "merged"
  and .storage == "primary_ram+fallback_flash"
  and (.records | length) >= $min
  and (.primary.stats.TotalRecords | tonumber) > 0
  and (.fallback.stats.TotalRecords | tonumber) > 0
' "$merged_can_ring" >/dev/null || fail "merged CAN ring contract failed: $(cat "$merged_can_ring")"
merged_duplicate_keys=$(jq '[.records[] | "\(.time)|\(.interface)|\(.id_hex)|\(.dlc)|\(.data_hex)"] as $s | ($s | length) - ($s | unique | length)' "$merged_can_ring")
if (( merged_duplicate_keys != 0 )); then
    fail "merged CAN ring returned duplicate mirrored frame keys: $merged_duplicate_keys"
fi
ok "merged CAN ring reconciles RAM and flash fallback with no duplicate frame keys"

tree="$tmpdir/tree.json"
fetch "/api/discovery/tree" "$tree"
target_count=$(jq '[.. | objects | select(has("id") and has("metadata"))] | length' "$tree")
writable_count=$(jq '[.. | objects | select(.metadata.writable? == "true")] | length' "$tree")
if (( target_count < MIN_DISCOVERY_TARGETS )); then
    fail "discovery target count too low: $target_count < $MIN_DISCOVERY_TARGETS"
fi
if (( writable_count < MIN_WRITABLE_TARGETS )); then
    fail "writable target count too low: $writable_count < $MIN_WRITABLE_TARGETS"
fi
ok "discovery tree exposes $target_count targets and $writable_count writable paths"

for alias_path in \
    "/api/loom/discovery-tree" \
    "/api/loom/discovery/tree" \
    "/api/operator/meerstettergo/discovery/tree"
do
    alias_tree="$tmpdir/tree.$(printf '%s' "$alias_path" | tr '/?' '__').json"
    fetch "$alias_path" "$alias_tree"
    jq -e --argjson min_targets "$MIN_DISCOVERY_TARGETS" '
      ([.. | objects | select(has("id") and has("metadata"))] | length) >= $min_targets
    ' "$alias_tree" >/dev/null || fail "discovery alias $alias_path did not return the JSON tree contract"
done
ok "Loom/operator discovery aliases return JSON tree contracts"

writable_target=$(jq -r '[.. | objects | select(.metadata.writable? == "true") | .id][0] // empty' "$tree")
if [[ -z "$writable_target" ]]; then
    fail "no writable target available for write-guard verification"
fi
write_payload="$tmpdir/write_no_lease.json"
write_response="$tmpdir/write_no_lease.out"
jq -n --arg target_id "$writable_target" '{target_id:$target_id,value:0}' > "$write_payload"
write_code=$(curl -sS --max-time "$TIMEOUT" -o "$write_response" -w '%{http_code}' -H 'Content-Type: application/json' -X POST --data-binary "@$write_payload" "$BASE_URL/api/target/write")
if [[ "$write_code" != "428" ]]; then
    fail "write without sequencer lease was not rejected with 428: code=$write_code body=$(cat "$write_response")"
fi
grep -q 'lease_id' "$write_response" || fail "write rejection did not mention lease_id: $(cat "$write_response")"
ok "writable target route rejects command without explicit sequencer lease"

catalogue="$tmpdir/source_catalogue.json"
fetch "/api/loom/source-catalogue" "$catalogue"
entry_count=$(jq '[.catalogues[].entries[]?] | length' "$catalogue")
write_rows=$(jq '[.catalogues[].entries[]? | select(.access == "read_write")] | length' "$catalogue")
jq -e '
  (.catalogues | length) > 0
  and ([.catalogues[].command_inputs[]?] | length) >= 1
' "$catalogue" >/dev/null || fail "source catalogue missing command inputs"
if (( entry_count < MIN_SOURCE_ENTRIES )); then
    fail "source catalogue entry count too low: $entry_count < $MIN_SOURCE_ENTRIES"
fi
if (( write_rows < MIN_WRITABLE_TARGETS )); then
    fail "source catalogue write rows too low: $write_rows < $MIN_WRITABLE_TARGETS"
fi
ok "Loom/SignalForge source catalogue exposes $entry_count entries and $write_rows write rows"

operator_health="$tmpdir/operator_health.json"
fetch "/api/operator/meerstettergo/health" "$operator_health"
jq -e '.ok == true and (.devices | tonumber) >= 1' "$operator_health" >/dev/null || fail "operator health alias failed: $(cat "$operator_health")"
ok "operator health alias returns edge health JSON"

graph="$tmpdir/graph_wall.json"
fetch "/api/graph-wall" "$graph"
jq -e '
  length >= 4
  and ([.[].target.name?] | index("All Temperatures") != null)
  and ([.[].target.name?] | index("All Target Values") != null)
  and ([.[].target.name?] | index("All Output Power") != null)
  and ([.[].target.name?] | index("Event Swimlane") != null)
' "$graph" >/dev/null || fail "graph wall is missing required four-controller tiles"
ok "graph wall exposes temperature, target, power, and event tiles"

tiles="$tmpdir/tiles.json"
fetch "/api/tiles?aggregate=temperature&limit=1" "$tiles"
jq -e '
  (.series | length) > 0
  and ((.series[0].points | length) > 0)
  and (.series[0].latest != null)
' "$tiles" >/dev/null || fail "temperature tile has no live points"
ok "temperature graph tile has live points"

aggregate_pseudo_tiles="$tmpdir/aggregate_pseudo_tiles.json"
fetch "/api/tiles?target_id=aggregate:temperatures&aggregate=temperature&limit=1" "$aggregate_pseudo_tiles"
jq -e '
  (.series | length) > 0
  and ([.series[].points | length] | add) > 0
  and ([.series[].target_id? | select(type == "string" and startswith("device:"))] | length) > 0
' "$aggregate_pseudo_tiles" >/dev/null || fail "aggregate pseudo-target tile query returned no live device series"
ok "aggregate graph-wall pseudo-target resolves to live device series"

archive_manifest="$tmpdir/archive_manifest.json"
fetch "/api/log/archive/manifest" "$archive_manifest"
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
' "$archive_manifest" >/dev/null || fail "archive manifest contract failed: $(cat "$archive_manifest")"
ok "archive manifest exposes durable NDJSON/Arrow/HDF5 stream contract"

exported="$tmpdir/export.ndjson"
curl -fsS --max-time "$TIMEOUT" "$BASE_URL/api/log/export?tail=true&limit=$LOG_LIMIT" -o "$exported"
if [[ ! -s "$exported" ]]; then
    fail "log export produced no data"
fi

arrow_export="$tmpdir/export.arrow"
arrow_headers="$tmpdir/export.arrow.headers"
fetch_arrow "/api/log/export?format=arrow_ipc&tail=true&limit=$LOG_LIMIT" "$arrow_export" "$arrow_headers"
if [[ ! -s "$arrow_export" ]]; then
    fail "Arrow IPC export produced no data"
fi
grep -qi '^content-type: application/vnd\.apache\.arrow\.stream' "$arrow_headers" || fail "Arrow IPC export returned unexpected headers: $(cat "$arrow_headers")"
arrow_marker=$(od -An -tx1 -N4 "$arrow_export" | tr -d ' \n')
if [[ "$arrow_marker" != "ffffffff" ]]; then
    fail "Arrow IPC export does not start with stream continuation marker: $arrow_marker"
fi
ok "Arrow IPC export emits a non-empty telemetry stream"

review="$tmpdir/review.json"
curl -fsS --max-time "$TIMEOUT" -X POST --data-binary "@$exported" "$BASE_URL/api/log/import/review" -o "$review"
jq -e --argjson expected "$EXPECTED_DEVICES" '
  .ok == true
  and .mode == "review_only"
  and .committed == false
  and (.duplicate_seq_count | tonumber) == 0
  and (.devices | length) >= $expected
' "$review" >/dev/null || fail "export/import review failed: $(cat "$review")"
ok "NDJSON export/import review succeeds without duplicate sequence IDs"

printf 'PASS PiXtend route verified at %s\n' "$BASE_URL"
