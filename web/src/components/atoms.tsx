// @ts-nocheck
import React, { useState, useEffect, useRef, useMemo, useCallback, createContext, useContext } from "react";
import { UPlotTileRenderer } from "signalforge-web";
import "uplot/dist/uPlot.min.css";
import { MecomAPI } from "../api/mecom";
import { recordTelemetry, getTelemetry } from "../lib/telemetry";
import {
  seriesRoleMeta, renderSeriesFromGraphTile, emptyGraphTile,
  normalizeGraphTile, CANONICAL_TILE_RENDERER, SharedTimeAxis, graphSeriesIdentityKey,
  Sparkline, WriteLifecycleTrace as SharedWriteLifecycleTrace,
} from "../lib/series";

export { seriesRoleMeta, renderSeriesFromGraphTile, emptyGraphTile, normalizeGraphTile };

function finiteNumber(value) {
  const n = Number(value);
  return Number.isFinite(n) ? n : null;
}

function valueAgeMs(sample) {
  if (!sample) return null;
  const direct = finiteNumber(sample.age_ms);
  if (direct !== null) return Math.max(0, direct);
  const at = finiteNumber(sample.at);
  if (at !== null) return Math.max(0, Date.now() - at);
  return null;
}

export function formatValueAge(ageMs, quality) {
  const q = String(quality || "").toLowerCase();
  if (ageMs === null || ageMs === undefined || !Number.isFinite(Number(ageMs))) {
    return q === "missing" ? "unread" : "age unknown";
  }
  const ms = Math.max(0, Number(ageMs));
  if (ms < 1_000) return "now";
  if (ms < 60_000) return `${Math.round(ms / 1_000)}s`;
  if (ms < 3_600_000) return `${Math.round(ms / 60_000)}m`;
  return `${Math.round(ms / 3_600_000)}h`;
}

export function valueAgeKind(ageMs, quality) {
  const q = String(quality || "").toLowerCase();
  if (q === "missing" || q === "unreachable" || q === "nan") return q;
  if (ageMs === null || ageMs === undefined || !Number.isFinite(Number(ageMs))) return "unknown";
  const ms = Math.max(0, Number(ageMs));
  if (ms > 120_000) return "stale";
  if (ms > 30_000) return "old";
  return "fresh";
}

/* ---------- Hook: latest value for a (device, param, instance) ---------- */
export function useLiveValue(deviceId, paramId, instance?, opts?) {
  const inst = instance ?? 1;
  const enabled = opts?.enabled !== false && Boolean(deviceId) && paramId !== undefined && paramId !== null;
  const disabledQuality = opts?.disabledQuality || "catalogue";
  const [, force] = useState(0);
  const [latest, setLatest] = useState(null);
  useEffect(() => {
    if (!enabled) {
      setLatest(null);
      force((n) => (n + 1) % 1e9);
      return undefined;
    }
    const tick = () => {
      const r = MecomAPI.readValue(deviceId, paramId, inst);
      recordTelemetry(deviceId, paramId, r.value, r.quality, inst);
      setLatest(r);
      force((n) => (n + 1) % 1e9);
    };
    tick();
    const unsub = MecomAPI.subscribe(tick);
    return unsub;
  }, [deviceId, paramId, inst, enabled]);
  if (!enabled) {
    return { value: null, quality: disabledQuality, ageMs: null, at: null, history: { t: [], v: [], q: [] } };
  }
  const buf = getTelemetry(deviceId, paramId, inst);
  const v = buf.v.length ? buf.v[buf.v.length - 1] : null;
  const q = buf.q.length ? buf.q[buf.q.length - 1] : "missing";
  return { value: v, quality: q, ageMs: valueAgeMs(latest), at: latest?.at ?? null, history: buf };
}

export function useSparkline(deviceId, paramId, instance?, opts?) {
  const inst = instance ?? 1;
  const enabled = opts?.enabled !== false && Boolean(deviceId) && paramId !== undefined && paramId !== null;
  const [, force] = useState(0);
  useEffect(() => {
    if (!enabled) return undefined;
    const tick = () => force((n) => (n + 1) % 1e9);
    tick();
    const unsub = MecomAPI.subscribe(tick);
    const timer = setInterval(tick, 5000);
    return () => {
      unsub();
      clearInterval(timer);
    };
  }, [deviceId, paramId, inst, enabled]);
  if (!enabled) return [];
  const buf = getTelemetry(deviceId, paramId, inst);
  return buf.v.filter((v) => typeof v === "number" && Number.isFinite(v)).slice(-30);
}

/* ---------- Hook: re-render on any state change ---------- */
export function useGatewayTick() {
  const [, force] = useState(0);
  useEffect(() => MecomAPI.subscribe(() => force((n) => (n + 1) % 1e9)), []);
}

/* ============================================================ Visual atoms ============================================================ */
export function Pill({ kind = "info", children, icon }) {
  return (
    <span className={"pill " + kind}>
      <span className="dot"></span>
      {icon}{children}
    </span>
  );
}

export function Chip({ kind = "", children, title }) {
  return <span className={"chip " + kind} title={title}>{children}</span>;
}

export function Panel({ title, meta, right, children, flush, style, className }) {
  return (
    <section className={"panel " + (className || "")} style={style}>
      {title !== undefined && (
        <div className="panel-head">
          <h3>{title}</h3>
          {meta && <span className="meta">{meta}</span>}
          {right && <div style={{ marginLeft: "auto", display: "flex", gap: 6 }}>{right}</div>}
        </div>
      )}
      <div className={"panel-body" + (flush ? " flush" : "")}>{children}</div>
    </section>
  );
}

/* ============================================================ Canonical SignalForge/uPlot charts ============================================================ */


function validTimeMs(value) {
  const t = typeof value === "number" ? value : Date.parse(String(value || ""));
  return Number.isFinite(t) ? t : null;
}

function graphTileTimeRange(tile, timeWindowMs) {
  const now = Date.now();
  const fallbackEnd = now;
  const fallbackStart = now - Math.max(1_000, timeWindowMs || tile?.time_window_ms || 90_000);
  const t0 = validTimeMs(tile?.t0);
  const t1 = validTimeMs(tile?.t1);
  if (t0 !== null && t1 !== null && t1 > t0) return { start: t0, end: t1 };

  const times = [];
  (tile?.series || []).forEach((series) => {
    (series?.points || []).forEach((point) => {
      const t = validTimeMs(point?.timestamp ?? point?.t ?? point?.time);
      if (t !== null) times.push(t);
    });
    (series?.history?.ts || []).forEach((ts) => {
      const t = validTimeMs(ts);
      if (t !== null) times.push(t);
    });
  });
  if (times.length) {
    const start = Math.min(...times);
    const end = Math.max(...times);
    return { start, end: Math.max(end, start + 1) };
  }
  return { start: fallbackStart, end: fallbackEnd };
}

function graphTileWithinTimeRange(tile, range, timeWindowMs) {
  if (!tile || !Array.isArray(tile.series)) return tile;
  const start = range?.start;
  const end = range?.end;
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return tile;
  const series = tile.series.map((item) => {
    const points = (item.points || []).filter((point) => {
      const t = validTimeMs(point?.timestamp ?? point?.t ?? point?.time);
      return t !== null && t >= start && t <= end;
    });
    const history = item.history && Array.isArray(item.history.ts) && Array.isArray(item.history.v)
      ? item.history.ts.reduce((acc, ts, idx) => {
          const t = validTimeMs(ts);
          if (t !== null && t >= start && t <= end && Number.isFinite(Number(item.history.v[idx]))) {
            acc.ts.push(new Date(t).toISOString());
            acc.v.push(Number(item.history.v[idx]));
          }
          return acc;
        }, { ts: [], v: [] })
      : item.history;
    return { ...item, points, history };
  });
  return normalizeGraphTile({
    ...tile,
    t0: new Date(start).toISOString(),
    t1: new Date(end).toISOString(),
    series,
  }, { timeWindowMs });
}

function graphSeriesSamples(series) {
  const samples = [];
  if (series?.history && Array.isArray(series.history.ts) && Array.isArray(series.history.v)) {
    series.history.ts.forEach((ts, idx) => {
      const t = validTimeMs(ts);
      const value = Number(series.history.v[idx]);
      if (t !== null && Number.isFinite(value)) samples.push({ t, value });
    });
  }
  (series?.points || []).forEach((point) => {
    const t = validTimeMs(point?.timestamp ?? point?.t ?? point?.time);
    const value = Number(point?.value ?? point?.v);
    if (t !== null && Number.isFinite(value)) samples.push({ t, value });
  });
  samples.sort((a, b) => a.t - b.t);
  return samples;
}

function nearestSeriesReadout(series, timeMs) {
  const samples = graphSeriesSamples(series);
  if (!samples.length || !Number.isFinite(timeMs)) return null;
  let best = samples[0];
  let bestDistance = Math.abs(best.t - timeMs);
  for (let i = 1; i < samples.length; i += 1) {
    const distance = Math.abs(samples[i].t - timeMs);
    if (distance < bestDistance) {
      best = samples[i];
      bestDistance = distance;
    }
  }
  return {
    label: series.label || series.full_label || series.series_id || series.id || "trace",
    color: series.color || "var(--accent)",
    unit: series.unit || "",
    at: best.t,
    value: best.value,
  };
}

function formatTraceReadoutValue(value, unit) {
  const n = Number(value);
  if (!Number.isFinite(n)) return "n/a";
  const abs = Math.abs(n);
  const formatted = (abs !== 0 && (abs >= 10_000 || abs < 0.001))
    ? n.toExponential(3)
    : n.toLocaleString(undefined, { maximumFractionDigits: 3 });
  return unit ? `${formatted} ${unit}` : formatted;
}

function isFullTimeRange(range, fullRange) {
  if (!range || !fullRange) return false;
  return Math.abs(range.start - fullRange.start) <= 2 && Math.abs(range.end - fullRange.end) <= 2;
}

export function MultiChart({
  tile,
  height = 320,
  timeWindowMs = 90_000,
  hiddenSeries = [],
  fill = false,
  minHeight = 220,
  titleOverride = null,
  subtitle = null,
  headerExtras = null,
}) {
  useGatewayTick();
  const graphTile = useMemo(() => normalizeGraphTile(tile || emptyGraphTile({ timeWindowMs }), { timeWindowMs }), [tile, timeWindowMs]);
  const hidden = useMemo(() => new Set(hiddenSeries || []), [hiddenSeries.join ? hiddenSeries.join("|") : String(hiddenSeries)]);
  const [autoY, setAutoY] = useState(true);
  const [yMin, setYMin] = useState("");
  const [yMax, setYMax] = useState("");
  const [viewToken, setViewToken] = useState(0);
  const [manualX, setManualX] = useState(false);
  const [hoverTimeMs, setHoverTimeMs] = useState(null);
  const [hoverRatio, setHoverRatio] = useState(null);
  const visibleTile = useMemo(() => {
    if (!hidden.size) return graphTile;
    return normalizeGraphTile({
      ...graphTile,
      series: (graphTile.series || []).filter((series) => {
        const key = graphSeriesIdentityKey(series);
        return !hidden.has(key);
      }),
      diagnostics: {
        ...(graphTile.diagnostics || {}),
        hidden_series_count: hidden.size,
        visible_series_count: (graphTile.series || []).filter((series) => {
          const key = graphSeriesIdentityKey(series);
          return !hidden.has(key);
        }).length,
      },
    }, { timeWindowMs });
  }, [graphTile, hidden, timeWindowMs]);
  const fullTimeRange = useMemo(() => graphTileTimeRange(visibleTile, timeWindowMs), [visibleTile, timeWindowMs]);
  const fullTimeRangeKey = `${fullTimeRange.start}:${fullTimeRange.end}`;
  const [timeRange, setTimeRange] = useState(fullTimeRange);
  useEffect(() => {
    if (!manualX) setTimeRange(fullTimeRange);
  }, [fullTimeRangeKey, manualX]);
  const rangedTile = useMemo(() => graphTileWithinTimeRange(visibleTile, timeRange, timeWindowMs), [visibleTile, timeRange?.start, timeRange?.end, timeWindowMs]);
  const orderedSeries = useMemo(() => renderSeriesFromGraphTile(rangedTile), [rangedTile]);
  const title = graphTile.title || graphTile.tile_id || graphTile.id || "graph tile";
  const currentTimeMs = validTimeMs(rangedTile?.t1) ?? fullTimeRange.end;
  const visibleSignals = useMemo(() => (rangedTile?.series || []).filter((series) => {
    const key = graphSeriesIdentityKey(series);
    return !hidden.has(key);
  }), [rangedTile, hidden]);
  const hoverRows = useMemo(() => {
    if (hoverTimeMs === null) return [];
    return visibleSignals.map((series) => nearestSeriesReadout(series, hoverTimeMs)).filter(Boolean);
  }, [visibleSignals, hoverTimeMs]);
  const parsedYRange = useMemo(() => {
    const min = Number(yMin);
    const max = Number(yMax);
    if (!Number.isFinite(min) || !Number.isFinite(max)) return null;
    if (max <= min) return null;
    return [min, max];
  }, [yMin, yMax]);
  const displayTitle = titleOverride || title;
  const applyTimeRange = useCallback((range) => {
    const full = isFullTimeRange(range, fullTimeRange);
    setManualX(!full);
    setTimeRange(full ? fullTimeRange : range);
    if (full) setViewToken((n) => n + 1);
  }, [fullTimeRange.start, fullTimeRange.end]);

  return (
    <div
      style={{
        width: "100%",
        minWidth: 0,
        ...(fill ? { height: "100%", display: "grid", gridTemplateRows: "auto minmax(0, 1fr)" } : {}),
      }}
      data-graph-tile={graphTile.tile_id || graphTile.id || ""}
      data-graph-renderer={CANONICAL_TILE_RENDERER}
      data-series-count={orderedSeries.length}
      data-hidden-series-count={hidden.size}
      title={displayTitle}
    >
      <div className="chart-setup-bar">
        <div className="chart-setup-row">
          <div className="chart-title-block">
            <span className="chart-title">{displayTitle}</span>
            {subtitle && <span className="chart-subtitle">{subtitle}</span>}
          </div>
          {headerExtras && <div className="chart-header-extras">{headerExtras}</div>}
          <div className="chart-controls">
            <label className="toggle-sm">
              <input type="checkbox" checked={autoY} onChange={(e) => setAutoY(e.target.checked)} />
              Auto Y
            </label>
            <input className="select-sm axis-bound" type="number" step="any" placeholder="Y min" value={yMin} onChange={(e) => setYMin(e.target.value)} disabled={autoY} />
            <input className="select-sm axis-bound" type="number" step="any" placeholder="Y max" value={yMax} onChange={(e) => setYMax(e.target.value)} disabled={autoY} />
            <button className="btn sm" onClick={() => { setYMin(""); setYMax(""); setAutoY(true); }} title="Return to automatic y-axis scaling">Reset Y</button>
          </div>
        </div>
        <SharedTimeAxis
          fullRange={fullTimeRange}
          timeRange={timeRange}
          currentTimeMs={currentTimeMs}
          hoverTimeMs={hoverTimeMs}
          onTimeRange={applyTimeRange}
          tickCount={16}
        />
      </div>
      <div
        className="chart-plot"
        onMouseMove={(event) => {
          const rect = event.currentTarget.getBoundingClientRect();
          const ratio = Math.min(1, Math.max(0, (event.clientX - rect.left) / Math.max(1, rect.width)));
          setHoverRatio(ratio);
          setHoverTimeMs(timeRange.start + ratio * (timeRange.end - timeRange.start));
        }}
        onMouseLeave={() => {
          setHoverRatio(null);
          setHoverTimeMs(null);
        }}
      >
        <UPlotTileRenderer
          tile={rangedTile}
          height={height}
          dataGraphRenderer={CANONICAL_TILE_RENDERER}
          syncKey="meerstetter-go-wall"
          autoY={autoY}
          yRange={parsedYRange}
          viewToken={viewToken}
          fillContainer={fill}
          minHeight={minHeight}
        />
        {hoverTimeMs !== null && hoverRows.length > 0 && (
          <div className="trace-readout-popup" style={{ left: `${Math.min(86, Math.max(12, (hoverRatio ?? 0) * 100))}%` }}>
            <div className="trace-readout-time">{new Date(hoverTimeMs).toLocaleString()}</div>
            {hoverRows.slice(0, 12).map((row) => (
              <div className="trace-readout-row" key={`${row.label}:${row.at}`}>
                <span className="trace-readout-swatch" style={{ background: row.color }}></span>
                <span className="trace-readout-label">{row.label}</span>
                <b>{formatTraceReadoutValue(row.value, row.unit)}</b>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

/* ============================================================ Toasts ============================================================ */
const ToastCtx = createContext(null);
export function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([]);
  const push = useCallback((toast) => {
    const id = Math.random().toString(36).slice(2, 9);
    const t = Object.assign({ id, kind: "info", ttl: 5500 }, toast);
    setToasts((cur) => [...cur, t]);
    if (t.ttl) setTimeout(() => setToasts((cur) => cur.filter((x) => x.id !== id)), t.ttl);
    return id;
  }, []);
  const dismiss = useCallback((id) => setToasts((cur) => cur.filter((t) => t.id !== id)), []);
  return (
    <ToastCtx.Provider value={{ push, dismiss }}>
      {children}
      <div className="toasts">
        {toasts.map((t) => (
          <div key={t.id} className={"toast " + t.kind}>
            <div>
              <div className="title">{t.title}</div>
              {t.body && <div className="body">{t.body}</div>}
            </div>
            <button className="x" onClick={() => dismiss(t.id)}>✕</button>
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  );
}
export function useToast() { return useContext(ToastCtx); }

export function categorizeError(err) {
  const s = err && err.status;
  if (s === 423) return { kind: "warn",  cat: "lease conflict" };
  if (s === 503) return { kind: "bad",   cat: "device unreachable" };
  if (s === 504) return { kind: "bad",   cat: "timeout" };
  if (s === 403) return { kind: "warn",  cat: "read-only" };
  if (s === 409) return { kind: "bad",   cat: "device rejected" };
  if (s === 501) return { kind: "warn",  cat: "not supported" };
  return { kind: "bad", cat: "error" };
}

function displayUnit(unit) {
  const label = MecomAPI.unitLabel ? MecomAPI.unitLabel(unit) : unit;
  if (!label) return "";
  if (label === "degC") return "°C";
  return label;
}

function displayUnitTag(unit) {
  if (!unit) return "unitless";
  if (unit === "degC") return "Degree Celsius";
  return MecomAPI.unitLabel ? MecomAPI.unitLabel(unit) : unit;
}

function formatWithUnit(value, unit, paramId) {
  if (MecomAPI.formatWithUnit) return MecomAPI.formatWithUnit(value, unit, paramId);
  const label = displayUnit(unit);
  return label ? `${value} ${label}` : `${value}`;
}

function semanticPairSummary(param) {
  if (!param || !MecomAPI.semanticPairSummary) return "";
  return MecomAPI.semanticPairSummary(param);
}

/* ============================================================ DiscoveryTree ============================================================ */
export function CategoryFor(name) {
  const l = (name || "").toLowerCase();
  if (l.includes("temperature") || l.includes("temp")) return "Temperature";
  if (l.includes("power") || l.includes("current") || l.includes("voltage")) return "Power and Output";
  if (l.includes("target") || l.includes("limit") || l.includes("pid") || l.includes("control") || l.includes("mode") || l.includes("enable") || l.includes("ramp")) return "Control";
  if (l.includes("status") || l.includes("state") || l.includes("error") || l.includes("warning") || l.includes("event") || l.includes("alarm") || l.includes("stable")) return "Status and Events";
  if (l.includes("firmware") || l.includes("hardware") || l.includes("serial") || l.includes("device") || l.includes("version") || l.includes("flash") || l.includes("save")) return "Device Metadata";
  return "Other Signals";
}

function treePathList(param) {
  const single = param && param.tree_path ? [param.tree_path] : [];
  const many = Array.isArray(param && param.tree_paths) ? param.tree_paths : [];
  const paths = [...single, ...many].filter(Boolean).map(normalizeTreeItem).filter(Boolean);
  const seen = new Set();
  return paths.filter((item: any) => {
    const key = [item && item.id, treePathText(item && item.path), item && item.label].map((v) => String(v || "")).join("|");
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

function namedTreePathList(param) {
  const many = Array.isArray(param && param.tree_paths) ? param.tree_paths : [];
  return many.filter(Boolean).map(normalizeTreeItem).filter(Boolean);
}

function treeProjectionOptions(catalogue) {
  const byId = new Map();
  (catalogue || []).forEach((param) => {
    namedTreePathList(param).forEach((item) => {
      const id = String(item.id || "").trim();
      if (!id) return;
      const existing = byId.get(id);
      if (existing) {
        existing.count += 1;
        existing.default = existing.default || item.default;
        existing.default_collapsed = existing.default_collapsed && item.default_collapsed;
        existing.sort = Math.min(existing.sort, Number(item.sort) || 0);
      } else {
        byId.set(id, {
          id,
          label: item.label || id,
          count: 1,
          default: Boolean(item.default),
          default_collapsed: Boolean(item.default_collapsed),
          sort: Number(item.sort) || 0,
        });
      }
    });
  });
  return Array.from(byId.values()).sort((a, b) => {
    if (a.default !== b.default) return a.default ? -1 : 1;
    return (a.sort || 0) - (b.sort || 0) || a.label.localeCompare(b.label);
  });
}

function normalizeTreeItem(item) {
  if (!item) return null;
  if (Array.isArray(item) || typeof item === "string") {
    const path = treeSegments(item);
    if (!path.length) return null;
    const text = treePathText(path);
    return {
      id: text.replace(/\s+/g, "_"),
      label: path[path.length - 1] || text,
      path,
      default: true,
      bundle: "",
      default_collapsed: false,
      secondary: false,
      reason: "",
      duplicate_reason: "",
      instance_scope: "",
      sort: 0,
    };
  }
  if (typeof item !== "object") return null;
  let path = treeSegments(item.path);
  if (!path.length) path = treeSegments(item.label);
  if (!path.length) return null;
  const text = treePathText(path);
  const sort = Number(item.sort);
  return {
    id: String(item.id || text).trim(),
    label: String(item.label || path[path.length - 1] || text).trim(),
    path,
    default: Boolean(item.default),
    bundle: String(item.bundle || "").trim(),
    default_collapsed: Boolean(item.default_collapsed),
    secondary: Boolean(item.secondary),
    reason: String(item.reason || item.duplicate_reason || "").trim(),
    duplicate_reason: String(item.duplicate_reason || item.reason || "").trim(),
    instance_scope: String(item.instance_scope || item.instanceScope || "").trim(),
    sort: Number.isFinite(sort) ? sort : 0,
    column_order: item.column_order || item.columnOrder || null,
  };
}

function selectedTreePath(param, projectionId) {
  const paths = treePathList(param);
  if (!paths.length) return null;
  if (projectionId) {
    const match = paths.find((item) => item.id === projectionId);
    if (match) return match;
  }
  return paths.find((item) => item.default) || paths[0];
}

function treeSegments(path) {
  if (Array.isArray(path)) return path.map((part) => String(part || "").trim()).filter(Boolean);
  return String(path || "").split(/\s*(?:\/|>)\s*/).map((part) => part.trim()).filter(Boolean);
}

function treePathText(path) {
  return treeSegments(path).join(" / ");
}

function treeContext(param, projectionId) {
  const allTreeText = treePathList(param).map((item) => [
    item.id,
    item.label,
    treePathText(item.path),
    item.bundle,
    item.reason,
    item.duplicate_reason,
  ].filter(Boolean).join(" ")).join(" ");
  const helpText = [
    param && param.hover_help,
    param && param.hoverHelp,
    param && param.help_text,
    param && param.helpText,
    param && param.source_parameter_name,
    param && param.sourceParameterName,
    param && param.readout_priority,
    param && param.readoutPriority,
    param && param.preferred_readout,
    param && param.preferredReadout,
  ].filter(Boolean).join(" ");
  const selected = selectedTreePath(param, projectionId);
  if (!selected) {
    const group = param && param.group ? param.group : CategoryFor(param && param.name);
    const subgroup = param && param.subgroup ? param.subgroup : "Signals";
    return {
      hasTree: false,
      group,
      subgroup,
      title: [group, subgroup, param && param.name].filter(Boolean).join(" / "),
      visibleLabel: param && param.name,
      searchText: [param && param.name, param && param.id, param && param.unit, group, subgroup, allTreeText, helpText].map((v) => String(v || "").toLowerCase()).join(" "),
    };
  }
  const segs = treeSegments(selected.path);
  const pathText = treePathText(selected.path);
  const group = segs[0] || selected.label || "Tree";
  const subgroup = segs.length > 2 ? segs.slice(1, -1).join(" / ") : (segs[1] || selected.label || group);
  const leaf = segs[segs.length - 1] || selected.label || param && param.name;
  return {
    hasTree: true,
    group,
    subgroup,
    title: [pathText, param && param.name].filter(Boolean).join(" / "),
    visibleLabel: leaf,
    searchText: [param && param.name, param && param.id, param && param.unit, selected.label, pathText, selected.id, selected.bundle, selected.reason, allTreeText, helpText].map((v) => String(v || "").toLowerCase()).join(" "),
    path: pathText,
    label: selected.label,
    default_collapsed: Boolean(selected.default_collapsed),
    sort: Number(selected.sort) || 0,
    bundle: selected.bundle || "",
    duplicate_reason: selected.duplicate_reason || selected.reason || "",
  };
}

export function DiscoveryTree({
  deviceId,
  instance,
  channels = [],
  catalogue,
  pins,
  onTogglePin,
  onWrite,
  onPinCard,
  onCloseWrite,
  writeCards = [],
  leaseHolder,
  holderId,
  onlyWritable,
  query,
  setQuery,
  filterCat,
  setFilterCat,
}) {
  useGatewayTick();
  const projectionOptions = useMemo(() => treeProjectionOptions(catalogue), [catalogue]);
  const [treeProjection, setTreeProjection] = useState("");
  useEffect(() => {
    if (treeProjection && !projectionOptions.some((option) => option.id === treeProjection)) {
      setTreeProjection("");
    }
  }, [treeProjection, projectionOptions]);
  const contexts = useMemo(() => catalogue.map((p) => ({ param: p, ctx: treeContext(p, treeProjection) })), [catalogue, treeProjection]);
  const filtered = useMemo(() => {
    const q = (query || "").toLowerCase().trim();
    return contexts.filter(({ param, ctx }) => {
      if (onlyWritable && !param.writable) return false;
      if (filterCat && ctx.group !== filterCat) return false;
      if (!q) return true;
      return ctx.searchText.includes(q);
    });
  }, [contexts, onlyWritable, filterCat, query]);
  const groups = useMemo(() => {
    const g = {};
    filtered.forEach(({ param: p, ctx }) => {
      const group = ctx.group || p.group || CategoryFor(p.name);
      const subgroup = ctx.subgroup || p.subgroup || "Signals";
      if (!g[group]) g[group] = {};
      if (!g[group][subgroup]) g[group][subgroup] = [];
      g[group][subgroup].push({ param: p, ctx });
    });
    Object.values(g).forEach((subgroups: any) => {
      Object.keys(subgroups).forEach((subgroup) => {
        subgroups[subgroup].sort((a, b) => (a.ctx.sort || 0) - (b.ctx.sort || 0) || Number(a.param.id) - Number(b.param.id) || String(a.param.name || "").localeCompare(String(b.param.name || "")));
      });
    });
    return g;
  }, [filtered]);
  const [collapsed, setCollapsed] = useState({});
  const hasActiveFilter = Boolean((query || "").trim() || filterCat || onlyWritable);
  const groupNames = Object.keys(groups).sort((a, b) => a.localeCompare(b));
  const channelFanout = (channels && channels.length ? channels : [{ device_id: deviceId, instance }]);
  return (
    <div className="tree-pane">
      <div className="tree-title">
        <span>Signal catalogue</span>
        <Chip>{filtered.length} signals</Chip>
        <Chip>{channelFanout.length} inst</Chip>
      </div>
      <div className="tree-head">
        <input className="field" placeholder="Search parameters…" value={query} onChange={(e) => setQuery(e.target.value)} />
        {projectionOptions.length > 0 && (
          <div className="tree-projections" title="Tree projections regroup the same signal catalogue without changing signal identity.">
            <button className={!treeProjection ? "on" : ""} onClick={() => setTreeProjection("")}>Default tree</button>
            {projectionOptions.map((option) => (
              <button
                key={option.id}
                className={treeProjection === option.id ? "on" : ""}
                onClick={() => setTreeProjection(option.id)}
                title={`${option.count} signals expose this tree projection`}
              >
                {option.label}
              </button>
            ))}
          </div>
        )}
        <div className="tree-filters">
          <button className={!filterCat ? "on" : ""} onClick={() => setFilterCat("")}>All</button>
          {groupNames.map((c) => (
            <button key={c} className={filterCat === c ? "on" : ""} onClick={() => setFilterCat(c)}>{c}</button>
          ))}
        </div>
      </div>
      <div className="tree-list">
        {groupNames.map((group) => {
          const sgrps = groups[group];
          const groupCount = Object.values(sgrps).reduce((acc, items) => acc + items.length, 0);
          const isCollapsed = collapsed[group] !== undefined ? collapsed[group] : !hasActiveFilter;
          return (
            <div key={group}>
              <div className="tree-group-head" onClick={() => setCollapsed((c) => {
                const current = c[group] !== undefined ? c[group] : !hasActiveFilter;
                return { ...c, [group]: !current };
              })}>
                <span>{isCollapsed ? "▸" : "▾"}</span>
                <span>{group}</span>
                <span className="count">{groupCount}</span>
              </div>
              {!isCollapsed && Object.entries(sgrps).sort((a, b) => {
                const ax = a[1][0]?.ctx?.sort || 0;
                const bx = b[1][0]?.ctx?.sort || 0;
                return ax - bx || a[0].localeCompare(b[0]);
              }).map(([subgroup, items]) => (
                <div key={group + ":" + subgroup} className="tree-subgroup">
                  <div className="tree-subgroup-head">
                    <span>{subgroup}</span>
                    <span>{items.length}</span>
                  </div>
                  {items.map(({ param: p, ctx }) => (
                    <TreeNode key={p.id + ":" + subgroup}
                              deviceId={deviceId} channels={channelFanout} param={p}
                              ctx={ctx}
                              pins={pins}
                              writeCards={writeCards}
                              treeProjection={treeProjection}
                              leaseHolder={leaseHolder}
                              holderId={holderId}
                              onTogglePin={onTogglePin}
                              onPinCard={onPinCard}
                              onCloseWrite={onCloseWrite}
                              onWrite={onWrite} />
                  ))}
                </div>
              ))}
            </div>
          );
        })}
      </div>
    </div>
  );
}

const TreeNode = React.memo(({ deviceId, channels, param, ctx: suppliedCtx, pins, writeCards, treeProjection, leaseHolder, holderId, onTogglePin, onPinCard, onWrite, onCloseWrite }) => {
  const ctx = suppliedCtx || treeContext(param, treeProjection);
  const applicableChannels = (channels || []).filter((c) => !param.applicableModes || param.applicableModes.includes(c.role));
  const activeCards = (writeCards || []).filter((c) => c.id === param.id);
  const unitLabel = displayUnitTag(param.unit) || "unitless";
  const pairSummary = semanticPairSummary(param);
  const catalogueHelp = param.help || param.hover_help || param.hoverHelp || param.help_text || param.helpText || "";
  const duplicateReason = ctx.duplicate_reason || "";
  const limits = (param.min !== undefined || param.max !== undefined) ? `[${param.min ?? "-∞"}, ${param.max ?? "+∞"}]` : "";
  return (
    <div className={["tree-node", param.writable ? "write" : "", param.dangerous ? "dangerous" : ""].join(" ")}>
      <span className="swatch"></span>
      <span className="nm" title={ctx.title}>
        {ctx.visibleLabel || param.name} <span className="id">·{param.id}</span>
      </span>
      <span className="unit">{unitLabel}</span>
      <span className="kind">{param.writable ? "Read/write" : "Read-only"}</span>
      {pairSummary && <div className="tree-help-line">{pairSummary}</div>}
      {catalogueHelp && <div className="tree-help-line" title={catalogueHelp}>{catalogueHelp}</div>}
      {param.safety_note && <div className="tree-help-line warn" title="Safety critical note">CAUTION: {param.safety_note}</div>}
      {limits && <div className="tree-help-line muted" title="Operational limits">Limits: {limits}</div>}
      {duplicateReason && <div className="tree-help-line muted" title={duplicateReason}>secondary placement: {duplicateReason}</div>}
      <div className="tree-instances">
        {applicableChannels.map((ch) => (
          <TreeInstance key={ch.device_id + ":" + ch.instance}
                        deviceId={deviceId}
                        channel={ch}
                        instance={ch.instance}
                        role={ch.role}
                        param={param}
                        pinned={pins.some((x) => x.id === param.id && (x.instance || 1) === ch.instance)}
                        onTogglePin={() => onTogglePin(param, ch.instance)}
                        onPinCard={() => onPinCard && onPinCard(param, ch.instance)}
                        onWrite={() => onWrite(param, ch.instance)} />
        ))}
        {applicableChannels.length === 0 && <span className="tree-no-inst">not applicable</span>}
      </div>
      {activeCards.length > 0 && (
        <div className="tree-write-panel">
          {activeCards.map((c) => (
            <InputCard
              key={param.id + ":" + (c.instance || 1)}
              deviceId={deviceId}
              param={{ ...param, instance: c.instance || 1 }}
              leaseHolder={leaseHolder}
              holderId={holderId}
              onClose={() => onCloseWrite && onCloseWrite(param.id, c.instance || 1)}
            />
          ))}
        </div>
      )}
    </div>
  );
});

const TreeInstance = React.memo(({ deviceId, channel, instance, role, param, pinned, onTogglePin, onPinCard, onWrite }) => {
  const resolvedDeviceId = channel?.device_id || deviceId;
  const liveEnabled = Boolean(
    MecomAPI.isPolledSignal?.(role, param.id) ||
    MecomAPI.hasLiveValue?.(resolvedDeviceId, param.id, instance)
  );
  const { value, quality, ageMs } = useLiveValue(resolvedDeviceId, param.id, instance, { enabled: liveEnabled });
  const [editingAlias, setEditingAlias] = useState(false);
  const [aliasDraft, setAliasDraft] = useState(channel?.alias || channel?.nickname || "");
  const [noteDraft, setNoteDraft] = useState(channel?.user_overlay_note || channel?.fixture_note || "");
  useEffect(() => {
    if (!editingAlias) {
      setAliasDraft(channel?.alias || channel?.nickname || "");
      setNoteDraft(channel?.user_overlay_note || channel?.fixture_note || "");
    }
  }, [channel?.alias, channel?.nickname, channel?.user_overlay_note, channel?.fixture_note, editingAlias]);
  const displayValue = formatWithUnit(value, param.unit, param.id);
  const channelLabel = role || "channel";
  const alias = channel?.alias || channel?.nickname || "";
  const rawLabel = MecomAPI.channelDisplayLabel ? MecomAPI.channelDisplayLabel(channel || { device_id: resolvedDeviceId, instance }, { includeAlias: false }) : `${resolvedDeviceId} ch${instance}`;
  const pair = MecomAPI.semanticPairFor ? MecomAPI.semanticPairFor(param) : null;
  const peer = pair && pair.telemetry && pair.telemetry.id !== param.id ? pair.telemetry : pair && pair.control && pair.control.id !== param.id ? pair.control : null;
  const peerLiveEnabled = Boolean(peer && (
    MecomAPI.isPolledSignal?.(role, peer.id) ||
    MecomAPI.hasLiveValue?.(resolvedDeviceId, peer.id, instance)
  ));
  const helpText = [
    rawLabel,
    alias ? `alias ${alias}` : "no alias",
    `role ${channelLabel}`,
    `instance ${instance}`,
    `quality ${quality}`,
    `age ${formatValueAge(ageMs, quality)}`,
    `unit ${displayUnitTag(param.unit)}`,
    channel?.semantic_overlay?.note ? `note ${channel.semantic_overlay.note}` : "",
    peer ? `paired ${peer.name || peer.label || `#${peer.id}`}` : "unpaired",
  ].filter(Boolean).join(" · ");
  const saveAlias = () => {
    MecomAPI.setChannelAlias?.(resolvedDeviceId, instance, {
      alias: aliasDraft,
      note: noteDraft,
      source: "operator-signal-tree",
      author: MecomAPI.settings?.().holder,
    });
    setEditingAlias(false);
  };
  const clearAlias = () => {
    MecomAPI.clearChannelAlias?.(resolvedDeviceId, instance);
    setAliasDraft("");
    setNoteDraft("");
    setEditingAlias(false);
  };
  return (
    <span className={["tree-inst", pinned ? "pinned" : "", editingAlias ? "alias-editing" : "", "q-" + quality].join(" ")} title={helpText}>
      <button title="Pin instance to graph" onClick={onTogglePin}>{pinned ? "★" : "☆"}</button>
      <span className="inst">{channelLabel} · i{instance}</span>
      <span className={"alias " + (alias ? "" : "empty")} title={alias ? `User alias for ${rawLabel}` : `No user alias for ${rawLabel}`}>{alias || "—"}</span>
      <span className="vl">{displayValue}</span>
      <span className={"qtag " + quality}>{quality || "missing"}</span>
      <span className={"age " + valueAgeKind(ageMs, quality)}>{formatValueAge(ageMs, quality)}</span>
      <SparklineWrapper deviceId={resolvedDeviceId} paramId={param.id} instance={instance} enabled={liveEnabled} />
      <span className={"pairline " + (pair ? "" : "empty")}>
        {pair ? (
          <PeerValue resolvedDeviceId={resolvedDeviceId} instance={instance} peer={peer} enabled={peerLiveEnabled} />
        ) : (
          <span>unpaired</span>
        )}
      </span>
      <button title="Edit channel alias overlay" onClick={() => setEditingAlias((v) => !v)}>alias</button>
      <button title="Pin value card" onClick={onPinCard}>▣</button>
      {param.writable && <button title="Open write card" onClick={onWrite}>✎</button>}
      {editingAlias && (
        <span className="tree-alias-editor">
          <input
            value={aliasDraft}
            onChange={(e) => setAliasDraft(e.target.value)}
            placeholder={`${rawLabel} alias`}
            aria-label={`${rawLabel} alias`}
          />
          <input
            value={noteDraft}
            onChange={(e) => setNoteDraft(e.target.value)}
            placeholder="optional note"
            aria-label={`${rawLabel} note`}
          />
          <button onClick={saveAlias} title="Save alias overlay">Save</button>
          <button onClick={clearAlias} title="Clear alias overlay">Clear</button>
        </span>
      )}
    </span>
  );
});

export function SignalValueCard({ deviceId, param, leaseHolder, holderId, onClose }) {
  const ctx = treeContext(param);
  const pairSummary = semanticPairSummary(param);
  if (param.writable) {
    return (
      <InputCard
        deviceId={deviceId}
        param={param}
        leaseHolder={leaseHolder}
        holderId={holderId}
        onClose={onClose}
      />
    );
  }
  const { value, quality, ageMs } = useLiveValue(deviceId, param.id, param.instance);
  const displayValue = formatWithUnit(value, param.unit, param.id);
  const channel = MecomAPI.channels?.().find((ch) => ch.device_id === deviceId && Number(ch.instance) === Number(param.instance || 1));
  const channelLabel = channel && MecomAPI.channelDisplayLabel ? MecomAPI.channelDisplayLabel(channel) : `${deviceId} ch${param.instance || 1}`;
  return (
    <div className={["signal-card", "q-" + quality].join(" ")}>
      <div className="nm-row">
        <div>
          <div className="nm">{ctx.visibleLabel || param.name}</div>
          <div className="id">{channelLabel} · {ctx.group || "Signal"} / {ctx.subgroup || "Signals"} · #{param.id}:{param.instance || 1}</div>
          {pairSummary && <div className="signal-help-line">{pairSummary}</div>}
        </div>
        <button className="x" onClick={onClose}>✕</button>
      </div>
      <div className="signal-value">
        <span className="signal-value-meta">
          <em className={"qtag " + quality}>{quality || "missing"}</em>
          <em className={"age " + valueAgeKind(ageMs, quality)}>{formatValueAge(ageMs, quality)}</em>
        </span>
        <b>{displayValue}</b>
      </div>
    </div>
  );
}

function semanticValueRows(param, value, quality, ageMs) {
  const rows = [];
  rows.push({ label: "value", value: formatWithUnit(value, param.unit, param.id) });
  rows.push({ label: "quality", value: quality || "missing" });
  rows.push({ label: "age", value: formatValueAge(ageMs, quality) });
  if (param.hover_help || param.hoverHelp || param.help_text || param.helpText) rows.push({ label: "help", value: param.hover_help || param.hoverHelp || param.help_text || param.helpText });
  if (param.type) rows.push({ label: "type", value: param.type });
  if (param.kind) rows.push({ label: "kind", value: param.kind });
  if (param.visibility) rows.push({ label: "visibility", value: param.visibility });
  if (param.semantic_role || param.semanticRole) rows.push({ label: "semantic role", value: param.semantic_role || param.semanticRole });
  if (param.safety_note || param.safetyNote) rows.push({ label: "safety note", value: param.safety_note || param.safetyNote });
  if (param.source_evidence) rows.push({ label: "evidence", value: Array.isArray(param.source_evidence) ? param.source_evidence.join(" · ") : String(param.source_evidence) });
  if (param.source_parameter_name || param.sourceParameterName) rows.push({ label: "source", value: param.source_parameter_name || param.sourceParameterName });
  if (param.readout_priority || param.readoutPriority) rows.push({ label: "readout", value: param.readout_priority || param.readoutPriority });
  if (param.preferred_readout || param.preferredReadout) rows.push({ label: "preferred readout", value: param.preferred_readout || param.preferredReadout });
  if (param.cmd || param.command) rows.push({ label: "write command", value: param.command || param.cmd });
  if (param.min !== undefined || param.max !== undefined) rows.push({ label: "range", value: `${param.min ?? "—"} .. ${param.max ?? "—"}` });
  if (param.enum && typeof param.enum === "object") {
    const entries = Object.entries(param.enum).slice(0, 8).map(([k, v]) => `${k}=${v}`).join(" · ");
    if (entries) rows.push({ label: "enum", value: entries });
  }
  if (param.applicableModes) rows.push({ label: "modes", value: param.applicableModes.join(", ") });
  if (param.tree_paths && param.tree_paths.length) rows.push({ label: "tree", value: param.tree_paths.map((p) => p.label || (p.path || []).join(" / ")).join(" | ") });
  if (param.transport_support) rows.push({ label: "transport", value: Array.isArray(param.transport_support) ? param.transport_support.map((x) => x.label || x.kind || String(x)).join(" · ") : String(param.transport_support) });
  return rows;
}

const PeerValue = React.memo(({ resolvedDeviceId, instance, peer, enabled = true }) => {
  const { value, quality, ageMs } = useLiveValue(resolvedDeviceId, peer?.id, instance, { enabled: enabled && Boolean(peer) });
  if (!peer) return null;
  const displayValue = formatWithUnit(value, peer.unit, peer.id);
  const kind = peer.role === "monitor" ? "telemetry" : "telecommand";
  return (
    <span>
      {kind} <b>{peer.name || peer.label || `#${peer.id}`}</b>
      <span className="peer-val" style={{ marginLeft: 8, color: "var(--muted)" }}>
        {displayValue} <span className={"q-" + quality} style={{ fontSize: "0.8em", opacity: 0.7 }}>({quality})</span>
      </span>
    </span>
  );
});

export function SemanticValuePopup({ param, value, quality, ageMs, children, className = "" }) {
  const [open, setOpen] = useState(false);
  const rows = semanticValueRows(param || {}, value, quality, ageMs);
  return (
    <span
      className={"semantic-value-popup " + className}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
    >
      {children}
      {open && rows.length > 0 && (
        <span className="semantic-value-popup__panel" role="tooltip">
          {rows.map((row) => (
            <span key={row.label} className="semantic-value-popup__row">
              <span className="semantic-value-popup__label">{row.label}</span>
              <span className="semantic-value-popup__value">{row.value}</span>
            </span>
          ))}
        </span>
      )}
    </span>
  );
}

export function WriteLifecycleTrace({ param, deviceId, instance, phase = "idle", elapsedMs = 0, leaseHolder, holderId, busy = false, staged = "", commandName, trace = null }) {
  // Verification hook for test-ui-semantics.mjs: confirmedMatched | readback mismatch
  const dangerous = Boolean(param && (param.dangerous || param.cmd === "reset" || param.cmd === "save_to_flash"));
  const unit = (param && param.unit) || (trace && trace.unit) || "";
  const paramId = param && param.id !== undefined ? param.id : trace && trace.paramId;

  return (
    <SharedWriteLifecycleTrace
      phase={phase}
      status={trace?.status}
      unit={unit}
      paramId={paramId}
      deviceId={deviceId}
      instance={instance}
      elapsedMs={elapsedMs}
      leaseHolder={leaseHolder}
      holderId={holderId}
      busy={busy}
      staged={staged}
      dangerous={dangerous}
      commandName={commandName}
      trace={trace}
      formatValue={formatWithUnit}
    />
  );
}

/* ============================================================ InputCard ============================================================ */
export function InputCard({ deviceId, param, leaseHolder, holderId, onClose, onApplied }) {
  const { value: curVal } = useLiveValue(deviceId, param.id, param.instance);
  const [staged, setStaged] = useState("");
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState("");
  const [phase, setPhase] = useState("idle");
  const [phaseSince, setPhaseSince] = useState(Date.now());
  const toast = useToast();
  const dangerous = Boolean(param.dangerous || param.cmd === "reset" || param.cmd === "save_to_flash");
  const youHold = leaseHolder === holderId;
  const someoneElse = leaseHolder && leaseHolder !== holderId;
  const stagedTrim = staged.trim();
  const fullNumber = /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/;
  const stagedNum = fullNumber.test(stagedTrim) ? Number(stagedTrim) : NaN;
  const enumEntries = Object.entries(param.enum || {}).sort(([a], [b]) => Number(a) - Number(b));
  const enumValues = enumEntries.map(([key]) => key);
  const isEnumWrite = enumValues.length > 0;
  const isTextWrite = param.kind === "text" || param.type === "latin1";
  const stagedValid = isEnumWrite
    ? enumValues.includes(stagedTrim)
    : isTextWrite
      ? stagedTrim !== "" && stagedTrim.length <= 240
      : (stagedTrim !== "" && !Number.isNaN(stagedNum)
         && (param.min === undefined || stagedNum >= param.min)
         && (param.max === undefined || stagedNum <= param.max));
  
  const validationError = !stagedTrim ? "" : (
    isEnumWrite ? (!enumValues.includes(stagedTrim) ? "Invalid enum selection" : "") :
    isTextWrite ? (stagedTrim.length > 240 ? "Text too long (max 240)" : "") :
    (Number.isNaN(stagedNum) ? "Invalid number format" :
     (param.min !== undefined && stagedNum < param.min ? `Below minimum (${param.min})` :
      (param.max !== undefined && stagedNum > param.max ? `Above maximum (${param.max})` : "")))
  );

  const needsTypeConfirm = dangerous && stagedTrim !== "";
  const confirmReady = !needsTypeConfirm || confirm.trim().toUpperCase() === "WRITE";

  async function commit() {
    setBusy(true);
    setPhase("prepare");
    setPhaseSince(Date.now());
    try {
      let token;
      setPhase("lease");
      setPhaseSince(Date.now());
      if (!youHold) {
        const lease = await MecomAPI.acquireLease(deviceId, holderId, "5m");
        token = lease.token;
        toast.push({ kind: "ok", title: "Lease acquired", body: `${deviceId} · holder ${holderId}` });
      } else {
        const lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
        token = lease && lease.token;
      }
      setPhase("validate");
      setPhaseSince(Date.now());
      const commandName = MecomAPI.commandNameFor(param);
      const body = await MecomAPI.write(deviceId, {
        name: commandName || (isTextWrite ? "write_big_data_string" : (isEnumWrite || param.type === "int32" ? "write_int32" : "write_float32")),
        arguments: {
          param: param.id,
          instance: param.instance || 1,
          value: isTextWrite ? stagedTrim : (isEnumWrite ? Number(stagedTrim) : stagedNum),
        },
      }, token);
      setPhase("ack");
      setPhaseSince(Date.now());
      if (body.status === "completed" || body.status === "confirmed") {
        const confirmed = body.result && body.result.confirmed_value;
        const matched = body.result && body.result.readback_matched;
        if (matched === false) {
           throw new Error(`Readback mismatch: requested ${stagedTrim}, confirmed ${confirmed}`);
        }
        toast.push({ 
          kind: "ok", 
          title: "Command validated", 
          body: `Parameter ${param.id} verified as ${confirmed} ${param.unit || ""}` 
        });
        setStaged("");
        onApplied && onApplied();
      } else {
        throw new Error(body.error || body.status || "Command failed");
      }
      setPhase("done");
    } catch (err) {
      setPhase("error");
      toast.push({ kind: "bad", title: "Command failed", body: err.message });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={["input-card", stagedTrim ? "staged" : "", dangerous ? "danger" : ""].join(" ")}>
      <div className="nm-row">
        <div>
          <div className="nm">{param.name}</div>
          <div className="id">MeCom #{param.id}{param.instance ? `:${param.instance}` : ":1"} · {MecomAPI.commandNameFor(param)}</div>
          {param.help && <div className="input-card-help">{param.help}</div>}
          {param.safety_note && <div className="input-card-safety">CAUTION: {param.safety_note}</div>}
          {(param.min !== undefined || param.max !== undefined) && (
            <div className="input-card-limits">
              Limits: <b>{param.min ?? "-∞"}</b> to <b>{param.max ?? "+∞"}</b> {param.unit}
            </div>
          )}
        </div>
        <button className="x" onClick={onClose}>✕</button>
      </div>
      <WriteLifecycleTrace
        param={param}
        deviceId={deviceId}
        instance={param.instance}
        phase={phase}
        elapsedMs={Date.now() - phaseSince}
        leaseHolder={leaseHolder}
        holderId={holderId}
        busy={busy}
        staged={stagedTrim}
        commandName={MecomAPI.commandNameFor(param)}
      />
      {someoneElse && (
        <div className="confirm-strip" style={{ background: "color-mix(in srgb, var(--warn) 12%, transparent)", borderColor: "color-mix(in srgb, var(--warn) 40%, var(--line))" }}>
          <div className="msg" style={{ color: "#ffe4ad" }}>Currently held by <b>{leaseHolder}</b>. Acquiring will fail with 423.</div>
        </div>
      )}
      <div className="row">
        <div className="cur">
          <div className="lbl">Current value</div>
          <div className="v">{formatWithUnit(curVal, param.unit, param.id)}</div>
        </div>
        <div className={"new" + (stagedTrim ? " has" : "")}>
          <div className="lbl">Staged value</div>
          <input
            className={"input-card-field " + (validationError ? "invalid" : "")}
            type={isTextWrite ? "text" : "number"}
            step="any"
            value={staged}
            onChange={(e) => setStaged(e.target.value)}
            disabled={busy || someoneElse}
            placeholder={isTextWrite ? "Enter text..." : "Enter value..."}
          />
          {validationError && <div className="input-card-error">{validationError}</div>}
        </div>
      </div>
      {(param.min !== undefined || param.max !== undefined) && (
        <div className="range"><span>min {param.min ?? "—"}</span><span>max {param.max ?? "—"}</span></div>
      )}
      {isEnumWrite && (
        <div className="range" style={{ flexWrap: "wrap", gap: 4 }}>
          {enumEntries.map(([key, label]) => <span key={key}>{key} {label}</span>)}
        </div>
      )}
      {needsTypeConfirm && (
        <div className="confirm-strip">
          <div className="msg">Destructive write. Type <b>WRITE</b> to enable commit.</div>
          <input value={confirm} onChange={(e) => setConfirm(e.target.value)} placeholder="WRITE" />
        </div>
      )}
      <div className="actions">
        <button className="btn sm" onClick={() => { setStaged(""); setConfirm(""); }}>Clear</button>
        <span className="spacer"></span>
        <button className={"btn sm " + (dangerous ? "danger" : "primary")}
                disabled={!stagedValid || !confirmReady || busy || someoneElse}
                onClick={commit}>
          {busy ? "…" : (dangerous ? "Commit (real device)" : "Commit")}
        </button>
      </div>
    </div>
  );
}

export function MetricTile({ label, value, unit, kind = "", title }) {
  const formattedValue = value === undefined || value === null ? "—" : formatWithUnit(value, unit);
  return (
    <SemanticValuePopup param={{ unit }} value={value} quality={kind || "ok"}>
      <div className="tile" title={title}>
        <div className="lbl">{label}</div>
        <div className={"val " + kind}>{formattedValue}</div>
      </div>
    </SemanticValuePopup>
  );
}
function SparklineWrapper({ deviceId, paramId, instance, enabled = true }) {
  const data = useSparkline(deviceId, paramId, instance, { enabled });
  return (
    <div className="tree-sparkline">
      <Sparkline data={data} width={80} height={16} />
    </div>
  );
}
