// @ts-nocheck
export const SERIES_ROLE_META = Object.freeze({
  cmd:    { label: "target / command",   rank: 10, className: "cmd",    dash: "6,4", width: 2.2, opacity: 0.98 },
  actual: { label: "actual",             rank: 20, className: "actual", dash: "",    width: 2.2, opacity: 0.98 },
  ghost:  { label: "reference / sink",   rank: 30, className: "ghost",  dash: "2,4", width: 1.8, opacity: 0.86 },
  dut:    { label: "power / load",       rank: 40, className: "dut",    dash: "",    width: 2.0, opacity: 0.94 },
  aux:    { label: "auxiliary",          rank: 50, className: "aux",    dash: "8,4", width: 1.8, opacity: 0.86 },
});

export function seriesRoleMeta(role) {
  return SERIES_ROLE_META[role] || SERIES_ROLE_META.actual;
}

export function emptyGraphTile(opts: any = {}) {
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
    diagnostics: { status: "empty", point_count: 0 },
    provenance: { source: "empty-graph-tile", generated_at: now },
    series: [],
  };
}

export function renderSeriesFromGraphTile(tile) {
  if (!tile || !Array.isArray(tile.series)) return [];
  return tile.series.map((s) => {
    const role = s.role || s.seriesRole || "actual";
    const source = (s.source && typeof s.source === "object") ? s.source : {};
    const history = s.history || {
      ts: (s.points || []).map((p) => Date.parse(p.timestamp)),
      v:  (s.points || []).map((p) => p.value),
      q:  (s.points || []).map(() => "ok"),
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

export function seriesRoleColor(role, fallback = "var(--series-actual)") {
  const meta = seriesRoleMeta(role);
  const css = meta.className ? `--series-${meta.className}` : "";
  if (!css || typeof document === "undefined") return fallback;
  return getComputedStyle(document.documentElement).getPropertyValue(css).trim() || fallback;
}

export function canvasCssColor(value, fallback = "#58a6ff") {
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

export function chartNumber(v) {
  if (!Number.isFinite(v)) return "";
  const abs = Math.abs(v);
  if (abs >= 1000 || abs < 0.01) return v.toExponential(1).replace("e", "E");
  return v.toFixed(abs >= 100 ? 0 : abs >= 10 ? 1 : 2);
}

export const CHART_MIN_WIDTH = 160;
export const CHART_DEFAULT_WIDTH = 640;

export function measuredElementWidth(el) {
  if (!el) return 0;
  const rect = el.getBoundingClientRect ? el.getBoundingClientRect() : null;
  return Math.floor((rect && rect.width) || el.clientWidth || 0);
}

export function chartRenderWidth(canvas, requestedWidth?) {
  const requested = Number(requestedWidth);
  const measured = Number.isFinite(requested) && requested > 0
    ? requested
    : measuredElementWidth(canvas) || measuredElementWidth(canvas && canvas.parentElement) || CHART_DEFAULT_WIDTH;
  return Math.max(CHART_MIN_WIDTH, Math.floor(measured));
}

const droppedAxisWarnings = new Set();

export function drawCanvasChart(canvas, orderedSeries, opts: any = {}) {
  if (!canvas) return;
  const width = chartRenderWidth(canvas, opts.width);
  const height = Math.max(120, opts.height || 320);
  const dpr = window.devicePixelRatio || 1;
  canvas.width = Math.round(width * dpr);
  canvas.height = Math.round(height * dpr);
  canvas.style.width = "100%";
  canvas.style.maxWidth = "100%";
  canvas.style.height = `${height}px`;
  const ctx = canvas.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, width, height);
  ctx.font = "10px var(--font-mono)";
  ctx.textBaseline = "middle";
  const grid  = canvasCssColor("var(--hairline)", "rgba(110,118,129,0.34)");
  const line  = canvasCssColor("var(--line)", "rgba(139,148,158,0.55)");
  const muted = canvasCssColor("var(--muted)", "#8b949e");
  const panel = canvasCssColor("var(--panel)", "#0d1117");
  const padT = 12, padB = 24;
  const tMax = Date.now();
  const timeWindowMs = opts.timeWindowMs || opts.time_window_ms || 90_000;
  const tMin = tMax - timeWindowMs;
  const units: string[] = [];
  const unitGroups = new Map();
  orderedSeries.forEach((s) => {
    const unit = s.unit || "_";
    if (!unitGroups.has(unit)) { unitGroups.set(unit, []); units.push(unit); }
    unitGroups.get(unit).push(s);
  });
  const scales = new Map();
  units.forEach((unit) => {
    let min = Infinity, max = -Infinity;
    unitGroups.get(unit).forEach((s) => {
      (s.history && s.history.v || []).forEach((x) => {
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
    ctx.beginPath(); ctx.moveTo(padL, y); ctx.lineTo(width - padR, y); ctx.stroke();
  }
  ctx.fillStyle = muted;
  ctx.textAlign = "center";
  for (let i = 0; i <= 6; i++) {
    const x = padL + (i / 6) * innerW;
    const ago = Math.round(((6 - i) / 6) * timeWindowMs / 1000);
    ctx.strokeStyle = grid;
    ctx.setLineDash([2, 3]);
    ctx.beginPath(); ctx.moveTo(x, padT); ctx.lineTo(x, padT + innerH); ctx.stroke();
    ctx.setLineDash([]);
    ctx.fillText(ago === 0 ? "now" : `-${ago}s`, x, height - 9);
  }
  units.slice(0, 2).forEach((unit, idx) => {
    const sc = scales.get(unit);
    const isLeft = idx === 0;
    const x = isLeft ? padL : width - padR;
    ctx.strokeStyle = line;
    ctx.beginPath(); ctx.moveTo(x, padT); ctx.lineTo(x, padT + innerH); ctx.stroke();
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
      console.warn(`drawCanvasChart: ${dropped.length} unit group(s) not drawn:`, dropped);
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
    const points: {x: number; y: number}[] = [];
    let latestNumeric: any = null;
    values.forEach((value, i) => {
      if (typeof value !== "number" || Number.isNaN(value)) return;
      const rawT = (typeof times[i] === "number" && times[i] > 0) ? times[i] : tMax;
      latestNumeric = { t: rawT, value };
      if (rawT < tMin) return;
      points.push({ x: xOf(Math.min(Math.max(rawT, tMin), tMax)), y: yOf(unit, value) });
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
      points.forEach((p, i) => { if (i === 0) ctx.moveTo(p.x, p.y); else ctx.lineTo(p.x, p.y); });
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

export function drawSparklineCanvas(canvas, history, color, showAxis?) {
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
    ctx.beginPath(); ctx.moveTo(0, h - 0.5); ctx.lineTo(w, h - 0.5); ctx.stroke();
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
    if (i === 0) ctx.moveTo(px, py); else ctx.lineTo(px, py);
  });
  ctx.stroke();
}
