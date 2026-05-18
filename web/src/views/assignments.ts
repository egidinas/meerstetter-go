// @ts-nocheck
import { useState, useEffect } from "react";
import { MecomAPI } from "../api/mecom";
import { getTelemetry, recordTelemetry } from "../lib/telemetry";
import { CANONICAL_TILE_RENDERER, seriesRoleMeta, pickTileLevel } from "../lib/series";

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

const SEED_VERSION = 8;

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
  const tempChannels = channels.filter((c) => c.role === "temp");
  const supplyChannels = channels.filter((c) => c.role === "supply");
  next = assignmentsWithPriorityDefaults(next, WALLS.fleetTemp.wall_id, tempChannels);
  next = assignmentsWithPriorityDefaults(next, WALLS.fleetSupply.wall_id, supplyChannels);
  channels.forEach((ch) => {
    const wallId = wallForDevice(ch.device_id).wall_id + "-" + ch.instance;
    next = assignmentsWithPriorityDefaults(next, wallId, [ch]);
  });
  const changed = next.length !== current.length || parseInt(localStorage.getItem("mecomgw.assignments.version") || "0", 10) !== SEED_VERSION;
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
    const sourceRef = `device=${deviceId} param=${paramId} instance=${instance} endpoint=${(ch && ch.endpoint) || ""}`;
    let seriesHistory = history;
    let suppressedPointCount = 0;
    if (opts.ignoreOpenSensorOutliers && unit === "degC") {
      const keptTs = [];
      const keptV = [];
      (history.ts || []).forEach((ts, idx) => {
        const value = (history.v || [])[idx];
        if (typeof value === "number" && value < -50) {
          suppressedPointCount += 1;
          return;
        }
        keptTs.push(ts);
        keptV.push(value);
      });
      seriesHistory = { ...history, ts: keptTs, v: keptV };
    }
    let points = (seriesHistory.ts || []).map((ts, idx) => ({ timestamp: new Date(ts).toISOString(), value: (seriesHistory.v || [])[idx] || 0 }));
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
      label: def.name + " · " + deviceId + (instance > 1 ? "/" + instance : ""),
      full_label: signalPath || def.name,
      color,
      unit,
      history: seriesHistory,
      role: seriesRole,
      axis_id: axisIdForUnit(unit, seriesRole),
      role_rank: roleMeta.rank,
      provenance,
      source_ref: sourceRef,
      source: { device_id: deviceId, instance, param_id: paramId, signal_id: def.sid || String(def.id), endpoint: ch && ch.endpoint },
      diagnostics: suppressedPointCount ? { suppressed_open_sensor_points: suppressedPointCount } : undefined,
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
    tile_files: [
      { level: "live", time_window_ms: 90_000 },
      { level: "minute", time_window_ms: 6 * 60_000 },
      { level: "hour", time_window_ms: 60 * 60_000 },
    ],
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
      outlier_policy: opts.ignoreOpenSensorOutliers ? "drop_degC_below_-50" : "off",
      suppressed_open_sensor_points: series.reduce((acc, s) => acc + ((s.diagnostics && s.diagnostics.suppressed_open_sensor_points) || 0), 0),
    },
    provenance: { source: "meerstetter-go.graphwall.assignments", generated_at: now },
    series,
  };
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
