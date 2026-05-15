// @ts-nocheck
import React, { useState, useEffect, useMemo } from "react";
import { MecomAPI } from "../api/mecom";
import { Chip, useToast, useLiveValue, useGatewayTick, categorizeError } from "../components/atoms";
import { useAssignments, WALLS, channelColor } from "./assignments";

export function SignalDictionaryView() {
  useGatewayTick();
  const [tab, setTab] = useState("monitor");
  const [selected, setSelected] = useState(null);
  const [query, setQuery] = useState("");

  const catalogue = MecomAPI.catalogue();
  const channels = MecomAPI.channels();
  const settings = MecomAPI.settings();

  const filteredByRole = useMemo(() => catalogue.filter((p) => p.role === tab), [tab, catalogue]);

  const groups = useMemo(() => {
    const g = {};
    filteredByRole.forEach((p) => {
      if (!g[p.group]) g[p.group] = {};
      if (!g[p.group][p.subgroup]) g[p.group][p.subgroup] = [];
      g[p.group][p.subgroup].push(p);
    });
    return g;
  }, [filteredByRole]);

  useEffect(() => {
    if (selected && groups[selected.group] && groups[selected.group][selected.subgroup]) return;
    const firstGroup = Object.keys(groups)[0];
    if (firstGroup) {
      const firstSgrp = Object.keys(groups[firstGroup])[0];
      setSelected({ group: firstGroup, subgroup: firstSgrp });
    } else {
      setSelected(null);
    }
  }, [tab, groups]);

  const signals = selected && groups[selected.group] ? (groups[selected.group][selected.subgroup] || []) : [];

  return (
    <div className="dict">
      <div className="dict-rail">
        <div className="head">
          <div className="tab-row">
            <button className={tab === "monitor" ? "on" : ""} onClick={() => setTab("monitor")}>Telemetry</button>
            <button className={tab === "control" ? "on" : ""} onClick={() => setTab("control")}>Telecommands</button>
          </div>
          <input className="field" placeholder="search signals…" value={query} onChange={(e) => setQuery(e.target.value)} />
        </div>
        {Object.entries(groups).map(([grp, sgrps]) => (
          <GroupSection key={grp} group={grp} sgrps={sgrps} selected={selected} setSelected={setSelected} tab={tab} query={query} />
        ))}
      </div>

      <div className="dict-main">
        {selected && (
          <div className="dict-bread">
            <span className="grp">{selected.group}</span>
            <span className="sgrp"><span className="sep">›</span>{selected.subgroup}</span>
            <h2 style={{ marginLeft: 8 }}>{tab === "monitor" ? "Telemetry signals" : "Telecommand signals"}</h2>
            <Chip kind="accent">{signals.length} signal{signals.length === 1 ? "" : "s"}</Chip>
          </div>
        )}

        {signals.filter((s) => !query || s.name.toLowerCase().includes(query.toLowerCase()) || String(s.id).includes(query))
                .map((signal) => (
          <SignalBlock key={signal.id} signal={signal} channels={channels} holderId={settings.holder} tab={tab} />
        ))}

        {signals.length === 0 && (
          <div style={{ padding: 32, color: "var(--muted)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
            No signals here yet.
          </div>
        )}

        {selected && selected.group === "Metadata" && <ChannelsEditor />}
      </div>
    </div>
  );
}

function GroupSection({ group, sgrps, selected, setSelected, tab, query }) {
  const [open, setOpen] = useState(true);
  const total = Object.values(sgrps).reduce((a, b) => a + b.length, 0);
  if (query) {
    const filtered = {};
    Object.entries(sgrps).forEach(([k, sgs]) => {
      const matches = sgs.filter((s) => s.name.toLowerCase().includes(query.toLowerCase()) || String(s.id).includes(query));
      if (matches.length) filtered[k] = matches;
    });
    sgrps = filtered;
    if (Object.keys(sgrps).length === 0) return null;
  }
  return (
    <div className="dict-grp">
      <div className="grp-h" onClick={() => setOpen((o) => !o)}>
        <span>{open ? "▾" : "▸"}</span>
        <span>{group}</span>
        <span className="count">{total}</span>
      </div>
      {open && Object.entries(sgrps).map(([sgrp, items]) => {
        const sel = selected && selected.group === group && selected.subgroup === sgrp;
        const hasControl = items.some((s) => s.role === "control");
        return (
          <div key={sgrp} className={"sgrp" + (sel ? " on" : "")} onClick={() => setSelected({ group, subgroup: sgrp })}>
            <span className={"role-dot " + (hasControl ? "control" : "monitor")}></span>
            <span>{sgrp}</span>
            <span className="ct">{items.length}</span>
          </div>
        );
      })}
    </div>
  );
}

function SignalBlock({ signal, channels, holderId, tab }) {
  const assigns = useAssignments();
  const applicableChannels = useMemo(() => {
    if (!signal.applicableModes) return channels;
    return channels.filter((c) => signal.applicableModes.includes(c.role));
  }, [channels, signal.applicableModes]);

  const wallOptions = useMemo(() => {
    const opts = [];
    if (signal.role === "monitor") {
      if (signal.applicableModes && signal.applicableModes.includes("temp"))
        opts.push({ wall_id: WALLS.fleetTemp.wall_id, label: "Fleet hero · Temperature", kind: "trend" });
      if (signal.applicableModes && signal.applicableModes.includes("supply"))
        opts.push({ wall_id: WALLS.fleetSupply.wall_id, label: "Fleet hero · Power supplies", kind: "trend" });
    } else {
      if (signal.applicableModes && signal.applicableModes.includes("temp"))
        opts.push({ wall_id: "fleet-temp-cards", label: "Fleet · Temperature command cards", kind: "command" });
      if (signal.applicableModes && signal.applicableModes.includes("supply"))
        opts.push({ wall_id: "fleet-supply-cards", label: "Fleet · Power supply command cards", kind: "command" });
    }
    return opts;
  }, [signal]);

  return (
    <div className="dict-signal-block">
      <div className="dict-signal-head">
        <div className="nm">
          <span>{signal.name}</span>
          <span className="pid">MeCom #{signal.id} · {signal.type}</span>
        </div>
        <div className="badges">
          <Chip kind="accent">{signal.group}</Chip>
          <Chip>{signal.subgroup}</Chip>
          <Chip>kind · {signal.kind}</Chip>
          <Chip kind={signal.role === "control" ? "w" : ""}>{signal.role === "control" ? "control" : "monitor"}</Chip>
          {signal.unit && <Chip>{signal.unit}</Chip>}
          {signal.writable && <Chip kind="w">writable</Chip>}
        </div>
      </div>
      <div className="dict-signal-body">
        <div className="left">
          {tab === "monitor"
            ? <MonitorRows signal={signal} channels={applicableChannels} />
            : <CommandRows signal={signal} channels={applicableChannels} holderId={holderId} />}
        </div>
        <div className="dict-assign">
          <h4>{tab === "monitor" ? "Show in graph" : "Place command card in"}</h4>
          {wallOptions.length === 0 && (
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--muted)" }}>No applicable walls.</div>
          )}
          {wallOptions.map((opt) => (
            <SignalAssignmentRow key={opt.wall_id} signal={signal} wall={opt} channels={applicableChannels} assigns={assigns} />
          ))}
        </div>
      </div>
    </div>
  );
}

function SignalAssignmentRow({ signal, wall, channels, assigns }) {
  const allAssigned = channels.every((c) => assigns.hasAssignment(wall.wall_id, signal.id, c.device_id, c.instance));
  function toggle() {
    if (allAssigned) {
      channels.forEach((c) => assigns.remove(wall.wall_id, signal.id, c.device_id, c.instance));
    } else {
      channels.forEach((c) => assigns.add(wall.wall_id, signal.id, c.device_id, c.instance));
    }
  }
  const role = MecomAPI.roleForParam(signal.id);
  const color = MecomAPI.colorForRole(role);
  return (
    <label>
      <input type="checkbox" checked={allAssigned} onChange={toggle} />
      <span className="sw" style={{ background: color }}></span>
      {wall.label}
      <span className="key">{channels.length} channel{channels.length === 1 ? "" : "s"}</span>
    </label>
  );
}

function RoleSourceBadge({ channel }) {
  const confidence = MecomAPI.roleConfidence
    ? MecomAPI.roleConfidence(channel)
    : { kind: "warn", label: "unconfirmed", detail: "Channel role source is not available." };
  return <span className={"role-source " + confidence.kind} title={confidence.detail}>{confidence.label}</span>;
}

function RoleCell({ channel }) {
  return (
    <span className="role-cell">
      <span className={"role-pill " + channel.role}>{channel.role === "temp" ? "temp ctrl" : "supply"}</span>
      <RoleSourceBadge channel={channel} />
    </span>
  );
}

function MonitorRows({ signal, channels }) {
  return (
    <table className="dict-table">
      <thead>
        <tr>
          <th style={{ width: "30%" }}>Device · Channel</th>
          <th>Mode</th><th>Instance</th><th>Live value</th><th>Quality</th>
        </tr>
      </thead>
      <tbody>
        {channels.map((c) => <MonitorRow key={c.device_id + "/" + c.instance} signal={signal} channel={c} />)}
        {channels.length === 0 && (
          <tr><td colSpan={5} style={{ color: "var(--muted)" }}>No channels apply for this signal.</td></tr>
        )}
      </tbody>
    </table>
  );
}

function MonitorRow({ signal, channel }) {
  const v = useLiveValue(channel.device_id, signal.id, channel.instance);
  const disabled = v.quality === "unreachable";
  return (
    <tr className={disabled ? "disabled" : ""}>
      <td>
        <span style={{ display: "inline-block", width: 8, height: 8, borderRadius: 2, background: channelColor(channel.device_id, channel.instance), marginRight: 6 }}></span>
        {channel.device_id} <span style={{ color: "var(--muted)" }}>· {channel.label}</span>
      </td>
      <td><RoleCell channel={channel} /></td>
      <td>{channel.instance}</td>
      <td><b style={{ color: "var(--text)" }}>{MecomAPI.formatValue(v.value, signal.unit, signal.id)}</b>{signal.unit && <span style={{ color: "var(--muted)", marginLeft: 4 }}>{signal.unit}</span>}</td>
      <td><span className={"q " + v.quality}></span><span style={{ color: "var(--muted)" }}>{v.quality}</span></td>
    </tr>
  );
}

function CommandRows({ signal, channels, holderId }) {
  return (
    <table className="dict-table">
      <thead>
        <tr>
          <th style={{ width: "26%" }}>Device · Channel</th>
          <th>Mode</th><th>Inst</th><th>Current</th><th>Range</th><th>Quick write</th>
        </tr>
      </thead>
      <tbody>
        {channels.map((c) => <CommandRow key={c.device_id + "/" + c.instance} signal={signal} channel={c} holderId={holderId} />)}
        {channels.length === 0 && (
          <tr><td colSpan={6} style={{ color: "var(--muted)" }}>No channels apply for this signal.</td></tr>
        )}
      </tbody>
    </table>
  );
}

function CommandRow({ signal, channel, holderId }) {
  const v = useLiveValue(channel.device_id, signal.id, channel.instance);
  const [staged, setStaged] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  const disabled = v.quality === "unreachable";
  const isEnum = signal.kind === "enum" && signal.id === 2010;
  const stagedNum = parseFloat(staged);
  const stagedValid = isEnum
    ? ["0", "1", "2", "3"].includes(staged.trim())
    : (staged.trim() !== "" && !Number.isNaN(stagedNum)
       && (signal.min === undefined || stagedNum >= signal.min)
       && (signal.max === undefined || stagedNum <= signal.max));
  async function commit() {
    setBusy(true);
    try {
      let lease = MecomAPI.leases().find((l) => l.device_id === channel.device_id);
      if (!lease || lease.holder !== holderId) {
        lease = await MecomAPI.acquireLease(channel.device_id, holderId, "5m");
      }
      const value = isEnum ? parseInt(staged, 10) : stagedNum;
      await MecomAPI.write(channel.device_id, {
        name: MecomAPI.commandNameFor(signal),
        arguments: { param: signal.id, instance: channel.instance, value },
      }, lease.token);
      toast.push({ kind: "ok", title: signal.name, body: `${channel.device_id}/${channel.instance} → ${staged}${signal.unit ? " " + signal.unit : ""}` });
      setStaged("");
    } catch (err) {
      const c = categorizeError(err);
      toast.push({ kind: c.kind, title: c.cat.toUpperCase(), body: err.message });
    } finally {
      setBusy(false);
    }
  }
  return (
    <tr className={disabled ? "disabled" : ""}>
      <td>
        <span style={{ display: "inline-block", width: 8, height: 8, borderRadius: 2, background: channelColor(channel.device_id, channel.instance), marginRight: 6 }}></span>
        {channel.device_id} <span style={{ color: "var(--muted)" }}>· {channel.label}</span>
      </td>
      <td><RoleCell channel={channel} /></td>
      <td>{channel.instance}</td>
      <td><span style={{ color: "var(--series-actual)", fontWeight: 600 }}>{MecomAPI.formatValue(v.value, signal.unit, signal.id)}</span>{signal.unit && <span style={{ color: "var(--muted)", marginLeft: 4 }}>{signal.unit}</span>}</td>
      <td style={{ color: "var(--muted)" }}>
        {signal.min !== undefined || signal.max !== undefined
          ? `${signal.min ?? "—"} … ${signal.max ?? "—"}`
          : isEnum ? "0..3" : "—"}
      </td>
      <td>
        <span className="quick-write">
          <input className={staged ? "staged" : ""} placeholder={isEnum ? "0–3" : "value"} value={staged} disabled={disabled}
                 onChange={(e) => setStaged(e.target.value)} />
          <button disabled={!stagedValid || disabled || busy} onClick={commit}>{busy ? "…" : "Send"}</button>
        </span>
      </td>
    </tr>
  );
}

function ChannelsEditor() {
  useGatewayTick();
  const channels = MecomAPI.channels();
  const devices = MecomAPI.devices();
  function setRole(deviceId, instance, role) {
    MecomAPI.setChannelRole(deviceId, instance, role);
  }
  const rows = [];
  devices.forEach((d) => {
    [1, 2].forEach((inst) => {
      const ch = channels.find((c) => c.device_id === d.id && c.instance === inst);
      rows.push({ device: d, instance: inst, channel: ch });
    });
  });
  return (
    <div className="channels-editor" style={{ marginTop: 14 }}>
      <div className="head">
        <span>Channels configuration</span>
        <Chip>{channels.length} active</Chip>
      </div>
      <table>
        <thead>
          <tr><th style={{ width: "30%" }}>Device</th><th>Instance</th><th>Active</th><th>Role</th><th>Label</th></tr>
        </thead>
        <tbody>
          {rows.map(({ device, instance, channel }) => (
            <tr key={device.id + "/" + instance}>
              <td>
                <span style={{ display: "inline-block", width: 8, height: 8, borderRadius: 2, background: channel ? channelColor(device.id, instance) : "var(--muted-2)", marginRight: 6 }}></span>
                {device.label} <span style={{ color: "var(--muted)" }}>{device.id}</span>
              </td>
              <td>{instance}</td>
              <td>{channel ? <Chip kind="ok">active</Chip> : <Chip>inactive</Chip>}</td>
              <td>
                <div className="role-toggle">
                  <button className={"temp " + (channel && channel.role === "temp" ? "on" : "")}
                          onClick={() => setRole(device.id, instance, "temp")}>Temp ctrl</button>
                  <button className={"supply " + (channel && channel.role === "supply" ? "on" : "")}
                          onClick={() => setRole(device.id, instance, "supply")}>Supply</button>
                </div>
                {channel && <div style={{ marginTop: 4 }}><RoleSourceBadge channel={channel} /></div>}
              </td>
              <td style={{ color: "var(--muted)" }}>{channel ? channel.label : "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
