// @ts-nocheck
import { useState, useEffect, useMemo } from "react";
import {
  loadAssignments as loadSignalForgeAssignments,
  saveAssignments as saveSignalForgeAssignments,
  makeAssignment as makeSignalForgeAssignment,
  useAssignments as useSignalForgeAssignments,
} from "signalforge-web";
import { MecomAPI } from "../api/mecom";
import { CANONICAL_TILE_RENDERER, DEFAULT_TILE_LEVELS, graphSeriesIdentityKey, seriesRoleMeta, pickTileLevel } from "../lib/series";

const ASSIGNMENT_STORE = { namespace: "mecomgw" };

export function loadAssignments() {
  return loadSignalForgeAssignments(ASSIGNMENT_STORE).map(normalizeAssignment).filter((a) => a.device_id && Number.isFinite(a.param_id));
}
export function saveAssignments(list) {
  const normalized = (Array.isArray(list) ? list : [])
    .map(normalizeAssignment)
    .filter((a) => a.device_id && Number.isFinite(a.param_id));
  saveSignalForgeAssignments(normalized, ASSIGNMENT_STORE);
}

export const WALLS = {
  fleetTemp:   { wall_id: "fleet-temp",   label: "Fleet hero · Temperature" },
  fleetSupply: { wall_id: "fleet-supply", label: "Fleet hero · Power supplies" },
};
export const DEFAULT_GRAPH_TILE_LEVEL = "session";
export const SESSION_GRAPH_TILE_LEVEL_SPEC = { level: "session", label: "full session", timeWindowMs: 3 * 24 * 60 * 60_000 };
export const GRAPH_TILE_LEVELS = [
  SESSION_GRAPH_TILE_LEVEL_SPEC,
  ...DEFAULT_TILE_LEVELS.filter((option) => option.level !== "session"),
];
export function graphTileWindowForLevel(level) {
  return GRAPH_TILE_LEVELS.find((option) => option.level === level)
    || GRAPH_TILE_LEVELS.find((option) => option.level === DEFAULT_GRAPH_TILE_LEVEL)
    || GRAPH_TILE_LEVELS[0]
    || SESSION_GRAPH_TILE_LEVEL_SPEC;
}
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
  return normalizeAssignment(makeSignalForgeAssignment(wallId, paramId, deviceId, instance || 1));
}

export function tileSeriesKey(a) {
  const n = normalizeAssignment(a);
  return `${n.device_id}:${n.param_id}:${n.instance || 1}`;
}

export function useAssignments() {
  const handle = useSignalForgeAssignments(ASSIGNMENT_STORE);
  return {
    ...handle,
    list: handle.list.map(normalizeAssignment),
    forWall: (wallId) => handle.forWall(wallId).map(normalizeAssignment),
  };
}

const SEED_VERSION = 10;

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

export function configDrivenGraphTileAssignments(_wallId, _channels) {
  // Empty series list asks the gateway for the config-owned tile defaults.
  return [];
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
  let next = current.filter((item) => {
    const a = normalizeAssignment(item);
    const fleetWall = a.wall_id === WALLS.fleetTemp.wall_id || a.wall_id === WALLS.fleetSupply.wall_id;
    return !fleetWall;
  });
  const seedVersion = parseInt(localStorage.getItem("mecomgw.assignments.version") || "0", 10);
  channels.forEach((ch) => {
    const wallId = wallForDevice(ch.device_id).wall_id + "-" + ch.instance;
    next = assignmentsWithPriorityDefaults(next, wallId, [ch]);
  });
  const changed = next.length !== current.length || seedVersion !== SEED_VERSION;
  if (changed) saveAssignments(next);
  localStorage.setItem("mecomgw.assignments.version", String(SEED_VERSION));
}

export const CHANNEL_COLORS = [
  "#58a6ff", "#3fb950", "#d29922", "#a371f7",
  "#56d4dd", "#db61a2", "#e3b341", "#f47067",
  "#2dd4bf", "#f97316", "#84cc16", "#e879f9",
  "#22c55e", "#fb7185", "#38bdf8", "#facc15",
];
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
  const id = Number(def && def.id);
  if (id === 1000) return "OT";
  if (id === 1001) return "ST";
  if (id === 3000) return "NOT";
  if (id === 1022) return "OP";
  if (id === 1021) return "OV";
  if (id === 1020) return "OC";
  const name = String((def && def.name) || "").trim();
  const key = name.toLowerCase().replace(/[_/-]+/g, " ").replace(/\s+/g, " ");
  if (PARAM_SHORTHANDS[key]) return PARAM_SHORTHANDS[key];
  if (key.includes("object temp") && !key.includes("target") && !key.includes("nominal")) return "OT";
  if (key.includes("sink temp")) return "ST";
  if (key.includes("target") && key.includes("temp")) return "NOT";
  if (key.includes("nominal") && key.includes("temp")) return "NOT";
  if (key.includes("output power")) return "OP";
  if (key.includes("output voltage")) return "OV";
  if (key.includes("output current")) return "OC";
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

function isDegreeCUnit(unit) {
  const u = String(unit || "").toLowerCase();
  return u === "degc" || u === "c" || u.includes("deg") || u.includes("°") || u.includes("celsius");
}

function fallbackSeriesVisibility(history, filteredHistory, unit) {
  const rawValues = (history?.v || []).map(Number).filter((value) => Number.isFinite(value));
  const visibleValues = (filteredHistory?.v || []).map(Number).filter((value) => Number.isFinite(value));
  if (visibleValues.length > 0) return { quality: "ok", default_visible: true, visibility_reason: "" };
  if (isDegreeCUnit(unit) && rawValues.some((value) => value < -50)) {
    return {
      quality: "detached",
      default_visible: false,
      visibility_reason: "hidden by default because the measured temperature is below the detached-sensor floor",
    };
  }
  return {
    quality: "missing",
    default_visible: false,
    visibility_reason: "hidden by default because no live value or history is available",
  };
}

function graphTileRequestOptions(opts: any = {}) {
  const explicitLevel = opts.level || opts.tileLevel;
  const requestedLevel = explicitLevel || DEFAULT_GRAPH_TILE_LEVEL;
  const requestedWindow = graphTileWindowForLevel(requestedLevel);
  const explicitWindow = opts.time_window_ms || opts.timeWindowMs;
  const timeWindowMs = explicitWindow || requestedWindow.timeWindowMs;
  const level = explicitLevel || (explicitWindow ? (pickTileLevel(timeWindowMs) || requestedLevel) : requestedLevel);
  return { level, timeWindowMs };
}

export function buildGraphTileFromAssignments(assignments, opts: any = {}) {
  const catalogue = MecomAPI.catalogue();
  const normalized = (assignments || []).map(normalizeAssignment).filter((a) => a.device_id && Number.isFinite(a.param_id));
  const units: string[] = [];
  const nowMs = Date.now();
  const now = new Date(nowMs).toISOString();
  const tileId = opts.tile_id || opts.tileId || "graph-tile";
  const { level, timeWindowMs } = graphTileRequestOptions(opts);
  const series = normalized.map((a) => {
    const paramId = a.options.param_id;
    const deviceId = a.options.device_id;
    const instance = a.options.instance || 1;
    const def = catalogue.find((p) => p.id === paramId) || { id: paramId, name: "#" + paramId, unit: "" };
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
    return {
      id: tileSeriesKey(a),
      series_id: tileSeriesKey(a),
      target_id: a.target_id,
      label: compactSeriesLabel(deviceId, instance, def, ch),
      full_label: `${channelContext} · param ${paramId} · ${signalPath || def.name}`,
      color,
      unit,
      history: { ts: [], v: [] },
      role: seriesRole,
      axis_id: axisIdForUnit(unit, seriesRole),
      role_rank: roleMeta.rank,
      quality: "missing",
      default_visible: false,
      visibility_reason: "waiting for gateway archive tile; frontend does not synthesize graph history",
      provenance,
      source_ref: sourceRef,
      source: { device_id: deviceId, instance, param_id: paramId, signal_id: def.sid || String(def.id), endpoint: ch && ch.endpoint },
      diagnostics: { status: "missing", message: "gateway tile unavailable" },
      points: [],
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
    tile_files: GRAPH_TILE_LEVELS.map((item) => ({ level: item.level, time_window_ms: item.timeWindowMs })),
    axes: units.slice(0, 2).map((unit, idx) => ({ unit, side: idx === 0 ? "left" : "right" })),
    bands: [],
    markers: [],
    events: [],
    diagnostics: {
      status: "unavailable",
      series_count: series.length,
      point_count: 0,
      decimation: "gateway-owned",
      renderer: CANONICAL_TILE_RENDERER,
      tile_level: level,
      tile_source: "gateway-tile-unavailable",
      message: "frontend placeholder only; graph history must come from /api/graph/tiles",
      outlier_policy: "gateway-owned",
      suppressed_open_sensor_points: 0,
      suppressed_initial_outlier_points: 0,
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
  return { ...history, ts, v };
}

function seriesSourceKey(series) {
  return graphSeriesIdentityKey(series);
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
    const exactSeriesKey = `${deviceId}:${paramId}:${instance}`;
    const unit = raw.unit || def.unit || "_";
    if (units.indexOf(unit) === -1) units.push(unit);
    const history = historyFromServerSeries(raw);
    const filtered = ignoreOpenSensorOutliers ? filterSeriesHistoryForScale(history, unit) : { history, suppressedOpenSensorPoints: 0, suppressedInitialOutliers: 0 };
    const points = (filtered.history.ts || []).map((ts, idx) => ({ timestamp: new Date(ts).toISOString(), value: (filtered.history.v || [])[idx] }));
    const role = raw.role || MecomAPI.roleForParam(paramId);
    const roleMeta = seriesRoleMeta(role);
    const color = opts.colorByChannel ? channelColor(deviceId, instance) : (raw.color || MecomAPI.colorForRole(role));
    const signalPath = [def.group, def.subgroup, def.name].filter(Boolean).join(" / ");
    const channelContext = `${deviceId} · channel ${instance}${ch && ch.label ? " · " + ch.label : ""}`;
    return {
      ...raw,
      id: exactSeriesKey,
      series_id: exactSeriesKey,
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
  const { level, timeWindowMs } = graphTileRequestOptions(opts);
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
