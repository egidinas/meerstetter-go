// @ts-nocheck
import { useState, useEffect, useMemo } from "react";
import { MecomAPI } from "../api/mecom";
import { getTelemetry, recordTelemetry } from "../lib/telemetry";
import { CANONICAL_TILE_RENDERER, DEFAULT_TILE_LEVELS, seriesRoleMeta, pickTileLevel } from "../lib/series";

const ASSIGNMENT_KEY = "mecomgw.assignments";

export function loadAssignments() {
  try {
    const raw = JSON.parse(localStorage.getItem(ASSIGNMENT_KEY) || "[]");
    if (!Array.isArray(raw)) return [];
    return raw.map(normalizeAssignment).filter((a) => a.device_id && Number.isFinite(a.param_id));
  } catch (_) { return []; }
}
export function saveAssignments(list) {
  const normalized = (Array.isArray(list) ? list : [])
    .map(normalizeAssignment)
    .filter((a) => a.device_id && Number.isFinite(a.param_id));
  localStorage.setItem(ASSIGNMENT_KEY, JSON.stringify(normalized));
  window.dispatchEvent(new CustomEvent("mecomgw-assignments-changed"));
}

export const WALLS = {
  fleetTemp:   { wall_id: "fleet-temp",   label: "Fleet hero · Temperature" },
  fleetSupply: { wall_id: "fleet-supply", label: "Fleet hero · Power supplies" },
};
export const GRAPH_ORIGIN_DEVICE_ID = "tec-76";
export function wallForDevice(deviceId) { return { wall_id: "device-" + deviceId, label: "Device · " + deviceId }; }

export function signalAddress(paramId, deviceId, instance?) {
  return paramId + "@" + deviceId + "/" + (instance || 1);
}
export function parseSignalAddress(targetId) {
  const m = String(targetId || "").match(/^(\d+)@([^/]+)\/(\d+)$/);
  if (!m) return null;
  return { param_id: parseInt(m[1], 10), device_id: m[2], instance: parseInt(m[3], 10) || 1 };
}

function firstDefined(...args) {
  for (let i = 0; i < args.length; i++) {
    if (args[i] !== undefined && args[i] !== null) return args[i];
  }
  return undefined;
}

export function normalizeAssignment(a) {
  const item = a || {};
  const parsed = parseSignalAddress(item.target_id);
  const opts = item.options || {};
  const paramId = Number(firstDefined(item.param_id, opts.param_id, parsed && parsed.param_id));
  const deviceId = String(firstDefined(item.device_id, opts.device_id, parsed && parsed.device_id, ""));
  const instance = Number(firstDefined(item.instance, opts.instance, parsed && parsed.instance, 1)) || 1;
  const wallId = String(firstDefined(item.wall_id, item.wallId, "wall"));
  const targetId = item.target_id || signalAddress(paramId, deviceId, instance);
  return {
    wall_id: wallId,
    tile_id: firstDefined(item.tile_id, item.tileId, wallId + "-" + targetId),
    target_id: targetId,
    kind: item.kind || "trend",
    options: Object.assign({}, opts, { param_id: paramId, device_id: deviceId, instance }),
    param_id: paramId,
    device_id: deviceId,
    instance,
  };
}

export function makeAssignment(wallId, paramId, deviceId, instance?) {
  return normalizeAssignment({
    wall_id: wallId,
    target_id: signalAddress(paramId, deviceId, instance || 1),
    kind: "trend",
  });
}

export function tileSeriesKey(a) {
  const n = normalizeAssignment(a);
  return [n.wall_id || "wall", n.tile_id || n.target_id || "target", n.kind || "trend"].join(":");
}

export function useAssignments() {
  const [list, setList] = useState(loadAssignments);
  useEffect(() => {
    const fn = () => setList(loadAssignments());
    window.addEventListener("mecomgw-assignments-changed", fn);
    return () => window.removeEventListener("mecomgw-assignments-changed", fn);
  }, []);
  return {
    list,
    add: (wallId, paramId, deviceId, instance?) => {
      const cur = loadAssignments();
      const next = makeAssignment(wallId, paramId, deviceId, instance);
      if (cur.find((a) => a.wall_id === wallId && a.target_id === next.target_id)) return;
      cur.push(next);
      saveAssignments(cur);
    },
    remove: (wallId, paramId, deviceId, instance?) => {
      const addr = signalAddress(paramId, deviceId, instance);
      const cur = loadAssignments().filter((a) => !(a.wall_id === wallId && a.target_id === addr));
      saveAssignments(cur);
    },
    forWall: (wallId) => list.filter((a) => a.wall_id === wallId),
    hasAssignment: (wallId, paramId, deviceId, instance?) => {
      const addr = signalAddress(paramId, deviceId, instance);
      return list.some((a) => a.wall_id === wallId && a.target_id === addr);
    },
  };
}

const SEED_VERSION = 9;

function priorityParamIdsForChannel(ch) {
  const catalogue = MecomAPI.catalogue();
  const role = ch.role || "temp";
  const ids = catalogue
    .filter((p) => p.high_priority && (!p.applicableModes || p.applicableModes.includes(role)))
    .filter((p) => role !== "supply" || String(p.unit || "").toLowerCase() === "w")
    .map((p) => p.id);
  const defaults = role === "temp" ? [3000, 1000, 1001] : [1022];
  return Array.from(new Set(defaults.concat(ids))).filter((id) => Number.isFinite(Number(id)));
}

export function defaultAssignmentsForChannels(wallId, channels) {
  const seeds = [];
  (channels || []).forEach((ch) => {
    priorityParamIdsForChannel(ch).forEach((pid) => {
      seeds.push(makeAssignment(wallId, pid, ch.device_id, ch.instance));
    });
  });
  return seeds;
}

export function assignmentsWithPriorityDefaults(existing, wallId, channels) {
  const out = (existing || []).map(normalizeAssignment);
  const seen = new Set(out.map((a) => `${a.wall_id}:${a.target_id}`));
  defaultAssignmentsForChannels(wallId, channels).forEach((a) => {
    const key = `${a.wall_id}:${a.target_id}`;
    if (!seen.has(key)) {
      seen.add(key);
      out.push(a);
    }
  });
  return out;
}

export function seedAssignments() {
  const channels = MecomAPI.channels();
  const current = loadAssignments();
  let next = current.slice();
  const seedVersion = parseInt(localStorage.getItem("mecomgw.assignments.version") || "0", 10);
  const originDeviceId = (typeof MecomAPI.primaryDeviceId === "function" && MecomAPI.primaryDeviceId()) || GRAPH_ORIGIN_DEVICE_ID;
  const tempChannels = channels.filter((c) => c.role === "temp");
  const supplyChannels = channels.filter((c) => c.role === "supply");
  const originTempChannels = tempChannels.filter((c) => c.device_id === originDeviceId);
  const originSupplyChannels = supplyChannels.filter((c) => c.device_id === originDeviceId);
  if (seedVersion !== SEED_VERSION) {
    next = next.filter((item) => {
      const a = normalizeAssignment(item);
      const fleetWall = a.wall_id === WALLS.fleetTemp.wall_id || a.wall_id === WALLS.fleetSupply.wall_id;
      return !fleetWall || a.device_id === originDeviceId;
    });
  }
  next = assignmentsWithPriorityDefaults(next, WALLS.fleetTemp.wall_id, originTempChannels);
  next = assignmentsWithPriorityDefaults(next, WALLS.fleetSupply.wall_id, originSupplyChannels);
  channels.forEach((ch) => {
    const wallId = wallForDevice(ch.device_id).wall_id + "-" + ch.instance;
    next = assignmentsWithPriorityDefaults(next, wallId, [ch]);
  });
  const changed = next.length !== current.length || seedVersion !== SEED_VERSION;
  if (changed) saveAssignments(next);
  localStorage.setItem("mecomgw.assignments.version", String(SEED_VERSION));
}

export const CHANNEL_COLORS = ["#58a6ff", "#3fb950", "#d29922", "#a371f7", "#56d4dd", "#db61a2", "#e3b341", "#f47067"];
export function channelColor(deviceId, instance?) {
  const channels = MecomAPI.channels();
  const idx = channels.findIndex((c) => c.device_id === deviceId && c.instance === instance);
  return CHANNEL_COLORS[Math.max(0, idx) % CHANNEL_COLORS.length];
}

export function graphBucketForParam(paramId) {
  const p = MecomAPI.catalogue().find((x) => x.id === Number(paramId));
  const unit = String((p && p.unit) || "").toLowerCase();
  const group = String((p && p.group) || "").toLowerCase();
  if (unit === "degc" || group === "thermal" || Number(paramId) === 3000) return "thermal";
  if (unit === "w") return "power";
  if (unit === "v") return "voltage";
  if (unit === "a") return "current";
  if (group === "power") return "power";
  return "other";
}

const PARAM_SHORTHANDS = {
  "object temperature": "OT",
  "sink temperature": "ST",
  "target object temperature": "NOT",
  "nominal object temperature": "NOT",
  "output power": "OP",
  "output voltage": "OV",
  "output current": "OC",
};

function compactParamLabel(def) {
  const name = String((def && def.name) || "").trim();
  const key = name.toLowerCase().replace(/\s+/g, " ");
  if (PARAM_SHORTHANDS[key]) return PARAM_SHORTHANDS[key];
  if (def && def.sid) {
    const sid = String(def.sid)
      .replace(/_c$|_v$|_a$|_w$/i, "")
      .split("_")
      .filter(Boolean)
      .map((part) => part.charAt(0).toUpperCase())
      .join("");
    if (sid) return sid;
  }
  return name || "#" + (def && def.id);
}

function compactDeviceLabel(deviceId) {
  const text = String(deviceId || "").trim();
  const serial = text.match(/(?:sn|tec[-_ ]?)(\d+)/i) || text.match(/(\d+)$/);
  if (serial) return "SN" + serial[1];
  return text || "device";
}

function channelNickname(ch) {
  if (!ch) return "";
  const direct = ch.nickname || ch.custom_nickname || ch.customNickname || ch.alias || ch.short_label || ch.shortLabel;
  if (direct) return String(direct).trim();
  const note = String(ch.user_note || "");
  const controlMatch = note.match(/\b([A-Z]{1,4}\d+)\s+is\s+([^.;]*control[^.;]*)/i);
  if (controlMatch) return `${controlMatch[1].toUpperCase()} control`;
  return "";
}

function compactSeriesLabel(deviceId, instance, def, ch) {
  const base = `${compactDeviceLabel(deviceId)}-ch${instance} ${compactParamLabel(def)}`;
  const nick = channelNickname(ch);
  return nick ? `${base} · ${nick}` : base;
}

function median(values) {
  const sorted = values.filter((v) => Number.isFinite(v)).slice().sort((a, b) => a - b);
  if (!sorted.length) return 0;
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}

function firstPointOutlierThreshold(unit, restValues) {
  const u = String(unit || "").toLowerCase();
  const restMedian = median(restValues);
  const min = Math.min(...restValues);
  const max = Math.max(...restValues);
  const span = Math.max(0, max - min);
  if (u === "degc" || u.includes("deg")) return Math.max(15, span * 4);
  if (u === "v") return Math.max(2, Math.abs(restMedian) * 0.75, span * 4);
  if (u === "a") return Math.max(0.5, Math.abs(restMedian) * 1.5, span * 4);
  if (u === "w") return Math.max(1, Math.abs(restMedian) * 2, span * 4);
  return Math.max(1, Math.abs(restMedian) * 5, span * 6);
}

function filterSeriesHistoryForScale(history, unit) {
  const tsIn = history.ts || [];
  const vIn = history.v || [];
  const ts = [];
  const v = [];
  let suppressedOpenSensorPoints = 0;
  let suppressedInitialOutliers = 0;

  tsIn.forEach((stamp, idx) => {
    const value = vIn[idx];
    if (!Number.isFinite(value)) return;
    if ((String(unit || "").toLowerCase() === "degc" || String(unit || "").toLowerCase().includes("deg")) && value < -50) {
      suppressedOpenSensorPoints += 1;
      return;
    }
    ts.push(stamp);
    v.push(value);
  });

  if (v.length >= 3) {
    const rest = v.slice(1).filter((value) => Number.isFinite(value));
    if (rest.length >= 2) {
      const restMedian = median(rest);
      const threshold = firstPointOutlierThreshold(unit, rest);
      if (Math.abs(v[0] - restMedian) > threshold) {
        ts.shift();
        v.shift();
        suppressedInitialOutliers += 1;
      }
    }
  }

  return {
    history: { ...history, ts, v },
    suppressedOpenSensorPoints,
    suppressedInitialOutliers,
  };
}

export function buildGraphTileFromAssignments(assignments, opts: any = {}) {
  const catalogue = MecomAPI.catalogue();
  const normalized = (assignments || []).map(normalizeAssignment).filter((a) => a.device_id && Number.isFinite(a.param_id));
  const units: string[] = [];
  const nowMs = Date.now();
  const now = new Date(nowMs).toISOString();
  const tileId = opts.tile_id || opts.tileId || "graph-tile";
  const timeWindowMs = opts.time_window_ms || opts.timeWindowMs || 90_000;
  const level = opts.level || pickTileLevel(timeWindowMs);
  const series = normalized.map((a) => {
    const paramId = a.options.param_id;
    const deviceId = a.options.device_id;
    const instance = a.options.instance || 1;
    const def = catalogue.find((p) => p.id === paramId) || { id: paramId, name: "#" + paramId, unit: "" };
    let history = getTelemetry(deviceId, paramId, instance);
    if (typeof MecomAPI.readValue === "function") {
      const latest = MecomAPI.readValue(deviceId, paramId, instance);
      if (latest && typeof latest.value === "number" && !Number.isNaN(latest.value)) {
        recordTelemetry(deviceId, paramId, latest.value, latest.quality || "ok", instance);
        history = getTelemetry(deviceId, paramId, instance);
      }
    }
    const seriesRole = MecomAPI.roleForParam(paramId);
    const roleMeta = seriesRoleMeta(seriesRole);
    let color;
    if (opts.colorByChannel) {
      color = channelColor(deviceId, instance);
    } else {
      color = MecomAPI.colorForRole(seriesRole);
    }
    const signalPath = [def.group, def.subgroup, def.name].filter(Boolean).join(" / ");
    const provenance = MecomAPI.provenance(deviceId, paramId, instance);
    const ch = MecomAPI.channels().find((c) => c.device_id === deviceId && c.instance === instance);
    const unit = def.unit || "_";
    if (units.indexOf(unit) === -1) units.push(unit);
    const channelContext = `${deviceId} · channel ${instance}${ch && ch.label ? " · " + ch.label : ""}`;
    const sourceRef = `device=${deviceId} param=${paramId} instance=${instance} endpoint=${(ch && ch.endpoint) || ""}`;
    const filtered = filterSeriesHistoryForScale(history, unit);
    const seriesHistory = filtered.history;
    const suppressedPointCount = filtered.suppressedOpenSensorPoints;
    const suppressedInitialOutliers = filtered.suppressedInitialOutliers;
    let points = (seriesHistory.ts || []).map((ts, idx) => ({ timestamp: new Date(ts).toISOString(), value: (seriesHistory.v || [])[idx] }));
    if (points.length === 1) {
      points = [
        { timestamp: new Date(Date.parse(points[0].timestamp) - 1_000).toISOString(), value: points[0].value },
        points[0],
      ];
    }
    return {
      id: tileSeriesKey(a),
      series_id: tileSeriesKey(a),
      target_id: a.target_id,
      label: compactSeriesLabel(deviceId, instance, def, ch),
      full_label: `${channelContext} · param ${paramId} · ${signalPath || def.name}`,
      color,
      unit,
      history: seriesHistory,
      role: seriesRole,
      axis_id: axisIdForUnit(unit, seriesRole),
      role_rank: roleMeta.rank,
      provenance,
      source_ref: sourceRef,
      source: { device_id: deviceId, instance, param_id: paramId, signal_id: def.sid || String(def.id), endpoint: ch && ch.endpoint },
      diagnostics: suppressedPointCount || suppressedInitialOutliers ? { suppressed_open_sensor_points: suppressedPointCount, suppressed_initial_outlier_points: suppressedInitialOutliers } : undefined,
      points,
    };
  }).sort((a, b) => {
    if ((a.role_rank || 0) !== (b.role_rank || 0)) return (a.role_rank || 0) - (b.role_rank || 0);
    return String(a.series_id || "").localeCompare(String(b.series_id || ""));
  });
  return {
    schema_version: "signalforge.graph_tile.v1",
    id: tileId, card_id: tileId, level,
    t0: new Date(nowMs - timeWindowMs).toISOString(),
    t1: now,
    generated_at: now,
    renderer: CANONICAL_TILE_RENDERER,
    kind: "timeseries",
    tile_id: tileId,
    title: opts.title || "",
    time_window_ms: timeWindowMs,
    latest_endpoint: `/api/graph/tiles/${encodeURIComponent(tileId)}/live`,
    tile_endpoint: `/api/graph/tiles/${encodeURIComponent(tileId)}/${level}`,
    tile_files: DEFAULT_TILE_LEVELS.map((item) => ({ level: item.level, time_window_ms: item.timeWindowMs })),
    axes: units.slice(0, 2).map((unit, idx) => ({ unit, side: idx === 0 ? "left" : "right" })),
    bands: [],
    markers: [],
    events: [],
    diagnostics: {
      status: series.length > 0 ? "ok" : "empty",
      series_count: series.length,
      point_count: series.reduce((acc, s) => acc + ((s.points && s.points.length) || 0), 0),
      decimation: "none",
      renderer: CANONICAL_TILE_RENDERER,
      tile_level: level,
      tile_source: level === "live" ? "live-read-cache" : "history-read-cache",
      outlier_policy: "drop_detached_degC_below_-50_and_initial_out_of_family",
      suppressed_open_sensor_points: series.reduce((acc, s) => acc + ((s.diagnostics && s.diagnostics.suppressed_open_sensor_points) || 0), 0),
      suppressed_initial_outlier_points: series.reduce((acc, s) => acc + ((s.diagnostics && s.diagnostics.suppressed_initial_outlier_points) || 0), 0),
    },
    provenance: { source: "meerstetter-go.graphwall.assignments", generated_at: now },
    series,
  };
}

function graphTileSeriesFromAssignments(assignments) {
  return (assignments || []).map(normalizeAssignment).filter((a) => a.device_id && Number.isFinite(a.param_id));
}

function graphTileAssignmentsKey(assignments) {
  return JSON.stringify(graphTileSeriesFromAssignments(assignments).map((a) => [
    a.device_id,
    a.param_id,
    a.instance || 1,
    a.wall_id || "",
    a.tile_id || "",
    a.kind || "",
  ]));
}

function historyFromServerSeries(series) {
  const history = series && series.history ? series.history : {};
  const ts = Array.isArray(history.ts) ? history.ts : [];
  const v = Array.isArray(history.v) ? history.v : [];
  if (ts.length || v.length) return { ...history, ts, v };
  const points = Array.isArray(series && series.points) ? series.points : [];
  return {
    ...history,
    ts: points.map((p) => p.timestamp || p.t).filter(Boolean),
    v: points.map((p) => Number(p.value ?? p.v)).filter((value) => Number.isFinite(value)),
  };
}

function seriesSourceKey(series) {
  const source = (series && series.source) || {};
  const id = source.device_id || series?.device_id || String(series?.series_id || series?.id || "").split(":")[0];
  const param = Number(source.param_id ?? series?.param_id ?? String(series?.series_id || series?.id || "").split(":")[1]);
  const inst = Number(source.instance ?? series?.instance ?? String(series?.series_id || series?.id || "").split(":")[2] ?? 1) || 1;
  return `${id}:${param}:${inst}`;
}

function axesForUnits(units) {
  return (units || []).slice(0, 2).map((unit, idx) => ({
    id: axisIdForUnit(unit, ""),
    label: unitLabelForAxis(unit),
    unit,
    side: idx === 0 ? "left" : "right",
  }));
}

function unitLabelForAxis(unit) {
  const u = String(unit || "").trim().toLowerCase();
  if (u === "degc" || u === "c") return "Temperature [°C]";
  if (u === "v") return "Voltage [V]";
  if (u === "a") return "Current [A]";
  if (u === "w") return "Power [W]";
  if (u === "%") return "Percent [%]";
  return unit && unit !== "_" ? `Value [${unit}]` : "Value";
}

function enrichServerGraphTile(tile, assignments, opts: any = {}) {
  const catalogue = MecomAPI.catalogue();
  const channels = MecomAPI.channels();
  const assignmentBySource = new Map(graphTileSeriesFromAssignments(assignments).map((a) => [`${a.device_id}:${a.param_id}:${a.instance || 1}`, a]));
  const units = [];
  const ignoreOpenSensorOutliers = opts.ignoreOpenSensorOutliers !== false;
  const series = ((tile && tile.series) || []).map((raw) => {
    const source = raw.source || {};
    const key = seriesSourceKey(raw);
    const assignment = assignmentBySource.get(key);
    const parts = key.split(":");
    const deviceId = source.device_id || assignment?.device_id || parts[0];
    const paramId = Number(source.param_id ?? assignment?.param_id ?? parts[1]);
    const instance = Number(source.instance ?? assignment?.instance ?? parts[2] ?? 1) || 1;
    const def = catalogue.find((p) => p.id === paramId) || { id: paramId, name: "#" + paramId, unit: raw.unit || "" };
    const ch = channels.find((c) => c.device_id === deviceId && c.instance === instance);
    const unit = raw.unit || def.unit || "_";
    if (units.indexOf(unit) === -1) units.push(unit);
    const history = historyFromServerSeries(raw);
    const filtered = ignoreOpenSensorOutliers ? filterSeriesHistoryForScale(history, unit) : { history, suppressedOpenSensorPoints: 0, suppressedInitialOutliers: 0 };
    const points = (filtered.history.ts || []).map((ts, idx) => ({ timestamp: new Date(ts).toISOString(), value: (filtered.history.v || [])[idx] }));
    const role = raw.role || MecomAPI.roleForParam(paramId);
    const roleMeta = seriesRoleMeta(role);
    const color = raw.color || (opts.colorByChannel ? channelColor(deviceId, instance) : MecomAPI.colorForRole(role));
    const signalPath = [def.group, def.subgroup, def.name].filter(Boolean).join(" / ");
    const channelContext = `${deviceId} · channel ${instance}${ch && ch.label ? " · " + ch.label : ""}`;
    return {
      ...raw,
      id: raw.id || (assignment && tileSeriesKey(assignment)) || key,
      series_id: raw.series_id || (assignment && tileSeriesKey(assignment)) || key,
      target_id: raw.target_id || (assignment && assignment.target_id),
      label: compactSeriesLabel(deviceId, instance, def, ch),
      full_label: `${channelContext} · param ${paramId} · ${signalPath || def.name}`,
      color,
      unit,
      history: filtered.history,
      role,
      axis_id: axisIdForUnit(unit, role),
      role_rank: raw.role_rank || roleMeta.rank,
      source: {
        ...source,
        device_id: deviceId,
        instance,
        param_id: paramId,
        signal_id: source.signal_id || def.sid || String(def.id || paramId),
        endpoint: source.endpoint || (ch && ch.endpoint),
      },
      diagnostics: {
        ...(raw.diagnostics || {}),
        suppressed_open_sensor_points: filtered.suppressedOpenSensorPoints,
        suppressed_initial_outlier_points: filtered.suppressedInitialOutliers,
      },
      points,
    };
  }).sort((a, b) => {
    if ((a.role_rank || 0) !== (b.role_rank || 0)) return (a.role_rank || 0) - (b.role_rank || 0);
    return String(a.series_id || "").localeCompare(String(b.series_id || ""));
  });
  return {
    ...tile,
    renderer: CANONICAL_TILE_RENDERER,
    title: opts.title || tile?.title || "",
    axes: axesForUnits(units.length ? units : ((tile && tile.axes) || []).map((axis) => axis.unit).filter(Boolean)),
    diagnostics: {
      ...(tile && tile.diagnostics || {}),
      renderer: CANONICAL_TILE_RENDERER,
      tile_source: (tile && tile.diagnostics && tile.diagnostics.tile_source) || "gateway-tile-endpoint",
      suppressed_open_sensor_points: series.reduce((acc, s) => acc + ((s.diagnostics && s.diagnostics.suppressed_open_sensor_points) || 0), 0),
      suppressed_initial_outlier_points: series.reduce((acc, s) => acc + ((s.diagnostics && s.diagnostics.suppressed_initial_outlier_points) || 0), 0),
    },
    series,
  };
}

export function useGraphTileFromAssignments(assignments, opts: any = {}) {
  const assignmentsKey = graphTileAssignmentsKey(assignments);
  const normalized = useMemo(() => graphTileSeriesFromAssignments(assignments), [assignmentsKey]);
  const tileId = opts.tile_id || opts.tileId || "graph-tile";
  const timeWindowMs = opts.time_window_ms || opts.timeWindowMs || 90_000;
  const level = opts.level || pickTileLevel(timeWindowMs);
  const title = opts.title || "";
  const colorByChannel = !!opts.colorByChannel;
  const ignoreOpenSensorOutliers = opts.ignoreOpenSensorOutliers !== false;
  const refreshMs = opts.refreshMs || (level === "live" ? 1500 : 5000);
  const [remoteTile, setRemoteTile] = useState(null);

  const fallback = useMemo(() => buildGraphTileFromAssignments(normalized, {
    ...opts,
    tile_id: tileId,
    title,
    timeWindowMs,
    level,
    colorByChannel,
    ignoreOpenSensorOutliers,
  }), [assignmentsKey, tileId, title, timeWindowMs, level, colorByChannel, ignoreOpenSensorOutliers]);

  useEffect(() => {
    let cancelled = false;
    let timer = null;
    async function load() {
      if (typeof MecomAPI.graphTile !== "function") {
        if (!cancelled) setRemoteTile(null);
        return;
      }
      try {
        const tile = await MecomAPI.graphTile(tileId, level, normalized);
        if (!cancelled) setRemoteTile(enrichServerGraphTile(tile, normalized, { title, colorByChannel, ignoreOpenSensorOutliers }));
      } catch (_) {
        if (!cancelled) setRemoteTile(null);
      }
    }
    load();
    timer = window.setInterval(load, refreshMs);
    return () => {
      cancelled = true;
      if (timer) window.clearInterval(timer);
    };
  }, [assignmentsKey, tileId, level, title, colorByChannel, ignoreOpenSensorOutliers, refreshMs]);

  return remoteTile || fallback;
}

function axisIdForUnit(unit, role) {
  const u = String(unit || "").trim().toLowerCase();
  if (u === "a") return "current_a";
  if (u === "v") return "voltage_v";
  if (u === "w") return "power_w";
  if (u === "%" || u === "percent") return "percent";
  if (u === "ms") return "bus_ms";
  if (u === "s" || u === "sec" || u === "seconds") return "seconds";
  if (u.includes("deg") || u === "c" || u === "degc") return "temperature_c";
  if (role === "counter") return "counter";
  return "generic_numeric";
}
