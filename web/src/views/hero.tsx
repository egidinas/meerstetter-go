// @ts-nocheck
import React, { useEffect, useMemo, useState } from "react";
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
import { HistoryController } from "../components/HistoryController";
import { graphSeriesIdentityKey, renderSeriesFromGraphTile } from "../lib/series";
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
const PARAM_VOLTAGE_ERROR_THRESHOLD = 2033;
const PARAM_POWER_LIMIT = 2035;
const PARAM_MEASURED_RESISTANCE = 2036;
const PARAM_POWER_LOOP_STATUS = 2038;
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

function tileSeriesKey(series) {
  return graphSeriesIdentityKey(series);
}

export function HeroGraph({ wall, role, tile, height = 280, live = true, initialHiddenSeries = [], children }) {
  useGatewayTick();
  const renderedSeries = renderSeriesFromGraphTile(tile);
  const renderedSeriesKey = renderedSeries.map((series) => tileSeriesKey(series)).sort().join("|");
  const rawSeriesKeys = useMemo(() => (tile?.series || []).map((series) => tileSeriesKey(series)).filter(Boolean), [tile]);
  const tileSeriesCount = rawSeriesKeys.length;
  const renderedSeriesKeys = useMemo(() => new Set(renderedSeries.map((series) => tileSeriesKey(series))), [renderedSeriesKey]);
  const rawSeriesMeta = useMemo(() => {
    const out = new Map();
    (tile?.series || []).forEach((series) => {
      const key = tileSeriesKey(series);
      if (!key) return;
      out.set(key, {
        quality: series.quality || series.diagnostics?.status || "ok",
        visibilityReason: series.visibility_reason || series.visibilityReason || "",
      });
    });
    return out;
  }, [tile]);
  const renderedLegendSeries = useMemo(() => renderedSeries.map((series) => {
    const key = tileSeriesKey(series);
    const meta = rawSeriesMeta.get(key) || {};
    return {
      ...series,
      key,
      quality: series.quality || meta.quality || "ok",
      visibilityReason: series.visibilityReason || meta.visibilityReason || "",
    };
  }), [renderedSeriesKey, rawSeriesMeta]);
  const rawOnlyLegendSeries = useMemo(() => (tile?.series || [])
    .map((series) => {
      const key = tileSeriesKey(series);
      if (!key || renderedSeriesKeys.has(key)) return null;
      const history = series.history || {};
      const values = Array.isArray(history.v) ? history.v : [];
      const source = series.source || {};
      return {
        key,
        label: series.label || key,
        fullLabel: series.full_label || series.fullLabel || "",
        visibilityReason: series.visibility_reason || series.visibilityReason || "",
        unit: series.unit || "",
        paramId: source.param_id || series.param_id || series.paramId,
        color: channelColor(source.device_id || source.deviceId || "", source.instance || series.instance || 1),
        quality: series.quality || series.diagnostics?.status || "missing",
        history: { v: values },
      };
    })
    .filter(Boolean), [tile, renderedSeriesKey]);
  const legendSeries = renderedLegendSeries.concat(rawOnlyLegendSeries);
  const validHiddenSeriesKeys = useMemo(() => Array.from(new Set(renderedSeries.map((series) => tileSeriesKey(series)).concat(rawSeriesKeys))), [renderedSeriesKey, rawSeriesKeys.join("|")]);
  const validHiddenSeriesKey = useMemo(() => validHiddenSeriesKeys.slice().sort().join("|"), [validHiddenSeriesKeys.join("|")]);
  const initialHiddenKey = useMemo(() => (initialHiddenSeries || []).slice().sort().join("|"), [initialHiddenSeries.join ? initialHiddenSeries.join("|") : String(initialHiddenSeries)]);
  const [manualHiddenSeries, setManualHiddenSeries] = useState([]);
  const [manualShownSeries, setManualShownSeries] = useState([]);
  const [autoHiddenSeries, setAutoHiddenSeries] = useState(() => initialHiddenSeries || []);
  const initialHiddenApplyKey = `${initialHiddenKey}:${validHiddenSeriesKey}`;
  const manualHiddenSeriesKey = manualHiddenSeries.join ? manualHiddenSeries.join("|") : String(manualHiddenSeries);
  const manualShownSeriesKey = manualShownSeries.join ? manualShownSeries.join("|") : String(manualShownSeries);
  const autoHiddenSeriesKey = autoHiddenSeries.join ? autoHiddenSeries.join("|") : String(autoHiddenSeries);
  const effectiveHiddenSeries = useMemo(() => {
    const valid = new Set(validHiddenSeriesKeys);
    const manuallyShown = new Set((manualShownSeries || []).filter((key) => valid.has(key)));
    const next = new Set();
    (autoHiddenSeries || []).forEach((key) => {
      if (valid.has(key) && !manuallyShown.has(key)) next.add(key);
    });
    (manualHiddenSeries || []).forEach((key) => {
      if (valid.has(key)) next.add(key);
    });
    return Array.from(next);
  }, [manualHiddenSeriesKey, manualShownSeriesKey, autoHiddenSeriesKey, validHiddenSeriesKey]);
  useEffect(() => {
    const valid = new Set(validHiddenSeriesKeys);
    setAutoHiddenSeries((initialHiddenSeries || []).filter((key) => valid.has(key)));
    setManualHiddenSeries((cur) => (cur || []).filter((key) => valid.has(key)));
    setManualShownSeries((cur) => (cur || []).filter((key) => valid.has(key)));
  }, [initialHiddenApplyKey]);
  function toggleSeries(key) {
    if (effectiveHiddenSeries.includes(key)) {
      setManualHiddenSeries((cur) => (cur || []).filter((item) => item !== key));
      setManualShownSeries((cur) => (cur || []).includes(key) ? cur : (cur || []).concat(key));
      return;
    }
    setManualShownSeries((cur) => (cur || []).filter((item) => item !== key));
    setManualHiddenSeries((cur) => (cur || []).includes(key) ? cur.filter((item) => item !== key) : (cur || []).concat(key));
  }

  const [isLiveLocal, setIsLiveLocal] = useState(true);
  const [historicalTile, setHistoricalTile] = useState(null);
  const [range, setRange] = useState({ t0: null, t1: null });

  useEffect(() => {
    if (!isLiveLocal && range.t0 && range.t1) {
      MecomAPI.graphTile(wall.wall_id, "range", wall.series, { t0: range.t0, t1: range.t1 })
        .then(setHistoricalTile)
        .catch(console.error);
    }
  }, [isLiveLocal, range.t0, range.t1, wall.wall_id]);

  const effectiveTile = isLiveLocal ? tile : (historicalTile || tile);
  const isStreaming = isLiveLocal && live;

  return (
    <div className={"hero" + (isStreaming ? " live" : "")}>
      <div className="hero-head">
        <div className="title-grp">
          {isStreaming && <span className="live-dot"></span>}
          {!isStreaming && <span className="history-icon">◔</span>}
          <h2>{wall.label}</h2>
          <span className="sub">
            · {tileSeriesCount} tile series · {isStreaming ? "live" : "historical"}
          </span>
        </div>
        <div className="badge-row">
          <Chip kind={isStreaming ? "accent" : "info"}>{axisBadgeLabel(role, effectiveTile)}</Chip>
          <HistoryController 
            isLive={isLiveLocal} 
            onSetLive={setIsLiveLocal}
            onRangeChange={(t0, t1) => setRange({ t0, t1 })}
          />
        </div>
      </div>
      <div className="hero-chart-row">
        <div className="hero-plot">
          {tileSeriesCount === 0 ? (
            <div style={{ padding: 36, textAlign: "center", color: "var(--muted)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
              No signals assigned. Add from the Signal Dictionary →
            </div>
          ) : (
            <MultiChart tile={effectiveTile} height={height} hiddenSeries={effectiveHiddenSeries} fill minHeight={height} />
          )}
        </div>
        <div className="hero-legend">
          {legendSeries.map((s) => {
            const off = effectiveHiddenSeries.includes(s.key);
            const last = s.history.v[s.history.v.length - 1];
            return (
              <span
                key={s.key}
                className={"item " + (off ? "off" : "")}
                data-series-key={s.key}
                data-series-quality={s.quality || "ok"}
                data-series-visible={off ? "false" : "true"}
                onClick={() => toggleSeries(s.key)}
                title={`${s.fullLabel || "Click to show/hide this tile series"}${s.quality ? ` · quality ${s.quality}` : ""}${s.visibilityReason ? ` · ${s.visibilityReason}` : ""}`}
              >
                <span className="sw" style={{ background: s.color }}></span>
                <span className="series-label">{s.label}</span>
                <span className="cur">{MecomAPI.formatWithUnit(last, s.unit, s.paramId)}</span>
                {s.quality && s.quality !== "ok" ? <span className="quality-mini">{s.quality}</span> : null}
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
      const confirmation = await MecomAPI.confirmWriteValue(deviceId, PARAM_TARGET_TEMPERATURE, instance, numeric);
      setTraces((t) => ({
        ...t,
        [traceKey]: {
          ...(t[traceKey] || {}),
          phase: confirmation.matched ? "done" : "error",
          status: confirmation.status || (body && body.status) || (confirmation.matched ? "completed" : "readback mismatch"),
          at: Date.now(),
          confirmedValue: confirmation.value,
          confirmedMatched: confirmation.matched,
          ...(confirmation.matched ? {} : {
            error: `Readback mismatch: expected ${MecomAPI.formatWithUnit(numeric, signal.unit, PARAM_TARGET_TEMPERATURE)}, got ${MecomAPI.formatWithUnit(confirmation.value, signal.unit, PARAM_TARGET_TEMPERATURE)}`,
          }),
        },
      }));
      if (confirmation.matched) {
        toast.push({ kind: "ok", title: "Target set", body: `${deviceId}/${instance} → ${value} °C` });
        setStaged((s) => ({ ...s, [key]: "" }));
      } else {
        toast.push({
          kind: "bad",
          title: "READBACK MISMATCH",
          body: `${deviceId}/${instance} expected ${MecomAPI.formatWithUnit(numeric, signal.unit, PARAM_TARGET_TEMPERATURE)} but read back ${MecomAPI.formatWithUnit(confirmation.value, signal.unit, PARAM_TARGET_TEMPERATURE)}`,
        });
      }
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
            <th style={{ width: "18%" }}>Channel</th>
            <th>Target [°C]</th><th>Object [°C]</th><th>Sink [°C]</th><th>Electrical live</th><th>Output</th><th>Stable</th><th>Quick-set target [°C]</th>
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
  const measuredVoltageSignal = signalFor(PARAM_OUTPUT_VOLTAGE, ch.instance, { unit: "V", name: "Measured output voltage" });
  const measuredCurrentSignal = signalFor(PARAM_OUTPUT_CURRENT, ch.instance, { unit: "A", name: "Measured output current" });
  const measuredPowerSignal = signalFor(PARAM_OUTPUT_POWER, ch.instance, { unit: "W", name: "Measured output power" });
  const cascadeEnableSignal = signalFor(PARAM_CASCADE_ENABLE, ch.instance, { name: "Cascade enable" });
  const stableSignal = signalFor(PARAM_STABLE_STATE, ch.instance, { name: "Temperature stability" });
  const tgt    = useLiveValue(ch.device_id, PARAM_TARGET_TEMPERATURE, ch.instance);
  const cascadeTarget = useLiveValue(ch.device_id, PARAM_CASCADE_TARGET_TEMPERATURE, ch.instance);
  const cascadeEnable = useLiveValue(ch.device_id, PARAM_CASCADE_ENABLE, ch.instance);
  const obj    = useLiveValue(ch.device_id, PARAM_OBJECT_TEMPERATURE, ch.instance);
  const sink   = useLiveValue(ch.device_id, PARAM_SINK_TEMPERATURE, ch.instance);
  const actV   = useLiveValue(ch.device_id, PARAM_OUTPUT_VOLTAGE, ch.instance);
  const actI   = useLiveValue(ch.device_id, PARAM_OUTPUT_CURRENT, ch.instance);
  const power  = useLiveValue(ch.device_id, PARAM_OUTPUT_POWER, ch.instance);
  const out    = useLiveValue(ch.device_id, PARAM_OUTPUT_ENABLE, ch.instance);
  const stable = useLiveValue(ch.device_id, PARAM_STABLE_STATE, ch.instance);
  const livePower = power.value != null ? power.value : ((actV.value != null && actI.value != null) ? actV.value * actI.value : null);
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
      <td className="num actual">
        <span className="electrical-stack">
          <SemanticValuePopup param={measuredPowerSignal} value={livePower} quality={power.quality} className="semantic-inline-value">
            <span>{MecomAPI.formatWithUnit(livePower, "W", PARAM_OUTPUT_POWER)}</span>
          </SemanticValuePopup>
          <span className="subtle-line">
            <SemanticValuePopup param={measuredVoltageSignal} value={actV.value} quality={actV.quality} className="semantic-inline-value">
              <span>{MecomAPI.formatWithUnit(actV.value, "V", PARAM_OUTPUT_VOLTAGE)}</span>
            </SemanticValuePopup>
            {" · "}
            <SemanticValuePopup param={measuredCurrentSignal} value={actI.value} quality={actI.quality} className="semantic-inline-value">
              <span>{MecomAPI.formatWithUnit(actI.value, "A", PARAM_OUTPUT_CURRENT)}</span>
            </SemanticValuePopup>
          </span>
        </span>
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
      const confirmation = await MecomAPI.confirmWriteValue(deviceId, param, instance, numeric);
      setTraces((t) => ({
        ...t,
        [key]: {
          ...(t[key] || {}),
          phase: confirmation.matched ? "done" : "error",
          status: confirmation.status || (body && body.status) || (confirmation.matched ? "completed" : "readback mismatch"),
          at: Date.now(),
          confirmedValue: confirmation.value,
          confirmedMatched: confirmation.matched,
          ...(confirmation.matched ? {} : {
            error: `Readback mismatch: expected ${MecomAPI.formatWithUnit(numeric, signal.unit, param)}, got ${MecomAPI.formatWithUnit(confirmation.value, signal.unit, param)}`,
          }),
        },
      }));
      if (confirmation.matched) {
        toast.push({
          kind: "ok",
          title: commandTitle(param),
          body: `${deviceId}/channel ${instance} → ${MecomAPI.formatWithUnit(numeric, signal.unit, param)}; mode and output state unchanged`,
        });
        setStaged((s) => ({ ...s, [key]: "" }));
      } else {
        toast.push({
          kind: "bad",
          title: "READBACK MISMATCH",
          body: `${deviceId}/channel ${instance} expected ${MecomAPI.formatWithUnit(numeric, signal.unit, param)} but read back ${MecomAPI.formatWithUnit(confirmation.value, signal.unit, param)}`,
        });
      }
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
            <th>Voltage & Limits [V]</th>
            <th>Current & Limit [A]</th>
            <th>Power & Target [W]</th>
            <th>Operating Status</th>
          </tr>
        </thead>
        <tbody>
          {channels.map((ch) => <SupplySettingsRow
            key={ch.device_id + "/" + ch.instance} ch={ch}
            stagedV={staged[keyFor(ch.device_id, ch.instance, PARAM_FIXED_VOLTAGE)] || ""}
            stagedVL={staged[keyFor(ch.device_id, ch.instance, PARAM_VOLTAGE_LIMIT)] || ""}
            stagedVET={staged[keyFor(ch.device_id, ch.instance, PARAM_VOLTAGE_ERROR_THRESHOLD)] || ""}
            stagedI={staged[keyFor(ch.device_id, ch.instance, PARAM_CURRENT_LIMIT)] || ""}
            stagedP={staged[keyFor(ch.device_id, ch.instance, PARAM_POWER_LIMIT)] || ""}
            setStagedV={(v) => setStaged((s) => ({ ...s, [keyFor(ch.device_id, ch.instance, PARAM_FIXED_VOLTAGE)]: v }))}
            setStagedVL={(v) => setStaged((s) => ({ ...s, [keyFor(ch.device_id, ch.instance, PARAM_VOLTAGE_LIMIT)]: v }))}
            setStagedVET={(v) => setStaged((s) => ({ ...s, [keyFor(ch.device_id, ch.instance, PARAM_VOLTAGE_ERROR_THRESHOLD)]: v }))}
            setStagedI={(v) => setStaged((s) => ({ ...s, [keyFor(ch.device_id, ch.instance, PARAM_CURRENT_LIMIT)]: v }))}
            setStagedP={(v) => setStaged((s) => ({ ...s, [keyFor(ch.device_id, ch.instance, PARAM_POWER_LIMIT)]: v }))}
            busyV={busy[keyFor(ch.device_id, ch.instance, PARAM_FIXED_VOLTAGE)]}
            busyVL={busy[keyFor(ch.device_id, ch.instance, PARAM_VOLTAGE_LIMIT)]}
            busyVET={busy[keyFor(ch.device_id, ch.instance, PARAM_VOLTAGE_ERROR_THRESHOLD)]}
            busyI={busy[keyFor(ch.device_id, ch.instance, PARAM_CURRENT_LIMIT)]}
            busyP={busy[keyFor(ch.device_id, ch.instance, PARAM_POWER_LIMIT)]}
            writeTraceV={traces[keyFor(ch.device_id, ch.instance, PARAM_FIXED_VOLTAGE)]}
            writeTraceVL={traces[keyFor(ch.device_id, ch.instance, PARAM_VOLTAGE_LIMIT)]}
            writeTraceVET={traces[keyFor(ch.device_id, ch.instance, PARAM_VOLTAGE_ERROR_THRESHOLD)]}
            writeTraceI={traces[keyFor(ch.device_id, ch.instance, PARAM_CURRENT_LIMIT)]}
            writeTraceP={traces[keyFor(ch.device_id, ch.instance, PARAM_POWER_LIMIT)]}
            onCommitField={commitField} onToggleOutput={toggleOutput} />)}
        </tbody>
      </table>
    </div>
  );
}

function SupplySettingsRow({
  ch,
  stagedV, stagedVL, stagedVET, stagedI, stagedP,
  setStagedV, setStagedVL, setStagedVET, setStagedI, setStagedP,
  busyV, busyVL, busyVET, busyI, busyP,
  writeTraceV, writeTraceVL, writeTraceVET, writeTraceI, writeTraceP,
  onCommitField, onToggleOutput
}) {
  const fixedVoltageSignal = signalFor(PARAM_FIXED_VOLTAGE, ch.instance, { unit: "V", name: "Fixed voltage" });
  const fixedCurrentSignal = signalFor(PARAM_FIXED_CURRENT, ch.instance, { unit: "A", name: "Fixed current" });
  const currentLimitSignal = signalFor(PARAM_CURRENT_LIMIT, ch.instance, { unit: "A", name: "Current limit" });
  const voltageLimitSignal = signalFor(PARAM_VOLTAGE_LIMIT, ch.instance, { unit: "V", name: "Voltage limit" });
  const voltageErrorThresholdSignal = signalFor(PARAM_VOLTAGE_ERROR_THRESHOLD, ch.instance, { unit: "V", name: "Voltage error threshold" });
  const powerLimitSignal = signalFor(PARAM_POWER_LIMIT, ch.instance, { unit: "W", name: "Third-party power control target" });
  const measuredVoltageSignal = signalFor(PARAM_OUTPUT_VOLTAGE, ch.instance, { unit: "V", name: "Measured output voltage" });
  const measuredCurrentSignal = signalFor(PARAM_OUTPUT_CURRENT, ch.instance, { unit: "A", name: "Measured output current" });
  const measuredPowerSignal = signalFor(PARAM_OUTPUT_POWER, ch.instance, { unit: "W", name: "Measured output power" });
  const measuredResistanceSignal = signalFor(PARAM_MEASURED_RESISTANCE, ch.instance, { unit: "Ohm", name: "Third-party measured resistance" });
  const loopStatusSignal = signalFor(PARAM_POWER_LOOP_STATUS, ch.instance, { name: "Third-party power control loop status" });
  const modeSignal = signalFor(PARAM_OUTPUT_MODE, ch.instance, { name: "Output operating mode" });

  const actV  = useLiveValue(ch.device_id, PARAM_OUTPUT_VOLTAGE, ch.instance);
  const actI  = useLiveValue(ch.device_id, PARAM_OUTPUT_CURRENT, ch.instance);
  const power = useLiveValue(ch.device_id, PARAM_OUTPUT_POWER, ch.instance);
  const actR  = useLiveValue(ch.device_id, PARAM_MEASURED_RESISTANCE, ch.instance);
  const loopStatus = useLiveValue(ch.device_id, PARAM_POWER_LOOP_STATUS, ch.instance);
  const fixedV = useLiveValue(ch.device_id, PARAM_FIXED_VOLTAGE, ch.instance);
  const fixedI = useLiveValue(ch.device_id, PARAM_FIXED_CURRENT, ch.instance);
  const currentLimit = useLiveValue(ch.device_id, PARAM_CURRENT_LIMIT, ch.instance);
  const voltageLimit = useLiveValue(ch.device_id, PARAM_VOLTAGE_LIMIT, ch.instance);
  const voltageErrorThreshold = useLiveValue(ch.device_id, PARAM_VOLTAGE_ERROR_THRESHOLD, ch.instance);
  const powerLimit = useLiveValue(ch.device_id, PARAM_POWER_LIMIT, ch.instance);
  const mode  = useLiveValue(ch.device_id, PARAM_OUTPUT_MODE, ch.instance);
  const out   = useLiveValue(ch.device_id, PARAM_OUTPUT_ENABLE, ch.instance);

  const setV  = MecomAPI.setpoint(ch.device_id, PARAM_FIXED_VOLTAGE, ch.instance) ?? fixedV.value;
  const setVL = MecomAPI.setpoint(ch.device_id, PARAM_VOLTAGE_LIMIT, ch.instance) ?? voltageLimit.value;
  const setVET = MecomAPI.setpoint(ch.device_id, PARAM_VOLTAGE_ERROR_THRESHOLD, ch.instance) ?? voltageErrorThreshold.value;
  const setI  = MecomAPI.setpoint(ch.device_id, PARAM_CURRENT_LIMIT, ch.instance) ?? currentLimit.value;
  const setP  = MecomAPI.setpoint(ch.device_id, PARAM_POWER_LIMIT, ch.instance) ?? powerLimit.value;
  const reachable = actV.quality !== "unreachable";
  const powerControlEnabled = Boolean(ch.third_party_power_control_enabled || ch.thirdPartyPowerControlEnabled);
  const livePower = power.value != null ? power.value : ((actV.value != null && actI.value != null) ? actV.value * actI.value : null);

  const stagedVNumber = Number(stagedV);
  const stagedVLNumber = Number(stagedVL);
  const stagedVETNumber = Number(stagedVET);
  const stagedINumber = Number(stagedI);
  const stagedPNumber = Number(stagedP);

  const stagedVValid = stagedV !== "" && Number.isFinite(stagedVNumber);
  const stagedVLValid = stagedVL !== "" && Number.isFinite(stagedVLNumber);
  const stagedVETValid = stagedVET !== "" && Number.isFinite(stagedVETNumber);
  const stagedIValid = stagedI !== "" && Number.isFinite(stagedINumber);
  const stagedPValid = powerControlEnabled && stagedP !== "" && Number.isFinite(stagedPNumber) && stagedPNumber >= 0 && stagedPNumber <= 500;

  return (
    <tr title={MecomAPI.provenance(ch.device_id, PARAM_FIXED_VOLTAGE, ch.instance)}>
      <td>
        <span className="swatch" style={{ background: channelColor(ch.device_id, ch.instance) }}></span>
        <span className="chan-name">{ch.device_id}</span>
        <span className="chan-sub">/{ch.instance} · {ch.label}</span>
      </td>
      <td className="supply-cell">
        <div className="supply-measured">
          <SemanticValuePopup param={measuredVoltageSignal} value={actV.value} quality={actV.quality} className="semantic-inline-value">
            <span className="live-val">{MecomAPI.formatWithUnit(actV.value, "V", PARAM_OUTPUT_VOLTAGE)}</span>
          </SemanticValuePopup>
        </div>
        <div className="supply-inputs">
          <div className="input-group">
            <span className="input-label">Cmd</span>
            <span className="quick-input">
              <input className={stagedV ? "staged" : ""} placeholder={(setV ?? 0).toFixed(3)} value={stagedV} disabled={!reachable}
                     title="Fixed voltage command (writes parameter 2021)"
                     onChange={(e) => setStagedV(e.target.value)} />
              <button className={stagedVValid ? "primary" : ""} disabled={!stagedVValid || !reachable || busyV}
                      onClick={() => onCommitField(ch.device_id, ch.instance, PARAM_FIXED_VOLTAGE, stagedV)}>
                {busyV ? "…" : "Set"}
              </button>
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
          </div>
          <div className="input-group">
            <span className="input-label">Limit</span>
            <span className="quick-input">
              <input className={stagedVL ? "staged" : ""} placeholder={(setVL ?? 0).toFixed(3)} value={stagedVL} disabled={!reachable}
                     title="Voltage limit (writes parameter 2031)"
                     onChange={(e) => setStagedVL(e.target.value)} />
              <button className={stagedVLValid ? "primary" : ""} disabled={!stagedVLValid || !reachable || busyVL}
                      onClick={() => onCommitField(ch.device_id, ch.instance, PARAM_VOLTAGE_LIMIT, stagedVL)}>
                {busyVL ? "…" : "Set"}
              </button>
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
          </div>
          <div className="input-group">
            <span className="input-label">Thresh</span>
            <span className="quick-input">
              <input className={stagedVET ? "staged" : ""} placeholder={(setVET ?? 0).toFixed(3)} value={stagedVET} disabled={!reachable}
                     title="Voltage error threshold (writes parameter 2033)"
                     onChange={(e) => setStagedVET(e.target.value)} />
              <button className={stagedVETValid ? "primary" : ""} disabled={!stagedVETValid || !reachable || busyVET}
                      onClick={() => onCommitField(ch.device_id, ch.instance, PARAM_VOLTAGE_ERROR_THRESHOLD, stagedVET)}>
                {busyVET ? "…" : "Set"}
              </button>
            </span>
            {writeTraceVET && (
              <span className="write-inline-trace">
                <WriteLifecycleTrace
                  param={voltageErrorThresholdSignal}
                  deviceId={ch.device_id}
                  instance={ch.instance}
                  busy={busyVET}
                  staged={stagedVET}
                  commandName={MecomAPI.commandNameFor(voltageErrorThresholdSignal)}
                  trace={writeTraceVET}
                />
              </span>
            )}
          </div>
        </div>
      </td>
      <td className="supply-cell">
        <div className="supply-measured">
          <SemanticValuePopup param={measuredCurrentSignal} value={actI.value} quality={actI.quality} className="semantic-inline-value">
            <span className="live-val">{MecomAPI.formatWithUnit(actI.value, "A", PARAM_OUTPUT_CURRENT)}</span>
          </SemanticValuePopup>
        </div>
        <div className="supply-inputs">
          <div className="input-group">
            <span className="input-label">Limit</span>
            <span className="quick-input">
              <input className={stagedI ? "staged" : ""} placeholder={(setI ?? 0).toFixed(3)} value={stagedI} disabled={!reachable}
                     title="Current limit (writes parameter 2030)"
                     onChange={(e) => setStagedI(e.target.value)} />
              <button className={stagedIValid ? "primary" : ""} disabled={!stagedIValid || !reachable || busyI}
                      onClick={() => onCommitField(ch.device_id, ch.instance, PARAM_CURRENT_LIMIT, stagedI)}>
                {busyI ? "…" : "Set"}
              </button>
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
          </div>
        </div>
      </td>
      <td className="supply-cell">
        <div className="supply-measured">
          <SemanticValuePopup param={measuredPowerSignal} value={livePower} quality={power.quality} className="semantic-inline-value">
            <span className="live-val">{MecomAPI.formatWithUnit(livePower, "W", PARAM_OUTPUT_POWER)}</span>
          </SemanticValuePopup>
          {powerControlEnabled && actR.value != null && actR.value > 0 && (
            <div className="resistance-telem-wrap" style={{ fontSize: "0.8rem", opacity: 0.8, marginTop: "2px", display: "flex", gap: "6px", alignItems: "center" }}>
              <SemanticValuePopup param={measuredResistanceSignal} value={actR.value} quality={actR.quality} className="semantic-inline-value">
                <span>{actR.value.toFixed(2)} Ω</span>
              </SemanticValuePopup>
              {loopStatus.value === 1 && <span style={{ color: "#10b981", fontSize: "0.74rem", fontWeight: "bold" }}>● Active Loop</span>}
              {loopStatus.value === 2 && <span style={{ color: "#ef4444", fontSize: "0.74rem", fontWeight: "bold" }}>● Fallback</span>}
            </div>
          )}
        </div>
        <div className="supply-inputs">
          <div className="input-group">
            <span className="input-label">Target</span>
            <span className="quick-input">
              <input className={stagedP ? "staged" : ""} placeholder={(setP ?? 150.0).toFixed(3)} value={stagedP} disabled={!reachable || !powerControlEnabled}
                     title={powerControlEnabled ? "Third-party downstream power-control target (writes virtual parameter 2035; requires write lease)" : "Third-party downstream power-control target is disabled for this device"}
                     onChange={(e) => setStagedP(e.target.value)} />
              <button className={stagedPValid ? "primary" : ""} disabled={!stagedPValid || !reachable || !powerControlEnabled || busyP}
                      onClick={() => onCommitField(ch.device_id, ch.instance, PARAM_POWER_LIMIT, stagedP)}>
                {busyP ? "…" : "Set"}
              </button>
            </span>
            {writeTraceP && (
              <span className="write-inline-trace">
                <WriteLifecycleTrace
                  param={powerLimitSignal}
                  deviceId={ch.device_id}
                  instance={ch.instance}
                  busy={busyP}
                  staged={stagedP}
                  commandName={MecomAPI.commandNameFor(powerLimitSignal)}
                  trace={writeTraceP}
                />
              </span>
            )}
          </div>
        </div>
      </td>
      <td className="status-cell">
        <div className="status-mode-wrap">
          <SemanticValuePopup param={modeSignal} value={mode.value} quality={mode.quality} className="semantic-inline-value">
            <Chip kind={mode.value === 3 ? "warn" : "ok"}>
              {MecomAPI.formatValue(mode.value, "", PARAM_OUTPUT_MODE)}
            </Chip>
          </SemanticValuePopup>
        </div>
        <div className="status-toggle-wrap">
          <span className={"out-toggle " + (out.value === 1 ? "on" : "off") + (!reachable ? " locked" : "")}
                onClick={() => reachable && onToggleOutput(ch.device_id, ch.instance, out.value === 1)}>
            <span>OFF</span><span>ON</span>
          </span>
        </div>
      </td>
    </tr>
  );
}
