// @ts-nocheck
import React, { useState, useEffect, useRef, useMemo } from "react";
import { MecomAPI } from "../api/mecom";
import { Chip, Pill, Panel, MultiChart, useToast, useLiveValue, useGatewayTick } from "../components/atoms";
import { CANONICAL_TILE_RENDERER, seriesRoleMeta } from "../lib/series";
import { HeroGraph } from "./hero";

const SEQ_LIBRARY = [
  {
    id: "mecom-four-cycle-sample",
    name: "MeCom LUT four-cycle sample",
    description: "Preload-only safe sample. 4 cycles · 2 steps · no output enable.",
    automation: "mecom_lut",
    safeStart: "preload_only_no_output_enable",
    targets: ["tec-75", "tec-76", "tec-81", "tec-84"],
    timeout: 60,
    steps: [
      { id: "preload-lut",  kind: "send_command", command_name: "mecom.lut.preload", await_ack: true, duration: 0.8 },
      { id: "verify-load",  kind: "assert",       command_name: "lut.checksum", duration: 0.3 },
      { id: "settle",       kind: "wait",         duration: 1 },
      { id: "log-preload",  kind: "log",          duration: 0.2 },
    ],
  },
  {
    id: "bench-soak-25c",
    name: "Bench soak · hold 25 °C",
    description: "Set 25 °C, wait until stable, hold 5 min, log.",
    automation: "ad-hoc",
    targets: ["tec-75"],
    timeout: 360,
    steps: [
      { id: "set-target",    kind: "send_command", command_name: MecomAPI.commandNameFor(MecomAPI.paramById(3000)), arguments: { param: 3000, value: 25 }, await_ack: true, duration: 0.6 },
      { id: "enable-output", kind: "send_command", command_name: "write_int32",  arguments: { param: 2010, value: 1 }, await_ack: true, duration: 0.4 },
      { id: "wait-stable",   kind: "wait_stable",  duration: 8 },
      { id: "hold",          kind: "wait",         duration: 12 },
      { id: "log",           kind: "log",          duration: 0.4 },
      { id: "disable",       kind: "send_command", command_name: "write_int32",  arguments: { param: 2010, value: 0 }, await_ack: true, duration: 0.4 },
    ],
  },
  {
    id: "thermal-step-response",
    name: "PID step response capture",
    description: "Step from 20 → 25 °C, capture 60 s. Used by PID advisor.",
    automation: "ad-hoc",
    targets: ["tec-75"],
    timeout: 90,
    steps: [
      { id: "set-baseline", kind: "send_command", command_name: MecomAPI.commandNameFor(MecomAPI.paramById(3000)), arguments: { param: 3000, value: 20 }, await_ack: true, duration: 0.6 },
      { id: "settle",       kind: "wait_stable",  duration: 8 },
      { id: "step",         kind: "send_command", command_name: MecomAPI.commandNameFor(MecomAPI.paramById(3000)), arguments: { param: 3000, value: 25 }, await_ack: true, duration: 0.4 },
      { id: "capture",      kind: "wait",         duration: 16 },
      { id: "log",          kind: "log",          duration: 0.4 },
    ],
  },
];

export function SequencerView({ deviceId }) {
  useGatewayTick();
  const [activeId, setActiveId] = useState("mecom-four-cycle-sample");
  const [running, setRunning] = useState(false);
  const [dryRun, setDryRun] = useState(false);
  const [paused, setPaused] = useState(false);
  const [progress, setProgress] = useState(0);
  const [stepLog, setStepLog] = useState([]);
  const [stepStatuses, setStepStatuses] = useState({});
  const timerRef = useRef(null);
  const startRef = useRef(0);
  const toast = useToast();

  const script = SEQ_LIBRARY.find((s) => s.id === activeId);
  const totalDuration = script ? script.steps.reduce((a, s) => a + s.duration, 0) : 0;

  const layout = useMemo(() => {
    if (!script) return [];
    let acc = 0;
    return script.steps.map((s) => {
      const start = acc / totalDuration;
      acc += s.duration;
      const end = acc / totalDuration;
      return { ...s, start, end };
    });
  }, [script, totalDuration]);

  function reset() {
    setProgress(0);
    setStepLog([]);
    setStepStatuses({});
    if (timerRef.current) { cancelAnimationFrame(timerRef.current); timerRef.current = null; }
  }

  function msgFor(s, dry) {
    const tag = dry ? "[dry-run] " : "";
    if (s.kind === "send_command") return tag + (s.command_name || "command") + (s.arguments ? " " + JSON.stringify(s.arguments) : "");
    if (s.kind === "wait") return tag + "wait " + s.duration + "s";
    if (s.kind === "wait_stable") return tag + "wait until stable (≤0.05 °C)";
    if (s.kind === "assert") return tag + "assert " + (s.command_name || "");
    if (s.kind === "log") return tag + "snapshot telemetry";
    return tag + s.kind;
  }

  function startRun() {
    reset();
    setRunning(true);
    setPaused(false);
    startRef.current = performance.now();
    const tick = () => {
      const elapsed = (performance.now() - startRef.current) / 1000;
      const p = Math.min(1, elapsed / totalDuration);
      setProgress(p);
      const sst = {};
      layout.forEach((s) => {
        if (p >= s.end) sst[s.id] = "ok";
        else if (p >= s.start) sst[s.id] = "run";
        else sst[s.id] = "pending";
      });
      setStepStatuses(sst);
      setStepLog((cur) => {
        const out = cur.slice();
        layout.forEach((s) => {
          const existing = out.find((l) => l.stepId === s.id);
          if (p >= s.end && (!existing || existing.status !== "ok")) {
            if (!existing) out.push({ stepId: s.id, status: "ok", msg: msgFor(s, dryRun), t: new Date() });
            else existing.status = "ok";
          } else if (p >= s.start && p < s.end && !existing) {
            out.push({ stepId: s.id, status: "run", msg: msgFor(s, dryRun), t: new Date() });
          }
        });
        return out;
      });
      if (p < 1 && !paused) timerRef.current = requestAnimationFrame(tick);
      if (p >= 1) {
        setRunning(false);
        toast.push({ kind: "ok", title: "Sequence completed", body: script.name });
      }
    };
    tick();
  }

  function abort() {
    if (timerRef.current) cancelAnimationFrame(timerRef.current);
    setRunning(false);
    setPaused(false);
    setStepLog((cur) => cur.concat({ stepId: "abort", status: "fail", msg: "operator abort", t: new Date() }));
    toast.push({ kind: "warn", title: "Sequence aborted", body: script.name });
  }

  function togglePause() {
    setPaused((cur) => {
      if (cur) {
        startRef.current = performance.now() - progress * totalDuration * 1000;
        if (timerRef.current) cancelAnimationFrame(timerRef.current);
        setRunning(true);
        timerRef.current = requestAnimationFrame(() => {
          const loop = () => {
            const elapsed = (performance.now() - startRef.current) / 1000;
            const p = Math.min(1, elapsed / totalDuration);
            setProgress(p);
            const sst = {};
            layout.forEach((s) => {
              if (p >= s.end) sst[s.id] = "ok";
              else if (p >= s.start) sst[s.id] = "run";
              else sst[s.id] = "pending";
            });
            setStepStatuses(sst);
            if (p < 1) timerRef.current = requestAnimationFrame(loop);
            else { setRunning(false); toast.push({ kind: "ok", title: "Sequence completed", body: script.name }); }
          };
          loop();
        });
        return false;
      } else {
        if (timerRef.current) cancelAnimationFrame(timerRef.current);
        return true;
      }
    });
  }

  return (
    <div className="seq">
      <div>
        <div style={{ display: "flex", gap: 6, marginBottom: 10 }}>
          <button className="btn sm">+ Import script</button>
          <button className="btn sm">+ New</button>
        </div>
        <div className="seq-list">
          {SEQ_LIBRARY.map((s) => (
            <div key={s.id} className={"seq-item" + (s.id === activeId ? " active" : "")}
                 onClick={() => { setActiveId(s.id); reset(); }}>
              <div className="name">{s.name}</div>
              <div className="desc">{s.description}</div>
              <div className="row">
                <Chip>{s.steps.length} steps</Chip>
                <Chip>{s.targets.length} target{s.targets.length === 1 ? "" : "s"}</Chip>
                <Chip kind={s.automation === "mecom_lut" ? "accent" : ""}>{s.automation}</Chip>
                {s.safeStart && <Chip kind="ok">safe</Chip>}
              </div>
            </div>
          ))}
        </div>
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 14, minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "baseline", gap: 12, flexWrap: "wrap" }}>
          <h2 style={{ margin: 0, fontSize: 18 }}>{script.name}</h2>
          <span className="mono dim" style={{ fontSize: 12 }}>{script.id}</span>
          <Chip kind="warn">execution endpoint planned</Chip>
          <span className="mono dim" style={{ fontSize: 12 }}>· timeout {script.timeout}s · ~{totalDuration.toFixed(1)}s walk-through</span>
        </div>

        <Gantt script={script} layout={layout} progress={progress} stepStatuses={stepStatuses} />

        <Panel title="Step log" meta={`${stepLog.length} entries`} flush>
          <div className="step-log">
            {stepLog.length === 0 && <div style={{ padding: 14, color: "var(--muted)", fontFamily: "var(--font-mono)", fontSize: 12 }}>No log yet. Press Run.</div>}
            {stepLog.map((l, i) => (
              <div key={i} className={"ln " + l.status}>
                <span className="t">{l.t.toLocaleTimeString()}</span>
                <span className="st">{l.status}</span>
                <span><b style={{ color: "var(--text)" }}>{l.stepId}</b> · <span style={{ color: "var(--muted)" }}>{l.msg}</span></span>
              </div>
            ))}
          </div>
        </Panel>

        <Panel title="Script JSON" right={<button className="btn sm">Copy</button>} flush>
          <pre style={{ margin: 0, padding: 12, background: "#06090f", color: "var(--text-soft)",
                        fontFamily: "var(--font-mono)", fontSize: 11, maxHeight: 220, overflow: "auto",
                        borderBottomLeftRadius: 6, borderBottomRightRadius: 6 }}>
{JSON.stringify({
  id: script.id, name: script.name, timeout: script.timeout + "s",
  steps: script.steps.map((s) => ({
    id: s.id, kind: s.kind, target_id: script.targets[0],
    command_name: s.command_name, arguments: s.arguments,
    await_ack: s.await_ack, duration: s.duration + "s",
    idempotency_key: script.id + ":" + s.id,
  })),
}, null, 2)}
          </pre>
        </Panel>
      </div>

      <div className="run-panel">
        <Panel title="Run controls" right={<Chip kind="warn">planned backend</Chip>} flush>
          <div style={{ padding: 12, display: "flex", flexDirection: "column", gap: 10 }}>
            <div className="planned-note">
              Gateway execution endpoint is planned; this view currently simulates script flow and exports JSON for cmd/mecomrun.
            </div>
            <div className="run-controls">
              {!running && <button className="btn primary" onClick={startRun}>Run</button>}
              {running && !paused && <button className="btn" onClick={togglePause}>Pause</button>}
              {running && paused && <button className="btn primary" onClick={togglePause}>Resume</button>}
              {running && <button className="btn danger" onClick={abort}>Abort</button>}
              {!running && progress > 0 && <button className="btn" onClick={reset}>Reset</button>}
            </div>
            <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12, color: "var(--text-soft)" }}>
              <input type="checkbox" checked={dryRun} onChange={(e) => setDryRun(e.target.checked)} />
              Dry-run · do not send writes
            </label>
            <div className="run-stats">
              <div className="cell">
                <div className="lbl">Progress</div>
                <div className="v">{Math.round(progress * 100)}<span style={{ color: "var(--muted)" }}>%</span></div>
              </div>
              <div className="cell">
                <div className="lbl">Elapsed</div>
                <div className="v">{(progress * totalDuration).toFixed(1)}<span style={{ color: "var(--muted)" }}>s</span></div>
              </div>
              <div className="cell">
                <div className="lbl">Step</div>
                <div className="v">
                  {(() => {
                    const cur = layout.findIndex((s) => stepStatuses[s.id] === "run");
                    return cur >= 0 ? (cur + 1) : "—";
                  })()}<span style={{ color: "var(--muted)" }}>/{layout.length}</span>
                </div>
              </div>
              <div className="cell">
                <div className="lbl">Targets</div>
                <div className="v">{script.targets.length}</div>
              </div>
            </div>
          </div>
        </Panel>

        <Panel title="Targets" flush>
          <div style={{ padding: 12, display: "flex", flexDirection: "column", gap: 6 }}>
            {script.targets.map((t) => {
              const d = MecomAPI.devices().find((x) => x.id === t);
              return (
                <div key={t} style={{ display: "flex", alignItems: "center", gap: 8, fontFamily: "var(--font-mono)", fontSize: 12 }}>
                  <Pill kind={d && d.bound ? "ok" : "bad"}>{t}</Pill>
                  <span className="dim">{d ? d.label : "missing from config"}</span>
                </div>
              );
            })}
          </div>
        </Panel>

        <Panel title="Lease impact" flush>
          <div style={{ padding: 12, fontFamily: "var(--font-mono)", fontSize: 11.5, color: "var(--text-soft)", lineHeight: 1.6 }}>
            This script will <b style={{ color: "var(--accent)" }}>acquire</b> a write lease per target before issuing <code>send_command</code> steps, and <b style={{ color: "var(--accent)" }}>release</b> on completion or abort.
            <hr className="sep" />
            <Chip>holder · {MecomAPI.settings().holder}</Chip>{" "}
            <Chip>ttl · 5m</Chip>
          </div>
        </Panel>
      </div>
    </div>
  );
}

function Gantt({ script, layout, progress, stepStatuses }) {
  return (
    <div className="gantt">
      <div className="ruler">
        <div className="lbl">target / step</div>
        <div className="ticks">
          {[0, 0.25, 0.5, 0.75, 1].map((g, i) => (
            <div key={i} className="tick" style={{ left: `${g * 100}%` }}>
              {(g * (script.steps.reduce((a, s) => a + s.duration, 0))).toFixed(1)}s
            </div>
          ))}
        </div>
      </div>
      {script.targets.map((t, ti) => (
        <div key={t} className="lane">
          <div className="lbl">
            <span style={{ width: 8, height: 8, borderRadius: 2, background: ["#58a6ff", "#3fb950", "#d29922", "#a371f7", "#56d4dd"][ti % 5] }}></span>
            {t}
          </div>
          <div className="track">
            {layout.map((s) => {
              const left = s.start * 100;
              const width = (s.end - s.start) * 100;
              const status = stepStatuses[s.id];
              return (
                <div key={s.id}
                     className={["step",
                       s.kind === "send_command" ? "send" : s.kind === "wait" ? "wait" : s.kind === "wait_stable" ? "stable" : s.kind === "assert" ? "assert" : "wait",
                       status === "ok" ? "ok" : "",
                       status === "run" ? "run" : "",
                       status === "fail" ? "fail" : ""].join(" ")}
                     style={{ left: `calc(${left}% + 2px)`, width: `calc(${width}% - 4px)` }}
                     title={`${s.id} · ${s.kind}`}>
                  <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{s.id}</span>
                </div>
              );
            })}
            <div className="playhead" style={{ left: `${progress * 100}%` }}></div>
          </div>
        </div>
      ))}
    </div>
  );
}

export function PIDAdvisor({ deviceId, onDeviceChange }) {
  useGatewayTick();
  const devices = MecomAPI.devices();
  const settings = MecomAPI.settings();
  const lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
  const youHold = lease && lease.holder === settings.holder;

  const kp = useLiveValue(deviceId, 2020);
  const ti = useLiveValue(deviceId, 2021);
  const td = useLiveValue(deviceId, 2022);

  const [capturing, setCapturing] = useState(false);
  const [response, setResponse] = useState(null);
  const capRef = useRef({ stop: null });
  const toast = useToast();

  function capture() {
    setCapturing(true);
    const dur = 24000;
    const start = Date.now();
    const ts = [], obj = [], target = [];
    const tick = () => {
      const elapsed = Date.now() - start;
      const o = MecomAPI.readValue(deviceId, 1000).value;
      const t = MecomAPI.readValue(deviceId, 3000).value;
      ts.push(elapsed / 1000);
      obj.push(o);
      target.push(t);
      if (elapsed < dur && capRef.current.stop !== "halt") {
        capRef.current.id = setTimeout(tick, 200);
      } else {
        setResponse({ ts, obj, target });
        setCapturing(false);
        toast.push({ kind: "ok", title: "Step response captured", body: `${ts.length} samples · ${dur / 1000}s` });
      }
    };
    tick();
  }
  function stopCapture() {
    capRef.current.stop = "halt";
    if (capRef.current.id) clearTimeout(capRef.current.id);
    setCapturing(false);
  }

  const analysis = useMemo(() => analyzeStep(response), [response]);
  const suggested = useMemo(() => {
    if (!analysis) return null;
    return suggestGains({ kp: kp.value, ti: ti.value, td: td.value }, analysis);
  }, [analysis, kp.value, ti.value, td.value]);

  function applyGains() {
    toast.push({
      kind: "warn",
      title: "PID apply planned",
      body: "Advisor can observe and suggest; backend gain writeback is intentionally disabled in this prototype.",
    });
  }

  return (
    <div className="pid">
      <div className="left">
        <Panel title="Step response capture" right={
          <>
            <select className="field" style={{ width: 160 }} value={deviceId} onChange={(e) => onDeviceChange(e.target.value)}>
              {devices.map((d) => <option key={d.id} value={d.id}>{d.label}</option>)}
            </select>
            {!capturing && <button className="btn primary sm" onClick={capture}>Capture 24s</button>}
            {capturing && <button className="btn sm" onClick={stopCapture}>Stop</button>}
          </>
        }>
          {!response && !capturing && (
            <div style={{ padding: 24, textAlign: "center", color: "var(--muted)" }}>
              <div style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>Press <b style={{ color: "var(--accent)" }}>Capture 24s</b> to record an open-loop step from current target.</div>
              <div style={{ marginTop: 8, fontSize: 11 }}>The advisor recommends ZN-style gain deltas from rise time, overshoot, and steady-state error.</div>
            </div>
          )}
          {(capturing || response) && (
            <StepResponseChart response={response} live={capturing && response === null} deviceId={deviceId} />
          )}
          {analysis && (
            <div className="anno">
              <div className="cell">
                <div className="lbl">Rise time (10→90%)</div>
                <div className={"v " + (analysis.riseTime > 6 ? "warn" : "ok")}>{analysis.riseTime.toFixed(1)}<span style={{ color: "var(--muted)" }}>s</span></div>
              </div>
              <div className="cell">
                <div className="lbl">Overshoot</div>
                <div className={"v " + (Math.abs(analysis.overshoot) > 15 ? "bad" : Math.abs(analysis.overshoot) > 5 ? "warn" : "ok")}>{analysis.overshoot.toFixed(1)}<span style={{ color: "var(--muted)" }}>%</span></div>
              </div>
              <div className="cell">
                <div className="lbl">Settling (±2%)</div>
                <div className={"v " + (analysis.settling > 12 ? "warn" : "ok")}>{analysis.settling.toFixed(1)}<span style={{ color: "var(--muted)" }}>s</span></div>
              </div>
              <div className="cell">
                <div className="lbl">Steady-state err</div>
                <div className={"v " + (Math.abs(analysis.sse) > 0.05 ? "warn" : "ok")}>{analysis.sse.toFixed(3)}<span style={{ color: "var(--muted)" }}>°C</span></div>
              </div>
            </div>
          )}
        </Panel>
      </div>

      <div className="right">
        <Panel title="Current gains" meta="MeCom #2020 / #2021 / #2022">
          <div className="gain-row">
            <div className="gain"><div className="lbl">Kp</div><div className="now">{kp.value != null ? kp.value.toFixed(2) : "—"}</div></div>
            <div className="gain"><div className="lbl">Ti</div><div className="now">{ti.value != null ? ti.value.toFixed(2) : "—"} <span className="dim">s</span></div></div>
            <div className="gain"><div className="lbl">Td</div><div className="now">{td.value != null ? td.value.toFixed(2) : "—"} <span className="dim">s</span></div></div>
          </div>
        </Panel>

        <Panel title="Advisor" right={<><Chip kind="warn">apply planned</Chip>{suggested && <Chip kind="accent">Ziegler-Nichols heuristic</Chip>}</>}>
          {!suggested && (
            <div className="dim mono" style={{ fontSize: 12, lineHeight: 1.6 }}>
              Capture a step response to see recommendations. The advisor estimates process gain & dead-time from rise/settling and proposes gain deltas.
            </div>
          )}
          {suggested && (
            <>
              <div className="gain-row">
                <div className="gain"><div className="lbl">Kp</div><div><span className="now">{kp.value?.toFixed(2)}</span><span className="arrow">→</span><span className="new">{suggested.kp.toFixed(2)}</span></div></div>
                <div className="gain"><div className="lbl">Ti</div><div><span className="now">{ti.value?.toFixed(2)}</span><span className="arrow">→</span><span className="new">{suggested.ti.toFixed(2)}</span></div></div>
                <div className="gain"><div className="lbl">Td</div><div><span className="now">{td.value?.toFixed(2)}</span><span className="arrow">→</span><span className="new">{suggested.td.toFixed(2)}</span></div></div>
              </div>
              <div className="rationale" style={{ marginTop: 10 }}>
                {suggested.rationale.map((r, i) => <div key={i}>· {r}</div>)}
              </div>
              <div style={{ display: "flex", gap: 6, marginTop: 10 }}>
                <button className="btn primary sm" onClick={applyGains}>Apply planned</button>
                <button className="btn sm" onClick={capture}>Re-capture</button>
              </div>
            </>
          )}
        </Panel>

        <Panel title="Lease" flush>
          <div style={{ padding: 12 }}>
            {lease ? <Pill kind={youHold ? "info" : "warn"}>{youHold ? "you hold" : "held by " + lease.holder}</Pill> : <Pill kind="ok">free</Pill>}
          </div>
        </Panel>
      </div>
    </div>
  );
}

function StepResponseChart({ response, live, deviceId }) {
  const [, force] = useState(0);
  useEffect(() => {
    if (!live) return;
    const id = setInterval(() => force((n) => (n + 1) % 1e9), 250);
    return () => clearInterval(id);
  }, [live]);
  const tile = useMemo(() => buildStepResponseGraphTile(response, deviceId), [response, deviceId]);
  return (
    <div style={{ paddingTop: 8 }}>
      {tile ? <MultiChart tile={tile} height={260} timeWindowMs={tile.time_window_ms} /> : null}
      {live && <div style={{ padding: 8, color: "var(--accent)", fontFamily: "var(--font-mono)", fontSize: 11 }}>● capturing…</div>}
    </div>
  );
}

function buildStepResponseGraphTile(response, deviceId) {
  if (!response || !response.ts || response.ts.length === 0) return null;
  const endpoint = "pid-advisor-capture";
  const idDevice = deviceId || "selected";
  const lastSec = response.ts[response.ts.length - 1] || 24;
  const baseMs = Date.now() - lastSec * 1000;
  const now = new Date().toISOString();
  const makeHistory = (values) => ({
    ts: response.ts.map((seconds) => Math.round(baseMs + seconds * 1000)),
    v: values,
    q: values.map(() => "ok"),
  });
  const makeSeries = ({ paramId, signalId, label, values, role }) => {
    const history = makeHistory(values);
    const roleMeta = seriesRoleMeta(role);
    const provenance = `device=${idDevice} param=${paramId} instance=1 endpoint=${endpoint}`;
    return {
      id: `pid-${idDevice}-1-${paramId}`,
      series_id: `pid-${idDevice}-1-${paramId}`,
      target_id: `${signalId}@${idDevice}/1`,
      label,
      full_label: `Thermal / Step response / ${label}`,
      color: MecomAPI.colorForRole(role),
      unit: "degC",
      role,
      role_rank: roleMeta.rank,
      kind: "continuous",
      provenance,
      source_ref: provenance,
      source: { device_id: idDevice, instance: 1, param_id: paramId, signal_id: signalId, endpoint },
      history,
      axis_id: "temperature_c",
      points: history.ts.map((ts, idx) => ({ timestamp: new Date(ts).toISOString(), value: history.v[idx], quality: "ok", provenance })),
    };
  };
  return {
    schema_version: "signalforge.graph_tile.v1",
    renderer: CANONICAL_TILE_RENDERER,
    kind: "timeseries",
    id: `pid-step-response-${idDevice}`,
    tile_id: `pid-step-response-${idDevice}`,
    card_id: `pid-step-response-${idDevice}`,
    level: "analysis",
    t0: new Date(baseMs).toISOString(),
    t1: new Date(baseMs + lastSec * 1000).toISOString(),
    title: "PID step response",
    generated_at: now,
    time_window_ms: lastSec * 1000,
    axes: [{ id: "temperature_c", unit: "degC", side: "left", label: "Temperature" }],
    bands: [],
    markers: [],
    events: [],
    diagnostics: { status: "ok", renderer: CANONICAL_TILE_RENDERER, source: "meerstetter-go.pid-advisor", mode: "capture", series_count: 2, point_count: response.ts.length * 2, decimation: "none" },
    provenance: { source: "meerstetter-go.pid-advisor", source_family: "mecom", generated_at: now, synthetic: true },
    series: [
      makeSeries({ paramId: 1000, signalId: "object_temp_c", label: "Object T", values: response.obj, role: "actual" }),
      makeSeries({ paramId: 3000, signalId: "target_temp_c", label: "Target T", values: response.target, role: "cmd" }),
    ],
  };
}

function analyzeStep(response) {
  if (!response || response.obj.length < 5) return null;
  const obj = response.obj.filter((v) => typeof v === "number");
  const tgt = response.target;
  const t0 = obj[0];
  const tEnd = tgt[tgt.length - 1];
  const step = tEnd - t0;
  if (Math.abs(step) < 0.5) return { riseTime: 0, overshoot: 0, settling: 0, sse: 0 };
  const t10 = t0 + 0.1 * step;
  const t90 = t0 + 0.9 * step;
  let i10 = obj.findIndex((v) => (step > 0 ? v >= t10 : v <= t10));
  let i90 = obj.findIndex((v) => (step > 0 ? v >= t90 : v <= t90));
  if (i10 < 0) i10 = 0;
  if (i90 < 0) i90 = obj.length - 1;
  const riseTime = response.ts[i90] - response.ts[i10];
  const peak = step > 0 ? Math.max(...obj) : Math.min(...obj);
  const overshoot = ((peak - tEnd) / step) * 100;
  const band = Math.abs(step) * 0.02;
  let settleIdx = obj.length - 1;
  for (let i = obj.length - 1; i >= 0; i--) {
    if (Math.abs(obj[i] - tEnd) > band) { settleIdx = i + 1; break; }
  }
  const settling = response.ts[settleIdx] || 0;
  const tail = obj.slice(-10);
  const sse = (tail.reduce((a, b) => a + b, 0) / tail.length) - tEnd;
  return { riseTime, overshoot, settling, sse };
}

function suggestGains(current, a) {
  const kp = current.kp || 18;
  const ti = current.ti || 80;
  const td = current.td || 1.5;
  const rationale = [];
  let dKp = 1, dTi = 1, dTd = 1;
  if (a.overshoot > 15) { dKp *= 0.75; rationale.push(`Overshoot ${a.overshoot.toFixed(1)}% is high → reduce Kp by 25%.`); }
  else if (a.riseTime > 8) { dKp *= 1.2; rationale.push(`Rise time ${a.riseTime.toFixed(1)}s is slow → raise Kp by 20%.`); }
  if (a.settling > 12) { dTd *= 1.25; rationale.push(`Settling ${a.settling.toFixed(1)}s long → add 25% derivative action.`); }
  if (Math.abs(a.sse) > 0.05) { dTi *= 0.85; rationale.push(`Steady-state error ${a.sse.toFixed(3)}°C → shorten Ti by 15%.`); }
  if (rationale.length === 0) rationale.push("Response within tolerance — no changes recommended.");
  return { kp: +(kp * dKp).toFixed(3), ti: +(ti * dTi).toFixed(3), td: +(td * dTd).toFixed(3), rationale };
}

const ARCHIVE_MANIFEST = {
  streams: [
    { name: "telemetry_samples", purpose: "Decoded MeCom/CAN values after catalogue mapping, reduction, and freshness tagging.", grain: "one row per target value sample", primary_key: ["seq"], fields: ["seq", "time", "target_id", "device_id", "parameter", "value", "unit", "quality", "source_path"] },
    { name: "can_frames", purpose: "Raw SocketCAN receive evidence from the PiXtend route, reconciled across RAM and flash rings.", grain: "one row per received CAN frame", primary_key: ["time", "interface", "id", "dlc", "data_hex"], fields: ["seq", "time", "interface", "id", "dlc", "data_hex", "source"] },
    { name: "command_events", purpose: "Sequencer and write-path receipts, including lease-gated accepts and rejects.", grain: "one row per command state transition", primary_key: ["seq"], fields: ["seq", "time", "target_id", "write_path", "status", "owner", "message"] },
    { name: "object_dictionary_snapshots", purpose: "Catalogue rows used to interpret telemetry and expose writable paths.", grain: "one row per discovered target/parameter/instance", primary_key: ["target_id", "parameter", "instance"], fields: ["generated_at", "target_id", "device_id", "type", "parameter", "instance", "read_path", "write_path", "writable"] },
    { name: "graph_wall_assignments", purpose: "Operator graph-wall tile layout and semantic target binding.", grain: "one row per tile assignment", primary_key: ["wall_id", "tile_id"], fields: ["generated_at", "wall_id", "tile_id", "target_id", "kind", "section", "position"] },
  ],
};

export function ArchiveView() {
  useGatewayTick();
  const [streamSel, setStreamSel] = useState({});
  const [format, setFormat] = useState("ndjson");
  const [range, setRange] = useState("15m");
  const [importedTile, setImportedTile] = useState(null);
  const toast = useToast();
  const formats = [
    { id: "ndjson", label: "NDJSON" },
    { id: "arrow_ipc", label: "Arrow IPC" },
    { id: "hdf5", label: "HDF5" },
  ];
  const ranges = ["5m", "15m", "1h", "6h", "24h"];

  function toggleStream(name) { setStreamSel((cur) => ({ ...cur, [name]: !cur[name] })); }
  function exportNow() {
    const picked = Object.entries(streamSel).filter(([, v]) => v).map(([k]) => k);
    if (!picked.length) { toast.push({ kind: "warn", title: "Pick at least one stream" }); return; }
    
    const url = `/api/log/export?format=${format}&range=${range}`;
    window.open(url, "_blank");
    
    toast.push({ kind: "ok", title: "Export started", body: `${picked.length} stream(s) · ${format} · range ${range}` });
  }

  async function loadRange() {
    try {
      const tileId = "tec-all"; // Default or selectable
      const t1 = new Date();
      const t0 = new Date(t1.getTime() - parseRange(range));
      const res = await fetch(`/api/graph/tiles/${tileId}/range?t0=${t0.toISOString()}&t1=${t1.toISOString()}`);
      if (!res.ok) throw new Error(`fetch failed: ${res.status}`);
      const data = await res.json();
      setImportedTile(data);
      toast.push({ kind: "ok", title: "Range loaded", body: `Loaded ${data.series.length} series for ${range}` });
    } catch (err) {
      toast.push({ kind: "bad", title: "Load failed", body: err.message });
    }
  }

  function parseRange(r) {
    const unit = r.slice(-1);
    const val = parseInt(r.slice(0, -1));
    if (unit === "m") return val * 60 * 1000;
    if (unit === "h") return val * 60 * 60 * 1000;
    if (unit === "d") return val * 24 * 60 * 60 * 1000;
    return 15 * 60 * 1000;
  }

  function handleFileImport(e) {
    const file = e.target.files[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = async (ev) => {
      try {
        const res = await fetch("/api/log/import", {
          method: "POST",
          body: ev.target.result,
          headers: { "Content-Type": "application/vnd.apache.arrow.stream" }
        });
        if (!res.ok) throw new Error(`import failed: ${res.status}`);
        const data = await res.json();
        toast.push({ kind: "ok", title: "Import successful", body: `Imported ${data.imported_samples} samples from ${file.name}` });
        // After import, try to load the range to see the data
        loadRange();
      } catch (err) {
        toast.push({ kind: "bad", title: "Import failed", body: err.message });
      }
    };
    reader.readAsArrayBuffer(file);
  }

  return (
    <div className="arc">
      <div className="arc-grid">
        <div className="arc-left">
          <Panel title="Campaign viewer" meta="Explore imported or historical data" flush>
             <div style={{ padding: 12 }}>
                <div style={{ display: "flex", gap: 10, marginBottom: 16 }}>
                    <label className="btn sm primary">
                       Import .arrow / .ndjson
                       <input type="file" style={{ display: "none" }} onChange={handleFileImport} accept=".arrow,.ndjson" />
                    </label>
                    <div style={{ flex: 1 }}></div>
                    <select className="field sm" style={{ width: 100 }} value={range} onChange={(e) => setRange(e.target.value)}>
                       {ranges.map((r) => <option key={r} value={r}>{r}</option>)}
                    </select>
                    <button className="btn sm" onClick={loadRange}>Load Range</button>
                </div>
                <div className="campaign-placeholder">
                   {importedTile ? (
                      <HeroGraph wall={{ wall_id: "campaign", label: "Imported Campaign", series: [] }} role="temp" tile={importedTile} height={400} />
                   ) : (
                      <div style={{ padding: 48, textAlign: "center", color: "var(--muted)", border: "1px dashed var(--border)", borderRadius: 6 }}>
                         <p>Select a historical range or import a campaign file to visualize data here.</p>
                         <p style={{ fontSize: 11 }}>Supports SignalForge Arrow IPC and NDJSON telemetry streams.</p>
                      </div>
                   )}
                </div>
             </div>
          </Panel>
        </div>

        <div className="arc-right">
          <Panel title="Export archive" right={<span className="dim mono" style={{ fontSize: 11 }}>route GET /api/log/export</span>}>
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr auto", gap: 10, alignItems: "end" }}>
          <div>
            <label className="lbl">Range</label>
            <select className="field" value={range} onChange={(e) => setRange(e.target.value)}>
              {ranges.map((r) => <option key={r} value={r}>last {r}</option>)}
            </select>
          </div>
          <div>
            <label className="lbl">Format</label>
            <div className="fmt-row">
              {formats.map((f) => (
                <button key={f.id} className={"fmt " + (format === f.id ? "on" : "") + (f.planned ? " planned" : "")} onClick={() => setFormat(f.id)}>{f.label}</button>
              ))}
            </div>
          </div>
          <div>
            <label className="lbl">Filter targets</label>
            <input className="field" placeholder="tec-* (default: all)" />
          </div>
          <button className="btn primary" onClick={exportNow}>Export now</button>
        </div>
      </Panel>

      <Panel title="Streams" meta={`${ARCHIVE_MANIFEST.streams.length} contracts`}>
        <div className="stream-row">
          {ARCHIVE_MANIFEST.streams.map((s) => (
            <div key={s.name} className="stream">
              <h4>{s.name}</h4>
              <div className="desc">{s.purpose}</div>
              <div className="dim mono small">grain · {s.grain}</div>
              <div className="dim mono small">pk · {s.primary_key.join(", ")}</div>
              <div className="fields">{s.fields.join(", ")}</div>
              <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12 }}>
                <input type="checkbox" checked={!!streamSel[s.name]} onChange={() => toggleStream(s.name)} />
                Include in export
              </label>
            </div>
          ))}
        </div>
      </Panel>

      <Panel title="Recent exports" meta="last 24h" flush>
        <div className="feed">
          {[
            { t: "10:14:22", name: "telemetry_samples,command_events", fmt: "arrow_ipc", size: "3.4 MB", status: "ok" },
            { t: "09:02:11", name: "can_frames", fmt: "ndjson", size: "812 kB", status: "ok" },
            { t: "07:48:01", name: "telemetry_samples", fmt: "ndjson", size: "2.1 MB", status: "ok" },
          ].map((e, i) => (
            <div key={i} className="ev">
              <span className="t">{e.t}</span>
              <span className="dev">{e.fmt}</span>
              <span className="msg">{e.name} · {e.size}</span>
              <Chip kind="ok">{e.status}</Chip>
            </div>
          ))}
        </div>
      </Panel>
    </div></div></div>
  );
}

export function SettingsView() {
  useGatewayTick();
  const [s, setS] = useState(() => MecomAPI.settings());
  const [gatewaySettings, setGatewaySettings] = useState(null);
  const [gatewaySettingsError, setGatewaySettingsError] = useState("");
  const [aliasJson, setAliasJson] = useState(() => JSON.stringify(MecomAPI.exportChannelAliases?.() || { entries: [] }, null, 2));
  const [aliasStatus, setAliasStatus] = useState("");
  function set(patch) { const next = MecomAPI.saveSettings(patch); setS(next); }
  useEffect(() => {
    let alive = true;
    MecomAPI.gatewaySettings?.()
      .then((data) => {
        if (!alive) return;
        setGatewaySettings(data || null);
        setGatewaySettingsError("");
      })
      .catch((err) => {
        if (!alive) return;
        setGatewaySettings(null);
        setGatewaySettingsError(err?.message || String(err));
      });
    return () => { alive = false; };
  }, [s.gateway, s.bridgeDefaultTransport, s.bridgeFallbackTransport, s.bridgeRouteSelection, s.bridgeAddressZero]);
  const bridgePolicy = gatewaySettings?.bridge || {
    default_transport: s.bridgeDefaultTransport,
    fallback_transport: s.bridgeFallbackTransport,
    route_selection: s.bridgeRouteSelection,
    address_zero: s.bridgeAddressZero,
  };
  const routeRows = Array.isArray(gatewaySettings?.routes) ? gatewaySettings.routes : [];
  function SegmentRow({ label, field, value, options }) {
    return (
      <div>
        <label className="lbl">{label}</label>
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
          {options.map((option) => (
            <button
              key={option}
              type="button"
              className={"btn sm " + (value === option ? "primary" : "")}
              onClick={() => set({ [field]: option })}
            >
              {option}
            </button>
          ))}
        </div>
      </div>
    );
  }
  function refreshAliasExport() {
    setAliasJson(JSON.stringify(MecomAPI.exportChannelAliases?.() || { entries: [] }, null, 2));
    setAliasStatus("refreshed");
  }
  function importAliasOverlay() {
    try {
      const saved = MecomAPI.importChannelAliases?.(aliasJson);
      setAliasJson(JSON.stringify(saved || MecomAPI.exportChannelAliases?.() || { entries: [] }, null, 2));
      setAliasStatus("imported");
    } catch (err) {
      setAliasStatus(`import failed: ${err?.message || err}`);
    }
  }
  return (
    <div className="set">
      <Panel title="Gateway connection">
        <div style={{ display: "grid", gap: 10 }}>
          <div>
            <label className="lbl">Gateway URL</label>
            <input className="field" value={s.gateway} onChange={(e) => set({ gateway: e.target.value })}
                   placeholder="https://mecomgo.jmeyer.space or blank = same-origin" />
            <div className="dim small mono" style={{ marginTop: 4 }}>
              Leave blank when served by <code>mecomgw</code>; the console uses same-origin <code>/api</code>. Mock data is only the fallback for local file previews.
            </div>
          </div>
          <div>
            <label className="lbl">Holder identifier (lease)</label>
            <input className="field" value={s.holder} onChange={(e) => set({ holder: e.target.value })} />
            <div className="dim small mono" style={{ marginTop: 4 }}>
              Used as the <code>holder</code> in <code>POST /api/devices/&#123;id&#125;/lease</code>.
            </div>
          </div>
        </div>
      </Panel>

      <Panel title="Bridge routing" meta={gatewaySettingsError ? "offline preference" : "gateway policy"}>
        <div style={{ display: "grid", gap: 12 }}>
          <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
            <Pill kind="ok">default · {bridgePolicy.default_transport || "serial"}</Pill>
            <Pill kind={(bridgePolicy.fallback_transport || "can") === "disabled" ? "warn" : "ok"}>
              fallback · {bridgePolicy.fallback_transport || "can"}
            </Pill>
            <Pill kind="info">{bridgePolicy.route_selection || "fixed-preference"}</Pill>
            {gatewaySettingsError && <Chip kind="warn">settings endpoint unavailable</Chip>}
          </div>
          <SegmentRow
            label="Default bus"
            field="bridgeDefaultTransport"
            value={s.bridgeDefaultTransport || bridgePolicy.default_transport || "serial"}
            options={["serial", "can"]}
          />
          <SegmentRow
            label="Fallback bus"
            field="bridgeFallbackTransport"
            value={s.bridgeFallbackTransport || bridgePolicy.fallback_transport || "can"}
            options={["can", "serial", "disabled"]}
          />
          <SegmentRow
            label="Route selection"
            field="bridgeRouteSelection"
            value={s.bridgeRouteSelection || bridgePolicy.route_selection || "fixed-preference"}
            options={["fixed-preference", "dynamic"]}
          />
          <SegmentRow
            label="Address-zero discovery"
            field="bridgeAddressZero"
            value={s.bridgeAddressZero || bridgePolicy.address_zero || "default-device"}
            options={["default-device", "route-order", "disabled"]}
          />
          <div style={{ display: "grid", gap: 6, maxHeight: 190, overflow: "auto" }}>
            {routeRows.length === 0 && <div className="dim small">No gateway route projection is available yet.</div>}
            {routeRows.map((route, i) => (
              <div
                key={`${route.device_id || "device"}:${route.endpoint || "route"}:${i}`}
                style={{ border: "1px solid var(--border)", borderRadius: 8, padding: 8, display: "grid", gap: 4 }}
              >
                <div style={{ display: "flex", gap: 6, flexWrap: "wrap", alignItems: "center" }}>
                  <Chip kind={route.active ? "accent" : "warn"}>{route.active ? "active" : route.role || "standby"}</Chip>
                  <span className="mono">{route.device_id}</span>
                  <span className="dim mono small">addr {route.address}</span>
                </div>
                <div className="dim mono small" style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {(route.transport || "route")} · {route.endpoint}
                </div>
              </div>
            ))}
          </div>
          {gatewaySettingsError && (
            <div className="dim small mono">
              Local preferences are still stored in this browser; runtime bridge changes require the gateway settings endpoint to be reachable.
            </div>
          )}
        </div>
      </Panel>

      <Panel title="Workspace">
        <div style={{ display: "grid", gap: 10 }}>
          <div>
            <label className="lbl">Theme</label>
            <Chip kind="accent">GitHub dark (only option in this wave)</Chip>
          </div>
          <div>
            <label className="lbl">Density</label>
            <div style={{ display: "flex", gap: 6 }}>
              <button className={"btn sm " + (document.documentElement.dataset.density !== "compact" ? "primary" : "")}
                      onClick={() => document.documentElement.dataset.density = "comfortable"}>Comfortable</button>
              <button className={"btn sm " + (document.documentElement.dataset.density === "compact" ? "primary" : "")}
                      onClick={() => document.documentElement.dataset.density = "compact"}>Compact</button>
            </div>
          </div>
          <div>
            <label className="lbl">Reset local layout</label>
            <button className="btn sm" onClick={() => {
              Object.keys(localStorage).filter((k) => k.startsWith("mecomgw.pins.") || k.startsWith("mecomgw.cards.")).forEach((k) => localStorage.removeItem(k));
              location.reload();
            }}>Reset pinned params & input cards</button>
          </div>
        </div>
      </Panel>

      <Panel title="Channel alias overlay" meta="SignalForge semantic overlay">
        <div style={{ display: "grid", gap: 8 }}>
          <div className="dim small">
            User aliases and fixture notes are stored as a separate overlay. The canonical MeCom signal catalogue and channel role defaults stay unchanged.
          </div>
          <textarea
            className="field mono small"
            style={{ minHeight: 180, resize: "vertical" }}
            value={aliasJson}
            onChange={(e) => setAliasJson(e.target.value)}
            spellCheck={false}
          />
          <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
            <button className="btn sm" onClick={refreshAliasExport}>Refresh export</button>
            <button className="btn sm primary" onClick={importAliasOverlay}>Import overlay JSON</button>
            {aliasStatus && <Chip>{aliasStatus}</Chip>}
          </div>
        </div>
      </Panel>

      <Panel title="Reference">
        <div className="mono small" style={{ lineHeight: 1.7, color: "var(--text-soft)" }}>
          <div>module · <code>github.com/egidinas/meerstetter-go</code></div>
          <div>handoff · <code>docs/gateway/HANDOFF.md</code></div>
          <div>openapi · <code>docs/gateway/openapi.yaml</code></div>
          <div>types · <code>docs/gateway/types.d.ts</code></div>
          <div>demo · <code>docs/gateway/demo/index.html</code></div>
        </div>
      </Panel>

      <Panel title="Endpoints used by this UI">
        <table style={{ width: "100%", fontFamily: "var(--font-mono)", fontSize: 11.5 }}>
          <thead>
            <tr style={{ color: "var(--muted)" }}>
              <th style={{ textAlign: "left", padding: "4px 0" }}>Method</th>
              <th style={{ textAlign: "left" }}>Path</th>
              <th style={{ textAlign: "left" }}>Used in</th>
            </tr>
          </thead>
          <tbody style={{ color: "var(--text-soft)" }}>
            {[
              ["GET", "/api/healthz", "topbar"],
              ["GET", "/api/settings", "bridge routing"],
              ["GET", "/api/devices", "fleet, workspace"],
              ["GET", "/api/catalogue", "discovery tree"],
              ["GET", "/api/leases", "fleet, workspace"],
              ["POST", "/api/devices/{id}/lease", "input cards, PID advisor"],
              ["DELETE", "/api/devices/{id}/lease", "workspace header"],
              ["GET", "/api/devices/{id}/read", "tree, sparklines"],
              ["GET", "/api/devices/{id}/poll", "live charts (SSE)"],
              ["POST", "/api/devices/{id}/write", "input cards, sequencer, PID apply"],
            ].map((r, i) => (
              <tr key={i}><td>{r[0]}</td><td>{r[1]}</td><td className="dim">{r[2]}</td></tr>
            ))}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}
