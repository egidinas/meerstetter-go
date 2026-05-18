// @ts-nocheck
import React, { useState, useEffect, useRef, useMemo, useCallback, createContext, useContext } from "react";
import { UPlotTileRenderer } from "signalforge-web";
import "uplot/dist/uPlot.min.css";
import { MecomAPI } from "../api/mecom";
import { recordTelemetry, getTelemetry } from "../lib/telemetry";
import {
  seriesRoleMeta, renderSeriesFromGraphTile, emptyGraphTile,
  normalizeGraphTile, CANONICAL_TILE_RENDERER,
} from "../lib/series";

export { seriesRoleMeta, renderSeriesFromGraphTile, emptyGraphTile, normalizeGraphTile };

/* ---------- Hook: latest value for a (device, param, instance) ---------- */
export function useLiveValue(deviceId, paramId, instance?) {
  const inst = instance ?? 1;
  const [, force] = useState(0);
  useEffect(() => {
    const tick = () => {
      const r = MecomAPI.readValue(deviceId, paramId, inst);
      recordTelemetry(deviceId, paramId, r.value, r.quality, inst);
      force((n) => (n + 1) % 1e9);
    };
    tick();
    const unsub = MecomAPI.subscribe(tick);
    return unsub;
  }, [deviceId, paramId, inst]);
  const buf = getTelemetry(deviceId, paramId, inst);
  const v = buf.v.length ? buf.v[buf.v.length - 1] : null;
  const q = buf.q.length ? buf.q[buf.q.length - 1] : "missing";
  return { value: v, quality: q, history: buf };
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
export function Sparkline({ history, color = "var(--accent)", w = 320, h = 56, showAxis = false }) {
  const tile = useMemo(() => normalizeGraphTile({
    ...emptyGraphTile({ tile_id: "sparkline", timeWindowMs: 90_000 }),
    series: [{
      id: "sparkline",
      label: "sparkline",
      role: "actual",
      unit: "_",
      color,
      history,
      points: (history?.ts || []).map((ts, idx) => ({ timestamp: new Date(ts).toISOString(), value: (history?.v || [])[idx] })),
    }],
  }), [history, history?.seq, history?.v?.length, history?.v?.[history?.v?.length - 1], history?.latestTs, color, showAxis, w]);
  return <UPlotTileRenderer tile={tile} height={h} dataGraphRenderer={CANONICAL_TILE_RENDERER} syncKey="meerstetter-go-sparkline" />;
}

export function MultiChart({ tile, height = 320, timeWindowMs = 90_000, hiddenSeries = [], fill = false, minHeight = 220 }) {
  useGatewayTick();
  const graphTile = useMemo(() => normalizeGraphTile(tile || emptyGraphTile({ timeWindowMs }), { timeWindowMs }), [tile, timeWindowMs]);
  const hidden = useMemo(() => new Set(hiddenSeries || []), [hiddenSeries.join ? hiddenSeries.join("|") : String(hiddenSeries)]);
  const [autoY, setAutoY] = useState(true);
  const [yMin, setYMin] = useState("");
  const [yMax, setYMax] = useState("");
  const [viewToken, setViewToken] = useState(0);
  const visibleTile = useMemo(() => {
    if (!hidden.size) return graphTile;
    return normalizeGraphTile({
      ...graphTile,
      series: (graphTile.series || []).filter((series) => {
        const key = series.series_id || series.id || series.target_id || series.label;
        return !hidden.has(key);
      }),
      diagnostics: {
        ...(graphTile.diagnostics || {}),
        hidden_series_count: hidden.size,
        visible_series_count: (graphTile.series || []).filter((series) => {
          const key = series.series_id || series.id || series.target_id || series.label;
          return !hidden.has(key);
        }).length,
      },
    }, { timeWindowMs });
  }, [graphTile, hidden, timeWindowMs]);
  const orderedSeries = useMemo(() => renderSeriesFromGraphTile(visibleTile), [visibleTile]);
  const title = graphTile.title || graphTile.tile_id || graphTile.id || "graph tile";
  const parsedYRange = useMemo(() => {
    const min = Number(yMin);
    const max = Number(yMax);
    if (!Number.isFinite(min) || !Number.isFinite(max)) return null;
    if (max <= min) return null;
    return [min, max];
  }, [yMin, yMax]);
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
      title={title}
    >
      <div className="chart-controls">
        <button className="btn sm" onClick={() => setViewToken((n) => n + 1)} title="Reset the x-axis to the full tile range">Auto X</button>
        <label className="toggle-sm">
          <input type="checkbox" checked={autoY} onChange={(e) => setAutoY(e.target.checked)} />
          Auto Y
        </label>
        <input className="select-sm axis-bound" type="number" step="any" placeholder="Y min" value={yMin} onChange={(e) => setYMin(e.target.value)} disabled={autoY} />
        <input className="select-sm axis-bound" type="number" step="any" placeholder="Y max" value={yMax} onChange={(e) => setYMax(e.target.value)} disabled={autoY} />
        <button className="btn sm" onClick={() => { setYMin(""); setYMax(""); setAutoY(true); }} title="Return to automatic y-axis scaling">Reset Y</button>
      </div>
      <UPlotTileRenderer
        tile={visibleTile}
        height={height}
        dataGraphRenderer={CANONICAL_TILE_RENDERER}
        syncKey="meerstetter-go-wall"
        autoY={autoY}
        yRange={parsedYRange}
        viewToken={viewToken}
        fillContainer={fill}
        minHeight={minHeight}
      />
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

function formatWithUnit(value, unit, paramId) {
  if (MecomAPI.formatWithUnit) return MecomAPI.formatWithUnit(value, unit, paramId);
  const label = displayUnit(unit);
  return label ? `${value} ${label}` : `${value}`;
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
      } else {
        byId.set(id, {
          id,
          label: item.label || id,
          count: 1,
          default: Boolean(item.default),
        });
      }
    });
  });
  return Array.from(byId.values()).sort((a, b) => {
    if (a.default !== b.default) return a.default ? -1 : 1;
    return a.label.localeCompare(b.label);
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
    };
  }
  if (typeof item !== "object") return null;
  let path = treeSegments(item.path);
  if (!path.length) path = treeSegments(item.label);
  if (!path.length) return null;
  const text = treePathText(path);
  return {
    id: String(item.id || text).trim(),
    label: String(item.label || path[path.length - 1] || text).trim(),
    path,
    default: Boolean(item.default),
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
  ].filter(Boolean).join(" ")).join(" ");
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
      searchText: [param && param.name, param && param.id, param && param.unit, group, subgroup, allTreeText].map((v) => String(v || "").toLowerCase()).join(" "),
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
    searchText: [param && param.name, param && param.id, param && param.unit, selected.label, pathText, selected.id, allTreeText].map((v) => String(v || "").toLowerCase()).join(" "),
    path: pathText,
    label: selected.label,
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
      g[group][subgroup].push(p);
    });
    return g;
  }, [filtered]);
  const [collapsed, setCollapsed] = useState({});
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
          const isCollapsed = collapsed[group];
          return (
            <div key={group}>
              <div className="tree-group-head" onClick={() => setCollapsed((c) => ({ ...c, [group]: !c[group] }))}>
                <span>{isCollapsed ? "▸" : "▾"}</span>
                <span>{group}</span>
                <span className="count">{groupCount}</span>
              </div>
              {!isCollapsed && Object.entries(sgrps).map(([subgroup, items]) => (
                <div key={group + ":" + subgroup} className="tree-subgroup">
                  <div className="tree-subgroup-head">
                    <span>{subgroup}</span>
                    <span>{items.length}</span>
                  </div>
                  {items.map((p) => (
                    <TreeNode key={p.id + ":" + subgroup}
                              deviceId={deviceId} channels={channelFanout} param={p}
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

function TreeNode({ deviceId, channels, param, pins, writeCards, treeProjection, leaseHolder, holderId, onTogglePin, onPinCard, onWrite, onCloseWrite }) {
  const ctx = treeContext(param, treeProjection);
  const applicableChannels = (channels || []).filter((c) => !param.applicableModes || param.applicableModes.includes(c.role));
  const activeCards = (writeCards || []).filter((c) => c.id === param.id);
  const unitLabel = displayUnit(param.unit) || "unitless";
  return (
    <div className={["tree-node", param.writable ? "write" : ""].join(" ")}>
      <span className="swatch"></span>
      <span className="nm" title={ctx.title}>
        {ctx.visibleLabel || param.name} <span className="id">·{param.id}</span>
      </span>
      <span className="unit">{unitLabel}</span>
      <span className="kind">{param.writable ? "Read/write" : "Read-only"}</span>
      <div className="tree-instances">
        {applicableChannels.map((ch) => (
          <TreeInstance key={ch.device_id + ":" + ch.instance}
                        deviceId={deviceId}
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
}

function TreeInstance({ deviceId, instance, role, param, pinned, onTogglePin, onPinCard, onWrite }) {
  const { value, quality } = useLiveValue(deviceId, param.id, instance);
  const displayValue = formatWithUnit(value, param.unit, param.id);
  const channelLabel = role || "channel";
  return (
    <span className={["tree-inst", pinned ? "pinned" : "", "q-" + quality].join(" ")} title={`channel ${channelLabel} · instance ${instance} · ${quality}`}>
      <button title="Pin instance to graph" onClick={onTogglePin}>{pinned ? "★" : "☆"}</button>
      <span className="inst">{channelLabel} · i{instance}</span>
      <span className="vl">{displayValue}</span>
      <button title="Pin value card" onClick={onPinCard}>▣</button>
      {param.writable && <button title="Open write card" onClick={onWrite}>✎</button>}
    </span>
  );
}

export function SignalValueCard({ deviceId, param, leaseHolder, holderId, onClose }) {
  const ctx = treeContext(param);
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
  const { value, quality } = useLiveValue(deviceId, param.id, param.instance);
  const displayValue = formatWithUnit(value, param.unit, param.id);
  return (
    <div className={["signal-card", "q-" + quality].join(" ")}>
      <div className="nm-row">
        <div>
          <div className="nm">{ctx.visibleLabel || param.name}</div>
          <div className="id">{ctx.group || "Signal"} / {ctx.subgroup || "Signals"} · #{param.id}:{param.instance || 1}</div>
        </div>
        <button className="x" onClick={onClose}>✕</button>
      </div>
      <div className="signal-value">
        <span>{quality}</span>
        <b>{displayValue}</b>
      </div>
    </div>
  );
}

function semanticValueRows(param, value, quality) {
  const rows = [];
  rows.push({ label: "value", value: formatWithUnit(value, param.unit, param.id) });
  rows.push({ label: "quality", value: quality || "missing" });
  if (param.type) rows.push({ label: "type", value: param.type });
  if (param.kind) rows.push({ label: "kind", value: param.kind });
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

export function SemanticValuePopup({ param, value, quality, children, className = "" }) {
  const [open, setOpen] = useState(false);
  const rows = semanticValueRows(param || {}, value, quality);
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
  const dangerous = Boolean(param && (param.dangerous || param.cmd === "reset" || param.cmd === "save_to_flash"));
  const youHold = leaseHolder === holderId;
  const tracePhase = trace && trace.phase ? trace.phase : phase;
  const traceStatus = trace && trace.status ? trace.status : (tracePhase === "done" ? "completed" : tracePhase === "error" ? "failed" : tracePhase);
  const unit = (param && param.unit) || (trace && trace.unit) || "";
  const paramId = param && param.id !== undefined ? param.id : trace && trace.paramId;
  const phaseElapsed = trace && trace.at ? Date.now() - trace.at : elapsedMs;
  const valueRows = trace ? [
    { label: "current", value: formatWithUnit(trace.currentValue, unit, paramId) },
    { label: "prospective", value: formatWithUnit(trace.prospectiveValue, unit, paramId) },
    { label: "submitted", value: formatWithUnit(trace.submittedValue, unit, paramId) },
    { label: "confirmed", value: trace.confirmedValue !== undefined ? formatWithUnit(trace.confirmedValue, unit, paramId) : traceStatus },
  ] : [];
  const steps = [
    { key: "prepare", label: "prepare", detail: staged ? `staged ${staged}` : "no staged value" },
    { key: "lease", label: "lease", detail: youHold ? "held locally" : (leaseHolder ? `held by ${leaseHolder}` : "available") },
    { key: "validate", label: "validate", detail: dangerous ? "confirmation required" : "range / type check" },
    { key: "write", label: "write", detail: commandName || (trace && trace.commandName) || (param && (param.command || param.cmd)) || "write_float32" },
    { key: "ack", label: "ack", detail: tracePhase === "done" ? traceStatus : (tracePhase === "error" ? (trace && trace.error || "failed") : (busy ? "waiting" : "idle")) },
  ];
  return (
    <div className="write-lifecycle-trace" data-phase={tracePhase} title={`device=${deviceId} instance=${instance || 1}`}>
      <div className="write-lifecycle-trace__head">
        <span className="write-lifecycle-trace__title">Write lifecycle</span>
        <span className="write-lifecycle-trace__meta">{busy ? "busy" : traceStatus} · {Math.max(0, Math.round(phaseElapsed))} ms</span>
      </div>
      <div className="write-lifecycle-trace__steps">
        {steps.map((step) => (
          <span key={step.key} className={"write-lifecycle-trace__step " + (tracePhase === step.key || (tracePhase === "done" && step.key === "ack") || (tracePhase === "error" && step.key === "ack") ? "on" : "")}>
            <b>{step.label}</b>
            <em>{step.detail}</em>
          </span>
        ))}
      </div>
      {valueRows.length > 0 && (
        <div className="write-lifecycle-trace__values">
          {valueRows.map((row) => (
            <span key={row.label} className="write-lifecycle-trace__value-row">
              <b>{row.label}</b>
              <em>{row.value}</em>
            </span>
          ))}
        </div>
      )}
    </div>
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
      const cmdName = MecomAPI.commandNameFor(param);
      const val = isTextWrite ? stagedTrim : (isEnumWrite ? parseInt(stagedTrim, 10) : stagedNum);
      const req = { name: cmdName, arguments: { param: param.id, instance: param.instance ?? 1, value: val } };
      setPhase("write");
      setPhaseSince(Date.now());
      await MecomAPI.write(deviceId, req, token);
      setPhase("done");
      setPhaseSince(Date.now());
      toast.push({ kind: "ok", title: `${param.name} = ${stagedTrim}${param.unit ? " " + param.unit : ""}`, body: `${deviceId}` });
      setStaged(""); setConfirm("");
      onApplied && onApplied();
    } catch (err) {
      setPhase("error");
      setPhaseSince(Date.now());
      const c = categorizeError(err);
      toast.push({ kind: c.kind, title: c.cat.toUpperCase(), body: err.message || String(err) });
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
            value={staged}
            placeholder={isTextWrite ? "note" : isEnumWrite ? enumValues.join("/") : "value"}
            onChange={(e) => setStaged(e.target.value)}
            inputMode={isTextWrite ? "text" : "decimal"}
            maxLength={isTextWrite ? 240 : undefined}
          />
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
