/* ========================================================
   Meerstetter Gateway — atomic UI components (shared)
   ======================================================== */
/* global React, MecomAPI */

const { useState, useEffect, useRef, useMemo, useCallback } = React;

/* ---------- Telemetry value buffer (per device + param + instance) ---------- */
const TELE_BUF = new Map(); // key `${dev}/${inst}:${id}` -> { ts: [], v: [], q: [] }
const TELE_MAX = 720; // ~6 minutes at 500ms cadence

function teleKey(deviceId, paramId, instance) {
  return deviceId + "/" + (instance || 1) + ":" + paramId;
}

function recordTelemetry(deviceId, paramId, value, quality, instance) {
  const key = teleKey(deviceId, paramId, instance);
  let buf = TELE_BUF.get(key);
  if (!buf) { buf = { ts: [], v: [], q: [] }; TELE_BUF.set(key, buf); }
  buf.ts.push(Date.now());
  buf.v.push(value);
  buf.q.push(quality);
  if (buf.ts.length > TELE_MAX) { buf.ts.shift(); buf.v.shift(); buf.q.shift(); }
  return buf;
}
function getTelemetry(deviceId, paramId, instance) {
  return TELE_BUF.get(teleKey(deviceId, paramId, instance)) || { ts: [], v: [], q: [] };
}

const SERIES_ROLE_META = Object.freeze({
  cmd: { label: "target / command", rank: 10, className: "cmd", dash: "6,4", width: 2.2, opacity: 0.98 },
  actual: { label: "actual", rank: 20, className: "actual", dash: "", width: 2.2, opacity: 0.98 },
  ghost: { label: "reference / sink", rank: 30, className: "ghost", dash: "2,4", width: 1.8, opacity: 0.86 },
  dut: { label: "power / load", rank: 40, className: "dut", dash: "", width: 2.0, opacity: 0.94 },
  aux: { label: "auxiliary", rank: 50, className: "aux", dash: "8,4", width: 1.8, opacity: 0.86 },
});

function seriesRoleMeta(role) {
  return SERIES_ROLE_META[role] || SERIES_ROLE_META.actual;
}

/* ---------- Hook: latest value for a (device, param, instance) ---------- */
function useLiveValue(deviceId, paramId, instance) {
  const inst = instance || 1;
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
function useGatewayTick() {
  const [, force] = useState(0);
  useEffect(() => MecomAPI.subscribe(() => force((n) => (n + 1) % 1e9)), []);
}

/* ============================================================
   Visual atoms
   ============================================================ */
function Pill({ kind = "info", children, icon }) {
  return (
    <span className={"pill " + kind}>
      <span className="dot"></span>
      {icon}{children}
    </span>
  );
}

function Chip({ kind = "", children, title }) {
  return <span className={"chip " + kind} title={title}>{children}</span>;
}

function Panel({ title, meta, right, children, flush, style, className }) {
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

/* ============================================================
   SignalForge graph tile adapter + canvas renderer
   ============================================================ */
function canvasCssColor(value, fallback = "#58a6ff") {
  if (!value) return fallback;
  if (typeof value === "string" && !value.includes("var(")) return value;
  if (typeof document === "undefined") return fallback;
  const probe = document.createElement("span");
  probe.style.color = value;
  probe.style.position = "absolute";
  probe.style.visibility = "hidden";
  document.body.appendChild(probe);
  const resolved = getComputedStyle(probe).color || fallback;
  probe.remove();
  return resolved;
}

function seriesRoleColor(role, fallback = "var(--series-actual)") {
  const meta = seriesRoleMeta(role);
  const css = meta.className ? `--series-${meta.className}` : "";
  if (!css || typeof document === "undefined") return fallback;
  return getComputedStyle(document.documentElement).getPropertyValue(css).trim() || fallback;
}

function emptyGraphTile(opts = {}) {
  const id = opts.tile_id || opts.tileId || "empty";
  const now = new Date().toISOString();
  return {
    schema_version: "signalforge.graph_tile.v1",
    id,
    card_id: id,
    level: "live",
    generated_at: now,
    renderer: "signalforge.tile.canvas",
    kind: "timeseries",
    tile_id: id,
    title: opts.title || "",
    time_window_ms: opts.time_window_ms || opts.timeWindowMs || 90_000,
    axes: opts.axes || [],
    diagnostics: {
      status: "empty",
      point_count: 0,
    },
    provenance: {
      source: "empty-graph-tile",
      generated_at: now,
    },
    series: [],
  };
}

function renderSeriesFromGraphTile(tile) {
  if (!tile || !Array.isArray(tile.series)) return [];
  return tile.series.map((s) => {
    const role = s.role || s.seriesRole || "actual";
    const source = (s.source && typeof s.source === "object") ? s.source : {};
    const history = s.history || {
      ts: (s.points || []).map((p) => Date.parse(p.timestamp)),
      v: (s.points || []).map((p) => p.value),
      q: (s.points || []).map(() => "ok"),
    };
    return {
      key: s.series_id || s.id || s.key || s.target_id || s.targetId || s.label,
      tileId: tile.tile_id || tile.id || tile.tileId,
      targetId: s.target_id || s.targetId,
      label: s.label,
      fullLabel: s.full_label || s.fullLabel || s.label,
      role,
      seriesRole: role,
      roleRank: s.role_rank ?? seriesRoleMeta(role).rank,
      color: s.color || seriesRoleColor(role),
      unit: s.unit || "_",
      provenance: s.provenance || "",
      source: s.source || null,
      paramId: s.param_id !== undefined ? s.param_id : source.param_id,
      deviceId: s.device_id !== undefined ? s.device_id : source.device_id,
      instance: s.instance !== undefined ? s.instance : source.instance,
      signalId: s.signal_id !== undefined ? s.signal_id : source.signal_id,
      history,
    };
  }).sort((a, b) => {
    if (a.roleRank !== b.roleRank) return a.roleRank - b.roleRank;
    return String(a.tileId || a.key || a.label || "").localeCompare(String(b.tileId || b.key || b.label || ""));
  });
}

function chartNumber(v) {
  if (!Number.isFinite(v)) return "";
  const abs = Math.abs(v);
  if (abs >= 1000 || abs < 0.01) return v.toExponential(1).replace("e", "E");
  return v.toFixed(abs >= 100 ? 0 : abs >= 10 ? 1 : 2);
}

const droppedAxisWarnings = new Set();

function drawCanvasChart(canvas, orderedSeries, opts = {}) {
  if (!canvas) return;
  const width = Math.max(320, opts.width || canvas.clientWidth || 900);
  const height = Math.max(120, opts.height || 320);
  const dpr = window.devicePixelRatio || 1;
  canvas.width = Math.round(width * dpr);
  canvas.height = Math.round(height * dpr);
  canvas.style.width = `${width}px`;
  canvas.style.height = `${height}px`;
  const ctx = canvas.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, width, height);
  ctx.font = "10px var(--font-mono)";
  ctx.textBaseline = "middle";
  const grid = canvasCssColor("var(--hairline)", "rgba(110,118,129,0.34)");
  const line = canvasCssColor("var(--line)", "rgba(139,148,158,0.55)");
  const muted = canvasCssColor("var(--muted)", "#8b949e");
  const panel = canvasCssColor("var(--panel)", "#0d1117");
  const padT = 12, padB = 24;
  const tMax = Date.now();
  const timeWindowMs = opts.timeWindowMs || opts.time_window_ms || 90_000;
  const tMin = tMax - timeWindowMs;
  const units = [];
  const unitGroups = new Map();
  orderedSeries.forEach((s) => {
    const unit = s.unit || "_";
    if (!unitGroups.has(unit)) {
      unitGroups.set(unit, []);
      units.push(unit);
    }
    unitGroups.get(unit).push(s);
  });
  const scales = new Map();
  units.forEach((unit) => {
    let min = Infinity, max = -Infinity;
    unitGroups.get(unit).forEach((s) => {
      const values = (s.history && s.history.v) || [];
      values.forEach((x) => {
        if (typeof x !== "number" || Number.isNaN(x)) return;
        min = Math.min(min, x);
        max = Math.max(max, x);
      });
    });
    if (min === Infinity) { min = 0; max = 1; }
    if (min === max) { min -= 0.5; max += 0.5; }
    const span = max - min || 1;
    scales.set(unit, { min: min - span * 0.1, max: max + span * 0.1 });
  });
  // Dynamic axis padding: measure worst-case tick-label width so long labels
  // (e.g. "1.2E-7 mbar", "-12.34 mA") don't clip the inner plot.
  function tickLabelsFor(unit) {
    const sc = scales.get(unit);
    if (!sc) return [];
    return [sc.max, sc.min + (sc.max - sc.min) * 0.5, sc.min].map(
      (lv) => `${chartNumber(lv)}${unit && unit !== "_" ? " " + unit : ""}`,
    );
  }
  function maxLabelWidth(unit) {
    return tickLabelsFor(unit).reduce((acc, t) => Math.max(acc, ctx.measureText(t).width), 0);
  }
  const padL = Math.max(50, Math.ceil((units[0] ? maxLabelWidth(units[0]) : 0) + 12));
  const padR = Math.max(50, Math.ceil((units[1] ? maxLabelWidth(units[1]) : 0) + 12));
  const innerW = Math.max(40, width - padL - padR);
  const innerH = Math.max(40, height - padT - padB);
  const xOf = (t) => padL + ((t - tMin) / (tMax - tMin)) * innerW;
  const yOf = (unit, v) => {
    const sc = scales.get(unit || "_") || { min: 0, max: 1 };
    return padT + innerH - ((v - sc.min) / (sc.max - sc.min || 1)) * innerH;
  };
  ctx.strokeStyle = grid;
  ctx.lineWidth = 1;
  for (let i = 0; i <= 4; i++) {
    const y = padT + (i / 4) * innerH;
    ctx.beginPath();
    ctx.moveTo(padL, y);
    ctx.lineTo(width - padR, y);
    ctx.stroke();
  }
  ctx.fillStyle = muted;
  ctx.textAlign = "center";
  for (let i = 0; i <= 6; i++) {
    const x = padL + (i / 6) * innerW;
    const ago = Math.round(((6 - i) / 6) * timeWindowMs / 1000);
    ctx.strokeStyle = grid;
    ctx.setLineDash([2, 3]);
    ctx.beginPath();
    ctx.moveTo(x, padT);
    ctx.lineTo(x, padT + innerH);
    ctx.stroke();
    ctx.setLineDash([]);
    ctx.fillText(ago === 0 ? "now" : `-${ago}s`, x, height - 9);
  }
  units.slice(0, 2).forEach((unit, idx) => {
    const sc = scales.get(unit);
    const isLeft = idx === 0;
    const x = isLeft ? padL : width - padR;
    ctx.strokeStyle = line;
    ctx.beginPath();
    ctx.moveTo(x, padT);
    ctx.lineTo(x, padT + innerH);
    ctx.stroke();
    ctx.textAlign = isLeft ? "right" : "left";
    const tx = isLeft ? padL - 6 : width - padR + 6;
    [sc.max, sc.min + (sc.max - sc.min) * 0.5, sc.min].forEach((lv, j) => {
      const y = j === 0 ? padT : j === 1 ? padT + innerH / 2 : padT + innerH;
      ctx.fillStyle = muted;
      ctx.fillText(`${chartNumber(lv)}${unit && unit !== "_" ? " " + unit : ""}`, tx, y + 3);
    });
  });
  if (units.length > 2) {
    const dropped = units.slice(2);
    const key = dropped.join("|");
    if (!droppedAxisWarnings.has(key)) {
      droppedAxisWarnings.add(key);
      console.warn(`drawCanvasChart: ${dropped.length} unit group(s) not drawn (only 2 axes supported):`, dropped);
    }
    ctx.save();
    ctx.fillStyle = canvasCssColor("var(--warn)", "#d29922");
    ctx.textAlign = "left";
    ctx.fillText(`+${dropped.length} axis hidden (${dropped.join(", ")})`, padL + 6, padT + 8);
    ctx.restore();
  }
  if (!orderedSeries.length) {
    ctx.textAlign = "center";
    ctx.fillStyle = muted;
    ctx.fillText("No graph tile series", width / 2, height / 2);
    return;
  }
  orderedSeries.forEach((s) => {
    const roleMeta = seriesRoleMeta(s.seriesRole || s.role);
    const unit = s.unit || "_";
    const values = (s.history && s.history.v) || [];
    const times = (s.history && s.history.ts) || [];
    const points = [];
    let latestNumeric = null;
    values.forEach((value, i) => {
      if (typeof value !== "number" || Number.isNaN(value)) return;
      const rawT = (typeof times[i] === "number" && times[i] > 0) ? times[i] : tMax;
      latestNumeric = { t: rawT, value };
      if (rawT < tMin) return;
      points.push({
        x: xOf(Math.min(Math.max(rawT, tMin), tMax)),
        y: yOf(unit, value),
      });
    });
    if (!points.length && latestNumeric) {
      points.push({ x: width - padR - 4, y: yOf(unit, latestNumeric.value) });
    }
    if (!points.length) return;
    const color = canvasCssColor(s.color, seriesRoleColor(s.seriesRole || s.role, "#58a6ff"));
    const last = points[points.length - 1];
    ctx.save();
    ctx.globalAlpha = roleMeta.opacity;
    ctx.strokeStyle = color;
    ctx.fillStyle = color;
    ctx.lineWidth = roleMeta.width || 2;
    const dash = roleMeta.dash ? roleMeta.dash.split(",").map((x) => Number(x.trim())).filter(Number.isFinite) : [];
    ctx.setLineDash(dash);
    ctx.beginPath();
    if (points.length === 1) {
      ctx.moveTo(padL, last.y);
      ctx.lineTo(width - padR, last.y);
    } else {
      points.forEach((p, i) => {
        if (i === 0) ctx.moveTo(p.x, p.y);
        else ctx.lineTo(p.x, p.y);
      });
    }
    ctx.stroke();
    ctx.globalAlpha = 1;
    ctx.setLineDash([]);
    ctx.beginPath();
    ctx.arc(points.length === 1 ? width - padR - 4 : last.x, last.y, points.length === 1 ? 3.2 : 2.8, 0, Math.PI * 2);
    ctx.fill();
    ctx.strokeStyle = panel;
    ctx.lineWidth = 1;
    ctx.stroke();
    ctx.restore();
  });
}

function drawSparklineCanvas(canvas, history, color, showAxis) {
  if (!canvas) return;
  const w = Math.max(80, canvas.clientWidth || Number(canvas.width) || 320);
  const h = Math.max(28, canvas.clientHeight || Number(canvas.height) || 56);
  const dpr = window.devicePixelRatio || 1;
  canvas.width = Math.round(w * dpr);
  canvas.height = Math.round(h * dpr);
  const ctx = canvas.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, w, h);
  if (showAxis) {
    ctx.strokeStyle = canvasCssColor("var(--hairline)", "rgba(110,118,129,0.34)");
    ctx.beginPath();
    ctx.moveTo(0, h - 0.5);
    ctx.lineTo(w, h - 0.5);
    ctx.stroke();
  }
  const values = (history && history.v) || [];
  const valid = values.filter((x) => typeof x === "number" && !Number.isNaN(x));
  if (valid.length < 2) return;
  const min = Math.min(...valid);
  const max = Math.max(...valid);
  const span = max - min || 1;
  const pad = 4;
  const inW = w - pad * 2;
  const inH = h - pad * 2;
  ctx.strokeStyle = canvasCssColor(color, "#58a6ff");
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  values.forEach((x, i) => {
    if (typeof x !== "number" || Number.isNaN(x)) return;
    const px = pad + (i / Math.max(1, values.length - 1)) * inW;
    const py = pad + inH - ((x - min) / span) * inH;
    if (i === 0) ctx.moveTo(px, py);
    else ctx.lineTo(px, py);
  });
  ctx.stroke();
}

function Sparkline({ history, color = "var(--accent)", w = 320, h = 56, showAxis = false }) {
  const canvasRef = useRef(null);
  useEffect(() => {
    drawSparklineCanvas(canvasRef.current, history, color, showAxis);
  }, [history && history.v && history.v.length, history && history.v && history.v[history.v.length - 1], color, showAxis, w, h]);
  return (
    <canvas
      ref={canvasRef}
      data-graph-renderer="signalforge.tile.canvas.sparkline"
      style={{ width: "100%", height: h, display: "block" }}
      width={w}
      height={h}
    />
  );
}

function MultiChart({ tile, height = 320, timeWindowMs = 90_000 }) {
  useGatewayTick();
  const wrapRef = useRef(null);
  const canvasRef = useRef(null);
  const [w, setW] = useState(900);
  useEffect(() => {
    if (!wrapRef.current) return;
    const update = () => setW(Math.max(320, wrapRef.current.clientWidth || 900));
    update();
    const ro = new ResizeObserver(update);
    ro.observe(wrapRef.current);
    return () => ro.disconnect();
  }, []);
  const graphTile = useMemo(() => tile || emptyGraphTile({ timeWindowMs }), [tile, timeWindowMs]);
  const orderedSeries = useMemo(() => renderSeriesFromGraphTile(graphTile), [graphTile]);
  useEffect(() => {
    drawCanvasChart(canvasRef.current, orderedSeries, {
      width: w,
      height,
      timeWindowMs: graphTile.time_window_ms || timeWindowMs,
    });
  }, [orderedSeries, w, height, graphTile.time_window_ms, timeWindowMs]);
  const title = graphTile.title || graphTile.tile_id || graphTile.id || "graph tile";
  return (
    <div
      ref={wrapRef}
      style={{ width: "100%" }}
      data-graph-tile={graphTile.tile_id || graphTile.id || ""}
      data-graph-renderer={graphTile.renderer || "signalforge.tile.canvas"}
      title={title}
    >
      <canvas ref={canvasRef} style={{ width: "100%", height, display: "block" }} />
    </div>
  );
}

/* ============================================================
   Toasts
   ============================================================ */
const ToastCtx = React.createContext(null);
function ToastProvider({ children }) {
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
function useToast() { return React.useContext(ToastCtx); }

/* Map HTTP status to error category (per HANDOFF.md) */
function categorizeError(err) {
  const s = err && err.status;
  if (s === 423) return { kind: "warn",  cat: "lease conflict" };
  if (s === 503) return { kind: "bad",   cat: "device unreachable" };
  if (s === 504) return { kind: "bad",   cat: "timeout" };
  if (s === 403) return { kind: "warn",  cat: "read-only" };
  if (s === 409) return { kind: "bad",   cat: "device rejected" };
  if (s === 501) return { kind: "warn",  cat: "not supported" };
  return { kind: "bad", cat: "error" };
}

/* ============================================================
   Discovery Tree (for device workspace)
   ============================================================ */
function CategoryFor(name) {
  const l = (name || "").toLowerCase();
  if (l.includes("temperature") || l.includes("temp")) return "Temperature";
  if (l.includes("power") || l.includes("current") || l.includes("voltage")) return "Power and Output";
  if (l.includes("target") || l.includes("limit") || l.includes("pid") || l.includes("control") || l.includes("mode") || l.includes("enable") || l.includes("ramp")) return "Control";
  if (l.includes("status") || l.includes("state") || l.includes("error") || l.includes("warning") || l.includes("event") || l.includes("alarm") || l.includes("stable")) return "Status and Events";
  if (l.includes("firmware") || l.includes("hardware") || l.includes("serial") || l.includes("device") || l.includes("version") || l.includes("flash") || l.includes("save")) return "Device Metadata";
  return "Other Signals";
}

function DiscoveryTree({ deviceId, instance, catalogue, pins, onTogglePin, onWrite, onlyWritable, query, setQuery, filterCat, setFilterCat }) {
  useGatewayTick();

  const filtered = useMemo(() => {
    const q = (query || "").toLowerCase().trim();
    return catalogue.filter((p) => {
      if (onlyWritable && !p.writable) return false;
      if (filterCat && CategoryFor(p.name) !== filterCat) return false;
      if (!q) return true;
      return (p.name.toLowerCase().includes(q)
        || String(p.id).includes(q)
        || (p.unit || "").toLowerCase().includes(q));
    });
  }, [catalogue, onlyWritable, filterCat, query]);

  const groups = useMemo(() => {
    const g = {};
    filtered.forEach((p) => {
      const cat = CategoryFor(p.name);
      if (!g[cat]) g[cat] = [];
      g[cat].push(p);
    });
    return g;
  }, [filtered]);

  const [collapsed, setCollapsed] = useState({});

  const cats = ["Temperature", "Power and Output", "Control", "Status and Events", "Device Metadata", "Other Signals"];

  return (
    <div className="tree-pane">
      <div className="tree-head">
        <input className="field" placeholder="Search parameters…" value={query} onChange={(e) => setQuery(e.target.value)} />
        <div className="tree-filters">
          <button className={!filterCat ? "on" : ""} onClick={() => setFilterCat("")}>All</button>
          {cats.map((c) => (
            <button key={c} className={filterCat === c ? "on" : ""} onClick={() => setFilterCat(c)}>{c.replace(" and Events", "").replace(" and Output", "")}</button>
          ))}
        </div>
      </div>
      <div className="tree-list">
        {cats.map((cat) => {
          const items = groups[cat];
          if (!items || !items.length) return null;
          const isCollapsed = collapsed[cat];
          return (
            <div key={cat}>
              <div className="tree-group-head" onClick={() => setCollapsed((c) => ({ ...c, [cat]: !c[cat] }))}>
                <span>{isCollapsed ? "▸" : "▾"}</span>
                <span>{cat}</span>
                <span className="count">{items.length}</span>
              </div>
              {!isCollapsed && items.map((p) => (
                <TreeNode key={p.id + ":" + (p.instance || 1)}
                          deviceId={deviceId}
                          instance={instance}
                          param={p}
                          pinned={pins.some((x) => x.id === p.id)}
                          onTogglePin={() => onTogglePin(p)}
                          onWrite={() => onWrite(p)} />
              ))}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function TreeNode({ deviceId, instance, param, pinned, onTogglePin, onWrite }) {
  const { value, quality } = useLiveValue(deviceId, param.id, instance);
  const displayValue = MecomAPI.formatValue(value, param.unit, param.id);
  return (
    <div className={[
      "tree-node",
      pinned ? "pinned" : "",
      param.writable ? "write" : "",
      "q-" + quality,
    ].join(" ")}
         onClick={onTogglePin}>
      <span className="swatch"></span>
      <span className="nm" title={param.name}>{param.name} <span className="id">·{param.id}</span></span>
      <span className="vl">{displayValue}{param.unit ? <span className="u">{param.unit}</span> : null}</span>
      <span className="actions" onClick={(e) => e.stopPropagation()}>
        <button title="Pin to chart" onClick={onTogglePin}>{pinned ? "★" : "☆"}</button>
        {param.writable && <button title="Open write card" onClick={onWrite}>✎</button>}
      </span>
    </div>
  );
}

/* ============================================================
   Input card (lease-gated write)
   ============================================================ */
function InputCard({ deviceId, param, leaseHolder, holderId, onClose, onApplied }) {
  const { value: curVal } = useLiveValue(deviceId, param.id, param.instance);
  const [staged, setStaged] = useState("");
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState("");
  const toast = useToast();
  const dangerous = param.id === 2010 || param.cmd === "reset" || param.cmd === "save_to_flash";
  const youHold = leaseHolder === holderId;
  const someoneElse = leaseHolder && leaseHolder !== holderId;
  const stagedTrim = staged.trim();
  const stagedNum = parseFloat(stagedTrim);
  const isEnumWrite = param.id === 2010;
  const stagedValid = isEnumWrite
    ? ["0", "1", "2", "3"].includes(stagedTrim)
    : (stagedTrim !== "" && !Number.isNaN(stagedNum)
       && (param.min === undefined || stagedNum >= param.min)
       && (param.max === undefined || stagedNum <= param.max));
  const needsTypeConfirm = dangerous && stagedTrim && stagedTrim !== "0";
  const confirmReady = !needsTypeConfirm || confirm.trim().toUpperCase() === "WRITE";

  async function commit() {
    setBusy(true);
    try {
      let token;
      // Lease auto-acquire
      if (!youHold) {
        const lease = await MecomAPI.acquireLease(deviceId, holderId, "5m");
        token = lease.token;
        toast.push({ kind: "ok", title: "Lease acquired", body: `${deviceId} · holder ${holderId}` });
      } else {
        // Use current lease token from registry
        const lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
        token = lease && lease.token;
      }
      // Build write request
      let cmdName = MecomAPI.commandNameFor(param);
      const val = isEnumWrite ? parseInt(stagedTrim, 10) : stagedNum;
      const req = {
        name: cmdName,
        arguments: { param: param.id, instance: param.instance || 1, value: val },
      };
      await MecomAPI.write(deviceId, req, token);
      toast.push({ kind: "ok", title: `${param.name} = ${stagedTrim}${param.unit ? " " + param.unit : ""}`, body: `${deviceId}` });
      setStaged("");
      setConfirm("");
      onApplied && onApplied();
    } catch (err) {
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
      {someoneElse && (
        <div className="confirm-strip" style={{ background: "color-mix(in srgb, var(--warn) 12%, transparent)", borderColor: "color-mix(in srgb, var(--warn) 40%, var(--line))" }}>
          <div className="msg" style={{ color: "#ffe4ad" }}>
            Currently held by <b>{leaseHolder}</b>. Acquiring will fail with 423.
          </div>
        </div>
      )}
      <div className="row">
        <div className="cur">
          <div className="lbl">Current</div>
          <div className="v">{MecomAPI.formatValue(curVal, param.unit, param.id)}{param.unit ? <span className="u">{" " + param.unit}</span> : null}</div>
        </div>
        <div className={"new" + (stagedTrim ? " has" : "")}>
          <div className="lbl">Stage</div>
          <input value={staged} placeholder={isEnumWrite ? "0–3" : "value"} onChange={(e) => setStaged(e.target.value)} inputMode="decimal" />
        </div>
      </div>
      {(param.min !== undefined || param.max !== undefined) && (
        <div className="range">
          <span>min {param.min ?? "—"}</span>
          <span>max {param.max ?? "—"}</span>
        </div>
      )}
      {isEnumWrite && (
        <div className="range" style={{ flexWrap: "wrap", gap: 4 }}>
          <span>0 OFF</span>
          <span>1 ON</span>
          <span>2 Live OFF</span>
          <span>3 HW ctrl</span>
        </div>
      )}
      {needsTypeConfirm && (
        <div className="confirm-strip">
          <div className="msg">
            Destructive write. Type <b>WRITE</b> to enable commit.
          </div>
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

/* ============================================================
   Tile (numeric readout, for cards)
   ============================================================ */
function MetricTile({ label, value, unit, kind = "", title }) {
  return (
    <div className="tile" title={title}>
      <div className="lbl">{label}</div>
      <div className={"val " + kind}>{value}{unit ? <span className="u">{" " + unit}</span> : null}</div>
    </div>
  );
}

Object.assign(window, {
  // hooks
  useLiveValue, useGatewayTick, getTelemetry, recordTelemetry,
  SERIES_ROLE_META, seriesRoleMeta,
  emptyGraphTile, renderSeriesFromGraphTile,
  // contexts
  ToastProvider, useToast, categorizeError, CategoryFor,
  // atoms
  Pill, Chip, Panel, Sparkline, MultiChart, MetricTile,
  DiscoveryTree, InputCard,
});
