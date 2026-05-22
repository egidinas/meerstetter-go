#!/usr/bin/env bash
set -euo pipefail

# Config-driven launcher for a LAN Meerstetter MeCom router.
#
# It starts one addressed MeCom listener and, when CAN or remote routes are
# available, an optional CAN-focused compatibility listener. Site-specific
# serial IDs, CAN nodes, and remote endpoints belong in the config file.

CONFIG_FILE="${MEERSTETTER_SERIAL_BRIDGES_CONFIG:-${XDG_CONFIG_HOME:-$HOME/.config}/meerstetter/serial-bridges.env}"
if [[ -r "$CONFIG_FILE" ]]; then
  # shellcheck disable=SC1090
  source "$CONFIG_FILE"
fi

BIND_ADDR="${BIND_ADDR:-0.0.0.0}"
SOCAT="${SOCAT:-socat}"
VSERVER="${VSERVER:-mecomvseriald}"
STATE_DIR="${STATE_DIR:-${XDG_RUNTIME_DIR:-/tmp}/meerstetter-serial}"
LOG_DIR="${LOG_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/meerstetter-serial}"
if [[ -z "${DEVICE_CACHE_DIR+x}" ]]; then
  DEVICE_CACHE_DIR="${LOG_DIR}/device-cache"
fi
BAUD="${BAUD:-57600}"
MODE="${MODE:-vserial}"
LISTEN_PORT="${LISTEN_PORT:-50000}"
CAN_LISTEN_PORT="${CAN_LISTEN_PORT:-50010}"
MECOM_ADDRESS_ZERO="${MECOM_ADDRESS_ZERO:-${ADDRESS_ZERO:-disabled}}"
CAN_MECOM_ADDRESS_ZERO="${CAN_MECOM_ADDRESS_ZERO:-${MECOM_CAN_ADDRESS_ZERO:-$MECOM_ADDRESS_ZERO}}"
MECOM_ROUTE_POLICY="${MECOM_ROUTE_POLICY:-dynamic}"
ENABLE_SERIAL_ROUTES="${ENABLE_SERIAL_ROUTES:-auto}"
CAN_IFACE="${CAN_IFACE:-can0}"
ENABLE_CAN_ROUTES="${ENABLE_CAN_ROUTES:-auto}"
ENABLE_CAN_TCP="${ENABLE_CAN_TCP:-auto}"
ENABLE_REMOTE_CAN_ROUTES="${ENABLE_REMOTE_CAN_ROUTES:-${ENABLE_PI_CAN_ROUTES:-auto}}"
REMOTE_CAN_HOST="${REMOTE_CAN_HOST:-${PI_CAN_HOST:-}}"
REMOTE_CAN_PORT="${REMOTE_CAN_PORT:-${PI_CAN_PORT:-50010}}"
REMOTE_CAN_PROBE="${REMOTE_CAN_PROBE:-${PI_CAN_PROBE:-tcp}}"
TRACE_FRAMES="${TRACE_FRAMES:-0}"
CAN_TRACE_FRAMES="${CAN_TRACE_FRAMES:-0}"

FIELD_SEP=$'\037'
REMOTE_DISABLED="__disabled__"
routes=()
legacy_ports=()

usage() {
  cat >&2 <<'USAGE'
usage: start-meerstetter-serial-bridges.sh {start|plan|start-legacy|stop|status}

Configuration is read from MEERSTETTER_SERIAL_BRIDGES_CONFIG, defaulting to
$XDG_CONFIG_HOME/meerstetter/serial-bridges.env or ~/.config/meerstetter/serial-bridges.env.

Route config uses MECOM_ROUTE_COUNT plus MECOM_ROUTE_N entries. The compact
legacy-compatible form is:
  MECOM_ROUTE_1=75:CONTROLLER_A:0x4b:50002

Fields are MeCom address, FTDI serial or /dev path, optional CAN node, and
optional legacy TCP port. Use "-" to omit a field:
  MECOM_ROUTE_2=76:-:0x4c:-
  MECOM_ROUTE_3=81:-:-:-

The extensible key/value form is easier for mixed fleets:
  MECOM_ROUTE_4=addr=84,serial=CONTROLLER_D,can=0x54,remote=off,legacy=50004
  MECOM_ROUTE_5=addr=90,serial=-,can=-,endpoint=tcp:192.168.6.160:50010

When remote is omitted, a row inherits REMOTE_CAN_HOST/REMOTE_CAN_PORT if the
remote route is enabled and healthy. Set remote=off to suppress that inheritance.
The remote/endpoint/target/bridge field can point at a PiXtend CAN bridge,
Kvaser DIN-rail TCP bridge, or any other transparent MeCom TCP router.

ENABLE_SERIAL_ROUTES=auto skips missing /dev serial paths while keeping any
available CAN or remote routes for the same MeCom address.
MECOM_ADDRESS_ZERO/CAN_MECOM_ADDRESS_ZERO accept disabled, auto-first,
route-order, or an explicit MeCom address. auto-first resolves to the first
active route generated for that listener.
USAGE
}

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

normalize_optional() {
  local value
  value="$(trim "${1:-}")"
  case "${value,,}" in
    ""|"-"|"none"|"off"|"disabled"|"null")
      echo ""
      ;;
    *)
      echo "$value"
      ;;
  esac
}

normalize_remote() {
  local value
  value="$(trim "${1:-}")"
  case "${value,,}" in
    "")
      echo ""
      ;;
    "-"|"none"|"off"|"disabled"|"null"|"no")
      echo "$REMOTE_DISABLED"
      ;;
    *)
      echo "$value"
      ;;
  esac
}

remote_is_disabled() {
  [[ "${1:-}" == "$REMOTE_DISABLED" ]]
}

add_route_fields() {
  local addr="$1"
  local serial="$2"
  local node="$3"
  local remote="$4"
  local legacy_port="$5"

  addr="$(trim "$addr")"
  serial="$(normalize_optional "$serial")"
  node="$(normalize_optional "$node")"
  remote="$(normalize_remote "$remote")"
  legacy_port="$(normalize_optional "$legacy_port")"

  if [[ -z "$addr" ]]; then
    echo "ERROR route address is required" >&2
    exit 2
  fi
  if [[ -n "$legacy_port" && -z "$serial" ]]; then
    echo "ERROR route ${addr} defines legacy port ${legacy_port} without a serial endpoint" >&2
    exit 2
  fi

  routes+=("${addr}${FIELD_SEP}${serial}${FIELD_SEP}${node}${FIELD_SEP}${remote}")
  if [[ -n "$legacy_port" ]]; then
    legacy_ports+=("${serial}${FIELD_SEP}${legacy_port}")
  fi
}

add_compact_route() {
  local spec="$1"
  local addr serial node legacy_port extra

  IFS=: read -r addr serial node legacy_port extra <<<"$spec"
  if [[ -n "${extra:-}" || -z "${addr:-}" ]]; then
    echo "ERROR invalid route spec ${spec@Q}; expected addr:serial[:can_node[:legacy_port]]" >&2
    exit 2
  fi

  add_route_fields "$addr" "${serial:-}" "${node:-}" "" "${legacy_port:-}"
}

add_key_value_route() {
  local spec="$1"
  local addr="" serial="" node="" remote="" legacy_port=""
  local delimiter=","
  local part key value
  local parts=()

  if [[ "$spec" == *";"* && "$spec" != *","* ]]; then
    delimiter=";"
  fi

  IFS="$delimiter" read -ra parts <<<"$spec"
  for part in "${parts[@]}"; do
    part="$(trim "$part")"
    [[ -n "$part" ]] || continue
    if [[ "$part" != *=* ]]; then
      echo "ERROR invalid route field ${part@Q} in ${spec@Q}; expected key=value" >&2
      exit 2
    fi
    key="$(trim "${part%%=*}")"
    value="$(trim "${part#*=}")"
    case "${key,,}" in
      addr|address|mecom|mecom_address)
        addr="$value"
        ;;
      serial|ftdi|dev|device)
        serial="$value"
        ;;
      can|can_node|node)
        node="$value"
        ;;
      remote|remote_can|remote_endpoint|endpoint|target|bridge|tcp|pi|pi_can)
        remote="$value"
        ;;
      legacy|legacy_port|port)
        legacy_port="$value"
        ;;
      *)
        echo "ERROR unknown route key ${key@Q} in ${spec@Q}" >&2
        exit 2
        ;;
    esac
  done

  add_route_fields "$addr" "$serial" "$node" "$remote" "$legacy_port"
}

add_config_route() {
  local spec="$1"

  spec="$(trim "$spec")"
  [[ -n "$spec" ]] || return 0
  [[ "$spec" != \#* ]] || return 0

  if [[ "$spec" == *"="* ]]; then
    add_key_value_route "$spec"
  else
    add_compact_route "$spec"
  fi
}

load_routes() {
  local count="${MECOM_ROUTE_COUNT:-0}"
  local i name spec

  if [[ "$count" =~ ^[0-9]+$ && "$count" -gt 0 ]]; then
    for ((i = 1; i <= count; i++)); do
      name="MECOM_ROUTE_${i}"
      spec="${!name:-}"
      if [[ -z "$spec" ]]; then
        echo "ERROR ${name} is empty but MECOM_ROUTE_COUNT=${count}" >&2
        exit 2
      fi
      add_config_route "$spec"
    done
  elif [[ -n "${MECOM_ROUTES:-}" ]]; then
    while IFS= read -r spec; do
      add_config_route "$spec"
    done <<<"$MECOM_ROUTES"
  fi

  if [[ "${#routes[@]}" == "0" ]]; then
    echo "ERROR no routes configured; set MECOM_ROUTE_COUNT and MECOM_ROUTE_N in ${CONFIG_FILE}" >&2
    exit 2
  fi
}

serial_path() {
  local serial="$1"
  if [[ "$serial" == /dev/* ]]; then
    echo "$serial"
  else
    echo "/dev/serial/by-id/usb-FTDI_FT230X_Basic_UART_${serial}-if00-port0"
  fi
}

serial_device_path() {
  local serial="$1"
  local target

  if [[ "$serial" == serial:/dev/* ]]; then
    target="${serial#serial:}"
    echo "${target%@*}"
  elif [[ "$serial" == serial:* ]]; then
    echo ""
  else
    serial_path "$serial"
  fi
}

serial_target() {
  local serial="$1"
  if [[ "$serial" == serial:* ]]; then
    echo "$serial"
  else
    echo "serial:$(serial_path "$serial")@${BAUD}"
  fi
}

serial_route_arg() {
  local addr="$1"
  local serial="$2"
  echo "${addr}=$(serial_target "$serial")"
}

can_route_arg() {
  local addr="$1"
  local node="$2"
  echo "${addr}=can:${CAN_IFACE}/${node}"
}

remote_target() {
  local target="$1"
  if [[ "$target" == tcp:* || "$target" == serial:* || "$target" == can:* ]]; then
    echo "$target"
  elif [[ "$target" == *:* ]]; then
    echo "tcp:${target}"
  else
    echo "tcp:${target}:${REMOTE_CAN_PORT}"
  fi
}

default_remote_target() {
  echo "tcp:${REMOTE_CAN_HOST}:${REMOTE_CAN_PORT}"
}

remote_route_arg() {
  local addr="$1"
  local target="$2"
  echo "${addr}=$(remote_target "$target")"
}

truthy() {
  case "${1,,}" in
    1|true|yes|on) return 0 ;;
    *) return 1 ;;
  esac
}

falsey() {
  case "${1,,}" in
    0|false|no|off) return 0 ;;
    *) return 1 ;;
  esac
}

serial_route_enabled() {
  local serial="$1"
  local dev

  if truthy "$ENABLE_SERIAL_ROUTES"; then
    return 0
  fi
  if falsey "$ENABLE_SERIAL_ROUTES"; then
    return 1
  fi
  if [[ "${ENABLE_SERIAL_ROUTES,,}" != "auto" && -n "$ENABLE_SERIAL_ROUTES" ]]; then
    echo "WARN unknown ENABLE_SERIAL_ROUTES=${ENABLE_SERIAL_ROUTES}; using auto" >&2
  fi

  dev="$(serial_device_path "$serial")"
  [[ -n "$dev" ]] || return 0
  [[ -e "$dev" ]]
}

can_iface_auto_healthy() {
  local details
  details="$(ip -details link show "$CAN_IFACE" 2>/dev/null || true)"
  [[ -n "$details" ]] || return 1
  grep -Eq '<[^>]*(UP|LOWER_UP)[^>]*>' <<<"$details" || return 1
  if grep -Fq 'can state ' <<<"$details"; then
    grep -Fq 'can state ERROR-ACTIVE' <<<"$details" || return 1
  fi
}

can_routes_enabled() {
  if truthy "$ENABLE_CAN_ROUTES"; then
    return 0
  fi
  if falsey "$ENABLE_CAN_ROUTES"; then
    return 1
  fi
  if [[ "${ENABLE_CAN_ROUTES,,}" != "auto" && -n "$ENABLE_CAN_ROUTES" ]]; then
    echo "WARN unknown ENABLE_CAN_ROUTES=${ENABLE_CAN_ROUTES}; using auto" >&2
  fi
  can_iface_auto_healthy
}

remote_tcp_probe() {
  [[ -n "$REMOTE_CAN_HOST" ]] || return 1
  if command -v nc >/dev/null 2>&1; then
    nc -z -w1 "$REMOTE_CAN_HOST" "$REMOTE_CAN_PORT" >/dev/null 2>&1
  else
    timeout 1 bash -c "</dev/tcp/${REMOTE_CAN_HOST}/${REMOTE_CAN_PORT}" >/dev/null 2>&1
  fi
}

remote_routes_enabled() {
  if truthy "$ENABLE_REMOTE_CAN_ROUTES"; then
    [[ -n "$REMOTE_CAN_HOST" ]] || {
      echo "WARN ENABLE_REMOTE_CAN_ROUTES=${ENABLE_REMOTE_CAN_ROUTES} but REMOTE_CAN_HOST is empty" >&2
      return 1
    }
    return 0
  fi
  if falsey "$ENABLE_REMOTE_CAN_ROUTES"; then
    return 1
  fi
  if [[ "${ENABLE_REMOTE_CAN_ROUTES,,}" != "auto" && -n "$ENABLE_REMOTE_CAN_ROUTES" ]]; then
    echo "WARN unknown ENABLE_REMOTE_CAN_ROUTES=${ENABLE_REMOTE_CAN_ROUTES}; using auto" >&2
  fi
  [[ -n "$REMOTE_CAN_HOST" ]] || return 1
  case "${REMOTE_CAN_PROBE,,}" in
    none)
      return 0
      ;;
    tcp|"")
      remote_tcp_probe
      ;;
    *)
      echo "WARN unknown REMOTE_CAN_PROBE=${REMOTE_CAN_PROBE}; using tcp" >&2
      remote_tcp_probe
      ;;
  esac
}

can_tcp_allowed() {
  if falsey "$ENABLE_CAN_TCP"; then
    return 1
  fi
  if [[ "${ENABLE_CAN_TCP,,}" != "auto" ]] && ! truthy "$ENABLE_CAN_TCP"; then
    echo "WARN unknown ENABLE_CAN_TCP=${ENABLE_CAN_TCP}; using auto" >&2
  fi
  return 0
}

append_remote_route_args() {
  local -n remote_out="$1"
  local addr="$2"
  local remote="$3"
  local default_remote_enabled="$4"
  local target=""

  if remote_is_disabled "$remote"; then
    return 0
  fi
  if [[ -n "$remote" ]]; then
    target="$remote"
  elif [[ "$default_remote_enabled" == "1" ]]; then
    target="$(default_remote_target)"
  fi
  [[ -n "$target" ]] || return 0

  remote_out+=("-route" "$(remote_route_arg "$addr" "$target")")
}

append_routed_args() {
  local -n routed_out="$1"
  local can_enabled="$2"
  local remote_enabled="$3"
  local item addr serial node remote

  for item in "${routes[@]}"; do
    IFS="$FIELD_SEP" read -r addr serial node remote <<<"$item"
    if [[ -n "$serial" ]] && serial_route_enabled "$serial"; then
      routed_out+=("-route" "$(serial_route_arg "$addr" "$serial")")
    fi
    if [[ "$can_enabled" == "1" && -n "${node:-}" ]]; then
      routed_out+=("-route" "$(can_route_arg "$addr" "$node")")
    fi
    append_remote_route_args routed_out "$addr" "$remote" "$remote_enabled"
  done
}

append_can_args() {
  local -n can_out="$1"
  local can_enabled="$2"
  local remote_enabled="$3"
  local item addr serial node remote

  for item in "${routes[@]}"; do
    IFS="$FIELD_SEP" read -r addr serial node remote <<<"$item"
    if [[ "$can_enabled" == "1" && -n "${node:-}" ]]; then
      can_out+=("-route" "$(can_route_arg "$addr" "$node")")
    fi
    append_remote_route_args can_out "$addr" "$remote" "$remote_enabled"
  done
}

args_have_remote() {
  local -n args_ref="$1"
  local i
  for ((i = 1; i < ${#args_ref[@]}; i += 2)); do
    if [[ "${args_ref[i]}" == *=tcp:* ]]; then
      return 0
    fi
  done
  return 1
}

first_route_address() {
  local -n route_args_ref="$1"
  local i route
  for ((i = 0; i < ${#route_args_ref[@]}; i += 2)); do
    [[ "${route_args_ref[i]:-}" == "-route" ]] || continue
    route="${route_args_ref[i + 1]:-}"
    route="${route%%=*}"
    if [[ -n "$route" ]]; then
      echo "$route"
      return 0
    fi
  done
  return 1
}

address_zero_auto_first() {
  case "${1,,}" in
    auto-first|auto|first) return 0 ;;
    *) return 1 ;;
  esac
}

resolve_address_zero_mode() {
  local mode="$1"
  local args_var="$2"

  if address_zero_auto_first "$mode"; then
    first_route_address "$args_var" || {
      echo "ERROR address-zero auto-first requested but no active routes are available" >&2
      return 1
    }
    return 0
  fi
  echo "$mode"
}

format_address_zero_mode() {
  local mode="$1"
  local args_var="$2"

  if address_zero_auto_first "$mode"; then
    first_route_address "$args_var" || echo "$mode"
    return 0
  fi
  echo "$mode"
}

print_plan() {
  local can_enabled=0
  local remote_enabled=0
  local remote_active=0
  local item addr serial node remote row_args=() routed_args=() can_args=()
  local listener_address_zero can_address_zero
  local i

  if can_routes_enabled; then
    can_enabled=1
  fi
  if remote_routes_enabled; then
    remote_enabled=1
  fi

  echo "CONFIG ${CONFIG_FILE}"
  append_routed_args routed_args "$can_enabled" "$remote_enabled"
  listener_address_zero="$(format_address_zero_mode "$MECOM_ADDRESS_ZERO" routed_args)"
  echo "LISTENER tcp://${BIND_ADDR}:${LISTEN_PORT} address-zero=${listener_address_zero} route-policy=${MECOM_ROUTE_POLICY} device-cache=${DEVICE_CACHE_DIR:-disabled}"
  for item in "${routes[@]}"; do
    row_args=()
    IFS="$FIELD_SEP" read -r addr serial node remote <<<"$item"
    if [[ -n "$serial" ]] && serial_route_enabled "$serial"; then
      row_args+=("-route" "$(serial_route_arg "$addr" "$serial")")
    fi
    if [[ "$can_enabled" == "1" && -n "${node:-}" ]]; then
      row_args+=("-route" "$(can_route_arg "$addr" "$node")")
    fi
    append_remote_route_args row_args "$addr" "$remote" "$remote_enabled"
    for ((i = 0; i < ${#row_args[@]}; i += 2)); do
      echo "ROUTE ${row_args[i + 1]}"
    done
    if [[ "${#row_args[@]}" == "0" ]]; then
      echo "ROUTE_DISABLED ${addr} no-active-downstream"
    fi
  done

  append_can_args can_args "$can_enabled" "$remote_enabled"
  if args_have_remote can_args; then
    remote_active=1
  fi
  if can_tcp_allowed && [[ "${#can_args[@]}" -gt 0 ]]; then
    can_address_zero="$(format_address_zero_mode "$CAN_MECOM_ADDRESS_ZERO" can_args)"
    echo "CAN_LISTENER tcp://${BIND_ADDR}:${CAN_LISTEN_PORT} address-zero=${can_address_zero} route-policy=${MECOM_ROUTE_POLICY} local-can=${can_enabled} remote-can=${remote_active} device-cache=${DEVICE_CACHE_DIR:-disabled}"
    for ((i = 0; i < ${#can_args[@]}; i += 2)); do
      echo "CAN_ROUTE ${can_args[i + 1]}"
    done
  else
    echo "CAN_LISTENER disabled local-can=${can_enabled} remote-can=${remote_active}"
  fi
}

stop_existing() {
  local pidfile pid
  for pidfile in "$STATE_DIR"/*.pid; do
    [[ -e "$pidfile" ]] || continue
    pid="$(cat "$pidfile" 2>/dev/null || true)"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
    rm -f "$pidfile"
  done
}

ensure_runtime_dirs() {
  mkdir -p "$STATE_DIR" "$LOG_DIR"
  if [[ -n "$DEVICE_CACHE_DIR" ]]; then
    mkdir -p "$DEVICE_CACHE_DIR"
  fi
}

start_can_tcp() {
  local can_enabled="$1"
  local remote_enabled="$2"
  local args=()
  local cache_args=()
  local trace_args=()
  local remote_active=0
  local address_zero
  local log="$LOG_DIR/mecomvseriald-can-${CAN_LISTEN_PORT}.log"

  append_can_args args "$can_enabled" "$remote_enabled"
  if [[ "${#args[@]}" == "0" ]]; then
    echo "WARN no CAN routes configured for tcp://${BIND_ADDR}:${CAN_LISTEN_PORT}" | tee -a "$log"
    return 0
  fi
  if args_have_remote args; then
    remote_active=1
  fi
  if truthy "$CAN_TRACE_FRAMES"; then
    trace_args+=("-trace-frames")
  fi
  if [[ -n "$DEVICE_CACHE_DIR" ]]; then
    cache_args+=("-device-cache-dir" "$DEVICE_CACHE_DIR")
  fi
  address_zero="$(resolve_address_zero_mode "$CAN_MECOM_ADDRESS_ZERO" args)"

  echo "START mecomvseriald CAN-only tcp://${BIND_ADDR}:${CAN_LISTEN_PORT} address-zero=${address_zero} route-policy=${MECOM_ROUTE_POLICY} local-can=${can_enabled}:${CAN_IFACE} remote-can=${remote_active}:${REMOTE_CAN_HOST:-}:${REMOTE_CAN_PORT} device-cache=${DEVICE_CACHE_DIR:-disabled}" | tee -a "$log"
  "$VSERVER" \
    -listen "${BIND_ADDR}:${CAN_LISTEN_PORT}" \
    -address-zero "$address_zero" \
    -route-policy "$MECOM_ROUTE_POLICY" \
    -timeout 2s \
    -reconnect-delay 500ms \
    "${cache_args[@]}" \
    "${trace_args[@]}" \
    "${args[@]}" \
    >>"$log" 2>&1 &
  echo "$!" >"$STATE_DIR/mecomvseriald-can-${CAN_LISTEN_PORT}.pid"
}

start_vserial() {
  local args=()
  local cache_args=()
  local trace_args=()
  local can_enabled=0
  local remote_enabled=0
  local address_zero
  local log="$LOG_DIR/mecomvseriald-${LISTEN_PORT}.log"

  if can_routes_enabled; then
    can_enabled=1
  fi
  if remote_routes_enabled; then
    remote_enabled=1
  fi

  append_routed_args args "$can_enabled" "$remote_enabled"
  if [[ "${#args[@]}" == "0" ]]; then
    echo "ERROR no active downstream routes for tcp://${BIND_ADDR}:${LISTEN_PORT}" >&2
    exit 2
  fi

  if can_tcp_allowed; then
    start_can_tcp "$can_enabled" "$remote_enabled"
  fi
  if truthy "$TRACE_FRAMES"; then
    trace_args+=("-trace-frames")
  fi
  if [[ -n "$DEVICE_CACHE_DIR" ]]; then
    cache_args+=("-device-cache-dir" "$DEVICE_CACHE_DIR")
  fi

  address_zero="$(resolve_address_zero_mode "$MECOM_ADDRESS_ZERO" args)"
  echo "START mecomvseriald tcp://${BIND_ADDR}:${LISTEN_PORT} address-zero=${address_zero} route-policy=${MECOM_ROUTE_POLICY} local-can=${can_enabled}:${CAN_IFACE} remote-can=${remote_enabled}:${REMOTE_CAN_HOST:-}:${REMOTE_CAN_PORT} device-cache=${DEVICE_CACHE_DIR:-disabled}" | tee -a "$log"
  exec "$VSERVER" \
    -listen "${BIND_ADDR}:${LISTEN_PORT}" \
    -address-zero "$address_zero" \
    -route-policy "$MECOM_ROUTE_POLICY" \
    -timeout 2s \
    -reconnect-delay 500ms \
    "${cache_args[@]}" \
    "${trace_args[@]}" \
    "${args[@]}" \
    >>"$log" 2>&1
}

start_legacy() {
  local item serial port dev log
  if [[ "${#legacy_ports[@]}" == "0" ]]; then
    echo "WARN no legacy ports configured; add legacy/port to route entries" >&2
    return 0
  fi
  for item in "${legacy_ports[@]}"; do
    IFS="$FIELD_SEP" read -r serial port <<<"$item"
    dev="$(serial_path "$serial")"
    log="$LOG_DIR/${serial}-${port}.log"
    if ! serial_route_enabled "$serial"; then
      echo "WARN serial route disabled or missing for ${serial} (${dev})" | tee -a "$log"
      continue
    fi
    if [[ ! -e "$dev" ]]; then
      echo "WARN missing ${dev}" | tee -a "$log"
      continue
    fi
    echo "START legacy ${serial} ${dev} tcp://${BIND_ADDR}:${port}" | tee -a "$log"
    "$SOCAT" -t0 \
      "TCP-LISTEN:${port},bind=${BIND_ADDR},fork,reuseaddr,keepalive" \
      "FILE:${dev},raw,echo=0,b${BAUD},cs8,parenb=0" \
      >>"$log" 2>&1 &
    echo "$!" >"$STATE_DIR/${serial}-${port}.pid"
  done
  wait
}

status() {
  if pgrep -af "$VSERVER" >/dev/null 2>&1; then
    pgrep -af "$VSERVER"
  else
    echo "STOPPED mecomvseriald port=${LISTEN_PORT}"
  fi
  print_plan
}

main() {
  local command="${1:-start}"
  case "$command" in
    start)
      ensure_runtime_dirs
      load_routes
      stop_existing
      if [[ "$MODE" == "legacy" ]]; then
        start_legacy
      else
        start_vserial
      fi
      ;;
    plan)
      load_routes
      print_plan
      ;;
    start-legacy)
      ensure_runtime_dirs
      load_routes
      stop_existing
      start_legacy
      ;;
    stop)
      mkdir -p "$STATE_DIR"
      stop_existing
      ;;
    status)
      load_routes
      status
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      usage
      exit 2
      ;;
  esac
}

main "$@"
