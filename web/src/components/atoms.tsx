// @ts-nocheck
import React, { useState, useEffect, useRef, useMemo, useCallback, createContext, useContext } from "react";
import { MecomAPI } from "../api/mecom";
import { recordTelemetry, getTelemetry } from "../lib/telemetry";
import {
  seriesRoleMeta, renderSeriesFromGraphTile, emptyGraphTile,
  drawCanvasChart, drawSparklineCanvas, measuredElementWidth,
} from "../lib/series";

export { seriesRoleMeta, renderSeriesFromGraphTile, emptyGraphTile };

/* ---------- Hook: latest value for a (device, param, instance) ---------- */
export function useLiveValue(deviceId, paramId, instance?) {
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

/* ============================================================ Canvas charts ============================================================ */
export function Sparkline({ history, color = "var(--accent)", w = 320, h = 56, showAxis = false }) {
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

export function MultiChart({ tile, height = 320, timeWindowMs = 90_000 }) {
  useGatewayTick();
  const wrapRef = useRef(null);
  const canvasRef = useRef(null);
  const [w, setW] = useState(0);
  useEffect(() => {
    const target = wrapRef.current;
    if (!target) return undefined;
    let raf = 0;
    const update = () => {
      if (raf) cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => {
        const next = measuredElementWidth(target);
        if (next > 0) setW((cur) => (cur === next ? cur : next));
      });
    };
    update();
    let ro;
    if (typeof ResizeObserver !== "undefined") {
      ro = new ResizeObserver(update);
      ro.observe(target);
    }
    window.addEventListener("resize", update, { passive: true });
    return () => {
      if (raf) cancelAnimationFrame(raf);
      if (ro) ro.disconnect();
      window.removeEventListener("resize", update);
    };
  }, []);
  const graphTile = useMemo(() => tile || emptyGraphTile({ timeWindowMs }), [tile, timeWindowMs]);
  const orderedSeries = useMemo(() => renderSeriesFromGraphTile(graphTile), [graphTile]);
  useEffect(() => {
    const measuredWidth = w || measuredElementWidth(wrapRef.current);
    drawCanvasChart(canvasRef.current, orderedSeries, {
      width: measuredWidth, height,
      timeWindowMs: graphTile.time_window_ms || timeWindowMs,
    });
  }, [orderedSeries, w, height, graphTile.time_window_ms, timeWindowMs]);
  const title = graphTile.title || graphTile.tile_id || graphTile.id || "graph tile";
  return (
    <div
      ref={wrapRef}
      style={{ width: "100%", minWidth: 0 }}
      data-graph-tile={graphTile.tile_id || graphTile.id || ""}
      data-graph-renderer={graphTile.renderer || "signalforge.tile.canvas"}
      title={title}
    >
      <canvas ref={canvasRef} style={{ width: "100%", maxWidth: "100%", height, display: "block" }} />
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

export function DiscoveryTree({ deviceId, instance, catalogue, pins, onTogglePin, onWrite, onlyWritable, query, setQuery, filterCat, setFilterCat }) {
  useGatewayTick();
  const filtered = useMemo(() => {
    const q = (query || "").toLowerCase().trim();
    return catalogue.filter((p) => {
      if (onlyWritable && !p.writable) return false;
      if (filterCat && CategoryFor(p.name) !== filterCat) return false;
      if (!q) return true;
      return (p.name.toLowerCase().includes(q) || String(p.id).includes(q) || (p.unit || "").toLowerCase().includes(q));
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
                          deviceId={deviceId} instance={instance} param={p}
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
    <div className={["tree-node", pinned ? "pinned" : "", param.writable ? "write" : "", "q-" + quality].join(" ")} onClick={onTogglePin}>
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

/* ============================================================ InputCard ============================================================ */
export function InputCard({ deviceId, param, leaseHolder, holderId, onClose, onApplied }) {
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
      if (!youHold) {
        const lease = await MecomAPI.acquireLease(deviceId, holderId, "5m");
        token = lease.token;
        toast.push({ kind: "ok", title: "Lease acquired", body: `${deviceId} · holder ${holderId}` });
      } else {
        const lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
        token = lease && lease.token;
      }
      const cmdName = MecomAPI.commandNameFor(param);
      const val = isEnumWrite ? parseInt(stagedTrim, 10) : stagedNum;
      const req = { name: cmdName, arguments: { param: param.id, instance: param.instance || 1, value: val } };
      await MecomAPI.write(deviceId, req, token);
      toast.push({ kind: "ok", title: `${param.name} = ${stagedTrim}${param.unit ? " " + param.unit : ""}`, body: `${deviceId}` });
      setStaged(""); setConfirm("");
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
          <div className="msg" style={{ color: "#ffe4ad" }}>Currently held by <b>{leaseHolder}</b>. Acquiring will fail with 423.</div>
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
        <div className="range"><span>min {param.min ?? "—"}</span><span>max {param.max ?? "—"}</span></div>
      )}
      {isEnumWrite && (
        <div className="range" style={{ flexWrap: "wrap", gap: 4 }}>
          <span>0 OFF</span><span>1 ON</span><span>2 Live OFF</span><span>3 HW ctrl</span>
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
  return (
    <div className="tile" title={title}>
      <div className="lbl">{label}</div>
      <div className={"val " + kind}>{value}{unit ? <span className="u">{" " + unit}</span> : null}</div>
    </div>
  );
}
