// @ts-nocheck
import React, { useState, useEffect, useRef, useMemo, useCallback } from "react";
import { MecomAPI } from "../api/mecom";
import { Chip, Pill, Panel, MultiChart, useToast, useLiveValue, useGatewayTick, categorizeError, DiscoveryTree, SignalValueCard } from "../components/atoms";
import { DEFAULT_TILE_LEVELS, renderSeriesFromGraphTile } from "../lib/series";
import { useAssignments, WALLS, wallForDevice, normalizeAssignment, useGraphTileFromAssignments, channelColor, assignmentsWithPriorityDefaults, graphBucketForParam, GRAPH_ORIGIN_DEVICE_ID } from "./assignments";
import { HeroGraph, TempSettingsTable, SupplySettingsTable } from "./hero";

function formatRouteLabel(route) {
  const kind = String(route?.kind || route?.role || "").toLowerCase();
  const label = String(route?.label || route?.name || "").trim();
  const detail = String(route?.detail || "").trim();
  const routeText = kind === "hot"
    ? "hot CAN path"
    : kind === "warm"
      ? "warm serial/FTDI"
      : kind === "fallback"
        ? "warm fallback route"
        : "route";
  const title = label || routeText;
  const suffix = detail ? ` · ${detail}` : "";
  return { title, text: `${title} · ${routeText}${suffix}` };
}

const GRAPH_TILE_WINDOW_OPTIONS = DEFAULT_TILE_LEVELS;

function graphTileWindowForLevel(level) {
  return GRAPH_TILE_WINDOW_OPTIONS.find((option) => option.level === level) || GRAPH_TILE_WINDOW_OPTIONS[0];
}

export function FleetView({ onOpenDevice }) {
  useGatewayTick();
  const assigns = useAssignments();
  const channels = MecomAPI.channels();
  const settings = MecomAPI.settings();

  const tempChannels = channels.filter((c) => c.role === "temp");
  const supplyChannels = channels.filter((c) => c.role === "supply");
  const originDeviceId = (typeof MecomAPI.primaryDeviceId === "function" && MecomAPI.primaryDeviceId()) || GRAPH_ORIGIN_DEVICE_ID;
  const originTempChannels = tempChannels.filter((c) => c.device_id === originDeviceId);
  const originSupplyChannels = supplyChannels.filter((c) => c.device_id === originDeviceId);
  const originTempStored = assigns.forWall(WALLS.fleetTemp.wall_id).filter((a) => normalizeAssignment(a).device_id === originDeviceId);
  const originSupplyStored = assigns.forWall(WALLS.fleetSupply.wall_id).filter((a) => normalizeAssignment(a).device_id === originDeviceId);

  const fleetTempAssignments = assignmentsWithPriorityDefaults(originTempStored, WALLS.fleetTemp.wall_id, originTempChannels);
  const fleetSupplyAssignments = assignmentsWithPriorityDefaults(originSupplyStored, WALLS.fleetSupply.wall_id, originSupplyChannels);

  const tempTile = useGraphTileFromAssignments(fleetTempAssignments.filter((a) => graphBucketForParam(normalizeAssignment(a).param_id) === "thermal"), {
    tile_id: WALLS.fleetTemp.wall_id,
    title: WALLS.fleetTemp.label,
    colorByChannel: false,
    timeWindowMs: 90_000,
    level: "live",
  });
  const supplyTile = useGraphTileFromAssignments(fleetSupplyAssignments.filter((a) => graphBucketForParam(normalizeAssignment(a).param_id) === "power"), {
    tile_id: WALLS.fleetSupply.wall_id,
    title: WALLS.fleetSupply.label,
    colorByChannel: false,
    timeWindowMs: 90_000,
    level: "live",
  });

  return (
    <div className="fleet">
      <div className="fleet-heroes">
        <HeroGraph wall={WALLS.fleetTemp} role="temp" tile={tempTile} height={260}>
          <TempSettingsTable channels={tempChannels} holderId={settings.holder} />
        </HeroGraph>
        <HeroGraph wall={WALLS.fleetSupply} role="supply" tile={supplyTile} height={260}>
          {supplyChannels.length === 0 ? (
            <div style={{ padding: 24, textAlign: "center", color: "var(--muted)", fontFamily: "var(--font-mono)", fontSize: 12 }}>
              No supply-mode channels configured. Set a channel role under Signal Dictionary → Metadata.
            </div>
          ) : (
            <SupplySettingsTable channels={supplyChannels} holderId={settings.holder} />
          )}
        </HeroGraph>
      </div>
    </div>
  );
}

export function DeviceMini({ device, onOpen }) {
  const channels = MecomAPI.channels().filter((c) => c.device_id === device.id);
  const leases = MecomAPI.leases();
  const lease = leases.find((l) => l.device_id === device.id);
  const bs = MecomAPI.brokerStats(device.id);
  const routes = Array.isArray(device.routes) ? device.routes : [];
  return (
    <div className={"dev-mini" + (device.last_error ? " bad" : "")} onClick={onOpen}>
      <div className="top">
        <span className="name">{device.label}</span>
        <span className="id">{device.id}</span>
        <span className="right">
          <Pill kind={device.bound ? "ok" : "bad"}>{device.bound ? "bound" : "unbound"}</Pill>
        </span>
      </div>
      <div className="chans">
        {channels.map((c) => (
          <Chip key={c.instance} kind={c.role === "supply" ? "warn" : "accent"}>
            channel {c.instance} · {c.role === "supply" ? "supply" : "temperature"}
          </Chip>
        ))}
        {channels.length === 0 && <Chip>no channels</Chip>}
        {lease && <Chip kind={lease.holder === MecomAPI.settings().holder ? "accent" : "warn"} title={lease.holder}>{lease.holder === MecomAPI.settings().holder ? "you hold" : lease.holder}</Chip>}
      </div>
      <div className="routes" title="Connection redundancy">
        {routes.map((route) => (
          <Chip key={`${device.id}:${route.label}`} kind={route.kind === "hot" ? "accent" : "warn"} title={route.detail || route.label}>
            {formatRouteLabel(route).text}
          </Chip>
        ))}
        {routes.length === 0 && <Chip kind="warn">no redundancy data</Chip>}
      </div>
      <div className="stats">
        <span title="CAN receive frames counted by the gateway since connection">{bs.frames_in.toLocaleString()} CAN RX frames</span>
        <span title="CAN transmit frames counted by the gateway since connection">{bs.frames_out.toLocaleString()} CAN TX frames</span>
        {bs.error_count > 0 && <span style={{ color: "var(--warn)" }} title="CAN bus or gateway transport errors">{bs.error_count} bus errors</span>}
        <span className="endpoint" title={device.endpoint}>{device.endpoint}</span>
      </div>
    </div>
  );
}

export function CommandFeed({ events }) {
  return (
    <div className="feed">
      {events.length === 0 && <div style={{ padding: 14, color: "var(--muted)", fontFamily: "var(--font-mono)", fontSize: 12 }}>No gateway command attempts recorded yet.</div>}
      {events.slice(0, 16).map((e) => {
        const t = new Date(e.time);
        const hh = String(t.getHours()).padStart(2, "0") + ":" + String(t.getMinutes()).padStart(2, "0") + ":" + String(t.getSeconds()).padStart(2, "0");
        let stChip = "ok";
        if (e.status === "rejected" || e.status === "failed") stChip = "bad";
        if (e.status === "accepted" || e.status === "sent") stChip = "accent";
        let msg;
        if (e.error) {
          msg = e.error_category ? `[${e.error_category}] ${e.error}` : e.error;
        } else if (e.signal_name !== undefined) {
          const unit = e.unit ?? e.signal_unit ?? "";
          const prev = e.prev_value === null || e.prev_value === undefined ? "—" : MecomAPI.formatWithUnit(e.prev_value, unit, e.param_id);
          const req = e.requested_value === null || e.requested_value === undefined ? "—" : MecomAPI.formatWithUnit(e.requested_value, unit, e.param_id);
          msg = `${e.signal_name} ${prev} → ${req}`;
        } else if (e.result && e.result.arguments) {
          msg = `${e.result.name || "write"} param=${e.result.arguments.param}` +
                (e.result.arguments.instance !== undefined ? `/${e.result.arguments.instance}` : "") +
                (e.result.arguments.value !== undefined ? ` value=${e.result.arguments.value}` : "");
        } else {
          msg = e.command_id || "—";
        }
        const tooltip = [
          e.transport && `endpoint=${e.transport}`,
          e.lease_holder && `holder=${e.lease_holder}`,
          e.instance !== undefined && e.param_id !== undefined && `param=${e.param_id} instance=${e.instance}`,
        ].filter(Boolean).join(" ");
        return (
          <div key={e.command_id || `${e.time}:${e.target_id}:${e.param_id}:${e.instance}`} className="ev">
            <span className="t">{hh}</span>
            <span className="dev">{e.target_id || "—"}{e.instance !== undefined ? "/" + e.instance : ""}</span>
            <span className="msg" title={tooltip}>{msg}</span>
            <Chip kind={stChip}>{e.status}</Chip>
          </div>
        );
      })}
    </div>
  );
}

export function LeaseSummary({ leases, holder }) {
  if (!leases.length) {
    return <div style={{ padding: 14, color: "var(--muted)", fontFamily: "var(--font-mono)", fontSize: 12 }}>No active leases.</div>;
  }
  return (
    <div className="feed">
      {leases.map((l) => {
        const remain = Math.max(0, new Date(l.expires_at).getTime() - Date.now());
        const mins = Math.floor(remain / 60000);
        const secs = Math.floor((remain % 60000) / 1000);
        const mine = l.holder === holder;
        return (
          <div key={l.token} className="ev" style={{ gridTemplateColumns: "76px minmax(0,1fr) auto" }}>
            <span className="t">{l.device_id}</span>
            <span className="msg">
              held by <b style={{ color: mine ? "var(--accent)" : "var(--text)" }}>{l.holder}</b>
            </span>
            <Chip kind={mine ? "accent" : "warn"}>{mins}m {String(secs).padStart(2, "0")}s</Chip>
          </div>
        );
      })}
    </div>
  );
}

export function DeviceWorkspace({ deviceId, onOpenSequencer }) {
  useGatewayTick();
  const catalogue = MecomAPI.catalogue();
  const device = MecomAPI.devices().find((d) => d.id === deviceId);
  const allChannels = MecomAPI.channels();
  const channels = allChannels.filter((c) => c.device_id === deviceId);
  const [activeChannelInst, setActiveChannelInst] = useState(() => channels[0]?.instance || 1);
  const activeChannel = channels.find((c) => c.instance === activeChannelInst) || channels[0];

  const settings = MecomAPI.settings();
  const leases = MecomAPI.leases();
  const lease = leases.find((l) => l.device_id === deviceId);
  const youHold = lease && lease.holder === settings.holder;
  const assigns = useAssignments();
  const [tileLevel, setTileLevel] = useState("live");
  const tileWindow = graphTileWindowForLevel(tileLevel);
  const tileWindowMs = tileWindow.timeWindowMs;
  const [hiddenSeries, setHiddenSeries] = useState({});
  const [ignoreOpenSensorOutliers, setIgnoreOpenSensorOutliers] = useState(true);
  const routes = Array.isArray(device?.routes) ? device.routes : [];

  const deviceWallId = wallForDevice(deviceId).wall_id + "-" + (activeChannel?.instance || 1);
  const storedPins = assigns.forWall(deviceWallId);
  const pins = assignmentsWithPriorityDefaults(storedPins, deviceWallId, activeChannel ? [activeChannel] : []);
  const graphSections = useMemo(() => {
    const sourcePins = device && activeChannel ? pins : [];
    const defs = [
      { bucket: "thermal", suffix: "thermal", title: "Temperature", empty: "Pin temperature parameters from the signal catalogue." },
      { bucket: "power", suffix: "power", title: "Power", empty: "Pin output power from the signal catalogue." },
      { bucket: "voltage", suffix: "voltage", title: "Voltage", empty: "Pin voltage parameters from the signal catalogue." },
      { bucket: "current", suffix: "current", title: "Current", empty: "Pin current parameters from the signal catalogue." },
      { bucket: "other", suffix: "other", title: "Status and metadata", empty: "Pin status, counter, or metadata parameters from the signal catalogue." },
    ];
    return defs.map((def) => {
      const bucketPins = sourcePins.filter((p) => graphBucketForParam(normalizeAssignment(p).param_id) === def.bucket);
      const tileId = `${deviceWallId}-${def.suffix}`;
      const tileOptions = {
        tile_id: tileId,
        title: `${device ? device.label : deviceId} · channel ${activeChannel ? activeChannel.instance : activeChannelInst} · ${def.title}`,
        colorByChannel: false,
        timeWindowMs: tileWindowMs,
        level: tileLevel,
        ignoreOpenSensorOutliers: ignoreOpenSensorOutliers && def.bucket === "thermal",
      };
      return { ...def, tileId, bucketPins, tileOptions };
    }).filter((section) => section.bucketPins.length > 0 || section.bucket === "thermal" || section.bucket === "power");
  }, [assigns.list, deviceWallId, device?.label, deviceId, activeChannel?.instance, activeChannelInst, tileWindowMs, tileLevel, ignoreOpenSensorOutliers]);
  function toggleSeriesVisibility(tileId, key) {
    setHiddenSeries((cur) => {
      const current = new Set(cur[tileId] || []);
      if (current.has(key)) current.delete(key);
      else current.add(key);
      return { ...cur, [tileId]: Array.from(current) };
    });
  }

  const pinShape = useMemo(() => pins.map((p) => {
    const a = normalizeAssignment(p);
    const paramId = a.options.param_id;
    const inst = a.options.instance || activeChannel?.instance || 1;
    return { id: paramId, instance: inst, color: MecomAPI.colorForRole(MecomAPI.roleForParam(paramId)) };
  }), [assigns.list, deviceWallId]);

  useEffect(() => {
    if (!activeChannel) return;
    const cur = assigns.forWall(deviceWallId);
    if (cur.length > 0) return;
    const defaults = activeChannel.role === "temp" ? [3000, 1000, 1001] : [1022];
    defaults.forEach((pid) => assigns.add(deviceWallId, pid, deviceId, activeChannel.instance));
  }, [deviceWallId, activeChannel?.instance, activeChannel?.role]);

  const cardsKey = "mecomgw.cards." + deviceId + "." + activeChannelInst;
  const [cards, setCards] = useState(() => {
    try { return JSON.parse(localStorage.getItem(cardsKey)) || []; } catch (_) { return []; }
  });
  useEffect(() => { localStorage.setItem(cardsKey, JSON.stringify(cards)); }, [cardsKey, cards]);
  useEffect(() => {
    try {
      const k = "mecomgw.cards." + deviceId + "." + activeChannelInst;
      setCards(JSON.parse(localStorage.getItem(k)) || []);
    } catch (_) { setCards([]); }
  }, [deviceId, activeChannelInst]);

  const [query, setQuery] = useState("");
  const [filterCat, setFilterCat] = useState("");
  const [onlyWritable, setOnlyWritable] = useState(false);
  const toast = useToast();

  if (!device) return <div style={{ padding: 32, color: "var(--muted)" }}>Device {deviceId} not found.</div>;
  if (!activeChannel) return <div style={{ padding: 32, color: "var(--muted)" }}>No channels active for {deviceId}.</div>;

  const deviceRoles = new Set(channels.map((c) => c.role));
  const applicableCatalogue = catalogue.filter((p) => !p.applicableModes || p.applicableModes.some((role) => deviceRoles.has(role)));

  function togglePin(param, instance) {
    const inst = instance || activeChannel.instance;
    if (assigns.hasAssignment(deviceWallId, param.id, deviceId, inst)) {
      assigns.remove(deviceWallId, param.id, deviceId, inst);
    } else {
      assigns.add(deviceWallId, param.id, deviceId, inst);
    }
  }
  function openCard(param, instance) {
    const inst = instance || activeChannel.instance;
    setCards((cur) => cur.find((c) => c.id === param.id && (c.instance || 1) === inst) ? cur : cur.concat({ id: param.id, instance: inst }));
  }
  function closeCard(id, instance) { setCards((cur) => cur.filter((c) => !(c.id === id && (c.instance || 1) === (instance || 1)))); }

  async function quickAcquire() {
    try {
      await MecomAPI.acquireLease(deviceId, settings.holder, "5m");
      toast.push({ kind: "ok", title: "Lease acquired", body: deviceId });
    } catch (err) {
      const c = categorizeError(err);
      toast.push({ kind: c.kind, title: c.cat.toUpperCase(), body: err.message });
    }
  }
  async function quickRelease() {
    if (!lease) return;
    await MecomAPI.releaseLease(deviceId, lease.token);
    toast.push({ kind: "ok", title: "Lease released", body: deviceId });
  }

  const bs = MecomAPI.brokerStats(deviceId);

  return (
    <div className="ws">
      <DiscoveryTree
        deviceId={deviceId}
        instance={activeChannel.instance}
        channels={channels}
        catalogue={applicableCatalogue}
        pins={pinShape}
        onTogglePin={(p, inst) => togglePin(p, inst)}
        onPinCard={(p, inst) => openCard(p, inst)}
        onWrite={(p, inst) => openCard(p, inst)}
        onlyWritable={onlyWritable}
        query={query}
        setQuery={setQuery}
        filterCat={filterCat}
        setFilterCat={setFilterCat}
        writeCards={cards}
        leaseHolder={lease ? lease.holder : null}
        holderId={settings.holder}
        onCloseWrite={closeCard}
      />
      <div className="canvas">
        <div className="ws-head">
          <h2>{device.label} <span className="id">{device.id} · {device.endpoint} · address {device.address}</span></h2>
          <div className="right">
            <div className="route-strip" title="Connection redundancy">
              {routes.map((route) => (
                <Chip key={`${device.id}:${route.label}`} kind={route.kind === "hot" ? "accent" : "warn"} title={route.detail || route.label}>
                  {formatRouteLabel(route).text}
                </Chip>
              ))}
            </div>
            {channels.length > 1 && (
              <div className="role-toggle" style={{ height: 28 }}>
                {channels.map((c) => (
                  <button key={c.instance}
                          className={(c.role === "temp" ? "temp " : "supply ") + (c.instance === activeChannelInst ? "on" : "")}
                          style={{ padding: "0 12px" }}
                          onClick={() => setActiveChannelInst(c.instance)}>
                    channel {c.instance} · {c.role === "temp" ? "temperature" : c.role}
                  </button>
                ))}
              </div>
            )}
            {channels.length === 1 && (
              <Chip kind={activeChannel.role === "supply" ? "warn" : "accent"}>channel {activeChannel.instance} · {activeChannel.role === "supply" ? "supply" : "temperature control"}</Chip>
            )}
            <button className="btn sm" onClick={() => setOnlyWritable((v) => !v)}>{onlyWritable ? "All params" : "Writable only"}</button>
            <button className="btn sm" onClick={onOpenSequencer}>Run sequence ▸</button>
            {lease ? (
              <>
                <Pill kind={youHold ? "info" : "warn"}>{youHold ? "lease · you" : "lease · " + lease.holder}</Pill>
                {youHold && <button className="btn sm" onClick={quickRelease}>Release</button>}
              </>
            ) : (
              <>
                <Pill kind="ok">lease free</Pill>
                <button className="btn sm primary" onClick={quickAcquire}>Acquire lease</button>
              </>
            )}
          </div>
        </div>

        <div style={{ display: "flex", gap: 12, flexWrap: "wrap", fontFamily: "var(--font-mono)", fontSize: 11.5, color: "var(--muted)" }}>
          <span>connected <b style={{ color: bs.connected ? "var(--ok)" : "var(--bad)" }}>{String(bs.connected)}</b></span>
          <span>CAN RX frames <b style={{ color: "var(--text)" }}>{bs.frames_in.toLocaleString()}</b></span>
          <span>CAN TX frames <b style={{ color: "var(--text)" }}>{bs.frames_out.toLocaleString()}</b></span>
          <span>bus errors <b style={{ color: bs.error_count ? "var(--warn)" : "var(--text)" }}>{bs.error_count}</b></span>
          <span>last connect <b style={{ color: "var(--text)" }}>{bs.last_connect_at ? new Date(bs.last_connect_at).toLocaleTimeString() : "—"}</b></span>
        </div>

        {cards.length > 0 && (
          <div className="signal-card-strip">
            {cards.map((c) => {
              const param = catalogue.find((p) => p.id === c.id);
              if (!param) return null;
              const inst = c.instance || activeChannel.instance;
              return (
                <SignalValueCard
                  key={c.id + ":" + inst}
                  deviceId={deviceId}
                  param={{ ...param, instance: inst }}
                  leaseHolder={lease ? lease.holder : null}
                  holderId={settings.holder}
                  onClose={() => closeCard(c.id, inst)}
                />
              );
            })}
          </div>
        )}

        <div className="chart-toolbar">
          <select className="select-sm" value={tileLevel} onChange={(e) => setTileLevel(e.target.value)}>
            {GRAPH_TILE_WINDOW_OPTIONS.map((option) => (
              <option key={option.level} value={option.level}>{option.label}</option>
            ))}
          </select>
          <Chip>{tileLevel} tile</Chip>
          <Chip>live refresh 500 ms</Chip>
          <label className="toggle-sm">
            <input type="checkbox" checked={ignoreOpenSensorOutliers} onChange={(e) => setIgnoreOpenSensorOutliers(e.target.checked)} />
            ignore open-sensor lows
          </label>
        </div>

        {graphSections.map((section) => (
          <GraphSectionCard
            key={section.tileId}
            section={section}
            hiddenForTile={hiddenSeries[section.tileId] || []}
            onToggleSeriesVisibility={toggleSeriesVisibility}
          />
        ))}
      </div>
    </div>
  );
}

function GraphSectionCard({ section, hiddenForTile, onToggleSeriesVisibility }) {
  const tile = useGraphTileFromAssignments(section.bucketPins, section.tileOptions);
  const series = useMemo(() => renderSeriesFromGraphTile(tile), [tile]);
  const suppressed = tile?.diagnostics?.suppressed_open_sensor_points || 0;
  return (
    <div className="chart-card" key={section.tileId}>
      <div className="head">
        <div className="title">{section.title}</div>
        <div className="right">
          {suppressed > 0 && <Chip kind="warn">{suppressed} outliers hidden</Chip>}
          <Chip>{tile.level} tile</Chip>
          <Chip>{tile.diagnostics?.tile_source || "gateway tile"}</Chip>
        </div>
      </div>
      <div className="body">
        {series.length === 0 ? (
          <div className="empty">{section.empty}<br />Use the signal catalogue on the left; star an instance to add it.</div>
        ) : (
          <div className="chart-layout">
            <div className="chart-plot">
              <MultiChart tile={tile} height={section.bucket === "other" ? 260 : 320} hiddenSeries={hiddenForTile} fill minHeight={section.bucket === "other" ? 220 : 280} />
            </div>
            <div className="legend chart-legend">
              {series.map((s) => {
                const last = s.history.v[s.history.v.length - 1];
                const off = hiddenForTile.includes(s.key);
                return (
                  <span key={s.key} className={"item " + (off ? "off" : "")} onClick={() => onToggleSeriesVisibility(section.tileId, s.key)} title={s.fullLabel || "Click to show/hide this tile series"}>
                    <span className="sw" style={{ background: s.color }}></span>
                    <span className="series-label">{s.label}</span>
                    <span className="cur">{MecomAPI.formatWithUnit(last, s.unit, s.paramId)}</span>
                  </span>
                );
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
