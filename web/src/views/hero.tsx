// @ts-nocheck
import React, { useState } from "react";
import { MecomAPI } from "../api/mecom";
import {
  Chip,
  MultiChart,
  SemanticValuePopup,
  WriteLifecycleTrace,
  useToast,
  categorizeError,
  useLiveValue,
  useGatewayTick,
} from "../components/atoms";
import { renderSeriesFromGraphTile } from "../lib/series";
import { channelColor } from "./assignments";

const PARAM_OBJECT_TEMPERATURE = 1000;
const PARAM_SINK_TEMPERATURE = 1001;
const PARAM_STABLE_STATE = 1200;
const PARAM_OUTPUT_ENABLE = 2010;
const PARAM_OUTPUT_CURRENT = 1020;
const PARAM_OUTPUT_VOLTAGE = 1021;
const PARAM_OUTPUT_POWER = 1022;
const PARAM_FIXED_CURRENT = 2020;
const PARAM_FIXED_VOLTAGE = 2021;
const PARAM_CURRENT_LIMIT = 2030;
const PARAM_VOLTAGE_LIMIT = 2031;
const PARAM_OUTPUT_MODE = 2040;
const PARAM_CASCADE_ENABLE = 53120;
const PARAM_CASCADE_TARGET_TEMPERATURE = 53123;
const PARAM_TARGET_TEMPERATURE = 3000;

function lifecycleKey(deviceId, instance, param) {
  return `${deviceId}/${instance || 1}/${param}`;
}

function signalFor(param, instance, fallback = {}) {
  return MecomAPI.paramById(param, instance) || MecomAPI.paramById(param) || {
    id: param,
    instance: instance || 1,
    unit: "",
    name: `Parameter ${param}`,
    ...fallback,
  };
}

function liveSnapshot(deviceId, param, instance) {
  const value = MecomAPI.readValue(deviceId, param, instance);
  return {
    value: value && value.value !== undefined ? value.value : null,
    quality: value && value.quality ? value.quality : "missing",
  };
}

function commandTitle(param) {
  if (param === PARAM_FIXED_VOLTAGE) return "Fixed voltage command set";
  if (param === PARAM_CURRENT_LIMIT) return "Current limit set";
  if (param === PARAM_TARGET_TEMPERATURE) return "Control-loop target set";
  if (param === PARAM_OUTPUT_ENABLE) return "Output stage updated";
  return `Parameter ${param} written`;
}

function axisBadgeLabel(role, tile) {
  if (role === "temp") return "Temperature [°C]";
  const units = Array.from(new Set((tile?.series || []).map((s) => String(s.unit || "")).filter(Boolean)));
  const labels = [];
  if (units.includes("W")) labels.push("Power [W]");
  if (units.includes("V")) labels.push("Voltage [V]");
  if (units.includes("A")) labels.push("Current [A]");
  return labels.length ? labels.join(" / ") : "Power [W]";
}

export function HeroGraph({ wall, role, tile, height = 280, live = true, children }) {
  useGatewayTick();
  const renderedSeries = renderSeriesFromGraphTile(tile);
  const [hiddenSeries, setHiddenSeries] = useState([]);
  function toggleSeries(key) {
    setHiddenSeries((cur) => cur.includes(key) ? cur.filter((item) => item !== key) : cur.concat(key));
  }
  return (
    <div className={"hero" + (live ? " live" : "")}>
      <div className="hero-head">
        <div className="title-grp">
          <span className="live-dot"></span>
          <h2>{wall.label}</h2>
          <span className="sub">· {renderedSeries.length} tile series · live</span>
        </div>
        <div className="badge-row">
          <Chip kind="accent">{axisBadgeLabel(role, tile)}</Chip>
          <Chip>shared timeline</Chip>
        </div>
      </div>
      <div className="hero-chart-row">
        <div className="hero-plot">
          {renderedSeries.length === 0 ? (
            <div style={{ padding: 36, textAlign: "center", color: "var(--muted)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
              No signals assigned. Add from the Signal Dictionary →
            </div>
          ) : (
            <MultiChart tile={tile} height={height} hiddenSeries={hiddenSeries} fill minHeight={220} />
          )}
        </div>
        <div className="hero-legend">
          {renderedSeries.map((s) => {
            const off = hiddenSeries.includes(s.key);
            const last = s.history.v[s.history.v.length - 1];
            return (
              <span key={s.key} className={"item " + (off ? "off" : "")} onClick={() => toggleSeries(s.key)} title={s.fullLabel || "Click to show/hide this tile series"}>
                <span className="sw" style={{ background: s.color }}></span>
                <span className="series-label">{s.label}</span>
                <span className="cur">{MecomAPI.formatWithUnit(last, s.unit, s.paramId)}</span>
              </span>
            );
          })}
        </div>
      </div>
      {children}
    </div>
  );
}

export function TempSettingsTable({ channels, holderId }) {
  const toast = useToast();
  const [staged, setStaged] = useState({});
  const [busy, setBusy] = useState({});
  const [traces, setTraces] = useState({});
  async function commit(deviceId, instance, value) {
    const key = `${deviceId}/${instance}`;
    const traceKey = lifecycleKey(deviceId, instance, PARAM_TARGET_TEMPERATURE);
    setBusy((b) => ({ ...b, [key]: true }));
    try {
      const signal = signalFor(PARAM_TARGET_TEMPERATURE, instance, { unit: "degC", name: "Control-loop target temperature" });
      const numeric = Number(value);
      const current = liveSnapshot(deviceId, PARAM_TARGET_TEMPERATURE, instance).value;
      setTraces((t) => ({
        ...t,
        [traceKey]: {
          phase: "write",
          status: "submitted",
          at: Date.now(),
          paramId: PARAM_TARGET_TEMPERATURE,
          unit: signal.unit || "degC",
          commandName: MecomAPI.commandNameFor(signal),
          currentValue: current,
          prospectiveValue: numeric,
          submittedValue: numeric,
        },
      }));
      let lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
      if (!lease || lease.holder !== holderId) { lease = await MecomAPI.acquireLease(deviceId, holderId, "5m"); }
      const body = await MecomAPI.write(deviceId, { name: MecomAPI.commandNameFor(signal), arguments: { param: PARAM_TARGET_TEMPERATURE, instance, value: numeric } }, lease.token);
      const confirmed = liveSnapshot(deviceId, PARAM_TARGET_TEMPERATURE, instance).value;
      setTraces((t) => ({
        ...t,
        [traceKey]: {
          ...(t[traceKey] || {}),
          phase: "done",
          status: (body && body.status) || "completed",
          at: Date.now(),
          confirmedValue: confirmed !== null ? confirmed : numeric,
        },
      }));
      toast.push({ kind: "ok", title: "Target set", body: `${deviceId}/${instance} → ${value} °C` });
      setStaged((s) => ({ ...s, [key]: "" }));
    } catch (err) {
      setTraces((t) => ({
        ...t,
        [traceKey]: {
          ...(t[traceKey] || {}),
          phase: "error",
          status: "failed",
          at: Date.now(),
          error: err.message || String(err),
        },
      }));
      const c = categorizeError(err);
      toast.push({ kind: c.kind, title: c.cat.toUpperCase(), body: err.message });
    } finally {
      setBusy((b) => ({ ...b, [key]: false }));
    }
  }
  async function toggleOutput(deviceId, instance, cur) {
    try {
      let lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
      if (!lease || lease.holder !== holderId) { lease = await MecomAPI.acquireLease(deviceId, holderId, "5m"); }
      await MecomAPI.write(deviceId, { name: "write_int32", arguments: { param: 2010, instance, value: cur ? 0 : 1 } }, lease.token);
      toast.push({ kind: "ok", title: cur ? "Output OFF" : "Output ON", body: `${deviceId}/${instance}` });
    } catch (err) {
      const c = categorizeError(err);
      toast.push({ kind: c.kind, title: c.cat.toUpperCase(), body: err.message });
    }
  }
  return (
    <div className="hero-settings-wrap">
      <table className="hero-settings">
        <thead>
          <tr>
            <th style={{ width: "22%" }}>Channel</th>
            <th>Target [°C]</th><th>Object [°C]</th><th>Sink [°C]</th><th>Output</th><th>Stable</th><th>Quick-set target [°C]</th>
          </tr>
        </thead>
        <tbody>
          {channels.map((ch) => <TempSettingsRow
            key={ch.device_id + "/" + ch.instance} ch={ch}
            staged={staged[`${ch.device_id}/${ch.instance}`] || ""}
            setStaged={(v) => setStaged((s) => ({ ...s, [`${ch.device_id}/${ch.instance}`]: v }))}
            busy={busy[`${ch.device_id}/${ch.instance}`]}
            writeTrace={traces[lifecycleKey(ch.device_id, ch.instance, PARAM_TARGET_TEMPERATURE)]}
            onCommit={commit} onToggleOutput={toggleOutput} />)}
        </tbody>
      </table>
    </div>
  );
}

function TempSettingsRow({ ch, staged, setStaged, busy, writeTrace, onCommit, onToggleOutput }) {
  const targetSignal = signalFor(PARAM_TARGET_TEMPERATURE, ch.instance, { unit: "degC", name: "Control-loop target temperature" });
  const cascadeTargetSignal = signalFor(PARAM_CASCADE_TARGET_TEMPERATURE, ch.instance, { unit: "degC", name: "Cascade target temperature" });
  const objSignal = signalFor(PARAM_OBJECT_TEMPERATURE, ch.instance, { unit: "degC", name: "Object temperature" });
  const sinkSignal = signalFor(PARAM_SINK_TEMPERATURE, ch.instance, { unit: "degC", name: "Sink temperature" });
  const cascadeEnableSignal = signalFor(PARAM_CASCADE_ENABLE, ch.instance, { name: "Cascade enable" });
  const stableSignal = signalFor(PARAM_STABLE_STATE, ch.instance, { name: "Temperature stability" });
  const tgt    = useLiveValue(ch.device_id, PARAM_TARGET_TEMPERATURE, ch.instance);
  const cascadeTarget = useLiveValue(ch.device_id, PARAM_CASCADE_TARGET_TEMPERATURE, ch.instance);
  const cascadeEnable = useLiveValue(ch.device_id, PARAM_CASCADE_ENABLE, ch.instance);
  const obj    = useLiveValue(ch.device_id, PARAM_OBJECT_TEMPERATURE, ch.instance);
  const sink   = useLiveValue(ch.device_id, PARAM_SINK_TEMPERATURE, ch.instance);
  const out    = useLiveValue(ch.device_id, PARAM_OUTPUT_ENABLE, ch.instance);
  const stable = useLiveValue(ch.device_id, PARAM_STABLE_STATE, ch.instance);
  const cascadeActive = cascadeEnable.value === 1;
  const dt = (obj.value != null && tgt.value != null) ? obj.value - tgt.value : null;
  const reachable = tgt.quality !== "unreachable";
  const stagedNum = parseFloat(staged);
  const stagedValid = !Number.isNaN(stagedNum) && stagedNum >= -40 && stagedNum <= 150;
  return (
    <tr title={MecomAPI.provenance(ch.device_id, PARAM_TARGET_TEMPERATURE, ch.instance)}>
      <td>
        <span className="swatch" style={{ background: channelColor(ch.device_id, ch.instance) }}></span>
        <span className="chan-name">{ch.device_id}</span>
        <span className="chan-sub">/{ch.instance} · {ch.label}</span>
      </td>
      <td className="num cmd">
        <div className="semantic-stack">
          <SemanticValuePopup param={targetSignal} value={tgt.value} quality={tgt.quality} className="semantic-inline-value">
            <span>{MecomAPI.formatWithUnit(tgt.value, "degC", PARAM_TARGET_TEMPERATURE)}</span>
          </SemanticValuePopup>
          {cascadeActive && (
            <SemanticValuePopup param={cascadeTargetSignal} value={cascadeTarget.value} quality={cascadeTarget.quality} className="semantic-inline-value">
              <span className="subtle-line">{`cascade ${MecomAPI.formatWithUnit(cascadeTarget.value, "degC", PARAM_CASCADE_TARGET_TEMPERATURE)}`}</span>
            </SemanticValuePopup>
          )}
        </div>
      </td>
      <td className={"num actual " + (dt !== null && Math.abs(dt) > 0.3 ? "warn" : "")}>
        <SemanticValuePopup param={objSignal} value={obj.value} quality={obj.quality} className="semantic-inline-value">
          <span>{MecomAPI.formatWithUnit(obj.value, "degC", PARAM_OBJECT_TEMPERATURE)}</span>
        </SemanticValuePopup>
      </td>
      <td>
        <SemanticValuePopup param={sinkSignal} value={sink.value} quality={sink.quality} className="semantic-inline-value">
          <span>{MecomAPI.formatWithUnit(sink.value, "degC", PARAM_SINK_TEMPERATURE)}</span>
        </SemanticValuePopup>
      </td>
      <td>
        <span className={"out-toggle " + (out.value === 1 ? "on" : "off") + (!reachable ? " locked" : "")}
              onClick={() => reachable && onToggleOutput(ch.device_id, ch.instance, out.value === 1)}
              title="Click to toggle Output Stage Enable (write_int32 param=2010)">
          <span>OFF</span><span>ON</span>
        </span>
      </td>
      <td>
        <SemanticValuePopup param={stableSignal} value={stable.value} quality={stable.quality} className="semantic-inline-value">
          <span>{stable.value === 1 ? <span className="num ok">stable</span> : reachable ? <span className="num warn">drift</span> : <span style={{color:"var(--bad)"}}>—</span>}</span>
        </SemanticValuePopup>
      </td>
      <td>
        <span className="quick-write-cell">
          <span className="quick-input">
            <input className={staged ? "staged" : ""} placeholder="°C" value={staged} disabled={!reachable}
                   onChange={(e) => setStaged(e.target.value)} />
            <button className={stagedValid ? "primary" : ""} disabled={!stagedValid || !reachable || busy}
                    onClick={() => onCommit(ch.device_id, ch.instance, staged)}>
              {busy ? "…" : "Set"}
            </button>
          </span>
          {writeTrace && (
            <span className="write-inline-trace">
              <WriteLifecycleTrace
                param={targetSignal}
                deviceId={ch.device_id}
                instance={ch.instance}
                busy={busy}
                staged={staged}
                commandName={MecomAPI.commandNameFor(targetSignal)}
                trace={writeTrace}
              />
            </span>
          )}
        </span>
      </td>
    </tr>
  );
}

export function SupplySettingsTable({ channels, holderId }) {
  const toast = useToast();
  const [staged, setStaged] = useState({});
  const [busy, setBusy] = useState({});
  const [traces, setTraces] = useState({});
  const keyFor = (deviceId, instance, param) => lifecycleKey(deviceId, instance, param);
  async function commitField(deviceId, instance, param, value) {
    const key = `${deviceId}/${instance}/${param}`;
    setBusy((b) => ({ ...b, [key]: true }));
    try {
      const numeric = Number(value);
      if (!Number.isFinite(numeric)) throw new Error("Invalid numeric value");
      const signal = signalFor(param, instance, { unit: param === PARAM_FIXED_VOLTAGE ? "V" : "A" });
      const current = liveSnapshot(deviceId, param, instance).value;
      setTraces((t) => ({
        ...t,
        [key]: {
          phase: "write",
          status: "submitted",
          at: Date.now(),
          paramId: param,
          unit: signal.unit || (param === PARAM_FIXED_VOLTAGE ? "V" : "A"),
          commandName: MecomAPI.commandNameFor(signal),
          currentValue: current,
          prospectiveValue: numeric,
          submittedValue: numeric,
        },
      }));
      let lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
      if (!lease || lease.holder !== holderId) { lease = await MecomAPI.acquireLease(deviceId, holderId, "5m"); }
      const body = await MecomAPI.write(deviceId, { name: MecomAPI.commandNameFor(signal), arguments: { param, instance, value: numeric } }, lease.token);
      const confirmed = liveSnapshot(deviceId, param, instance).value;
      setTraces((t) => ({
        ...t,
        [key]: {
          ...(t[key] || {}),
          phase: "done",
          status: (body && body.status) || "completed",
          at: Date.now(),
          confirmedValue: confirmed !== null ? confirmed : numeric,
        },
      }));
      toast.push({
        kind: "ok",
        title: commandTitle(param),
        body: `${deviceId}/channel ${instance} → ${MecomAPI.formatWithUnit(numeric, signal.unit, param)}; mode and output state unchanged`,
      });
      setStaged((s) => ({ ...s, [key]: "" }));
    } catch (err) {
      setTraces((t) => ({
        ...t,
        [key]: {
          ...(t[key] || {}),
          phase: "error",
          status: "failed",
          at: Date.now(),
          error: err.message || String(err),
        },
      }));
      const c = categorizeError(err);
      toast.push({ kind: c.kind, title: c.cat.toUpperCase(), body: err.message });
    } finally {
      setBusy((b) => ({ ...b, [key]: false }));
    }
  }
  async function toggleOutput(deviceId, instance, cur) {
    try {
      let lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
      if (!lease || lease.holder !== holderId) { lease = await MecomAPI.acquireLease(deviceId, holderId, "5m"); }
      await MecomAPI.write(deviceId, { name: "write_int32", arguments: { param: 2010, instance, value: cur ? 0 : 1 } }, lease.token);
      toast.push({ kind: "ok", title: cur ? "Output OFF" : "Output ON", body: `${deviceId}/${instance}` });
    } catch (err) {
      const c = categorizeError(err);
      toast.push({ kind: c.kind, title: c.cat.toUpperCase(), body: err.message });
    }
  }
  return (
    <div className="hero-settings-wrap">
      <table className="hero-settings">
        <thead>
          <tr>
            <th style={{ width: "20%" }}>Channel</th>
            <th>Voltage command [V]</th><th>Voltage limit [V]</th><th>Measured voltage [V]</th><th>Current limit [A]</th><th>Measured current [A]</th>
            <th>Measured power [W]</th><th>Reported mode</th><th>Output stage</th>
          </tr>
        </thead>
        <tbody>
          {channels.map((ch) => <SupplySettingsRow
            key={ch.device_id + "/" + ch.instance} ch={ch}
            stagedV={staged[keyFor(ch.device_id, ch.instance, PARAM_FIXED_VOLTAGE)] || ""}
            stagedVL={staged[keyFor(ch.device_id, ch.instance, PARAM_VOLTAGE_LIMIT)] || ""}
            stagedI={staged[keyFor(ch.device_id, ch.instance, PARAM_CURRENT_LIMIT)] || ""}
            setStagedV={(v) => setStaged((s) => ({ ...s, [keyFor(ch.device_id, ch.instance, PARAM_FIXED_VOLTAGE)]: v }))}
            setStagedVL={(v) => setStaged((s) => ({ ...s, [keyFor(ch.device_id, ch.instance, PARAM_VOLTAGE_LIMIT)]: v }))}
            setStagedI={(v) => setStaged((s) => ({ ...s, [keyFor(ch.device_id, ch.instance, PARAM_CURRENT_LIMIT)]: v }))}
            busyV={busy[keyFor(ch.device_id, ch.instance, PARAM_FIXED_VOLTAGE)]}
            busyVL={busy[keyFor(ch.device_id, ch.instance, PARAM_VOLTAGE_LIMIT)]}
            busyI={busy[keyFor(ch.device_id, ch.instance, PARAM_CURRENT_LIMIT)]}
            writeTraceV={traces[keyFor(ch.device_id, ch.instance, PARAM_FIXED_VOLTAGE)]}
            writeTraceVL={traces[keyFor(ch.device_id, ch.instance, PARAM_VOLTAGE_LIMIT)]}
            writeTraceI={traces[keyFor(ch.device_id, ch.instance, PARAM_CURRENT_LIMIT)]}
            onCommitField={commitField} onToggleOutput={toggleOutput} />)}
        </tbody>
      </table>
    </div>
  );
}

function SupplySettingsRow({ ch, stagedV, stagedVL, stagedI, setStagedV, setStagedVL, setStagedI, busyV, busyVL, busyI, writeTraceV, writeTraceVL, writeTraceI, onCommitField, onToggleOutput }) {
  const fixedVoltageSignal = signalFor(PARAM_FIXED_VOLTAGE, ch.instance, { unit: "V", name: "Fixed voltage" });
  const fixedCurrentSignal = signalFor(PARAM_FIXED_CURRENT, ch.instance, { unit: "A", name: "Fixed current" });
  const currentLimitSignal = signalFor(PARAM_CURRENT_LIMIT, ch.instance, { unit: "A", name: "Current limit" });
  const voltageLimitSignal = signalFor(PARAM_VOLTAGE_LIMIT, ch.instance, { unit: "V", name: "Voltage limit" });
  const measuredVoltageSignal = signalFor(PARAM_OUTPUT_VOLTAGE, ch.instance, { unit: "V", name: "Measured output voltage" });
  const measuredCurrentSignal = signalFor(PARAM_OUTPUT_CURRENT, ch.instance, { unit: "A", name: "Measured output current" });
  const measuredPowerSignal = signalFor(PARAM_OUTPUT_POWER, ch.instance, { unit: "W", name: "Measured output power" });
  const modeSignal = signalFor(PARAM_OUTPUT_MODE, ch.instance, { name: "Output operating mode" });
  const actV  = useLiveValue(ch.device_id, PARAM_OUTPUT_VOLTAGE, ch.instance);
  const actI  = useLiveValue(ch.device_id, PARAM_OUTPUT_CURRENT, ch.instance);
  const power = useLiveValue(ch.device_id, PARAM_OUTPUT_POWER, ch.instance);
  const fixedV = useLiveValue(ch.device_id, PARAM_FIXED_VOLTAGE, ch.instance);
  const fixedI = useLiveValue(ch.device_id, PARAM_FIXED_CURRENT, ch.instance);
  const currentLimit = useLiveValue(ch.device_id, PARAM_CURRENT_LIMIT, ch.instance);
  const voltageLimit = useLiveValue(ch.device_id, PARAM_VOLTAGE_LIMIT, ch.instance);
  const mode  = useLiveValue(ch.device_id, PARAM_OUTPUT_MODE, ch.instance);
  const out   = useLiveValue(ch.device_id, PARAM_OUTPUT_ENABLE, ch.instance);
  const setV  = MecomAPI.setpoint(ch.device_id, PARAM_FIXED_VOLTAGE, ch.instance) ?? fixedV.value;
  const setVL = MecomAPI.setpoint(ch.device_id, PARAM_VOLTAGE_LIMIT, ch.instance) ?? voltageLimit.value;
  const setI  = MecomAPI.setpoint(ch.device_id, PARAM_CURRENT_LIMIT, ch.instance) ?? currentLimit.value;
  const reachable = actV.quality !== "unreachable";
  const livePower = power.value != null ? power.value : ((actV.value != null && actI.value != null) ? actV.value * actI.value : null);
  const stagedVNumber = Number(stagedV);
  const stagedVLNumber = Number(stagedVL);
  const stagedINumber = Number(stagedI);
  const stagedVValid = stagedV !== "" && Number.isFinite(stagedVNumber);
  const stagedVLValid = stagedVL !== "" && Number.isFinite(stagedVLNumber);
  const stagedIValid = stagedI !== "" && Number.isFinite(stagedINumber);
  return (
    <tr title={MecomAPI.provenance(ch.device_id, PARAM_FIXED_VOLTAGE, ch.instance)}>
      <td>
        <span className="swatch" style={{ background: channelColor(ch.device_id, ch.instance) }}></span>
        <span className="chan-name">{ch.device_id}</span>
        <span className="chan-sub">/{ch.instance} · {ch.label}</span>
      </td>
      <td className="num cmd">
        <span className="quick-write-cell">
          <span className="quick-input">
            <input className={stagedV ? "staged" : ""} placeholder={(setV ?? 0).toFixed(3)} value={stagedV} disabled={!reachable}
                   title="Fixed voltage command. This writes parameter 2021 only; it does not change operating mode or output enable."
                   onChange={(e) => setStagedV(e.target.value)} />
            <button className={stagedVValid ? "primary" : ""} disabled={!stagedVValid || !reachable || busyV}
                    title="Write fixed voltage command only"
                    onClick={() => onCommitField(ch.device_id, ch.instance, PARAM_FIXED_VOLTAGE, stagedV)}>
              {busyV ? "…" : "Set"}
            </button>
          </span>
          <span className="cmd-live-line">
            fixed {MecomAPI.formatWithUnit(fixedV.value, "V", PARAM_FIXED_VOLTAGE)}
            {" · "}voltage limit {MecomAPI.formatWithUnit(voltageLimit.value, "V", PARAM_VOLTAGE_LIMIT)}
          </span>
          {writeTraceV && (
            <span className="write-inline-trace">
              <WriteLifecycleTrace
                param={fixedVoltageSignal}
                deviceId={ch.device_id}
                instance={ch.instance}
                busy={busyV}
                staged={stagedV}
                commandName={MecomAPI.commandNameFor(fixedVoltageSignal)}
                trace={writeTraceV}
              />
            </span>
          )}
        </span>
      </td>
      <td className="num cmd">
        <span className="quick-write-cell">
          <span className="quick-input">
            <input className={stagedVL ? "staged" : ""} placeholder={(setVL ?? 0).toFixed(3)} value={stagedVL} disabled={!reachable}
                   title="Voltage limit. This writes parameter 2031 only; whichever limiting value is lower determines voltage/current priority."
                   onChange={(e) => setStagedVL(e.target.value)} />
            <button className={stagedVLValid ? "primary" : ""} disabled={!stagedVLValid || !reachable || busyVL}
                    title="Write voltage limit only"
                    onClick={() => onCommitField(ch.device_id, ch.instance, PARAM_VOLTAGE_LIMIT, stagedVL)}>
              {busyVL ? "…" : "Set"}
            </button>
          </span>
          <span className="cmd-live-line">
            limit {MecomAPI.formatWithUnit(voltageLimit.value, "V", PARAM_VOLTAGE_LIMIT)}
          </span>
          {writeTraceVL && (
            <span className="write-inline-trace">
              <WriteLifecycleTrace
                param={voltageLimitSignal}
                deviceId={ch.device_id}
                instance={ch.instance}
                busy={busyVL}
                staged={stagedVL}
                commandName={MecomAPI.commandNameFor(voltageLimitSignal)}
                trace={writeTraceVL}
              />
            </span>
          )}
        </span>
      </td>
      <td className="num actual">
        <SemanticValuePopup param={measuredVoltageSignal} value={actV.value} quality={actV.quality} className="semantic-inline-value">
          <span>{MecomAPI.formatWithUnit(actV.value, "V", PARAM_OUTPUT_VOLTAGE)}</span>
        </SemanticValuePopup>
      </td>
      <td className="num cmd">
        <span className="quick-write-cell">
          <span className="quick-input">
            <input className={stagedI ? "staged" : ""} placeholder={(setI ?? 0).toFixed(3)} value={stagedI} disabled={!reachable}
                   title="Current limit. This writes parameter 2030 only; whichever limiting value is lower determines voltage/current priority."
                   onChange={(e) => setStagedI(e.target.value)} />
            <button className={stagedIValid ? "primary" : ""} disabled={!stagedIValid || !reachable || busyI}
                    title="Write current limit only"
                    onClick={() => onCommitField(ch.device_id, ch.instance, PARAM_CURRENT_LIMIT, stagedI)}>
              {busyI ? "…" : "Set"}
            </button>
          </span>
          <span className="cmd-live-line">
            limit {MecomAPI.formatWithUnit(currentLimit.value, "A", PARAM_CURRENT_LIMIT)}
            {" · "}fixed current {MecomAPI.formatWithUnit(fixedI.value, "A", PARAM_FIXED_CURRENT)}
          </span>
          {writeTraceI && (
            <span className="write-inline-trace">
              <WriteLifecycleTrace
                param={currentLimitSignal}
                deviceId={ch.device_id}
                instance={ch.instance}
                busy={busyI}
                staged={stagedI}
                commandName={MecomAPI.commandNameFor(currentLimitSignal)}
                trace={writeTraceI}
              />
            </span>
          )}
        </span>
      </td>
      <td className="num dut">
        <SemanticValuePopup param={measuredCurrentSignal} value={actI.value} quality={actI.quality} className="semantic-inline-value">
          <span>{MecomAPI.formatWithUnit(actI.value, "A", PARAM_OUTPUT_CURRENT)}</span>
        </SemanticValuePopup>
      </td>
      <td title={power.value == null && livePower != null ? "Calculated from live voltage and current because live power was not delivered by the gateway." : "Live output power from parameter 1022."}>
        <SemanticValuePopup param={measuredPowerSignal} value={livePower} quality={power.quality} className="semantic-inline-value">
          <span>{MecomAPI.formatWithUnit(livePower, "W", PARAM_OUTPUT_POWER)}</span>
        </SemanticValuePopup>
      </td>
      <td title="Reported operating mode from parameter 2040. Voltage and current limit edits do not change this value.">
        <SemanticValuePopup param={modeSignal} value={mode.value} quality={mode.quality} className="semantic-inline-value">
          <Chip kind={mode.value === 3 ? "warn" : "ok"}>{MecomAPI.formatValue(mode.value, "", PARAM_OUTPUT_MODE)}</Chip>
        </SemanticValuePopup>
      </td>
      <td>
        <span className={"out-toggle " + (out.value === 1 ? "on" : "off") + (!reachable ? " locked" : "")}
              onClick={() => reachable && onToggleOutput(ch.device_id, ch.instance, out.value === 1)}>
          <span>OFF</span><span>ON</span>
        </span>
      </td>
    </tr>
  );
}
