// @ts-nocheck
import React, { useState, useEffect, useRef, useMemo, useCallback } from "react";
import { MecomAPI } from "../api/mecom";
import { Chip, Pill, Panel, MultiChart, useToast, useLiveValue, useGatewayTick, categorizeError, DiscoveryTree, SignalValueCard } from "../components/atoms";
import { DEFAULT_TILE_LEVELS, graphSeriesIdentityKey, renderSeriesFromGraphTile } from "../lib/series";
import { useAssignments, WALLS, wallForDevice, normalizeAssignment, useGraphTileFromAssignments, channelColor, assignmentsWithPriorityDefaults, graphBucketForParam, DEFAULT_GRAPH_TILE_LEVEL, graphTileWindowForLevel } from "./assignments";
import { HeroGraph, TempSettingsTable, SupplySettingsTable } from "./hero";

function routeRole(route) {
  return String(route?.kind || route?.role || "").toLowerCase();
}

function routeEndpoint(route) {
  return String(route?.endpoint || "").trim();
}

function routeTransport(route) {
  const explicit = String(route?.transport || "").trim().toLowerCase();
  if (explicit) return explicit;
  const endpoint = routeEndpoint(route).toLowerCase();
  const label = `${route?.label || ""} ${route?.name || ""} ${route?.detail || ""}`.toLowerCase();
  if (endpoint.startsWith("serial+can:")) return "serial+can";
  if (endpoint.startsWith("can:") || endpoint.startsWith("canopen:") || label.includes("can")) return "can";
  if (endpoint.startsWith("serial:") || endpoint.includes("tty") || label.includes("ftdi") || label.includes("rs485") || label.includes("rs-485")) return "serial";
  if (endpoint.startsWith("tcp:")) return "tcp";
  return "";
}

function uninformativeRouteLabel(value) {
  const label = String(value || "").trim().toLowerCase();
  return !label || label === "configured" || label === "route" || label === "hot path" || label === "warm path" || label === "fallback";
}

function canInterfaceName(endpoint) {
  const match = String(endpoint || "").match(/^can(?:open)?:([^/]+)/i);
  return match ? match[1] : "";
}

function canonicalRouteName(route) {
  const raw = String(route?.label || route?.name || "").trim();
  const endpoint = routeEndpoint(route);
  const haystack = `${raw} ${route?.detail || ""} ${endpoint}`.toLowerCase();
  if (haystack.includes("pixtend")) return "PiXtend CAN";
  if (haystack.includes("kvaser")) return "Kvaser USB CAN";
  if (haystack.includes("ftdi") || haystack.includes("rs485") || haystack.includes("rs-485")) return "USB FTDI RS485";
  if (!uninformativeRouteLabel(raw)) return raw;

  const transport = routeTransport(route);
  if (transport === "can" || transport === "canopen") {
    const iface = canInterfaceName(endpoint);
    return iface ? `SocketCAN ${iface}` : "CAN bus";
  }
  if (transport === "serial+can") return "Serial MeCom over CAN";
  if (transport === "serial") return "Serial RS485";
  if (transport === "tcp") return "TCP route";
  return "Connection route";
}

function routeRoleText(route) {
  const role = routeRole(route);
  const transport = routeTransport(route);
  const isCan = transport === "can" || transport === "canopen" || transport === "serial+can";
  const isSerial = transport === "serial";
  if (role === "hot") return isCan ? "hot CAN" : isSerial ? "hot serial" : "hot route";
  if (role === "warm") return isCan ? "warm CAN" : isSerial ? "warm serial" : "warm standby";
  if (role === "fallback") return isCan ? "fallback CAN" : isSerial ? "fallback serial" : "fallback route";
  return "route";
}

function routeChipKind(route) {
  const role = routeRole(route);
  if (role === "hot") return "accent";
  if (role === "fallback") return "warn";
  return "warn";
}

function routeChipKey(deviceId, route) {
  return [deviceId, routeRole(route), routeEndpoint(route), route?.label || route?.name || ""].join(":");
}

function formatRouteLabel(route) {
  const name = canonicalRouteName(route);
  const roleText = routeRoleText(route);
  const detail = [
    roleText,
    route?.state,
    routeTransport(route),
    routeEndpoint(route),
  ].filter(Boolean).join(" · ");
  return { title: detail ? `${name} · ${detail}` : name, text: `${name} · ${roleText}` };
}

const GRAPH_TILE_WINDOW_OPTIONS = DEFAULT_TILE_LEVELS;

function channelSortRank(channel) {
  const instance = Number(channel?.instance || 0);
  const role = String(channel?.role || "");
  const roleRank = role === "temp" ? 0 : role === "supply" ? 1 : 2;
  return instance * 10 + roleRank;
}

const DEFAULT_HIDDEN_QUALITIES = new Set(["missing", "detached", "open_sensor", "no_data", "unreachable", "nan", "error"]);

function seriesKey(series) {
  return graphSeriesIdentityKey(series);
}

function seriesQuality(series) {
  const quality = series?.quality ?? series?.source_quality ?? series?.diagnostics?.quality ?? series?.source?.quality;
  return String(quality || "").trim().toLowerCase();
}

function seriesDefaultVisible(series) {
  const explicit = series?.default_visible ?? series?.defaultVisible;
  if (explicit === false) return false;
  if (explicit === true) return true;
  return null;
}

function seriesValues(series) {
  const values = [];
  (series?.history?.v || []).forEach((value) => values.push(Number(value)));
  (series?.points || []).forEach((point) => values.push(Number(point?.value ?? point?.v)));
  return values;
}

function isDegreeCSeries(series) {
  const unit = String(series?.unit || "").toLowerCase();
  return unit === "degc" || unit === "c" || unit.includes("deg") || unit.includes("°") || unit.includes("celsius");
}

function hasFiniteInFamilyValue(series) {
  const degreeC = isDegreeCSeries(series);
  return seriesValues(series).some((value) => Number.isFinite(value) && (!degreeC || value > -50));
}

function defaultHiddenSeriesForTile(tile) {
  const hiddenKeys = new Set();
  (tile?.series || []).forEach((series) => {
    const key = seriesKey(series);
    if (!key) return;
    const quality = seriesQuality(series);
    if (
      seriesDefaultVisible(series) === false ||
      DEFAULT_HIDDEN_QUALITIES.has(quality) ||
      (isDegreeCSeries(series) && !hasFiniteInFamilyValue(series))
    ) {
      hiddenKeys.add(key);
    }
  });
  return Array.from(hiddenKeys);
}

export function FleetView({ onOpenDevice }) {
  useGatewayTick();
  const assigns = useAssignments();
  const channels = MecomAPI.channels();
  const settings = MecomAPI.settings();

  const tempChannels = channels.filter((c) => c.role === "temp");
  const supplyChannels = channels.filter((c) => c.role === "supply");
  const fleetTempStored = assigns.forWall(WALLS.fleetTemp.wall_id);
  const fleetSupplyStored = assigns.forWall(WALLS.fleetSupply.wall_id);

  const fleetTempAssignments = assignmentsWithPriorityDefaults(fleetTempStored, WALLS.fleetTemp.wall_id, tempChannels);
  const fleetSupplyAssignments = assignmentsWithPriorityDefaults(fleetSupplyStored, WALLS.fleetSupply.wall_id, supplyChannels);
  const [fleetTileLevel, setFleetTileLevel] = useState(DEFAULT_GRAPH_TILE_LEVEL);
  const fleetTileWindow = graphTileWindowForLevel(fleetTileLevel);

  const tempTile = useGraphTileFromAssignments(fleetTempAssignments.filter((a) => graphBucketForParam(normalizeAssignment(a).param_id) === "thermal"), {
    tile_id: WALLS.fleetTemp.wall_id,
    title: WALLS.fleetTemp.label,
    colorByChannel: false,
    timeWindowMs: fleetTileWindow.timeWindowMs,
    level: fleetTileLevel,
  });
  const supplyTile = useGraphTileFromAssignments(fleetSupplyAssignments.filter((a) => graphBucketForParam(normalizeAssignment(a).param_id) === "power"), {
    tile_id: WALLS.fleetSupply.wall_id,
    title: WALLS.fleetSupply.label,
    colorByChannel: false,
    timeWindowMs: fleetTileWindow.timeWindowMs,
    level: fleetTileLevel,
  });
  const defaultTempHidden = useMemo(() => defaultHiddenSeriesForTile(tempTile), [tempTile]);
  const defaultSupplyHidden = useMemo(() => defaultHiddenSeriesForTile(supplyTile), [supplyTile]);

  return (
    <div className="fleet">
      <div className="chart-toolbar fleet-chart-toolbar">
        <label>
          History
          <select value={fleetTileLevel} onChange={(event) => setFleetTileLevel(event.target.value)}>
            {GRAPH_TILE_WINDOW_OPTIONS.map((option) => (
              <option key={option.level} value={option.level}>{option.label}</option>
            ))}
          </select>
        </label>
        <Chip>{fleetTileWindow.label} shared timeline</Chip>
      </div>
      <div className="fleet-heroes">
        <HeroGraph wall={WALLS.fleetTemp} role="temp" tile={tempTile} height={340} initialHiddenSeries={defaultTempHidden}>
          <TempSettingsTable channels={tempChannels} holderId={settings.holder} />
        </HeroGraph>
        <HeroGraph wall={WALLS.fleetSupply} role="supply" tile={supplyTile} height={340} initialHiddenSeries={defaultSupplyHidden}>
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
          <Chip key={routeChipKey(device.id, route)} kind={routeChipKind(route)} title={formatRouteLabel(route).title}>
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
  const channels = useMemo(() => allChannels
    .filter((c) => c.device_id === deviceId)
    .slice()
    .sort((a, b) => channelSortRank(a) - channelSortRank(b)), [allChannels, deviceId]);
  const channelKey = channels.map((c) => `${c.instance}:${c.role || ""}`).join("|");
  const defaultChannelInst = channels.find((c) => c.instance === 1)?.instance
    || channels.find((c) => c.role === "temp")?.instance
    || channels[0]?.instance
    || 1;
  const [activeChannelInst, setActiveChannelInst] = useState(defaultChannelInst);
  useEffect(() => {
    if (!channels.length) return;
    if (!channels.some((c) => c.instance === activeChannelInst)) {
      setActiveChannelInst(defaultChannelInst);
    }
  }, [activeChannelInst, channelKey, defaultChannelInst]);
  const activeChannel = channels.find((c) => c.instance === activeChannelInst)
    || channels.find((c) => c.instance === defaultChannelInst)
    || channels[0];

  const settings = MecomAPI.settings();
  const leases = MecomAPI.leases();
  const lease = leases.find((l) => l.device_id === deviceId);
  const youHold = lease && lease.holder === settings.holder;
  const assigns = useAssignments();
  const [tileLevel, setTileLevel] = useState(DEFAULT_GRAPH_TILE_LEVEL);
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
  function applyDefaultHiddenSeries(tileId, keys, validKeys) {
    setHiddenSeries((cur) => {
      const valid = new Set(validKeys || []);
      const previous = cur[tileId] || [];
      const current = new Set(previous.filter((key) => valid.size === 0 || valid.has(key)));
      let changed = current.size !== previous.length;
      (keys || []).forEach((key) => {
        if (!key || (valid.size > 0 && !valid.has(key)) || current.has(key)) return;
        current.add(key);
        changed = true;
      });
      if (!changed) return cur;
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
                <Chip key={routeChipKey(device.id, route)} kind={routeChipKind(route)} title={formatRouteLabel(route).title}>
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
            onApplyDefaultHidden={applyDefaultHiddenSeries}
          />
        ))}
      </div>
    </div>
  );
}

function GraphSectionCard({ section, hiddenForTile, onToggleSeriesVisibility, onApplyDefaultHidden }) {
  const tile = useGraphTileFromAssignments(section.bucketPins, section.tileOptions);
  const series = useMemo(() => renderSeriesFromGraphTile(tile), [tile]);
  const renderedSeriesKey = useMemo(() => series.map((s) => seriesKey(s)).sort().join("|"), [series]);
  const rawSeriesKeys = useMemo(() => (tile?.series || []).map((s) => seriesKey(s)).filter(Boolean), [tile]);
  const tileSeriesCount = rawSeriesKeys.length;
  const renderedSeriesKeys = useMemo(() => new Set(series.map((s) => seriesKey(s))), [renderedSeriesKey]);
  const rawSeriesMeta = useMemo(() => {
    const out = new Map();
    (tile?.series || []).forEach((raw) => {
      const key = seriesKey(raw);
      if (!key) return;
      out.set(key, {
        quality: raw.quality || raw.diagnostics?.status || "ok",
        visibilityReason: raw.visibility_reason || raw.visibilityReason || "",
      });
    });
    return out;
  }, [tile]);
  const renderedLegendSeries = useMemo(() => series.map((s) => {
    const key = seriesKey(s);
    const meta = rawSeriesMeta.get(key) || {};
    return {
      ...s,
      key,
      quality: s.quality || meta.quality || "ok",
      visibilityReason: s.visibilityReason || meta.visibilityReason || "",
    };
  }), [renderedSeriesKey, rawSeriesMeta]);
  const rawOnlyLegendSeries = useMemo(() => (tile?.series || [])
    .map((raw) => {
      const key = seriesKey(raw);
      if (!key || renderedSeriesKeys.has(key)) return null;
      const history = raw.history || {};
      const values = Array.isArray(history.v) ? history.v : [];
      const source = raw.source || {};
      return {
        key,
        label: raw.label || key,
        fullLabel: raw.full_label || raw.fullLabel || "",
        visibilityReason: raw.visibility_reason || raw.visibilityReason || "",
        unit: raw.unit || "",
        paramId: source.param_id || raw.param_id || raw.paramId,
        color: channelColor(source.device_id || source.deviceId || "", source.instance || raw.instance || 1),
        quality: raw.quality || raw.diagnostics?.status || "missing",
        history: { v: values },
      };
    })
    .filter(Boolean), [tile, renderedSeriesKey]);
  const legendSeries = useMemo(() => renderedLegendSeries.concat(rawOnlyLegendSeries), [renderedLegendSeries, rawOnlyLegendSeries]);
  const validSeriesKeys = useMemo(() => Array.from(new Set(renderedLegendSeries.map((s) => s.key).concat(rawSeriesKeys))), [renderedSeriesKey, rawSeriesKeys.join("|")]);
  const validSeriesKey = useMemo(() => validSeriesKeys.slice().sort().join("|"), [validSeriesKeys.join("|")]);
  const defaultHidden = useMemo(() => defaultHiddenSeriesForTile(tile), [tile]);
  const defaultHiddenKey = useMemo(() => defaultHidden.slice().sort().join("|"), [defaultHidden.join("|")]);
  const appliedDefaultHiddenKey = useRef("");
  const defaultHiddenApplyKey = `${section.tileId}:${defaultHiddenKey}:${validSeriesKey}`;
  const hiddenForTileKey = hiddenForTile.join ? hiddenForTile.join("|") : String(hiddenForTile);
  const effectiveHiddenForTile = useMemo(() => {
    const valid = new Set(validSeriesKeys || []);
    const current = new Set((hiddenForTile || []).filter((key) => valid.size === 0 || valid.has(key)));
    if (appliedDefaultHiddenKey.current !== defaultHiddenApplyKey) {
      (defaultHidden || []).forEach((key) => {
        if (!key || (valid.size > 0 && !valid.has(key))) return;
        current.add(key);
      });
    }
    return Array.from(current);
  }, [hiddenForTileKey, defaultHiddenKey, defaultHiddenApplyKey, validSeriesKey]);
  useEffect(() => {
    const applyKey = defaultHiddenApplyKey;
    if (appliedDefaultHiddenKey.current === applyKey) return;
    appliedDefaultHiddenKey.current = applyKey;
    onApplyDefaultHidden(section.tileId, defaultHidden, validSeriesKeys);
  }, [defaultHiddenApplyKey]);
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
        {tileSeriesCount === 0 ? (
          <div className="empty">{section.empty}<br />Use the signal catalogue on the left; star an instance to add it.</div>
        ) : (
          <div className="chart-layout">
            <div className="chart-plot">
              <MultiChart tile={tile} height={section.bucket === "other" ? 260 : 320} hiddenSeries={effectiveHiddenForTile} fill minHeight={section.bucket === "other" ? 220 : 280} />
            </div>
            <div className="legend chart-legend">
              {legendSeries.map((s) => {
                const values = Array.isArray(s.history?.v) ? s.history.v : [];
                const last = values.length ? values[values.length - 1] : null;
                const off = effectiveHiddenForTile.includes(s.key);
                return (
                  <span
                    key={s.key}
                    className={"item " + (off ? "off" : "")}
                    data-series-key={s.key}
                    data-series-quality={s.quality || "ok"}
                    data-series-visible={off ? "false" : "true"}
                    onClick={() => onToggleSeriesVisibility(section.tileId, s.key)}
                    title={[s.fullLabel || "Click to show/hide this tile series", s.visibilityReason || ""].filter(Boolean).join(" · ")}
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
        )}
      </div>
    </div>
  );
}
