/* ============================================================
   Meerstetter Gateway — hero graphs, settings tables, dictionary
   (signalforge-aligned)
   ============================================================ */
/* global React, MecomAPI, Pill, Chip, Panel, MultiChart, useToast,
   useLiveValue, useGatewayTick, getTelemetry, recordTelemetry, categorizeError,
   seriesRoleMeta, renderSeriesFromGraphTile */

const { useState, useEffect, useRef, useMemo, useCallback } = React;

/* ============================================================
   Graph assignment store — port of signalforge graphwall.Assignment.
   Persists per-wall { wallId, tileId, targetId } in localStorage.
   A "target" here is a signal address: paramId@deviceId/instance.
   ============================================================ */
const ASSIGNMENT_KEY = "mecomgw.assignments";

function loadAssignments() {
  try {
    const raw = JSON.parse(localStorage.getItem(ASSIGNMENT_KEY) || "[]");
    if (!Array.isArray(raw)) return [];
    return raw.map(normalizeAssignment).filter((a) => a.device_id && Number.isFinite(a.param_id));
  } catch (_) { return []; }
}
function saveAssignments(list) {
  const normalized = (Array.isArray(list) ? list : [])
    .map(normalizeAssignment)
    .filter((a) => a.device_id && Number.isFinite(a.param_id));
  localStorage.setItem(ASSIGNMENT_KEY, JSON.stringify(normalized));
  // Notify subscribers
  window.dispatchEvent(new CustomEvent("mecomgw-assignments-changed"));
}

/* Convenience: known wall IDs */
const WALLS = {
  fleetTemp:    { wall_id: "fleet-temp",    label: "Fleet hero · Temperature" },
  fleetSupply:  { wall_id: "fleet-supply",  label: "Fleet hero · Power supplies" },
};
function wallForDevice(deviceId) { return { wall_id: "device-" + deviceId, label: "Device · " + deviceId }; }

function signalAddress(paramId, deviceId, instance) {
  return paramId + "@" + deviceId + "/" + (instance || 1);
}

function parseSignalAddress(targetId) {
  const m = String(targetId || "").match(/^(\d+)@([^/]+)\/(\d+)$/);
  if (!m) return null;
  return { param_id: parseInt(m[1], 10), device_id: m[2], instance: parseInt(m[3], 10) || 1 };
}

function firstDefined() {
  for (let i = 0; i < arguments.length; i++) {
    if (arguments[i] !== undefined && arguments[i] !== null) return arguments[i];
  }
  return undefined;
}

function normalizeAssignment(a) {
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

function makeAssignment(wallId, paramId, deviceId, instance) {
  return normalizeAssignment({
    wall_id: wallId,
    target_id: signalAddress(paramId, deviceId, instance || 1),
    kind: "trend",
  });
}

function tileSeriesKey(a) {
  const n = normalizeAssignment(a);
  return [n.wall_id || "wall", n.tile_id || n.target_id || "target", n.kind || "trend"].join(":");
}

function useAssignments() {
  const [list, setList] = useState(loadAssignments);
  useEffect(() => {
    const fn = () => setList(loadAssignments());
    window.addEventListener("mecomgw-assignments-changed", fn);
    return () => window.removeEventListener("mecomgw-assignments-changed", fn);
  }, []);
  return {
    list,
    add: (wallId, paramId, deviceId, instance) => {
      const cur = loadAssignments();
      const next = makeAssignment(wallId, paramId, deviceId, instance);
      if (cur.find((a) => a.wall_id === wallId && a.target_id === next.target_id)) return;
      cur.push(next);
      saveAssignments(cur);
    },
    remove: (wallId, paramId, deviceId, instance) => {
      const addr = signalAddress(paramId, deviceId, instance);
      const cur = loadAssignments().filter((a) => !(a.wall_id === wallId && a.target_id === addr));
      saveAssignments(cur);
    },
    forWall: (wallId) => list.filter((a) => a.wall_id === wallId),
    hasAssignment: (wallId, paramId, deviceId, instance) => {
      const addr = signalAddress(paramId, deviceId, instance);
      return list.some((a) => a.wall_id === wallId && a.target_id === addr);
    },
  };
}

/* ============================================================
   Seed default assignments on first run (so the heroes are not empty)
   Versioned key — bump SEED_VERSION when contract IDs change.
   ============================================================ */
const SEED_VERSION = 5;
(function seedAssignments() {
  const ver = parseInt(localStorage.getItem("mecomgw.assignments.version") || "0", 10);
  if (ver === SEED_VERSION && loadAssignments().length > 0) return;
  const channels = MecomAPI.channels();
  const seeds = [];
  channels.forEach((ch) => {
    if (ch.role === "temp") {
      // Temperature hero: target + object + sink + optional cascade.
      // 1200 (stable) is a status lane in the contract — not a temp line.
      const params = [3000, 1000, 1001];
      if (ch.hasCascade) params.push(52200);
      params.forEach((pid) => {
        seeds.push(makeAssignment(WALLS.fleetTemp.wall_id, pid, ch.device_id, ch.instance));
      });
    } else {
      // Power supply hero: voltage + current + power.
      // 2010 / 2040 are command cards per contract — not graph series.
      [1021, 1020, 1022].forEach((pid) => {
        seeds.push(makeAssignment(WALLS.fleetSupply.wall_id, pid, ch.device_id, ch.instance));
      });
    }
  });
  saveAssignments(seeds);
  localStorage.setItem("mecomgw.assignments.version", String(SEED_VERSION));
})();

/* ============================================================
   Helper: build chart series from a list of assignments
   ============================================================ */
const CHANNEL_COLORS = ["#58a6ff", "#3fb950", "#d29922", "#a371f7", "#56d4dd", "#db61a2", "#e3b341", "#f47067"];
function channelColor(deviceId, instance) {
  const channels = MecomAPI.channels();
  const idx = channels.findIndex((c) => c.device_id === deviceId && c.instance === instance);
  return CHANNEL_COLORS[Math.max(0, idx) % CHANNEL_COLORS.length];
}

function buildGraphTileFromAssignments(assignments, opts = {}) {
  const catalogue = MecomAPI.catalogue();
  const normalized = (assignments || []).map(normalizeAssignment).filter((a) => a.device_id && Number.isFinite(a.param_id));
  const units = [];
  const now = new Date().toISOString();
  const tileId = opts.tile_id || opts.tileId || "graph-tile";
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
      role_rank: roleMeta.rank,
      provenance,
      source_ref: sourceRef,
      source: {
        device_id: deviceId,
        instance,
        param_id: paramId,
        signal_id: def.sid || String(def.id),
        endpoint: ch && ch.endpoint,
      },
      points: (history.ts || []).map((ts, idx) => ({
        timestamp: new Date(ts).toISOString(),
        value: (history.v || [])[idx] || 0,
      })),
    };
  }).sort((a, b) => {
    if ((a.role_rank || 0) !== (b.role_rank || 0)) return (a.role_rank || 0) - (b.role_rank || 0);
    return String(a.series_id || "").localeCompare(String(b.series_id || ""));
  });

  return {
    schema_version: "signalforge.graph_tile.v1",
    id: tileId,
    card_id: tileId,
    level: "live",
    generated_at: now,
    renderer: "signalforge.tile.canvas",
    kind: "timeseries",
    tile_id: tileId,
    title: opts.title || "",
    time_window_ms: opts.time_window_ms || opts.timeWindowMs || 90_000,
    axes: units.slice(0, 2).map((unit, idx) => ({ unit, side: idx === 0 ? "left" : "right" })),
    diagnostics: {
      status: series.length > 0 ? "ok" : "empty",
      series_count: series.length,
      renderer: "signalforge.tile.canvas",
    },
    provenance: {
      source: "meerstetter-go.graphwall.assignments",
      generated_at: now,
    },
    series,
  };
}

/* ============================================================
   Hero graph component
   ============================================================ */
function HeroGraph({ wall, role, tile, height = 280, live = true, children }) {
  useGatewayTick();
  const renderedSeries = renderSeriesFromGraphTile(tile);
  return (
    <div className={"hero" + (live ? " live" : "")}>
      <div className="hero-head">
        <div className="title-grp">
          <span className="live-dot"></span>
          <h2>{wall.label}</h2>
          <span className="sub">· {renderedSeries.length} tile series · live</span>
        </div>
        <div className="badge-row">
          <Chip kind="accent">{role === "temp" ? "temperature_c axis" : "voltage_v / current_a"}</Chip>
          <Chip>shared timeline</Chip>
        </div>
      </div>
      <div className="hero-plot">
        {renderedSeries.length === 0 ? (
          <div style={{ padding: 36, textAlign: "center", color: "var(--muted)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
            No signals assigned. Add from the Signal Dictionary →
          </div>
        ) : (
          <MultiChart tile={tile} height={height} />
        )}
      </div>
      <div className="hero-legend">
        {role === "temp" && (
          <>
            <span className="item"><span className="sw cmd"></span>target (cmd)</span>
            <span className="item"><span className="sw actual"></span>object (actual)</span>
            <span className="item"><span className="sw ghost"></span>sink (reference)</span>
          </>
        )}
        {role === "supply" && (
          <>
            <span className="item"><span className="sw aux"></span>set V (cmd)</span>
            <span className="item"><span className="sw actual"></span>actual V</span>
            <span className="item"><span className="sw dut"></span>actual I</span>
          </>
        )}
      </div>
      {children}
    </div>
  );
}

/* ============================================================
   Settings table (for hero panel attachments)
   role="temp": columns = channel | target | object | sink | stable | output | quick-set
   role="supply": columns = channel | set V | actual V | set I | actual I | power | mode | output
   ============================================================ */
function TempSettingsTable({ channels, holderId }) {
  const toast = useToast();
  const [staged, setStaged] = useState({});  // key `dev/inst` -> string
  const [busy, setBusy] = useState({});
  async function commit(deviceId, instance, value) {
    const key = `${deviceId}/${instance}`;
    setBusy((b) => ({ ...b, [key]: true }));
    try {
      let lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
      if (!lease || lease.holder !== holderId) {
        lease = await MecomAPI.acquireLease(deviceId, holderId, "5m");
      }
      await MecomAPI.write(deviceId, {
        name: "set_float32",
        arguments: { param: 3000, instance, value: parseFloat(value) },
      }, lease.token);
      toast.push({ kind: "ok", title: "Target set", body: `${deviceId}/${instance} → ${value} °C` });
      setStaged((s) => ({ ...s, [key]: "" }));
    } catch (err) {
      const c = categorizeError(err);
      toast.push({ kind: c.kind, title: c.cat.toUpperCase(), body: err.message });
    } finally {
      setBusy((b) => ({ ...b, [key]: false }));
    }
  }
  async function toggleOutput(deviceId, instance, cur) {
    try {
      let lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
      if (!lease || lease.holder !== holderId) {
        lease = await MecomAPI.acquireLease(deviceId, holderId, "5m");
      }
      await MecomAPI.write(deviceId, {
        name: "write_int32",
        arguments: { param: 2010, instance, value: cur ? 0 : 1 },
      }, lease.token);
      toast.push({ kind: "ok", title: cur ? "Output OFF" : "Output ON", body: `${deviceId}/${instance}` });
    } catch (err) {
      const c = categorizeError(err);
      toast.push({ kind: c.kind, title: c.cat.toUpperCase(), body: err.message });
    }
  }
  return (
    <table className="hero-settings">
      <thead>
        <tr>
          <th style={{ width: "22%" }}>Channel</th>
          <th>Target</th>
          <th>Object</th>
          <th>Sink</th>
          <th>Output</th>
          <th>Stable</th>
          <th>Quick-set target T</th>
        </tr>
      </thead>
      <tbody>
        {channels.map((ch) => <TempSettingsRow
          key={ch.device_id + "/" + ch.instance}
          ch={ch}
          staged={staged[`${ch.device_id}/${ch.instance}`] || ""}
          setStaged={(v) => setStaged((s) => ({ ...s, [`${ch.device_id}/${ch.instance}`]: v }))}
          busy={busy[`${ch.device_id}/${ch.instance}`]}
          onCommit={commit}
          onToggleOutput={toggleOutput}
        />)}
      </tbody>
    </table>
  );
}

function TempSettingsRow({ ch, staged, setStaged, busy, onCommit, onToggleOutput }) {
  const tgt = useLiveValue(ch.device_id, 3000, ch.instance);
  const obj = useLiveValue(ch.device_id, 1000, ch.instance);
  const sink = useLiveValue(ch.device_id, 1001, ch.instance);
  const out = useLiveValue(ch.device_id, 2010, ch.instance);
  const stable = useLiveValue(ch.device_id, 1200, ch.instance);
  const cascade = useLiveValue(ch.device_id, 52200, ch.instance);
  const dt = (obj.value != null && tgt.value != null) ? obj.value - tgt.value : null;
  const reachable = tgt.quality !== "unreachable";
  const stagedNum = parseFloat(staged);
  const stagedValid = !Number.isNaN(stagedNum) && stagedNum >= -40 && stagedNum <= 150;
  return (
    <tr title={MecomAPI.provenance(ch.device_id, 3000, ch.instance)}>
      <td>
        <span className="swatch" style={{ background: channelColor(ch.device_id, ch.instance) }}></span>
        <span className="chan-name">{ch.device_id}</span>
        <span className="chan-sub">/{ch.instance} · {ch.label}</span>
      </td>
      <td className="num cmd">{MecomAPI.formatValue(tgt.value, "degC", 3000)} <span style={{color:"var(--muted)"}}>°C</span></td>
      <td className={"num actual " + (dt !== null && Math.abs(dt) > 0.3 ? "warn" : "")}>{MecomAPI.formatValue(obj.value, "degC", 1000)} <span style={{color:"var(--muted)"}}>°C</span></td>
      <td>{MecomAPI.formatValue(sink.value, "degC", 1001)} <span style={{color:"var(--muted)"}}>°C</span></td>
      <td>
        <span className={"out-toggle " + (out.value === 1 ? "on" : "off") + (!reachable ? " locked" : "")}
              onClick={() => reachable && onToggleOutput(ch.device_id, ch.instance, out.value === 1)}
              title="Click to toggle Output Stage Enable (write_int32 param=2010)">
          <span>OFF</span><span>ON</span>
        </span>
      </td>
      <td>{stable.value === 1 ? <span className="num ok">stable</span> : reachable ? <span className="num warn">drift</span> : <span style={{color:"var(--bad)"}}>—</span>}</td>
      <td>
        <span className="quick-input">
          <input className={staged ? "staged" : ""} placeholder="°C" value={staged} disabled={!reachable}
                 onChange={(e) => setStaged(e.target.value)} />
          <button className={stagedValid ? "primary" : ""} disabled={!stagedValid || !reachable || busy}
                  onClick={() => onCommit(ch.device_id, ch.instance, staged)}>
            {busy ? "…" : "Set"}
          </button>
        </span>
      </td>
    </tr>
  );
}

function SupplySettingsTable({ channels, holderId }) {
  const toast = useToast();
  const [staged, setStaged] = useState({});
  const [busy, setBusy] = useState({});
  async function commitField(deviceId, instance, param, value) {
    const key = `${deviceId}/${instance}/${param}`;
    setBusy((b) => ({ ...b, [key]: true }));
    try {
      let lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
      if (!lease || lease.holder !== holderId) {
        lease = await MecomAPI.acquireLease(deviceId, holderId, "5m");
      }
      await MecomAPI.write(deviceId, {
        name: "write_float32",
        arguments: { param, instance, value: parseFloat(value) },
      }, lease.token);
      toast.push({ kind: "ok", title: param === 1021 ? "Set voltage" : "Set current",
                   body: `${deviceId}/${instance} → ${value}` });
      setStaged((s) => ({ ...s, [key]: "" }));
    } catch (err) {
      const c = categorizeError(err);
      toast.push({ kind: c.kind, title: c.cat.toUpperCase(), body: err.message });
    } finally {
      setBusy((b) => ({ ...b, [key]: false }));
    }
  }
  async function toggleOutput(deviceId, instance, cur) {
    try {
      let lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
      if (!lease || lease.holder !== holderId) {
        lease = await MecomAPI.acquireLease(deviceId, holderId, "5m");
      }
      await MecomAPI.write(deviceId, {
        name: "write_int32",
        arguments: { param: 2010, instance, value: cur ? 0 : 1 },
      }, lease.token);
      toast.push({ kind: "ok", title: cur ? "Output OFF" : "Output ON", body: `${deviceId}/${instance}` });
    } catch (err) {
      const c = categorizeError(err);
      toast.push({ kind: c.kind, title: c.cat.toUpperCase(), body: err.message });
    }
  }
  return (
    <table className="hero-settings">
      <thead>
        <tr>
          <th style={{ width: "20%" }}>Channel</th>
          <th>Set V (1021)</th>
          <th>Actual V</th>
          <th>Set I (1020)</th>
          <th>Actual I</th>
          <th>Power</th>
          <th>Mode</th>
          <th>Output</th>
        </tr>
      </thead>
      <tbody>
        {channels.map((ch) => <SupplySettingsRow
          key={ch.device_id + "/" + ch.instance}
          ch={ch}
          stagedV={staged[`${ch.device_id}/${ch.instance}/1021`] || ""}
          stagedI={staged[`${ch.device_id}/${ch.instance}/1020`] || ""}
          setStagedV={(v) => setStaged((s) => ({ ...s, [`${ch.device_id}/${ch.instance}/1021`]: v }))}
          setStagedI={(v) => setStaged((s) => ({ ...s, [`${ch.device_id}/${ch.instance}/1020`]: v }))}
          busyV={busy[`${ch.device_id}/${ch.instance}/1021`]}
          busyI={busy[`${ch.device_id}/${ch.instance}/1020`]}
          onCommitField={commitField}
          onToggleOutput={toggleOutput}
        />)}
      </tbody>
    </table>
  );
}

function SupplySettingsRow({ ch, stagedV, stagedI, setStagedV, setStagedI, busyV, busyI, onCommitField, onToggleOutput }) {
  // 1021 = output_voltage_v (write = setpoint; read = actual)
  // 1020 = output_current_a (write = setpoint; read = actual)
  const actV = useLiveValue(ch.device_id, 1021, ch.instance);
  const actI = useLiveValue(ch.device_id, 1020, ch.instance);
  const power = useLiveValue(ch.device_id, 1022, ch.instance);
  const mode = useLiveValue(ch.device_id, 2040, ch.instance);
  const out = useLiveValue(ch.device_id, 2010, ch.instance);
  // Setpoints come from gateway-side mock helper (read-back of last-written setpoint)
  const setV = MecomAPI.setpoint(ch.device_id, 1021, ch.instance);
  const setI = MecomAPI.setpoint(ch.device_id, 1020, ch.instance);
  const reachable = actV.quality !== "unreachable";
  return (
    <tr title={MecomAPI.provenance(ch.device_id, 1021, ch.instance)}>
      <td>
        <span className="swatch" style={{ background: channelColor(ch.device_id, ch.instance) }}></span>
        <span className="chan-name">{ch.device_id}</span>
        <span className="chan-sub">/{ch.instance} · {ch.label}</span>
      </td>
      <td className="num cmd">
        <span className="quick-input">
          <input className={stagedV ? "staged" : ""} placeholder={(setV ?? 0).toFixed(2)} value={stagedV} disabled={!reachable}
                 onChange={(e) => setStagedV(e.target.value)} />
          <button className={stagedV ? "primary" : ""} disabled={!stagedV || !reachable || busyV}
                  onClick={() => onCommitField(ch.device_id, ch.instance, 1021, stagedV)}>
            {busyV ? "…" : "V"}
          </button>
        </span>
      </td>
      <td className="num actual">{MecomAPI.formatValue(actV.value, "V", 1021)} <span style={{color:"var(--muted)"}}>V</span></td>
      <td className="num cmd">
        <span className="quick-input">
          <input className={stagedI ? "staged" : ""} placeholder={(setI ?? 0).toFixed(3)} value={stagedI} disabled={!reachable}
                 onChange={(e) => setStagedI(e.target.value)} />
          <button className={stagedI ? "primary" : ""} disabled={!stagedI || !reachable || busyI}
                  onClick={() => onCommitField(ch.device_id, ch.instance, 1020, stagedI)}>
            {busyI ? "…" : "I"}
          </button>
        </span>
      </td>
      <td className="num dut">{MecomAPI.formatValue(actI.value, "A", 1020)} <span style={{color:"var(--muted)"}}>A</span></td>
      <td>{MecomAPI.formatValue(power.value, "W", 1022)} <span style={{color:"var(--muted)"}}>W</span></td>
      <td><Chip kind={mode.value === 3 ? "warn" : "ok"}>{MecomAPI.formatValue(mode.value, "", 2040)}</Chip></td>
      <td>
        <span className={"out-toggle " + (out.value === 1 ? "on" : "off") + (!reachable ? " locked" : "")}
              onClick={() => reachable && onToggleOutput(ch.device_id, ch.instance, out.value === 1)}>
          <span>OFF</span><span>ON</span>
        </span>
      </td>
    </tr>
  );
}

Object.assign(window, {
  WALLS, wallForDevice, signalAddress,
  parseSignalAddress, normalizeAssignment, makeAssignment,
  useAssignments, loadAssignments, saveAssignments,
  buildGraphTileFromAssignments, channelColor,
  HeroGraph, TempSettingsTable, SupplySettingsTable,
});

/* useLiveValue / getTelemetry / recordTelemetry are now instance-aware
   from components.jsx — no patching needed here. */
