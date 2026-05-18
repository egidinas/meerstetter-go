// @ts-nocheck
import React, { useState } from "react";
import { MecomAPI } from "../api/mecom";
import { Chip, Pill, useToast, categorizeError, useLiveValue, useGatewayTick } from "../components/atoms";
import { MultiChart } from "../components/atoms";
import { renderSeriesFromGraphTile } from "../lib/series";
import { channelColor } from "./assignments";

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
          <Chip kind="accent">{role === "temp" ? "temperature_c axis" : "voltage_v / current_a"}</Chip>
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
            <MultiChart tile={tile} height={height} hiddenSeries={hiddenSeries} />
          )}
        </div>
        <div className="hero-legend">
          {renderedSeries.map((s) => {
            const off = hiddenSeries.includes(s.key);
            return (
              <span key={s.key} className={"item " + (off ? "off" : "")} onClick={() => toggleSeries(s.key)} title="Click to show/hide this tile series">
                <span className="sw" style={{ background: s.color }}></span>
                {s.label}
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
  async function commit(deviceId, instance, value) {
    const key = `${deviceId}/${instance}`;
    setBusy((b) => ({ ...b, [key]: true }));
    try {
      let lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
      if (!lease || lease.holder !== holderId) { lease = await MecomAPI.acquireLease(deviceId, holderId, "5m"); }
      await MecomAPI.write(deviceId, { name: "set_float32", arguments: { param: 3000, instance, value: parseFloat(value) } }, lease.token);
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
            <th>Target</th><th>Object</th><th>Sink</th><th>Output</th><th>Stable</th><th>Quick-set target T</th>
          </tr>
        </thead>
        <tbody>
          {channels.map((ch) => <TempSettingsRow
            key={ch.device_id + "/" + ch.instance} ch={ch}
            staged={staged[`${ch.device_id}/${ch.instance}`] || ""}
            setStaged={(v) => setStaged((s) => ({ ...s, [`${ch.device_id}/${ch.instance}`]: v }))}
            busy={busy[`${ch.device_id}/${ch.instance}`]}
            onCommit={commit} onToggleOutput={toggleOutput} />)}
        </tbody>
      </table>
    </div>
  );
}

function TempSettingsRow({ ch, staged, setStaged, busy, onCommit, onToggleOutput }) {
  const tgt    = useLiveValue(ch.device_id, 3000, ch.instance);
  const obj    = useLiveValue(ch.device_id, 1000, ch.instance);
  const sink   = useLiveValue(ch.device_id, 1001, ch.instance);
  const out    = useLiveValue(ch.device_id, 2010, ch.instance);
  const stable = useLiveValue(ch.device_id, 1200, ch.instance);
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

export function SupplySettingsTable({ channels, holderId }) {
  const toast = useToast();
  const [staged, setStaged] = useState({});
  const [busy, setBusy] = useState({});
  async function commitField(deviceId, instance, param, value) {
    const key = `${deviceId}/${instance}/${param}`;
    setBusy((b) => ({ ...b, [key]: true }));
    try {
      let lease = MecomAPI.leases().find((l) => l.device_id === deviceId);
      if (!lease || lease.holder !== holderId) { lease = await MecomAPI.acquireLease(deviceId, holderId, "5m"); }
      await MecomAPI.write(deviceId, { name: "write_float32", arguments: { param, instance, value: parseFloat(value) } }, lease.token);
      toast.push({ kind: "ok", title: param === 1021 ? "Set voltage" : "Set current", body: `${deviceId}/${instance} → ${value}` });
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
            <th>Set V (1021)</th><th>Actual V</th><th>Set I (1020)</th><th>Actual I</th>
            <th>Power</th><th>Mode</th><th>Output</th>
          </tr>
        </thead>
        <tbody>
          {channels.map((ch) => <SupplySettingsRow
            key={ch.device_id + "/" + ch.instance} ch={ch}
            stagedV={staged[`${ch.device_id}/${ch.instance}/1021`] || ""}
            stagedI={staged[`${ch.device_id}/${ch.instance}/1020`] || ""}
            setStagedV={(v) => setStaged((s) => ({ ...s, [`${ch.device_id}/${ch.instance}/1021`]: v }))}
            setStagedI={(v) => setStaged((s) => ({ ...s, [`${ch.device_id}/${ch.instance}/1020`]: v }))}
            busyV={busy[`${ch.device_id}/${ch.instance}/1021`]}
            busyI={busy[`${ch.device_id}/${ch.instance}/1020`]}
            onCommitField={commitField} onToggleOutput={toggleOutput} />)}
        </tbody>
      </table>
    </div>
  );
}

function SupplySettingsRow({ ch, stagedV, stagedI, setStagedV, setStagedI, busyV, busyI, onCommitField, onToggleOutput }) {
  const actV  = useLiveValue(ch.device_id, 1021, ch.instance);
  const actI  = useLiveValue(ch.device_id, 1020, ch.instance);
  const power = useLiveValue(ch.device_id, 1022, ch.instance);
  const mode  = useLiveValue(ch.device_id, 2040, ch.instance);
  const out   = useLiveValue(ch.device_id, 2010, ch.instance);
  const setV  = MecomAPI.setpoint(ch.device_id, 1021, ch.instance);
  const setI  = MecomAPI.setpoint(ch.device_id, 1020, ch.instance);
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
