// @ts-nocheck
import { useState, useEffect } from "react";
import { MecomAPI } from "../api/mecom";
import { getTelemetry, recordTelemetry } from "../lib/telemetry";
import { CANONICAL_TILE_RENDERER, seriesRoleMeta } from "../lib/series";

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

const SEED_VERSION = 5;
export function seedAssignments() {
  const ver = parseInt(localStorage.getItem("mecomgw.assignments.version") || "0", 10);
  if (ver === SEED_VERSION && loadAssignments().length > 0) return;
  const channels = MecomAPI.channels();
  const seeds = [];
  channels.forEach((ch) => {
    if (ch.role === "temp") {
      const params = [3000, 1000, 1001];
      if (ch.hasCascade) params.push(52200);
      params.forEach((pid) => seeds.push(makeAssignment(WALLS.fleetTemp.wall_id, pid, ch.device_id, ch.instance)));
    } else {
      [1021, 1020, 1022].forEach((pid) => seeds.push(makeAssignment(WALLS.fleetSupply.wall_id, pid, ch.device_id, ch.instance)));
    }
  });
  saveAssignments(seeds);
  localStorage.setItem("mecomgw.assignments.version", String(SEED_VERSION));
}

export const CHANNEL_COLORS = ["#58a6ff", "#3fb950", "#d29922", "#a371f7", "#56d4dd", "#db61a2", "#e3b341", "#f47067"];
export function channelColor(deviceId, instance?) {
  const channels = MecomAPI.channels();
  const idx = channels.findIndex((c) => c.device_id === deviceId && c.instance === instance);
  return CHANNEL_COLORS[Math.max(0, idx) % CHANNEL_COLORS.length];
}

export function buildGraphTileFromAssignments(assignments, opts: any = {}) {
  const catalogue = MecomAPI.catalogue();
  const normalized = (assignments || []).map(normalizeAssignment).filter((a) => a.device_id && Number.isFinite(a.param_id));
  const units: string[] = [];
  const nowMs = Date.now();
  const now = new Date(nowMs).toISOString();
  const tileId = opts.tile_id || opts.tileId || "graph-tile";
  const timeWindowMs = opts.time_window_ms || opts.timeWindowMs || 90_000;
  const series = normalized.map((a) => {
    const paramId = a.options.param_id;
    const deviceId = a.options.device_id;
    const instance = a.options.instance || 1;
    const def = catalogue.find((p) => p.id === paramId) || { id: paramId, name: "#" + paramId, unit: "" };
    let history = getTelemetry(deviceId, paramId, instance);
    if ((!history.v || history.v.length === 0) && typeof MecomAPI.readValue === "function") {
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
    return {
      id: tileSeriesKey(a),
      series_id: tileSeriesKey(a),
      target_id: a.target_id,
      label: def.name + " · " + deviceId + (instance > 1 ? "/" + instance : ""),
      full_label: signalPath || def.name,
      color,
      unit,
      history,
      role: seriesRole,
      axis_id: axisIdForUnit(unit, seriesRole),
      role_rank: roleMeta.rank,
      provenance,
      source_ref: sourceRef,
      source: { device_id: deviceId, instance, param_id: paramId, signal_id: def.sid || String(def.id), endpoint: ch && ch.endpoint },
      points: (history.ts || []).map((ts, idx) => ({ timestamp: new Date(ts).toISOString(), value: (history.v || [])[idx] || 0 })),
    };
  }).sort((a, b) => {
    if ((a.role_rank || 0) !== (b.role_rank || 0)) return (a.role_rank || 0) - (b.role_rank || 0);
    return String(a.series_id || "").localeCompare(String(b.series_id || ""));
  });
  return {
    schema_version: "signalforge.graph_tile.v1",
    id: tileId, card_id: tileId, level: "live",
    t0: new Date(nowMs - timeWindowMs).toISOString(),
    t1: now,
    generated_at: now,
    renderer: CANONICAL_TILE_RENDERER,
    kind: "timeseries",
    tile_id: tileId,
    title: opts.title || "",
    time_window_ms: timeWindowMs,
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
