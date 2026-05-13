#!/usr/bin/env sh
set -eu

log() {
    printf '%s\n' "pixtend-can-ring: $*"
}

need_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        log "missing required command: $1"
        exit 127
    fi
}

CAN_IF="${CAN_IF:-can0}"
CAN_OVERLAY="${CAN_OVERLAY:-mcp2515-can1}"
BITRATE="${BITRATE:-1000000}"
OSC="${OSC:-20000000}"
INTPIN="${INTPIN:-4}"
SPIMAX="${SPIMAX:-1000000}"
PIXTEND_CAN_ENABLE_GPIOS="${PIXTEND_CAN_ENABLE_GPIOS:-24 27}"
RING_PATH="${RING_PATH:-/run/meerstettergo-ring/pixtend-can0.ring}"
RING_SIZE="${RING_SIZE:-64MiB}"
RING_CHUNK="${RING_CHUNK:-64KiB}"
FALLBACK_RING_PATH="${FALLBACK_RING_PATH:-/var/lib/meerstettergo/pixtend-can0.ring}"
FALLBACK_RING_SIZE="${FALLBACK_RING_SIZE:-8GiB}"
FALLBACK_RING_CHUNK="${FALLBACK_RING_CHUNK:-4MiB}"
FALLBACK_RING_SYNC="${FALLBACK_RING_SYNC:-1}"
BOOTSTRAP_RING_PATH="${BOOTSTRAP_RING_PATH:-/var/lib/meerstettergo/pixtend-can0.bootstrap.ring}"
BOOTSTRAP_ON_START="${BOOTSTRAP_ON_START:-0}"
CHECKPOINT_ON_EXIT="${CHECKPOINT_ON_EXIT:-0}"
LISTEN_WINDOW="${LISTEN_WINDOW:-24h}"

need_cmd ip
need_cmd dtoverlay
need_cmd /usr/local/bin/teccanprobe
if [ -n "$PIXTEND_CAN_ENABLE_GPIOS" ]; then
    need_cmd gpioset
fi

mkdir -p "$(dirname "$RING_PATH")"
if [ -n "$FALLBACK_RING_PATH" ]; then
    mkdir -p "$(dirname "$FALLBACK_RING_PATH")"
fi

checkpoint_flash_ring() {
    if [ "$CHECKPOINT_ON_EXIT" != "1" ] || [ -z "$BOOTSTRAP_RING_PATH" ] || [ ! -s "$RING_PATH" ]; then
        return 0
    fi
    log "checkpointing RAM ring to flash fallback $BOOTSTRAP_RING_PATH"
    mkdir -p "$(dirname "$BOOTSTRAP_RING_PATH")"
    tmp_path="${BOOTSTRAP_RING_PATH}.tmp"
    cp -f "$RING_PATH" "$tmp_path"
    mv -f "$tmp_path" "$BOOTSTRAP_RING_PATH"
    sync "$BOOTSTRAP_RING_PATH" 2>/dev/null || sync
}

if [ "$BOOTSTRAP_ON_START" = "1" ] && [ -n "$BOOTSTRAP_RING_PATH" ] && [ ! -s "$RING_PATH" ] && [ -s "$BOOTSTRAP_RING_PATH" ]; then
    log "bootstrapping RAM ring from flash fallback $BOOTSTRAP_RING_PATH"
    cp -f "$BOOTSTRAP_RING_PATH" "$RING_PATH"
fi

gpio_hold_pid=""
start_gpio_hold() {
    if [ -z "$PIXTEND_CAN_ENABLE_GPIOS" ]; then
        return 0
    fi

    set -- 0
    for gpio in $PIXTEND_CAN_ENABLE_GPIOS; do
        set -- "$@" "$gpio=1"
    done

    log "holding PiXtend CAN enable GPIOs high: $PIXTEND_CAN_ENABLE_GPIOS"
    gpioset --mode=signal "$@" &
    gpio_hold_pid=$!
    sleep 0.1
}

stop_gpio_hold() {
    if [ -n "$gpio_hold_pid" ]; then
        kill "$gpio_hold_pid" 2>/dev/null || true
        wait "$gpio_hold_pid" 2>/dev/null || true
        gpio_hold_pid=""
    fi
}

start_gpio_hold

if ! ip link show "$CAN_IF" >/dev/null 2>&1; then
    log "$CAN_IF missing, enabling PiXtend CAN path and applying $CAN_OVERLAY"
    dtoverlay -r mcp2515-can0 >/dev/null 2>&1 || true
    dtoverlay -r mcp2515-can1 >/dev/null 2>&1 || true
    case "$CAN_OVERLAY" in
        mcp2515-can0|mcp2515-can1) ;;
        *)
            echo "unsupported CAN_OVERLAY=$CAN_OVERLAY; expected mcp2515-can0 or mcp2515-can1" >&2
            exit 2
            ;;
    esac
    dtoverlay "$CAN_OVERLAY" oscillator="$OSC" interrupt="$INTPIN" spimaxfrequency="$SPIMAX"

    tries=0
    while ! ip link show "$CAN_IF" >/dev/null 2>&1; do
        tries=$((tries + 1))
        if [ "$tries" -ge 20 ]; then
            log "$CAN_IF did not appear after overlay"
            exit 3
        fi
        sleep 0.25
    done
fi

log "bringing up $CAN_IF bitrate=$BITRATE"
ip link set "$CAN_IF" down >/dev/null 2>&1 || true
ip link set "$CAN_IF" up type can bitrate "$BITRATE"

set -- /usr/local/bin/teccanprobe \
    -if "$CAN_IF" \
    -listen "$LISTEN_WINDOW" \
    -ring-path "$RING_PATH" \
    -ring-size "$RING_SIZE" \
    -ring-chunk "$RING_CHUNK"
if [ -n "$FALLBACK_RING_PATH" ]; then
    set -- "$@" \
        -fallback-ring-path "$FALLBACK_RING_PATH" \
        -fallback-ring-size "$FALLBACK_RING_SIZE" \
        -fallback-ring-chunk "$FALLBACK_RING_CHUNK" \
        -fallback-ring-sync="$FALLBACK_RING_SYNC"
    log "capturing $CAN_IF to primary RAM ring $RING_PATH and flash fallback $FALLBACK_RING_PATH listen=$LISTEN_WINDOW"
else
    log "capturing $CAN_IF to primary RAM ring $RING_PATH size=$RING_SIZE chunk=$RING_CHUNK listen=$LISTEN_WINDOW"
fi
"$@" &
child_pid=$!

term_handler() {
    log "stopping CAN ring child pid=$child_pid"
    kill "$child_pid" 2>/dev/null || true
    wait "$child_pid" 2>/dev/null || true
    checkpoint_flash_ring
    stop_gpio_hold
    exit 0
}

trap term_handler INT TERM
status=0
wait "$child_pid" || status=$?
checkpoint_flash_ring
stop_gpio_hold
exit "$status"
