// @ts-nocheck
export const CANONICAL_TILE_RENDERER = "signalforge.tile.uplot";

export const SERIES_ROLE_META = Object.freeze({
  cmd: { label: "target / command", rank: 10, className: "cmd", dash: "6,4", width: 2.2, opacity: 0.98 },
  command: { label: "target / command", rank: 10, className: "cmd", dash: "6,4", width: 2.2, opacity: 0.98 },
  actual: { label: "actual", rank: 20, className: "actual", dash: "", width: 2.2, opacity: 0.98 },
  ghost: { label: "reference / sink", rank: 30, className: "ghost", dash: "2,4", width: 1.8, opacity: 0.86 },
  dut: { label: "power / load", rank: 40, className: "dut", dash: "", width: 2.0, opacity: 0.94 },
  aux: { label: "auxiliary", rank: 50, className: "aux", dash: "8,4", width: 1.8, opacity: 0.86 },
});

export function seriesRoleMeta(role) {
  return SERIES_ROLE_META[role] || SERIES_ROLE_META.actual;
}

export function seriesRoleColor(role, fallback = "var(--series-actual)") {
  const meta = seriesRoleMeta(role);
  const css = meta.className ? `--series-${meta.className}` : "";
  if (!css || typeof document === "undefined") return fallback;
  return getComputedStyle(document.documentElement).getPropertyValue(css).trim() || fallback;
}

export function measuredElementWidth(el) {
  if (!el) return 0;
  const rect = el.getBoundingClientRect ? el.getBoundingClientRect() : null;
  return Math.floor((rect && rect.width) || el.clientWidth || 0);
}

export function emptyGraphTile(opts: any = {}) {
  const id = opts.tile_id || opts.tileId || "empty";
  const nowMs = Date.now();
  const timeWindowMs = opts.time_window_ms || opts.timeWindowMs || 90_000;
  const now = new Date(nowMs).toISOString();
  return {
    schema_version: "signalforge.graph_tile.v1",
    id,
    card_id: id,
    level: "live",
    t0: new Date(nowMs - timeWindowMs).toISOString(),
    t1: now,
    generated_at: now,
    renderer: CANONICAL_TILE_RENDERER,
    kind: "timeseries",
    tile_id: id,
    title: opts.title || "",
    time_window_ms: timeWindowMs,
    axes: opts.axes || [],
    bands: [],
    markers: [],
    events: [],
    diagnostics: {
      status: "empty",
      point_count: 0,
      raw_point_count: 0,
      decimation: "none",
      freshness_ms: 0,
      renderer: CANONICAL_TILE_RENDERER,
      series_count: 0,
    },
    provenance: { source: "empty-graph-tile", generated_at: now },
    series: [],
  };
}

export function renderSeriesFromGraphTile(tile) {
  if (!tile || !Array.isArray(tile.series)) return [];
  const normalizedTile = normalizeGraphTile(tile);
  return normalizedTile.series.map((s) => {
    const legacyRole = s.seriesRole || (s.role === "command" ? "cmd" : s.role) || "actual";
    const source = (s.source_obj && typeof s.source_obj === "object")
      ? s.source_obj
      : (s.source && typeof s.source === "object") ? s.source : {};
    return {
      key: s.series_id || s.id || s.key || s.target_id || s.targetId || s.label,
      tileId: normalizedTile.tile_id || normalizedTile.id || normalizedTile.tileId,
      targetId: s.target_id || s.targetId,
      label: s.label,
      fullLabel: s.full_label || s.fullLabel || s.label,
      role: legacyRole,
      seriesRole: legacyRole,
      roleRank: s.role_rank ?? seriesRoleMeta(legacyRole).rank,
      color: s.color || seriesRoleColor(legacyRole),
      unit: s.unit || s.units || "_",
      provenance: s.provenance || "",
      source: s.source_obj || s.source || null,
      paramId: s.param_id !== undefined ? s.param_id : source.param_id,
      deviceId: s.device_id !== undefined ? s.device_id : source.device_id,
      instance: s.instance !== undefined ? s.instance : source.instance,
      signalId: s.signal_id !== undefined ? s.signal_id : source.signal_id,
      history: historyFromSeries(s),
    };
  }).sort((a, b) => {
    if (a.roleRank !== b.roleRank) return a.roleRank - b.roleRank;
    return String(a.tileId || a.key || a.label || "").localeCompare(String(b.tileId || b.key || b.label || ""));
  });
}

export function normalizeGraphTile(tile, opts: any = {}) {
  const fallback = emptyGraphTile({ tile_id: opts.tile_id || opts.tileId, timeWindowMs: opts.timeWindowMs || opts.time_window_ms });
  const sourceTile = tile || fallback;
  const timeWindowMs = sourceTile.time_window_ms || opts.timeWindowMs || opts.time_window_ms || fallback.time_window_ms;
  const series = (Array.isArray(sourceTile.series) ? sourceTile.series : []).map(normalizeSeries).filter((s) => s.points.length > 0 || s.spans?.length);
  const extents = series
    .flatMap((s) => s.points || [])
    .map((point) => Date.parse(point.timestamp))
    .filter(Number.isFinite);
  const nowMs = Date.now();
  const t0Ms = Number.isFinite(Date.parse(sourceTile.t0)) ? Date.parse(sourceTile.t0) : (extents.length ? Math.min(...extents) : nowMs - timeWindowMs);
  const t1Ms = Number.isFinite(Date.parse(sourceTile.t1)) ? Date.parse(sourceTile.t1) : (extents.length ? Math.max(...extents) : nowMs);
  const t0 = new Date(t0Ms).toISOString();
  const t1 = new Date(Math.max(t1Ms, t0Ms + 1)).toISOString();
  const pointCount = series.reduce((acc, s) => acc + (s.points?.length || 0), 0);
  return {
    ...fallback,
    ...sourceTile,
    schema_version: sourceTile.schema_version || fallback.schema_version,
    id: sourceTile.id || sourceTile.tile_id || fallback.id,
    card_id: sourceTile.card_id || sourceTile.tile_id || sourceTile.id || fallback.card_id,
    level: sourceTile.level || "live",
    t0,
    t1,
    generated_at: sourceTile.generated_at || new Date(nowMs).toISOString(),
    renderer: CANONICAL_TILE_RENDERER,
    kind: sourceTile.kind || "timeseries",
    tile_id: sourceTile.tile_id || sourceTile.id || fallback.tile_id,
    title: sourceTile.title || fallback.title,
    time_window_ms: timeWindowMs,
    axes: sourceTile.axes || fallback.axes,
    bands: Array.isArray(sourceTile.bands) ? sourceTile.bands : [],
    markers: Array.isArray(sourceTile.markers) ? sourceTile.markers : [],
    events: Array.isArray(sourceTile.events) ? sourceTile.events : [],
    diagnostics: {
      ...(fallback.diagnostics || {}),
      ...(sourceTile.diagnostics || {}),
      status: series.length > 0 ? "ok" : ((sourceTile.diagnostics && sourceTile.diagnostics.status) || "empty"),
      point_count: pointCount,
      raw_point_count: sourceTile.diagnostics?.raw_point_count ?? pointCount,
      decimation: sourceTile.diagnostics?.decimation || "none",
      renderer: CANONICAL_TILE_RENDERER,
      series_count: series.length,
    },
    provenance: sourceTile.provenance || fallback.provenance,
    series,
  };
}

function normalizeSeries(series) {
  const sourceObj = (series.source && typeof series.source === "object") ? series.source : null;
  const legacyRole = series.role || series.seriesRole || "actual";
  const role = canonicalRole(legacyRole);
  const id = String(series.series_id || series.id || series.key || series.target_id || series.targetId || series.label || "series");
  const unit = series.unit || series.units || "_";
  const points = normalizePoints(series);
  return {
    ...series,
    id,
    series_id: series.series_id || id,
    label: series.label || id,
    role,
    seriesRole: legacyRole,
    unit,
    units: unit,
    axis_id: series.axis_id || axisIdForSeries(series, unit),
    source: stringifySource(series.source_ref || series.source || sourceObj || id),
    source_obj: sourceObj || series.source_obj,
    color: series.color,
    points,
    spans: Array.isArray(series.spans) ? series.spans : [],
  };
}

function canonicalRole(role) {
  if (role === "cmd") return "command";
  if (role === "dut") return "actual";
  if (role === "aux") return "actual";
  return role || "actual";
}

function axisIdForSeries(series, unit) {
  const explicit = series.axis_id || series.axisId;
  if (explicit) return explicit;
  const u = String(unit || series.unit || series.units || "").trim().toLowerCase();
  const label = `${series.id || ""} ${series.label || ""} ${series.full_label || ""}`.toLowerCase();
  if (u === "a" || u === "amp" || u === "amps") return "current_a";
  if (u === "v" || u === "volt" || u === "volts") return "voltage_v";
  if (u === "w" || u === "watt" || u === "watts") return "power_w";
  if (u === "%" || u === "percent") return "percent";
  if (u === "ms" || u === "millisecond" || u === "milliseconds") return "bus_ms";
  if (u === "s" || u === "sec" || u === "secs" || u === "second" || u === "seconds") return "seconds";
  if (u.includes("deg") || u === "c" || u === "degc" || u === "deg c") return "temperature_c";
  if (label.includes("counter")) return "counter";
  if (series.role === "counter" || series.kind === "counter") return "counter";
  return "generic_numeric";
}

function normalizePoints(series) {
  if (Array.isArray(series.points) && series.points.length) {
    return series.points.flatMap((point) => normalizePoint(point));
  }
  const history = series.history || {};
  const ts = Array.isArray(history.ts) ? history.ts : [];
  const values = Array.isArray(history.v) ? history.v : [];
  return values.flatMap((value, idx) => normalizePoint({ t: ts[idx], v: value }));
}

function normalizePoint(point) {
  const rawTimestamp = point.timestamp ?? point.t ?? point.time;
  const rawValue = point.value ?? point.v ?? point.y;
  const value = Number(rawValue);
  const timeMs = typeof rawTimestamp === "number" ? rawTimestamp : Date.parse(String(rawTimestamp || ""));
  if (!Number.isFinite(value) || !Number.isFinite(timeMs)) return [];
  return [{ timestamp: new Date(timeMs).toISOString(), value }];
}

function historyFromSeries(series) {
  return {
    ts: (series.points || []).map((point) => Date.parse(point.timestamp)),
    v: (series.points || []).map((point) => point.value),
    q: (series.points || []).map(() => "ok"),
  };
}

function stringifySource(source) {
  if (!source || typeof source === "string") return source || "";
  const device = source.device_id || source.deviceId || "";
  const param = source.param_id || source.paramId || "";
  const instance = source.instance || "";
  const endpoint = source.endpoint || "";
  const signal = source.signal_id || source.signalId || "";
  return `device=${device} param=${param} instance=${instance} signal=${signal} endpoint=${endpoint}`.trim();
}
