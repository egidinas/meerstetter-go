#!/usr/bin/env bash
set -euo pipefail

# Non-invasive browser smoke verifier for the Meerstetter-Go PiXtend UI.
# It loads the real HTML UI, waits for client-side data fetches, checks the
# dynamic DOM for the four-controller graph/tree surface, and captures a
# screenshot. It does not write to TEC controllers.

BASE_URL=${BASE_URL:-http://192.168.6.229:18080}
EXPECTED_DEVICES=${EXPECTED_DEVICES:-tec-75,tec-76,tec-81,tec-84}
WINDOW_SIZE=${WINDOW_SIZE:-1440,1000}
VIRTUAL_TIME_BUDGET_MS=${VIRTUAL_TIME_BUDGET_MS:-10000}
MIN_SCREENSHOT_BYTES=${MIN_SCREENSHOT_BYTES:-50000}
MIN_SCREENSHOT_SD=${MIN_SCREENSHOT_SD:-1000}
MIN_PROVENANCE_TARGETS=${MIN_PROVENANCE_TARGETS:-160}
MIN_WRITABLE_TARGETS=${MIN_WRITABLE_TARGETS:-16}
KEEP_UI_SMOKE=${KEEP_UI_SMOKE:-0}
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

ok() {
    printf 'OK   %s\n' "$*"
}

fail() {
    printf 'FAIL %s\n' "$*" >&2
    exit 1
}

find_browser() {
    if [[ -n "${BROWSER:-}" ]]; then
        command -v "$BROWSER" >/dev/null 2>&1 || fail "BROWSER is set but not executable: $BROWSER"
        printf '%s\n' "$BROWSER"
        return
    fi
    for candidate in chromium chromium-browser google-chrome /snap/bin/chromium; do
        if command -v "$candidate" >/dev/null 2>&1; then
            command -v "$candidate"
            return
        fi
    done
    fail "no headless Chromium-compatible browser found"
}

browser=$(find_browser)
url="${BASE_URL%/}/"

# Snap Chromium cannot always write screenshots under /tmp or arbitrary hidden
# home directories because of confinement, so keep the temporary directory under
# the repository regardless of the caller's current working directory.
tmpdir=$(mktemp -d "$ROOT/.ui-smoke.XXXXXX")
cleanup() {
    if [[ "$KEEP_UI_SMOKE" != "1" ]]; then
        rm -rf "$tmpdir"
    else
        printf 'OK   retained browser smoke artifacts in %s\n' "$tmpdir"
    fi
}
trap cleanup EXIT

dom="$tmpdir/dom.html"
screenshot="$tmpdir/ui.png"
browser_log="$tmpdir/chromium.log"
browser_dom_log="$tmpdir/chromium-dom.log"

"$browser" \
    --headless=new \
    --no-sandbox \
    --disable-gpu \
    --window-size="$WINDOW_SIZE" \
    --virtual-time-budget="$VIRTUAL_TIME_BUDGET_MS" \
    --screenshot="$screenshot" \
    "$url" >"$browser_log" 2>&1 || fail "browser screenshot pass failed: $(tail -n 20 "$browser_log")"

[[ -s "$screenshot" ]] || fail "browser screenshot was not created"
screenshot_bytes=$(stat -c '%s' "$screenshot")
if (( screenshot_bytes < MIN_SCREENSHOT_BYTES )); then
    fail "browser screenshot too small: $screenshot_bytes < $MIN_SCREENSHOT_BYTES"
fi

if command -v identify >/dev/null 2>&1; then
    dimensions=$(identify -format '%wx%h' "$screenshot")
    screenshot_sd=$(identify -format '%[standard-deviation]' "$screenshot")
    awk -v sd="$screenshot_sd" -v min="$MIN_SCREENSHOT_SD" 'BEGIN { exit !(sd >= min) }' \
        || fail "browser screenshot appears blank or flat: standard-deviation=$screenshot_sd < $MIN_SCREENSHOT_SD"
    ok "browser screenshot captured: ${dimensions}, ${screenshot_bytes} bytes, sd=${screenshot_sd}"
else
    ok "browser screenshot captured: ${screenshot_bytes} bytes"
fi

"$browser" \
    --headless=new \
    --no-sandbox \
    --disable-gpu \
    --virtual-time-budget="$VIRTUAL_TIME_BUDGET_MS" \
    --dump-dom \
    "$url" >"$dom" 2>"$browser_dom_log" || fail "browser DOM pass failed: $(tail -n 20 "$browser_dom_log")"

require_dom() {
    local needle=$1
    local description=$2
    grep -Fq "$needle" "$dom" || fail "browser DOM missing $description ($needle)"
}

require_dom_count() {
    local needle=$1
    local description=$2
    local minimum=$3
    local count
    count=$( (grep -Fo "$needle" "$dom" || true) | wc -l | tr -d ' ')
    if (( count < minimum )); then
        fail "browser DOM $description count too low: $count < $minimum ($needle)"
    fi
    ok "browser DOM $description count: $count"
}

require_dom "Project" "project side panel"
require_dom "Signal Tree" "signal tree link"
require_dom "Graph Wall" "graph wall link"
require_dom "graph-assign" "graph assignment controls"
require_dom "target-refresh" "manual read controls"
require_dom "target-editor" "writable target editor surface"
require_dom "All Temperatures" "high-priority graph wall tile"
require_dom "All Output Power" "output-power graph wall tile"
require_dom "cascade_temp_c" "cascade temperature signal"
require_dom "output_current_a" "output current signal"
require_dom "output_voltage_v" "output voltage signal"
require_dom "Actual Output Current" "output-current display label"
require_dom "Output Voltage" "output-voltage display label"
require_dom "Primary path" "primary-path status row"
require_dom "PiXtend SocketCAN" "primary SocketCAN provenance"
require_dom "Fallbacks" "fallback status row"
require_dom "RAM ring" "RAM ring status"
require_dom "flash ring" "flash ring status"

IFS=',' read -r -a devices <<< "$EXPECTED_DEVICES"
for device in "${devices[@]}"; do
    require_dom "$device (TEC controller)" "$device tree section"
    require_dom "device:$device:" "$device target IDs"
done

require_dom_count 'class="target-provenance"' "rendered provenance rows" "$MIN_PROVENANCE_TARGETS"
require_dom_count 'class="chip device"' "device provenance chips" "$MIN_PROVENANCE_TARGETS"
require_dom_count 'class="chip instance"' "instance provenance chips" "$MIN_PROVENANCE_TARGETS"
require_dom_count 'class="chip parameter"' "parameter provenance chips" "$MIN_PROVENANCE_TARGETS"
require_dom_count 'class="chip transport"' "transport provenance chips" "$MIN_PROVENANCE_TARGETS"
require_dom_count 'class="chip readout"' "readout provenance chips" "$MIN_PROVENANCE_TARGETS"
require_dom_count 'class="chip write"' "writable target chips" "$MIN_WRITABLE_TARGETS"
require_dom_count 'Active transport:' "active-transport detail rows" "$MIN_PROVENANCE_TARGETS"
require_dom_count 'Read path:' "read-path detail rows" "$MIN_PROVENANCE_TARGETS"
require_dom_count 'Write path:' "write-path detail rows" "$MIN_PROVENANCE_TARGETS"

if grep -Fq "loading..." "$dom"; then
    fail "browser DOM still contains loading placeholder"
fi

target_count=$( (grep -o 'class="target"' "$dom" || true) | wc -l | tr -d ' ')
event_count=$( (grep -o 'class="event"' "$dom" || true) | wc -l | tr -d ' ')
graph_count=0
grep -Fq "All Temperatures" "$dom" && graph_count=$((graph_count + 1))
grep -Fq "All Output Power" "$dom" && graph_count=$((graph_count + 1))

if (( target_count < 160 )); then
    fail "browser DOM target count too low: $target_count"
fi
if (( graph_count < 2 )); then
    fail "browser DOM graph wall tile count too low: $graph_count"
fi

ok "browser DOM populated: ${target_count} targets, ${graph_count} graph wall tiles, ${event_count} events"
ok "browser UI smoke passed at $url"
