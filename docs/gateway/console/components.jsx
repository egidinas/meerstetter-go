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
   Sparkline + multi-overlay chart
   ============================================================ */
function Sparkline({ history, color = "var(--accent)", w = 320, h = 56, showAxis = false }) {
  const path = useMemo(() => {
    const v = history.v;
    if (!v || v.length < 2) return null;
    const valid = v.filter((x) => typeof x === "number" && !Number.isNaN(x));
    if (valid.length < 2) return null;
    const min = Math.min(...valid);
    const max = Math.max(...valid);
    const span = max - min || 1;
    const pad = 4;
    const inW = w - pad * 2;
    const inH = h - pad * 2;
    return v.map((x, i) => {
      const px = pad + (i / (v.length - 1)) * inW;
      const py = pad + inH - ((x - min) / span) * inH;
      return (i === 0 ? "M" : "L") + px.toFixed(1) + " " + py.toFixed(1);
    }).join(" ") + ` L${w - pad} ${h - pad} L${pad} ${h - pad} Z`;
  }, [history.v.length, history.v[history.v.length - 1]]);
  return (
    <svg width={w} height={h} preserveAspectRatio="none" viewBox={`0 0 ${w} ${h}`} style={{ width: "100%", display: "block" }}>
      {showAxis && (
        <line x1={0} x2={w} y1={h - 0.5} y2={h - 0.5} stroke="var(--hairline)" />
      )}
      {path && (
        <>
          <defs>
            <linearGradient id={"spg-" + color.replace(/[^a-z0-9]/gi, "")} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity="0.35" />
              <stop offset="100%" stopColor={color} stopOpacity="0" />
            </linearGradient>
          </defs>
          <path d={path} fill={`url(#spg-${color.replace(/[^a-z0-9]/gi, "")})`} stroke="none" />
          <path d={path.split(" Z")[0]} fill="none" stroke={color} strokeWidth="1.4" />
        </>
      )}
    </svg>
  );
}

/* Multi-param overlay chart with separate y-axes by unit family. */
function MultiChart({ series, height = 320, timeWindowMs = 90_000 }) {
  /* series: [{ key, label, color, unit, history }] */
  const wrapRef = useRef(null);
  const [w, setW] = useState(900);
  useEffect(() => {
    if (!wrapRef.current) return;
    const ro = new ResizeObserver(() => {
      if (wrapRef.current) setW(wrapRef.current.clientWidth);
    });
    ro.observe(wrapRef.current);
    return () => ro.disconnect();
  }, []);
  const h = height;
  const padL = 50, padR = 50, padT = 12, padB = 24;
  const innerW = Math.max(40, w - padL - padR);
  const innerH = h - padT - padB;
  const tMax = Date.now();
  const tMin = tMax - timeWindowMs;

  // Group series by unit family
  const unitGroups = useMemo(() => {
    const g = {};
    series.forEach((s) => {
      const u = s.unit || "_";
      if (!g[u]) g[u] = [];
      g[u].push(s);
    });
    return g;
  }, [series.map((s) => s.key + s.history.v.length).join("|")]);

  const axes = Object.keys(unitGroups);
  const scales = useMemo(() => {
    const out = {};
    axes.forEach((u) => {
      let min = Infinity, max = -Infinity;
      unitGroups[u].forEach((s) => {
        const v = s.history.v;
        for (let i = 0; i < v.length; i++) {
          const x = v[i];
          if (typeof x === "number" && !Number.isNaN(x)) {
            if (x < min) min = x;
            if (x > max) max = x;
          }
        }
      });
      if (min === Infinity) { min = 0; max = 1; }
      if (min === max) { min -= 0.5; max += 0.5; }
      const span = max - min;
      // Pad
      min -= span * 0.1;
      max += span * 0.1;
      out[u] = { min, max };
    });
    return out;
  }, [unitGroups]);

  const xOf = (t) => padL + ((t - tMin) / (tMax - tMin)) * innerW;
  const yOfFor = (unit) => (v) => {
    const sc = scales[unit];
    return padT + innerH - ((v - sc.min) / (sc.max - sc.min)) * innerH;
  };

  // Time ticks
  const xTicks = useMemo(() => {
    const out = [];
    for (let i = 0; i <= 6; i++) {
      const tt = tMin + (i / 6) * (tMax - tMin);
      const ago = Math.round((tMax - tt) / 1000);
      out.push({ x: padL + (i / 6) * innerW, label: ago === 0 ? "now" : ("-" + ago + "s") });
    }
    return out;
  }, [tMin, tMax, innerW]);

  return (
    <div ref={wrapRef} style={{ width: "100%" }}>
      <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} style={{ display: "block" }}>
        {/* gridlines */}
        {[0, 0.25, 0.5, 0.75, 1].map((g, i) => (
          <line key={i} x1={padL} x2={w - padR}
                y1={padT + g * innerH} y2={padT + g * innerH}
                stroke="var(--hairline)" />
        ))}
        {/* x ticks */}
        {xTicks.map((tk, i) => (
          <g key={i}>
            <line x1={tk.x} x2={tk.x} y1={padT} y2={padT + innerH}
                  stroke="var(--hairline)" strokeDasharray="2,3" opacity="0.5" />
            <text x={tk.x} y={h - 8} fontSize="10" fill="var(--muted)" textAnchor="middle"
                  fontFamily="var(--font-mono)">{tk.label}</text>
          </g>
        ))}
        {/* y axes (left = first unit, right = second) */}
        {axes.slice(0, 2).map((u, idx) => {
          const sc = scales[u];
          const isLeft = idx === 0;
          const x = isLeft ? padL : (w - padR);
          const align = isLeft ? "end" : "start";
          const tx = isLeft ? padL - 6 : (w - padR + 6);
          const labels = [sc.max, sc.min + (sc.max - sc.min) * 0.5, sc.min];
          return (
            <g key={u}>
              <line x1={x} x2={x} y1={padT} y2={padT + innerH} stroke="var(--line)" />
              {labels.map((lv, j) => {
                const ny = j === 0 ? padT : j === 1 ? padT + innerH / 2 : padT + innerH;
                return (
                  <text key={j} x={tx} y={ny + 3} fontSize="10" fill="var(--muted)" textAnchor={align}
                        fontFamily="var(--font-mono)">{lv.toFixed(2)}{u && u !== "_" ? " " + u : ""}</text>
                );
              })}
            </g>
          );
        })}
        {/* series */}
        {series.map((s) => {
          const yOf = yOfFor(s.unit || "_");
          const v = s.history.v;
          const ts = s.history.ts;
          const q = s.history.q;
          if (v.length < 2) return null;
          let d = "";
          for (let i = 0; i < v.length; i++) {
            if (typeof v[i] !== "number" || Number.isNaN(v[i])) continue;
            if (ts[i] < tMin) continue;
            const x = xOf(ts[i]);
            const y = yOf(v[i]);
            d += (d ? " L" : "M") + x.toFixed(1) + " " + y.toFixed(1);
          }
          if (!d) return null;
          const lastIdx = v.length - 1;
          const lastX = xOf(ts[lastIdx]);
          const lastY = yOf(v[lastIdx]);
          return (
            <g key={s.key}>
              <path d={d} fill="none" stroke={s.color} strokeWidth="1.4" opacity="0.95" />
              <circle cx={lastX} cy={lastY} r="2.4" fill={s.color} />
            </g>
          );
        })}
      </svg>
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
  // contexts
  ToastProvider, useToast, categorizeError, CategoryFor,
  // atoms
  Pill, Chip, Panel, Sparkline, MultiChart, MetricTile,
  DiscoveryTree, InputCard,
});
